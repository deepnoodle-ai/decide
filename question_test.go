package decide_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

func TestQuestionBuildersJSON(t *testing.T) {
	cases := []struct {
		name string
		q    decide.Question
		want string
	}{
		{"noul", decide.Noul("Is it billing?"),
			`{"type":"noul","instructions":"Is it billing?"}`},
		{"noul criteria", decide.Noul("Is it billing?", decide.NoulTrue("about money"), decide.NoulFalse(nil)),
			`{"type":"noul","instructions":"Is it billing?","criteria":{"true":"about money","false":null}}`},
		{"noul false only", decide.Noul("q", decide.NoulFalse("no")),
			`{"type":"noul","instructions":"q","criteria":{"true":null,"false":"no"}}`},
		{"choice", decide.Choice("Tone?", decide.Option("calm"), decide.Option("angry", "Hostile")),
			`{"type":"choice","instructions":"Tone?","criteria":{"calm":null,"angry":"Hostile"}}`},
		{"score", decide.Score("Urgency?", "low", "high"),
			`{"type":"score","instructions":"Urgency?","criteria":["low","high"]}`},
		{"structured instructions", decide.Noul(map[string]any{"ask": "x"}),
			`{"type":"noul","instructions":{"ask":"x"}}`},
		{"nil instructions", &decide.NoulQuestion{},
			`{"type":"noul","instructions":null}`},
		{"extra", &decide.ScoreQuestion{Instructions: "s", Criteria: []any{"a", "b"}, Extra: map[string]any{"z": 1, "hint": true}},
			`{"type":"score","instructions":"s","criteria":["a","b"],"hint":true,"z":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.q)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestChoiceOrderPreserved(t *testing.T) {
	q := decide.Choice("?", decide.Option("z"), decide.Option("a"), decide.Option("m"))
	b, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `{"z":null,"a":null,"m":null}`) {
		t.Fatalf("order lost: %s", b)
	}
	var back decide.ChoiceQuestion
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, o := range back.Criteria {
		keys = append(keys, o.Key)
	}
	if !reflect.DeepEqual(keys, []string{"z", "a", "m"}) {
		t.Fatalf("decoded order %v", keys)
	}
}

func TestChoiceBadKeys(t *testing.T) {
	for name, q := range map[string]*decide.ChoiceQuestion{
		"empty":     decide.Choice("?", decide.Option("")),
		"duplicate": decide.Choice("?", decide.Option("a"), decide.Option("a")),
	} {
		if _, err := json.Marshal(q); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestOptionTwoDescriptionsPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("want panic")
		}
	}()
	decide.Option("a", "one", "two")
}

func TestOptionsFromMap(t *testing.T) {
	got := decide.OptionsFromMap(map[string]any{"b": "B", "a": nil})
	want := []decide.ChoiceOption{{Key: "a"}, {Key: "b", Description: "B"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	typed := decide.OptionsFromMap(map[string]string{"z": "Z", "m": "M", "a": "A"})
	want = []decide.ChoiceOption{{Key: "a", Description: "A"}, {Key: "m", Description: "M"}, {Key: "z", Description: "Z"}}
	if !reflect.DeepEqual(typed, want) {
		t.Fatalf("typed: got %v", typed)
	}
}

func TestScoreOf(t *testing.T) {
	q := decide.ScoreOf("How urgent?", []string{"low", "medium", "high"})
	b, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"type":"score","instructions":"How urgent?","criteria":["low","medium","high"]}`; string(b) != want {
		t.Fatalf("got %s", b)
	}
}

func TestExtraCollision(t *testing.T) {
	for _, k := range []string{"type", "instructions", "criteria"} {
		qs := []decide.Question{
			&decide.NoulQuestion{Instructions: "x", Extra: map[string]any{k: 1}},
			&decide.ChoiceQuestion{Instructions: "x", Criteria: []decide.ChoiceOption{{Key: "a"}}, Extra: map[string]any{k: 1}},
			&decide.ScoreQuestion{Instructions: "x", Criteria: []any{"a", "b"}, Extra: map[string]any{k: 1}},
		}
		for _, q := range qs {
			if _, err := json.Marshal(q); err == nil {
				t.Errorf("%T with Extra %q: want error", q, k)
			}
		}
	}
}

func TestQuestionDecodeRoundTrip(t *testing.T) {
	for _, in := range []string{
		`{"type":"noul","instructions":"q","criteria":{"true":"yes","false":null},"x":[1,2]}`,
		`{"type":"choice","instructions":{"a":1},"criteria":{"b":"B","a":null}}`,
		`{"type":"score","instructions":"s","criteria":["lo",null,{"k":"hi"}]}`,
	} {
		q, err := decide.DecodeQuestion([]byte(in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		out, err := json.Marshal(q)
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != in {
			t.Errorf("round trip\n got %s\nwant %s", out, in)
		}
	}
}

func TestRawQuestion(t *testing.T) {
	in := `{"type":"rank","instructions":"Order these","items":["a","b"]}`
	q, err := decide.DecodeQuestion([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := q.(*decide.RawQuestion)
	if !ok || raw.QuestionType() != "rank" {
		t.Fatalf("got %T %v", q, q)
	}
	out, err := json.Marshal(raw)
	if err != nil || string(out) != in {
		t.Fatalf("round trip: %s, %v", out, err)
	}
	if _, ok := any(raw.NewAnswer()).(*decide.RawAnswer); !ok {
		t.Fatal("NewAnswer is not *RawAnswer")
	}

	bad := &decide.RawQuestion{Type: "rank", JSON: json.RawMessage(`{"type":"other"}`)}
	if _, err := json.Marshal(bad); err == nil {
		t.Error("mismatched type: want error")
	}
	if _, err := json.Marshal(&decide.RawQuestion{Type: "rank", JSON: json.RawMessage(`[1]`)}); err == nil {
		t.Error("non-object: want error")
	}
}

func TestDecodeQuestionErrors(t *testing.T) {
	for _, in := range []string{`[]`, `{"instructions":"x"}`, `{"type":1}`, `null`} {
		if _, err := decide.DecodeQuestion([]byte(in)); err == nil {
			t.Errorf("%s: want error", in)
		}
	}
}

func TestQuestionDecodeKeepsObjectOrder(t *testing.T) {
	in := `{"type":"choice","instructions":{"z":1,"a":2},"criteria":{"m":{"z":1,"a":2},"b":"B"}}`
	q, err := decide.DecodeQuestion([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := q.(*decide.ChoiceQuestion).Instructions.(json.RawMessage); !ok {
		t.Fatalf("instructions decoded as %T", q.(*decide.ChoiceQuestion).Instructions)
	}
	out, err := json.Marshal(q)
	if err != nil || string(out) != in {
		t.Fatalf("order lost\n got %s\nwant %s (%v)", out, in, err)
	}
	score := `{"type":"score","instructions":"s","criteria":[{"z":1,"a":2},"mid",null]}`
	q, err = decide.DecodeQuestion([]byte(score))
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := json.Marshal(q); string(out) != score {
		t.Fatalf("score levels: %s", out)
	}
	noul := `{"type":"noul","instructions":"n","criteria":{"true":{"z":1,"a":2},"false":"no"}}`
	q, _ = decide.DecodeQuestion([]byte(noul))
	if out, _ := json.Marshal(q); string(out) != noul {
		t.Fatalf("noul criteria: %s", out)
	}
}

// TestReadmeQuickLook runs the README's "Quick look" against the fake.
func TestReadmeQuickLook(t *testing.T) {
	srv := decidetest.NewServer(t)
	srv.Answer("billing", decidetest.NoulAnswer(0.93))
	srv.Answer("tone", decidetest.ChoiceAnswer(map[string]float64{"calm": 0.05, "frustrated": 0.15, "angry": 0.8}))
	srv.Answer("urgency", decidetest.ScoreAnswer([]any{"can wait", "this week", "today"}, 0.05, 0.25, 0.7))
	t.Setenv("TYPESAFE_API_KEY", "test-key-00000000")
	t.Setenv("TYPESAFE_BASE_URL", srv.URL)
	ctx := t.Context()
	ticketText := "I was charged twice and I need this fixed today."
	escalated := false
	escalate := func(string) { escalated = true }
	ticket := ticketText

	quickLook := func() error {
		client, err := decide.NewClient() // reads TYPESAFE_API_KEY
		if err != nil {
			return err
		}

		req := decide.NewRequest(ticketText)
		billing := decide.Ask(req, "billing", decide.Noul("Is this ticket about billing?"))
		tone := decide.Ask(req, "tone", decide.Choice("What is the customer's tone?",
			decide.Option("calm"), decide.Option("frustrated"), decide.Option("angry")))
		urgency := decide.Ask(req, "urgency", decide.Score("How urgent is this?",
			"can wait", "this week", "today"))

		resp, err := client.SystemOne(ctx, req)
		if err != nil {
			return err // network, auth, or an answer that failed validation
		}

		// Once SystemOne returned no error, every answer has been validated
		// against its question, so From cannot fail for these keys.
		b, _ := billing.From(resp) // *decide.NoulAnswer
		t, _ := tone.From(resp)    // *decide.ChoiceAnswer
		u, _ := urgency.From(resp) // *decide.ScoreAnswer

		if b.Noul > 0.8 && t.Choice == "angry" && u.Score >= 1.5 {
			escalate(ticket)
		}
		return nil
	}
	if err := quickLook(); err != nil {
		t.Fatal(err)
	}
	if !escalated {
		t.Fatal("quick look did not escalate")
	}
}

func TestHandleErrors(t *testing.T) {
	srv := decidetest.NewServer(t)
	c := srv.Client(t)
	req := decide.NewRequest("state")
	tone := decide.Ask(req, "tone", decide.Choice("Tone?",
		decide.Option("calm"), decide.Option("angry", "Hostile or threatening")))
	req.Questions["billing"] = &decide.NoulQuestion{Instructions: "About billing?"}
	resp, err := c.SystemOne(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}

	if tn, err := tone.From(resp); err != nil || tn.Choice != "calm" { // typed handle path
		t.Fatalf("From: %+v, %v", tn, err)
	}
	if b, ok := resp.Answers["billing"].(*decide.NoulAnswer); !ok || b.Noul != 0.5 { // raw map path
		t.Fatalf("raw path: %#v", resp.Answers["billing"])
	}

	reason := func(err error) string {
		ae, ok := errors.AsType[*decide.AnswerError](err)
		if !ok {
			t.Fatalf("not an *AnswerError: %v", err)
		}
		return ae.Reason
	}
	if a, err := decide.AnswerAs[*decide.NoulAnswer](resp, "nope"); a != nil || reason(err) != decide.ReasonMissingAnswer {
		t.Fatalf("missing key: %v %v", a, err)
	}
	if a, err := tone.From(nil); a != nil || reason(err) != decide.ReasonMissingAnswer {
		t.Fatalf("nil response: %v", err)
	}
	a, err := decide.AnswerAs[*decide.ScoreAnswer](resp, "billing")
	if a != nil || reason(err) != decide.ReasonTypeMismatch || !errors.Is(err, decide.ErrInvalidAnswer) {
		t.Fatalf("type mismatch: %v %v", a, err)
	}
	if tone.Key() != "tone" {
		t.Fatal(tone.Key())
	}
}

func TestHandleInvalidKeys(t *testing.T) {
	req := decide.NewRequest("state")
	notOption := decide.Ask(req, "c1", decide.Choice("?", decide.Option("a"), decide.Option("b")))
	notArgmax := decide.Ask(req, "c2", decide.Choice("?", decide.Option("a"), decide.Option("b")))
	broken := decide.Ask(req, "n1", decide.Noul("?"))
	fine := decide.Ask(req, "n2", decide.Noul("?"))
	c := stubClient(t, answering(map[string]decide.Answer{
		"c1": &decide.ChoiceAnswer{Choice: "z", Probabilities: map[string]float64{"a": 0.5, "b": 0.5}, Confidence: 0},
		"c2": &decide.ChoiceAnswer{Choice: "b", Probabilities: map[string]float64{"a": 0.9, "b": 0.1}, Confidence: 0.8},
		"n1": &decide.RawAnswer{Type: "noul", JSON: []byte(`{"type":"noul","noul":"x"}`), Err: errors.New("bad")},
		"n2": decidetest.NoulAnswer(0.2),
	}))
	resp, err := c.SystemOne(t.Context(), req)
	if resp == nil || err == nil || len(resp.Invalid) != 3 {
		t.Fatalf("resp %v err %v", resp, err)
	}
	if a, err := notOption.From(resp); a == nil || err != resp.Invalid["c1"] || a.Choice != "z" {
		t.Fatalf("choice_not_option: %v %v", a, err)
	}
	a, err := notArgmax.From(resp)
	if a == nil || !errors.Is(err, decide.ErrInconsistentAnswer) {
		t.Fatalf("choice_not_argmax: %v %v", a, err)
	}
	if n, err := broken.From(resp); n != nil || err != resp.Invalid["n1"] || resp.Invalid["n1"].Reason != decide.ReasonDecodeFailed {
		t.Fatalf("decode_failed on raw: %v %v", n, err)
	}
	if n, err := fine.From(resp); err != nil || n.Noul != 0.2 {
		t.Fatalf("other key: %v %v", n, err)
	}
}

func TestAskPanics(t *testing.T) {
	cases := map[string]func(){
		"nil request": func() { decide.Ask(nil, "k", decide.Noul("q")) },
		"empty key":   func() { decide.Ask(decide.NewRequest("s"), "", decide.Noul("q")) },
		"duplicate": func() {
			r := decide.NewRequest("s")
			decide.Ask(r, "k", decide.Noul("q"))
			decide.Ask(r, "k", decide.Noul("q"))
		},
	}
	for name, f := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: want panic", name)
				}
			}()
			f()
		}()
	}
	var r decide.Request // zero value: Ask allocates Questions
	decide.Ask(&r, "k", decide.Noul("q"))
	if len(r.Questions) != 1 {
		t.Fatal("Ask on zero Request")
	}
}
