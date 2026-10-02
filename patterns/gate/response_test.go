package gate_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/patterns/gate"
)

type askSet struct {
	req    *decide.Request
	noul   decide.Handle[*decide.NoulAnswer]
	choice decide.Handle[*decide.ChoiceAnswer]
	score  decide.Handle[*decide.ScoreAnswer]
}

func newAsk() askSet {
	req := decide.NewRequest("rm -rf ./build")
	return askSet{
		req:  req,
		noul: decide.Ask(req, "path_ok", decide.Noul("Is the path the one the user named?")),
		choice: decide.Ask(req, "action", decide.Choice("What file operation is proposed?",
			decide.Option("read_file"), decide.Option("delete_file"), decide.Option("none"))),
		score: decide.Ask(req, "risk", decide.Score("How risky is it?", "low", "medium", "high")),
	}
}

func send(t *testing.T, srv *decidetest.Server, req *decide.Request) (*decide.Response, error) {
	t.Helper()
	return srv.Client(t).SystemOne(t.Context(), req)
}

func TestFromResponseAllTypes(t *testing.T) {
	srv := decidetest.NewServer(t)
	srv.Answer("path_ok", decidetest.NoulAnswer(0))
	srv.Answer("action", decidetest.ChoiceAnswer(map[string]float64{"read_file": 0.1, "delete_file": 0.8, "none": 0.1}))
	srv.Answer("risk", decidetest.ScoreAnswer([]any{"low", "medium", "high"}, 0.1, 0.2, 0.7))
	q := newAsk()
	resp, err := send(t, srv, q.req)
	if err != nil {
		t.Fatal(err)
	}
	in := gate.FromResponse(resp)
	n, c, s := in.Get("path_ok"), in.Get("action"), in.Get("risk")
	if n.Kind != gate.KindNoul || n.Noul != 0 || n.HasConfidence {
		t.Errorf("noul %+v", n)
	}
	if v, err := n.Measure(gate.MeasureNoul, ""); err != nil || v != 0 {
		t.Errorf("Noul == 0 reads as %v, %v", v, err)
	}
	if c.Kind != gate.KindChoice || c.Choice != "delete_file" || !c.HasConfidence || c.Probabilities["delete_file"] != 0.8 {
		t.Errorf("choice %+v", c)
	}
	if s.Kind != gate.KindScore || !s.HasConfidence || s.Probabilities["2"] != 0.7 {
		t.Errorf("score %+v", s)
	}
	// The adapters copy the maps.
	resp.Answers["action"].(*decide.ChoiceAnswer).Probabilities["delete_file"] = 0
	if in.Get("action").Probabilities["delete_file"] != 0.8 {
		t.Error("probabilities aliased")
	}
}

func TestFromResponseInvalid(t *testing.T) {
	srv := decidetest.NewServer(t)
	srv.Answer("path_ok", decidetest.NoulAnswer(0.9))
	// "maybe" is not an option: a structural failure.
	srv.Answer("action", &decide.ChoiceAnswer{Choice: "maybe",
		Probabilities: map[string]float64{"read_file": 0.1, "delete_file": 0.8, "none": 0.1}, Confidence: 0.7})
	// Probabilities sum to 0.5: a consistency failure.
	srv.Answer("risk", &decide.ScoreAnswer{Score: 0.5, Legend: map[string]any{"0": "low", "1": "medium", "2": "high"},
		Probabilities: map[string]float64{"0": 0.1, "1": 0.2, "2": 0.2}, Confidence: 0.1})
	q := newAsk()
	resp, err := send(t, srv, q.req)
	if err == nil || resp == nil || len(resp.Invalid) != 2 {
		t.Fatalf("resp %v err %v", resp, err)
	}
	// resp plus a non-nil error still adapts.
	in := gate.FromResponse(resp)
	if in.Get("path_ok").Kind != gate.KindNoul {
		t.Errorf("valid key lost: %+v", in.Get("path_ok"))
	}
	for _, key := range []string{"action", "risk"} {
		x := in.Get(key)
		ae, ok := errors.AsType[*decide.AnswerError](x.Err)
		if x.Kind != gate.KindFailed || !ok || ae != resp.Invalid[key] {
			t.Errorf("%s: %+v", key, x)
		}
	}
	// AcceptInconsistent reads the score but not the structurally bad choice.
	in = gate.FromResponse(resp, gate.AcceptInconsistent())
	if in.Get("risk").Kind != gate.KindScore || in.Get("action").Kind != gate.KindFailed {
		t.Errorf("accept: risk %v action %v", in.Get("risk").Kind, in.Get("action").Kind)
	}
}

