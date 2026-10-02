package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

func selectionApp(t *testing.T, input string, server *decidetest.Server) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, diagnostics bytes.Buffer
	a := &App{In: strings.NewReader(input), Out: &out, Err: &diagnostics}
	a.NewClient = func() (*decide.Client, error) {
		if server == nil {
			t.Error("offline operation attempted to construct a client")
			return nil, fmt.Errorf("unexpected client construction")
		}
		return server.NewClient(decide.WithMaxRetries(0))
	}
	return a, &out, &diagnostics
}

func selectionOutput(t *testing.T, out *bytes.Buffer) []Envelope {
	t.Helper()
	var records []Envelope
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var record Envelope
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode output: %v\n%s", err, line)
		}
		records = append(records, record)
	}
	return records
}

func TestPickPreservesCandidateAndContext(t *testing.T) {
	server := decidetest.NewServer(t)
	server.Answer("pick", decidetest.ChoiceAnswer(map[string]float64{"c1": .1, "c2": .8, "none": .1}))
	input := `{"id":"doc","state":"Use the second email","candidates":[{"email":"a@example.com"},{"email":"b@example.com","offset":17,"large":9007199254740993}]}`
	a, out, diagnostics := selectionApp(t, input, server)
	if code := a.runPick(t.Context(), []string{"--question", "Which email?"}); code != 0 {
		t.Fatalf("code %d: %s", code, diagnostics)
	}
	record := selectionOutput(t, out)[0]
	if record.ID != "doc" || !strings.Contains(string(record.Data), "9007199254740993") {
		t.Fatalf("source changed: %+v", record)
	}
	var result struct {
		Picked, Abstained bool
		Item              json.RawMessage
		Index             int
	}
	if err := json.Unmarshal(record.Runs[0].Result, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Picked || result.Abstained || result.Index != 1 || !strings.Contains(string(result.Item), "9007199254740993") {
		t.Fatalf("pick result: %+v", result)
	}
	requests := server.Requests()
	state, _ := json.Marshal(requests[0].Request.State)
	if string(state) != `"Use the second email"` {
		t.Fatalf("state: %s", state)
	}
	if record.Runs[0].RequestID == "" || !bytes.Contains(record.Runs[0].Response, []byte("jev-1.13.0")) {
		t.Fatalf("provenance missing: %+v", record.Runs[0])
	}
}

func TestPickAbstentionAndEmptyWithoutClient(t *testing.T) {
	server := decidetest.NewServer(t)
	server.Answer("pick", decidetest.ChoiceAnswer(map[string]float64{"c1": .1, "none": .9}))
	a, out, diagnostics := selectionApp(t, `{"state":"No fit","candidates":["x"]}`, server)
	if code := a.runPick(t.Context(), []string{"--question", "Which?"}); code != 0 {
		t.Fatalf("code %d: %s", code, diagnostics)
	}
	if got := selectionOutput(t, out)[0].Runs[0].Result; string(got) != `{"picked":false,"abstained":true}` {
		t.Fatalf("abstention: %s", got)
	}
	a, out, diagnostics = selectionApp(t, `{"state":"No candidates","candidates":[]}`, nil)
	if code := a.runPick(t.Context(), []string{"--question", "Which?"}); code != 0 {
		t.Fatalf("empty code %d: %s", code, diagnostics)
	}
	run := selectionOutput(t, out)[0].Runs[0]
	if string(run.State) != `"No candidates"` || len(run.Response) != 0 || run.RequestID != "" || !bytes.Contains(run.Result, []byte(`"abstained":true`)) {
		t.Fatalf("empty pick fabricated evidence: %+v", run)
	}
}

func TestPickCandidateCap(t *testing.T) {
	for _, count := range []int{254, 255} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			server := decidetest.NewServer(t)
			items := make([]int, count)
			for i := range items {
				items[i] = i
			}
			data, _ := json.Marshal(map[string]any{"state": "Choose", "candidates": items})
			a, out, diagnostics := selectionApp(t, string(data), server)
			code := a.runPick(t.Context(), []string{"--question", "Which value?"})
			if count == 254 && (code != 0 || len(server.Requests()) != 1) {
				t.Fatalf("at cap code %d requests %d: %s", code, len(server.Requests()), diagnostics)
			}
			if count == 255 && (code != 2 || len(server.Requests()) != 0 || len(selectionOutput(t, out)) != 1) {
				t.Fatalf("over cap code %d requests %d: %s", code, len(server.Requests()), diagnostics)
			}
		})
	}
}

func TestJoinJudgesSuppliedPairOnly(t *testing.T) {
	server := decidetest.NewServer(t)
	server.Answer("relation", decidetest.NoulAnswer(.75))
	input := `{"id":"pair","left":{"vendor":"Acme"},"right":{"vendor":"ACME LLC"},"note":"preserved"}`
	a, out, diagnostics := selectionApp(t, input, server)
	if code := a.runJoin(t.Context(), []string{"--question", "Same vendor?"}); code != 0 {
		t.Fatalf("code %d: %s", code, diagnostics)
	}
	run := selectionOutput(t, out)[0].Runs[0]
	if string(run.Result) != `{"noul":0.75}` || bytes.Contains(run.State, []byte("note")) {
		t.Fatalf("unexpected join semantics: %+v", run)
	}
	if len(server.Requests()) != 1 || len(server.Requests()[0].Request.Questions) != 1 {
		t.Fatal("pair expanded or additional requests made")
	}
}

func TestSelectionMalformedInputAndCanceledContext(t *testing.T) {
	for _, input := range []string{`{"state":null,"candidates":[]}`, `{"state":"x","candidates":null}`} {
		a, _, _ := selectionApp(t, input, nil)
		if code := a.runPick(t.Context(), []string{"--question", "Which?"}); code != 2 {
			t.Fatalf("input %s exit %d", input, code)
		}
	}
	a, _, _ := selectionApp(t, `{"left":"a"}`, nil)
	if code := a.runJoin(t.Context(), []string{"--question", "Same?"}); code != 2 {
		t.Fatalf("missing right exit %d", code)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	a, _, _ = selectionApp(t, `{"state":"x","candidates":[]}`, nil)
	if code := a.runPick(ctx, []string{"--question", "Which?"}); code != 130 {
		t.Fatalf("canceled exit %d", code)
	}
}

func TestSelectionHelpAndUnsupportedAdapterFlags(t *testing.T) {
	for _, command := range []string{"pick", "join", "rank", "pack"} {
		t.Run(command, func(t *testing.T) {
			a, out, diagnostics := selectionApp(t, "", nil)
			if code := a.Run(t.Context(), []string{command, "--help"}); code != 0 || out.Len() == 0 {
				t.Fatalf("help exit %d out %s diagnostics %s", code, out, diagnostics)
			}
		})
	}
	for _, command := range []string{"pick", "join"} {
		a, _, _ := selectionApp(t, "", nil)
		if code := a.Run(t.Context(), []string{command, "--question", "Which?", "--state-field", "state"}); code != 2 {
			t.Fatalf("unsupported state-field %s exit %d", command, code)
		}
	}
}
