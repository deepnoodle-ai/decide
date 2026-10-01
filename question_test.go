package sod_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/sod"
	"github.com/deepnoodle-ai/sod/sodtest"
)

func TestQuestionBuildersJSON(t *testing.T) {
	cases := []struct {
		name string
		q    sod.Question
		want string
	}{
		{"noul", sod.Noul("Is it billing?"),
			`{"type":"noul","instructions":"Is it billing?"}`},
		{"noul criteria", sod.Noul("Is it billing?", sod.NoulTrue("about money"), sod.NoulFalse(nil)),
			`{"type":"noul","instructions":"Is it billing?","criteria":{"true":"about money","false":null}}`},
		{"noul false only", sod.Noul("q", sod.NoulFalse("no")),
			`{"type":"noul","instructions":"q","criteria":{"true":null,"false":"no"}}`},
		{"choice", sod.Choice("Tone?", sod.Option("calm"), sod.Option("angry", "Hostile")),
			`{"type":"choice","instructions":"Tone?","criteria":{"calm":null,"angry":"Hostile"}}`},
		{"score", sod.Score("Urgency?", "low", "high"),
			`{"type":"score","instructions":"Urgency?","criteria":["low","high"]}`},
		{"structured instructions", sod.Noul(map[string]any{"ask": "x"}),
			`{"type":"noul","instructions":{"ask":"x"}}`},
		{"nil instructions", &sod.NoulQuestion{},
			`{"type":"noul","instructions":null}`},
		{"extra", &sod.ScoreQuestion{Instructions: "s", Criteria: []any{"a", "b"}, Extra: map[string]any{"z": 1, "hint": true}},
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
	q := sod.Choice("?", sod.Option("z"), sod.Option("a"), sod.Option("m"))
	b, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `{"z":null,"a":null,"m":null}`) {
		t.Fatalf("order lost: %s", b)
	}
	var back sod.ChoiceQuestion
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
	for name, q := range map[string]*sod.ChoiceQuestion{
		"empty":     sod.Choice("?", sod.Option("")),
		"duplicate": sod.Choice("?", sod.Option("a"), sod.Option("a")),
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
	sod.Option("a", "one", "two")
}

func TestOptionsFromMap(t *testing.T) {
	got := sod.OptionsFromMap(map[string]any{"b": "B", "a": nil})
	want := []sod.ChoiceOption{{Key: "a"}, {Key: "b", Description: "B"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	typed := sod.OptionsFromMap(map[string]string{"z": "Z", "m": "M", "a": "A"})
	want = []sod.ChoiceOption{{Key: "a", Description: "A"}, {Key: "m", Description: "M"}, {Key: "z", Description: "Z"}}
	if !reflect.DeepEqual(typed, want) {
		t.Fatalf("typed: got %v", typed)
	}
}

func TestScoreOf(t *testing.T) {
	q := sod.ScoreOf("How urgent?", []string{"low", "medium", "high"})
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
		qs := []sod.Question{
			&sod.NoulQuestion{Instructions: "x", Extra: map[string]any{k: 1}},
			&sod.ChoiceQuestion{Instructions: "x", Criteria: []sod.ChoiceOption{{Key: "a"}}, Extra: map[string]any{k: 1}},
			&sod.ScoreQuestion{Instructions: "x", Criteria: []any{"a", "b"}, Extra: map[string]any{k: 1}},
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
		q, err := sod.DecodeQuestion([]byte(in))
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
	q, err := sod.DecodeQuestion([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := q.(*sod.RawQuestion)
	if !ok || raw.QuestionType() != "rank" {
		t.Fatalf("got %T %v", q, q)
	}
	out, err := json.Marshal(raw)
	if err != nil || string(out) != in {
		t.Fatalf("round trip: %s, %v", out, err)
	}
	if _, ok := any(raw.NewAnswer()).(*sod.RawAnswer); !ok {
		t.Fatal("NewAnswer is not *RawAnswer")
	}

	bad := &sod.RawQuestion{Type: "rank", JSON: json.RawMessage(`{"type":"other"}`)}
	if _, err := json.Marshal(bad); err == nil {
		t.Error("mismatched type: want error")
	}
	if _, err := json.Marshal(&sod.RawQuestion{Type: "rank", JSON: json.RawMessage(`[1]`)}); err == nil {
		t.Error("non-object: want error")
	}
}

func TestDecodeQuestionErrors(t *testing.T) {
	for _, in := range []string{`[]`, `{"instructions":"x"}`, `{"type":1}`, `null`} {
		if _, err := sod.DecodeQuestion([]byte(in)); err == nil {
			t.Errorf("%s: want error", in)
		}
	}
}

func TestQuestionDecodeKeepsObjectOrder(t *testing.T) {
	in := `{"type":"choice","instructions":{"z":1,"a":2},"criteria":{"m":{"z":1,"a":2},"b":"B"}}`
	q, err := sod.DecodeQuestion([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := q.(*sod.ChoiceQuestion).Instructions.(json.RawMessage); !ok {
		t.Fatalf("instructions decoded as %T", q.(*sod.ChoiceQuestion).Instructions)
	}
	out, err := json.Marshal(q)
	if err != nil || string(out) != in {
		t.Fatalf("order lost\n got %s\nwant %s (%v)", out, in, err)
	}
	score := `{"type":"score","instructions":"s","criteria":[{"z":1,"a":2},"mid",null]}`
	q, err = sod.DecodeQuestion([]byte(score))
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := json.Marshal(q); string(out) != score {
		t.Fatalf("score levels: %s", out)
	}
	noul := `{"type":"noul","instructions":"n","criteria":{"true":{"z":1,"a":2},"false":"no"}}`
	q, _ = sod.DecodeQuestion([]byte(noul))
	if out, _ := json.Marshal(q); string(out) != noul {
		t.Fatalf("noul criteria: %s", out)
	}
}

// TestReadmeQuickLook runs the README's "Quick look" against the fake.
func TestReadmeQuickLook(t *testing.T) {
	srv := sodtest.NewServer(t)
	srv.Answer("billing", sodtest.NoulAnswer(0.93))
	srv.Answer("tone", sodtest.ChoiceAnswer(map[string]float64{"calm": 0.05, "frustrated": 0.15, "angry": 0.8}))
	srv.Answer("urgency", sodtest.ScoreAnswer([]any{"can wait", "this week", "today"}, 0.05, 0.25, 0.7))
	t.Setenv("TYPESAFE_API_KEY", "test-key-00000000")
	t.Setenv("TYPESAFE_BASE_URL", srv.URL)
	ctx := t.Context()
	ticketText := "I was charged twice and I need this fixed today."
	escalated := false
	escalate := func(string) { escalated = true }
	ticket := ticketText

	quickLook := func() error {
		client, err := sod.NewClient() // reads TYPESAFE_API_KEY
		if err != nil {
			return err
		}

		req := sod.NewRequest(ticketText)
		billing := sod.Ask(req, "billing", sod.Noul("Is this ticket about billing?"))
		tone := sod.Ask(req, "tone", sod.Choice("What is the customer's tone?",
			sod.Option("calm"), sod.Option("frustrated"), sod.Option("angry")))
		urgency := sod.Ask(req, "urgency", sod.Score("How urgent is this?",
			"can wait", "this week", "today"))

		resp, err := client.SystemOne(ctx, req)
		if err != nil {
			return err // network, auth, or an answer that failed validation
		}

		// Once SystemOne returned no error, every answer has been validated
		// against its question, so From cannot fail for these keys.
		b, _ := billing.From(resp) // *sod.NoulAnswer
		t, _ := tone.From(resp)    // *sod.ChoiceAnswer
		u, _ := urgency.From(resp) // *sod.ScoreAnswer

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
	srv := sodtest.NewServer(t)
	c := srv.Client(t)
	req := sod.NewRequest("state")
	tone := sod.Ask(req, "tone", sod.Choice("Tone?",
		sod.Option("calm"), sod.Option("angry", "Hostile or threatening")))
	req.Questions["billing"] = &sod.NoulQuestion{Instructions: "About billing?"}
	resp, err := c.SystemOne(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}

	if tn, err := tone.From(resp); err != nil || tn.Choice != "calm" { // typed handle path
		t.Fatalf("From: %+v, %v", tn, err)
	}
	if b, ok := resp.Answers["billing"].(*sod.NoulAnswer); !ok || b.Noul != 0.5 { // raw map path
		t.Fatalf("raw path: %#v", resp.Answers["billing"])
	}

	reason := func(err error) string {
		ae, ok := errors.AsType[*sod.AnswerError](err)
		if !ok {
			t.Fatalf("not an *AnswerError: %v", err)
		}
		return ae.Reason
	}
	if a, err := sod.AnswerAs[*sod.NoulAnswer](resp, "nope"); a != nil || reason(err) != sod.ReasonMissingAnswer {
		t.Fatalf("missing key: %v %v", a, err)
	}
	if a, err := tone.From(nil); a != nil || reason(err) != sod.ReasonMissingAnswer {
		t.Fatalf("nil response: %v", err)
	}
	a, err := sod.AnswerAs[*sod.ScoreAnswer](resp, "billing")
	if a != nil || reason(err) != sod.ReasonTypeMismatch || !errors.Is(err, sod.ErrInvalidAnswer) {
		t.Fatalf("type mismatch: %v %v", a, err)
	}
	if tone.Key() != "tone" {
		t.Fatal(tone.Key())
	}
}

func TestHandleInvalidKeys(t *testing.T) {
	req := sod.NewRequest("state")
	notOption := sod.Ask(req, "c1", sod.Choice("?", sod.Option("a"), sod.Option("b")))
	notArgmax := sod.Ask(req, "c2", sod.Choice("?", sod.Option("a"), sod.Option("b")))
	broken := sod.Ask(req, "n1", sod.Noul("?"))
	fine := sod.Ask(req, "n2", sod.Noul("?"))
	c := stubClient(t, answering(map[string]sod.Answer{
		"c1": &sod.ChoiceAnswer{Choice: "z", Probabilities: map[string]float64{"a": 0.5, "b": 0.5}, Confidence: 0},
		"c2": &sod.ChoiceAnswer{Choice: "b", Probabilities: map[string]float64{"a": 0.9, "b": 0.1}, Confidence: 0.8},
		"n1": &sod.RawAnswer{Type: "noul", JSON: []byte(`{"type":"noul","noul":"x"}`), Err: errors.New("bad")},
		"n2": sodtest.NoulAnswer(0.2),
	}))
	resp, err := c.SystemOne(t.Context(), req)
	if resp == nil || err == nil || len(resp.Invalid) != 3 {
		t.Fatalf("resp %v err %v", resp, err)
	}
	if a, err := notOption.From(resp); a == nil || err != resp.Invalid["c1"] || a.Choice != "z" {
		t.Fatalf("choice_not_option: %v %v", a, err)
	}
	a, err := notArgmax.From(resp)
	if a == nil || !errors.Is(err, sod.ErrInconsistentAnswer) {
		t.Fatalf("choice_not_argmax: %v %v", a, err)
	}
	if n, err := broken.From(resp); n != nil || err != resp.Invalid["n1"] || resp.Invalid["n1"].Reason != sod.ReasonDecodeFailed {
		t.Fatalf("decode_failed on raw: %v %v", n, err)
	}
	if n, err := fine.From(resp); err != nil || n.Noul != 0.2 {
		t.Fatalf("other key: %v %v", n, err)
	}
}

func TestAskPanics(t *testing.T) {
	cases := map[string]func(){
		"nil request": func() { sod.Ask(nil, "k", sod.Noul("q")) },
		"empty key":   func() { sod.Ask(sod.NewRequest("s"), "", sod.Noul("q")) },
		"duplicate": func() {
			r := sod.NewRequest("s")
			sod.Ask(r, "k", sod.Noul("q"))
			sod.Ask(r, "k", sod.Noul("q"))
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
	var r sod.Request // zero value: Ask allocates Questions
	sod.Ask(&r, "k", sod.Noul("q"))
	if len(r.Questions) != 1 {
		t.Fatal("Ask on zero Request")
	}
}