func TestFromResponseMissingAndRaw(t *testing.T) {
	srv := decidetest.NewServer(t)
	srv.Respond(func(req *decide.Request) (*decide.Response, error) {
		return &decide.Response{Answers: map[string]decide.Answer{
			"path_ok": decidetest.NoulAnswer(0.9),
			"future":  &decide.RawAnswer{Type: "future", JSON: json.RawMessage(`{"type":"future","x":1}`)},
		}}, nil
	})
	req := decide.NewRequest("state")
	decide.Ask(req, "path_ok", decide.Noul("ok?"))
	action := decide.Ask(req, "action", decide.Choice("which?", decide.Option("a"), decide.Option("b")))
	decide.Ask(req, "future", &decide.RawQuestion{Type: "future", JSON: json.RawMessage(`{"type":"future"}`)})
	resp, err := send(t, srv, req)
	if err == nil || resp.Invalid["action"] == nil || resp.Invalid["action"].Reason != decide.ReasonMissingAnswer {
		t.Fatalf("err %v invalid %v", err, resp.Invalid)
	}
	in := gate.FromResponse(resp)
	if x := in.Get("action"); x.Kind != gate.KindFailed || !errors.Is(x.Err, decide.ErrInvalidAnswer) {
		t.Errorf("missing answer: %+v", x)
	}
	if x := in.Get("future"); x.Kind != gate.KindFailed || x.Err == nil {
		t.Errorf("raw answer: %+v", x)
	}
	// The handle path: missing_answer is Failed.
	a, err := action.From(resp)
	if x := gate.FromChoice("action", a, err); x.Kind != gate.KindFailed {
		t.Errorf("FromChoice: %+v", x)
	}
	// The policy escalates with Missing set.
	policy := gate.Asymmetric{Input: "action", Measure: gate.MeasureConfidence, Options: map[string]gate.OptionBand{"a": {}}}
	d := policy.Evaluate(in)
	check(t, d, gate.Escalate, "action", true)
	if d.Readings[0].Problem != "failed" {
		t.Errorf("%+v", d.Readings)
	}
}

func TestFromResponseNil(t *testing.T) {
	in := gate.FromResponse(nil)
	n := 0
	for range in.All() {
		n++
	}
	if n != 0 || in.Get("x").Kind != gate.KindMissing {
		t.Fatalf("nil resp: %d inputs", n)
	}
	check(t, fiveNouls.Evaluate(in), gate.Escalate, "q1", true)
}

func TestFromHandles(t *testing.T) {
	srv := decidetest.NewServer(t)
	srv.Answer("path_ok", decidetest.NoulAnswer(0.9))
	// Choice is not the argmax: a consistency failure.
	srv.Answer("action", &decide.ChoiceAnswer{Choice: "read_file",
		Probabilities: map[string]float64{"read_file": 0.2, "delete_file": 0.7, "none": 0.1}, Confidence: 0.55})
	srv.Answer("risk", decidetest.ScoreAnswer([]any{"low", "medium", "high"}, 0.6, 0.3, 0.1))
	q := newAsk()
	resp, _ := send(t, srv, q.req)

	n, nerr := q.noul.From(resp)
	if x := gate.FromNoul("path_ok", n, nerr); x.Kind != gate.KindNoul || x.Noul != 0.9 {
		t.Errorf("FromNoul: %+v", x)
	}
	s, serr := q.score.From(resp)
	if x := gate.FromScore("risk", s, serr); x.Kind != gate.KindScore {
		t.Errorf("FromScore: %+v", x)
	}

	c, cerr := q.choice.From(resp)
	if !errors.Is(cerr, decide.ErrInconsistentAnswer) || c == nil {
		t.Fatalf("setup: %v %v", c, cerr)
	}
	if x := gate.FromChoice("action", c, cerr); x.Kind != gate.KindFailed || !errors.Is(x.Err, decide.ErrInconsistentAnswer) {
		t.Errorf("default: %+v", x)
	}
	x := gate.FromChoice("action", c, cerr, gate.AcceptInconsistent())
	if x.Kind != gate.KindChoice || x.Choice != "read_file" {
		t.Errorf("accept: %+v", x)
	}
	// With a nil answer, AcceptInconsistent cannot help.
	if x := gate.FromChoice("action", nil, cerr, gate.AcceptInconsistent()); x.Kind != gate.KindFailed {
		t.Errorf("nil answer: %+v", x)
	}
	if x := gate.FromNoul("path_ok", nil, cerr, gate.AcceptInconsistent()); x.Kind != gate.KindFailed {
		t.Errorf("nil noul: %+v", x)
	}
	if x := gate.FromScore("risk", nil, cerr, gate.AcceptInconsistent()); x.Kind != gate.KindFailed {
		t.Errorf("nil score: %+v", x)
	}
	// Other errors stay Failed even with AcceptInconsistent.
	other := errors.New("boom")
	if x := gate.FromNoul("path_ok", n, other, gate.AcceptInconsistent()); x.Kind != gate.KindFailed {
		t.Errorf("other error: %+v", x)
	}
	// nil answer and nil error: Missing.
	if x := gate.FromScore("risk", nil, nil); x.Kind != gate.KindMissing {
		t.Errorf("nil, nil: %+v", x)
	}
}
