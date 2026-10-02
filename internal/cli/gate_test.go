package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/patterns/gate"
)

func offlineJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func offlineFile(t *testing.T, value any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, offlineJSON(t, value), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func offlineEnvelope(t *testing.T, id, state, label string, probability float64) Envelope {
	t.Helper()
	return Envelope{Version: 1, ID: id,
		Data: offlineJSON(t, map[string]any{"id": id, "state": state, "label": label}),
		Runs: []Run{{Name: "source", Command: "judge", State: offlineJSON(t, state),
			Questions: map[string]json.RawMessage{"relevant": offlineJSON(t, decide.Noul("Relevant?"))},
			Response:  offlineJSON(t, map[string]any{"model": "jev-1.13.0", "answers": map[string]any{"relevant": map[string]any{"type": "noul", "noul": probability}}, "usage": map[string]int{"input_tokens": 1, "output_tokens": 1}}),
		}},
	}
}

func offlineInput(t *testing.T, envelopes ...Envelope) string {
	t.Helper()
	var b bytes.Buffer
	for _, envelope := range envelopes {
		if err := WriteRecord(&b, envelope); err != nil {
			t.Fatal(err)
		}
	}
	return b.String()
}

func offlineApp(input string) (*App, *bytes.Buffer, *bytes.Buffer) {
	out, diagnostics := new(bytes.Buffer), new(bytes.Buffer)
	return &App{In: strings.NewReader(input), Out: out, Err: diagnostics,
		NewClient: func() (*decide.Client, error) { panic("offline command constructed network client") }}, out, diagnostics
}

func TestGateOfflineRevalidatesSavedEvidence(t *testing.T) {
	policy := gate.Bands{Input: "relevant", Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe, Allow: 0.8, Review: 0.5}
	path := offlineFile(t, policy)
	good := offlineEnvelope(t, "a", "state", "true", 0.9)
	bad := offlineEnvelope(t, "b", "state", "true", 0.9)
	bad.Runs[0].Response = json.RawMessage(`{"model":"jev-1.13.0","answers":{"relevant":{"type":"noul","noul":2}}}`)
	app, out, diagnostics := offlineApp(offlineInput(t, good, bad))
	if got := app.runGate(context.Background(), []string{"--policy", path}); got != 2 {
		t.Fatalf("exit %d, stderr %s", got, diagnostics)
	}
	var records []Envelope
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var env Envelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			t.Fatal(err)
		}
		records = append(records, env)
	}
	if len(records) != 2 || len(records[0].Runs) != 2 {
		t.Fatalf("records: %+v", records)
	}
	var first, second gate.Decision
	if err := json.Unmarshal(records[0].Runs[1].Result, &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(records[1].Runs[1].Result, &second); err != nil {
		t.Fatal(err)
	}
	if first.Outcome != gate.Allow || !second.Missing || records[1].Runs[1].Error == nil {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func TestGateSavedFailureDoesNotFallBack(t *testing.T) {
	env := offlineEnvelope(t, "a", "state", "true", 0.9)
	env.Runs = append(env.Runs, Run{Name: "later", Command: "judge", State: env.Runs[0].State, Questions: env.Runs[0].Questions, Error: &RecordError{Kind: "http", Message: "overloaded"}})
	path := offlineFile(t, gate.Bands{Input: "relevant", Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe, Allow: 0.8, Review: 0.5})
	app, out, _ := offlineApp(offlineInput(t, env))
	if got := app.runGate(context.Background(), []string{"--policy", path}); got != 2 {
		t.Fatalf("exit %d", got)
	}
	var result Envelope
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Runs[2].Error == nil {
		t.Fatal("latest failure disappeared")
	}
	var failedDecision gate.Decision
	if err := json.Unmarshal(result.Runs[2].Result, &failedDecision); err != nil {
		t.Fatal(err)
	}
	if len(failedDecision.Readings) != 1 || failedDecision.Readings[0].Problem != "failed" {
		t.Fatalf("saved HTTP failure reported as missing: %+v", failedDecision)
	}
	app, _, _ = offlineApp(offlineInput(t, env))
	if got := app.runGate(context.Background(), []string{"--policy", path, "--run", "source"}); got != 0 {
		t.Fatalf("explicit source exit %d", got)
	}
}

func TestGateAbstentionAndFailureRemainDistinct(t *testing.T) {
	rule := gate.Bands{Input: "pick", Measure: gate.MeasureTop, Polarity: gate.HighIsSafe, Allow: 0.8, Review: 0.5}
	empty := Run{Name: "source", Command: "pick", Result: json.RawMessage(`{"picked":false,"abstained":true}`)}
	decision, err := evaluatePolicy(empty, rule)
	if err != nil || !decision.Missing || len(decision.Readings) != 1 || decision.Readings[0].Problem != "abstained" {
		t.Fatalf("decision %+v error %v", decision, err)
	}
	empty.Error = &RecordError{Kind: "input", Message: "broken candidates"}
	if _, err := evaluatePolicy(empty, rule); err == nil {
		t.Fatal("failed pick became benign abstention")
	}
	valid := offlineEnvelope(t, "a", "state", "true", 0.9).Runs[0]
	valid.Invalid = map[string]RecordError{"relevant": {Kind: "validation", Message: "saved invalid", Reason: decide.ReasonMissingField}}
	if _, err := evaluatePolicy(valid, gate.Bands{Input: "relevant", Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe, Allow: 0.8, Review: 0.5}); err == nil {
		t.Fatal("saved invalid ignored")
	}
	valid.Invalid = nil
	valid.Error = &RecordError{Kind: "validation", Message: "unusable saved response"}
	decision, err = evaluatePolicy(valid, gate.Bands{Input: "relevant", Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe, Allow: 0.8, Review: 0.5})
	if err == nil || !decision.Missing || decision.Readings[0].Problem != "failed" {
		t.Fatalf("whole saved failure: %+v %v", decision, err)
	}
	// Nonempty abstention comes from the authoritative Choice even if the
	// convenience result was removed or edited in the saved envelope.
	nonempty := Run{Name: "source", Command: "pick", State: json.RawMessage(`"state"`),
		Questions: map[string]json.RawMessage{"pick": offlineJSON(t, decide.Choice("Which?", decide.Option("candidate", "Value"), decide.Option("none", "None")))},
		Response:  json.RawMessage(`{"model":"jev-1.13.0","answers":{"pick":{"type":"choice","choice":"none","confidence":0.9,"probabilities":{"candidate":0.1,"none":0.9}}}}`)}
	decision, err = evaluatePolicy(nonempty, rule)
	if err != nil || !decision.Missing || decision.Readings[0].Problem != "abstained" {
		t.Fatalf("Choice abstention: %+v %v", decision, err)
	}
	nonempty.Result = json.RawMessage(`{"abstained":false}`)
	decision, err = evaluatePolicy(nonempty, rule)
	if err != nil || decision.Readings[0].Problem != "abstained" {
		t.Fatalf("edited result overrode Choice: %+v %v", decision, err)
	}
}

func TestGateReviewIsSuccessfulOfflineAndPolicyStrict(t *testing.T) {
	env := offlineEnvelope(t, "a", "state", "true", 0.6)
	path := offlineFile(t, gate.Bands{Input: "relevant", Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe, Allow: 0.8, Review: 0.5})
	app, _, _ := offlineApp(offlineInput(t, env))
	if got := app.runGate(context.Background(), []string{"--policy", path}); got != 0 {
		t.Fatalf("review exit %d", got)
	}
	if err := os.WriteFile(path, []byte(`{"type":"bands","input":"relevant","measure":"noul","polarity":"high_is_safe","allow":0.8,"review":0.5,"alow":0.1}`), 0600); err != nil {
		t.Fatal(err)
	}
	app, out, _ := offlineApp(offlineInput(t, env))
	if got := app.runGate(context.Background(), []string{"--policy", path}); got != 2 || out.Len() != 0 {
		t.Fatalf("bad policy exit %d output %s", got, out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := os.WriteFile(path, offlineJSON(t, gate.Bands{Input: "relevant", Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe, Allow: 0.8, Review: 0.5}), 0600); err != nil {
		t.Fatal(err)
	}
	app, _, _ = offlineApp("")
	if got := app.runGate(ctx, []string{"--policy", path}); got != 130 {
		t.Fatalf("canceled exit %d", got)
	}
}

type offlineBrokenWriter struct{}

func (offlineBrokenWriter) Write([]byte) (int, error) { return 0, errors.New("closed output") }

type offlineCancelReader struct {
	input  io.Reader
	cancel context.CancelFunc
}

func (r offlineCancelReader) Read(p []byte) (int, error) {
	n, err := r.input.Read(p)
	r.cancel()
	return n, err
}

func TestOfflineCancellationDuringInputUses130(t *testing.T) {
	policy := offlineFile(t, gate.Bands{Input: "relevant", Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe, Allow: 0.8, Review: 0.5})
	for _, args := range [][]string{
		{"gate", "--policy", policy},
		{"eval", "dataset", "--answer", "relevant"},
	} {
		for _, empty := range []bool{false, true} {
			input := offlineInput(t, offlineEnvelope(t, "a", "state", "true", 0.9))
			if empty {
				input = ""
			}
			ctx, cancel := context.WithCancel(context.Background())
			app, out, diagnostics := offlineApp(input)
			app.In = offlineCancelReader{input: strings.NewReader(input), cancel: cancel}
			if got := app.Run(ctx, args); got != 130 || out.Len() != 0 {
				t.Fatalf("%v empty=%v: exit %d stdout %s stderr %s", args, empty, got, out, diagnostics)
			}
		}
	}
}
