package decide_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

// TestGoldenResponses compares semantically: MarshalJSON writes map keys
// (probabilities, legend) sorted, not in the fixture's order.
func TestGoldenResponses(t *testing.T) {
	for _, name := range []string{"api_score_response.json", "api_noul_response.json", "api_choice_response.json"} {
		t.Run(name, func(t *testing.T) {
			in := decidetest.ReadFile(t, name)
			var resp decide.Response
			if err := json.Unmarshal(in, &resp); err != nil {
				t.Fatal(err)
			}
			out, err := json.Marshal(&resp)
			if err != nil {
				t.Fatal(err)
			}
			if !jsonEqual(t, in, out) {
				t.Fatalf("round trip differs\n in: %s\nout: %s", compact(t, in), out)
			}
		})
	}
}

func TestGoldenScoreFields(t *testing.T) {
	resp, err := decide.DecodeResponse(decidetest.ReadFile(t, "api_score_response.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := resp.Answers["frustration"].(*decide.ScoreAnswer)
	if !ok {
		t.Fatalf("got %T", resp.Answers["frustration"])
	}
	want := &decide.ScoreAnswer{
		Score:         1.05,
		Legend:        map[string]any{"0": "Calm", "1": "Frustrated", "2": "Very angry"},
		Probabilities: map[string]float64{"0": 0, "1": 0.95, "2": 0.05},
		Confidence:    0.92,
	}
	if !reflect.DeepEqual(a, want) {
		t.Fatalf("got %+v", a)
	}
	if resp.Model != "jev-1.13.0" || !reflect.DeepEqual(resp.Usage, decide.Usage{InputTokens: 304, OutputTokens: 18}) {
		t.Fatalf("model %q usage %+v", resp.Model, resp.Usage)
	}
}

func TestExtraRoundTrip(t *testing.T) {
	in := `{"model":"jev-1.13.0","answers":{` +
		`"c":{"type":"choice","choice":"a","probabilities":{"a":1},"confidence":1,"reason":"x"},` +
		`"n":{"type":"noul","noul":0.4,"note":[1,2]},` +
		`"s":{"type":"score","score":0,"legend":{"0":"lo"},"probabilities":{"0":1},"confidence":1,"z":null}},` +
		`"usage":{"input_tokens":1,"output_tokens":2},"trace":{"id":"t1"}}`
	resp, err := decide.DecodeResponse([]byte(in), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(resp.Answers["n"].(*decide.NoulAnswer).Extra["note"]); got != "[1,2]" {
		t.Errorf("noul Extra: %q", got)
	}
	if got := string(resp.Extra["trace"]); got != `{"id":"t1"}` {
		t.Errorf("response Extra: %q", got)
	}
	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(t, []byte(in), out) {
		t.Fatalf("round trip\n in: %s\nout: %s", in, out)
	}

	model := `{"name":"jev-latest","description":"d","release_date":"2026-09-01","tier":"stable"}`
	var m decide.Model
	if err := json.Unmarshal([]byte(model), &m); err != nil {
		t.Fatal(err)
	}
	if string(m.Extra["tier"]) != `"stable"` {
		t.Fatalf("model Extra %v", m.Extra)
	}
	mb, _ := json.Marshal(m)
	if !jsonEqual(t, []byte(model), mb) {
		t.Fatalf("model round trip: %s", mb)
	}
}

func TestTopLevelTypeDrift(t *testing.T) {
	in := `{"model":{"id":"jev-1.13.0"},"answers":{"n":{"type":"noul","noul":0.9}},` +
		`"usage":{"input_tokens":12.0,"output_tokens":"3","cached_tokens":4}}`
	resp, err := decide.DecodeResponse([]byte(in), nil)
	if err != nil {
		t.Fatalf("drift failed the response: %v", err)
	}
	if resp.Model != "" || string(resp.Extra["model"]) != `{"id":"jev-1.13.0"}` {
		t.Errorf("model drift: %q %s", resp.Model, resp.Extra["model"])
	}
	want := decide.Usage{InputTokens: 12, Extra: map[string]json.RawMessage{
		"output_tokens": json.RawMessage(`"3"`), "cached_tokens": json.RawMessage(`4`)}}
	if !reflect.DeepEqual(resp.Usage, want) {
		t.Errorf("usage %+v", resp.Usage)
	}
	if _, ok := resp.Answers["n"].(*decide.NoulAnswer); !ok {
		t.Error("answers lost")
	}
	resp, err = decide.DecodeResponse([]byte(`{"usage":[1],"answers":{}}`), nil)
	if err != nil || string(resp.Extra["usage"]) != "[1]" || resp.Usage.InputTokens != 0 {
		t.Fatalf("usage not an object: %+v %v", resp, err)
	}
	if _, err := decide.DecodeResponse([]byte(`{"usage":{"input_tokens":1.5}}`), nil); err != nil {
		t.Fatal(err)
	}
}

func TestUsageRoundTrip(t *testing.T) {
	in := `{"input_tokens":304,"output_tokens":18,"cache":{"hit":true}}`
	var u decide.Usage
	if err := json.Unmarshal([]byte(in), &u); err != nil {
		t.Fatal(err)
	}
	if u.InputTokens != 304 || string(u.Extra["cache"]) != `{"hit":true}` {
		t.Fatalf("%+v", u)
	}
	out, err := json.Marshal(u)
	if err != nil || string(out) != in {
		t.Fatalf("round trip %s %v", out, err)
	}
}

func TestUnknownAnswerTypeKeptRaw(t *testing.T) {
	unknown := `{"type":"rank","order":["b","a"],"scores":{"a":0.1}}`
	in := `{"model":"m","answers":{"r":` + unknown + `,"n":{"type":"noul","noul":0.9}}}`
	resp, err := decide.DecodeResponse([]byte(in), nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := resp.Answers["r"].(*decide.RawAnswer)
	if !ok || raw.Type != "rank" || raw.Err != nil || string(raw.JSON) != unknown {
		t.Fatalf("got %#v", resp.Answers["r"])
	}
	if n, ok := resp.Answers["n"].(*decide.NoulAnswer); !ok || n.Noul != 0.9 {
		t.Fatalf("rest of response: %#v", resp.Answers["n"])
	}
}

func TestMalformedKnownAnswer(t *testing.T) {
	a, err := decide.DecodeAnswer([]byte(`{"type":"noul","noul":"high"}`))
	raw, ok := a.(*decide.RawAnswer)
	if err == nil || !ok || raw.Err == nil || raw.Type != "noul" {
		t.Fatalf("got %#v, %v", a, err)
	}
	for _, in := range []string{`[1]`, `{"noul":1}`, `{"type":3}`} {
		a, err := decide.DecodeAnswer([]byte(in))
		if raw, ok := a.(*decide.RawAnswer); err == nil || !ok || raw.Type != "" || raw.Err == nil {
			t.Errorf("%s: got %#v, %v", in, a, err)
		}
	}
}

// rankQuestion is a third-party question type registered nowhere; it
// decodes through AnswerMaker.
type rankQuestion struct{ Items []string }

type rankAnswer struct {
	Order []string `json:"order"`
}

func (*rankAnswer) AnswerType() string { return "tstest-rank" }

func (q *rankQuestion) QuestionType() string      { return "tstest-rank" }
func (q *rankQuestion) NewAnswer() *rankAnswer    { return &rankAnswer{} }
func (q *rankQuestion) MakeAnswer() decide.Answer { return q.NewAnswer() }
func (q *rankQuestion) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"type": "tstest-rank", "instructions": "rank", "items": q.Items})
}

// flagQuestion has no AnswerMaker; its answers come from the registry.
type flagQuestion struct{}

type flagAnswer struct {
	Flag bool `json:"flag"`
}

func (*flagAnswer) AnswerType() string { return "tstest-flag" }

func (flagQuestion) QuestionType() string         { return "tstest-flag" }
func (flagQuestion) MarshalJSON() ([]byte, error) { return []byte(`{"type":"tstest-flag"}`), nil }

func init() {
	if err := decide.RegisterAnswerType("tstest-flag", func() decide.Answer { return &flagAnswer{} }); err != nil {
		panic(err)
	}
}

func TestDecodeResponseUsesRequest(t *testing.T) {
	req := decide.NewRequest("s")
	rank := decide.Ask(req, "rank", &rankQuestion{Items: []string{"a", "b"}})
	req.Questions["flag"] = flagQuestion{}
	body := `{"model":"m","answers":{"rank":{"type":"tstest-rank","order":["b","a"]},"flag":{"type":"tstest-flag","flag":true}}}`

	resp, err := decide.DecodeResponse([]byte(body), req)
	if err != nil {
		t.Fatal(err)
	}
	r, err := rank.From(resp)
	if err != nil || !reflect.DeepEqual(r.Order, []string{"b", "a"}) {
		t.Fatalf("rank: %#v, %v", r, err)
	}
	if f, ok := resp.Answers["flag"].(*flagAnswer); !ok || !f.Flag {
		t.Fatalf("registry fallback: %#v", resp.Answers["flag"])
	}

	// Without the request, the unregistered type stays raw.
	resp, err = decide.DecodeResponse([]byte(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := resp.Answers["rank"].(*decide.RawAnswer); !ok {
		t.Fatalf("without request: %T", resp.Answers["rank"])
	}
	if _, ok := resp.Answers["flag"].(*flagAnswer); !ok {
		t.Fatalf("registry without request: %T", resp.Answers["flag"])
	}
}

func TestDecodeResponseErrors(t *testing.T) {
	for _, in := range []string{`[]`, `"x"`, `null`, `{"answers":[1]}`} {
		if _, err := decide.DecodeResponse([]byte(in), nil); err == nil {
			t.Errorf("%s: want error", in)
		}
	}
	resp, err := decide.DecodeResponse([]byte(`{}`), nil)
	if err != nil || resp.Answers == nil || !reflect.DeepEqual(resp.Usage, decide.Usage{}) {
		t.Fatalf("lenient empty: %+v, %v", resp, err)
	}
}

func TestRegisterAnswerTypeErrors(t *testing.T) {
	ctor := func() decide.Answer { return &flagAnswer{} }
	for _, typ := range []string{"", "noul", "choice", "score", "tstest-flag"} {
		if err := decide.RegisterAnswerType(typ, ctor); err == nil {
			t.Errorf("%q: want error", typ)
		}
	}
	if err := decide.RegisterAnswerType("tstest-nil", nil); err == nil {
		t.Error("nil constructor: want error")
	}
}

func TestChoiceHelpers(t *testing.T) {
	a := &decide.ChoiceAnswer{Probabilities: map[string]float64{"b": 0.4, "a": 0.4, "c": 0.2}}
	want := []decide.Prob{{Key: "a", P: 0.4}, {Key: "b", P: 0.4}, {Key: "c", P: 0.2}}
	if got := a.Ranked(); !reflect.DeepEqual(got, want) {
		t.Errorf("Ranked: %v", got)
	}
	cases := []struct {
		probs map[string]float64
		want  float64
	}{
		{map[string]float64{"a": 0.7, "b": 0.2, "c": 0.1}, 0.5},
		{map[string]float64{"a": 0.5, "b": 0.5}, 0},
		{map[string]float64{"a": 0.9}, 0.9},
		{nil, 0},
	}
	for _, c := range cases {
		got := (&decide.ChoiceAnswer{Probabilities: c.probs}).Margin()
		if diff := got - c.want; diff > 1e-12 || diff < -1e-12 {
			t.Errorf("Margin(%v) = %v, want %v", c.probs, got, c.want)
		}
	}
}

func TestScoreHelpers(t *testing.T) {
	a := &decide.ScoreAnswer{
		Score:         1.05,
		Legend:        map[string]any{"0": "Calm", "1": "Frustrated", "2": "Very angry"},
		Probabilities: map[string]float64{"0": 0.4, "1": 0.2, "2": 0.4},
	}
	if a.Levels() != 3 {
		t.Errorf("Levels %d", a.Levels())
	}
	if a.Level() != 0 {
		t.Errorf("Level tie: %d", a.Level())
	}
	if p, ok := a.ProbabilityAt(1); !ok || p != 0.2 {
		t.Errorf("ProbabilityAt(1) = %v, %v", p, ok)
	}
	if _, ok := a.ProbabilityAt(7); ok {
		t.Error("ProbabilityAt(7) ok")
	}
	if a.LevelLabel(2) != "Very angry" || a.LevelLabel(9) != nil {
		t.Errorf("LevelLabel: %v %v", a.LevelLabel(2), a.LevelLabel(9))
	}
	if a.Normalized() != 0.525 {
		t.Errorf("Normalized %v", a.Normalized())
	}
	partial := &decide.ScoreAnswer{Legend: map[string]any{"0": "lo"}, Probabilities: map[string]float64{"0": 0.2, "1": 0.3, "2": 0.5}, Score: 1.3}
	if partial.Levels() != 3 || partial.Normalized() != 0.65 {
		t.Errorf("partial legend: Levels %d Normalized %v", partial.Levels(), partial.Normalized())
	}
	empty := &decide.ScoreAnswer{Probabilities: map[string]float64{"0": 1}}
	if empty.Levels() != 1 || empty.Normalized() != 0 || (&decide.ScoreAnswer{}).Level() != -1 {
		t.Error("empty helpers")
	}
}

func TestRawAnswerMarshal(t *testing.T) {
	b, err := json.Marshal(&decide.RawAnswer{Type: "x"})
	if err != nil || string(b) != `{"type":"x"}` {
		t.Fatalf("%s %v", b, err)
	}
}

func BenchmarkDecodeResponse(b *testing.B) {
	b.ReportAllocs()
	body := decidetest.ReadFile(b, "api_score_response.json")
	req := decide.NewRequest("s")
	decide.Ask(req, "frustration", decide.Score("?", "Calm", "Frustrated", "Very angry"))
	for b.Loop() {
		if _, err := decide.DecodeResponse(body, req); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeRequest(b *testing.B) {
	b.ReportAllocs()
	req := decide.NewRequest("Help! My payouts have been failing for 3 days.", decide.WithRequestModel("jev-latest"))
	decide.Ask(req, "frustration", decide.Score("How frustrated is the customer?", "Calm", "Frustrated", "Very angry"))
	decide.Ask(req, "tone", decide.Choice("Tone?", decide.Option("calm"), decide.Option("angry", "Hostile")))
	decide.Ask(req, "billing", decide.Noul("About billing?"))
	for b.Loop() {
		if _, err := req.MarshalJSON(); err != nil {
			b.Fatal(err)
		}
	}
}
