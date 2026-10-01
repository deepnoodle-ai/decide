package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

func coreFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func coreOptions() CommonOptions {
	return CommonOptions{Input: "jsonl", As: "judge", Workers: 2, ChunkSize: 2, MaxRecordBytes: 1 << 20, MaxRecords: 10000, MaxBytes: 64 << 20}
}
func coreRecords(t *testing.T, b *bytes.Buffer) []Envelope {
	t.Helper()
	reader := NewRecordReader(bytes.NewReader(b.Bytes()), coreOptions())
	var result []Envelope
	for {
		e, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return result
		}
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, e)
	}
}
func coreApp(t *testing.T, input string, server *decidetest.Server) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out, diagnostics := &bytes.Buffer{}, &bytes.Buffer{}
	a := &App{In: strings.NewReader(input), Out: out, Err: diagnostics}
	if server != nil {
		a.NewClient = func() (*decide.Client, error) { return server.NewClient() }
	}
	return a, out, diagnostics
}

func TestRecordCodecKeepsJSONValuesAndExtensions(t *testing.T) {
	input := `{"id":"a","integer":900719925474099312345,"nested":{"large":18446744073709551615}}`
	reader := NewRecordReader(strings.NewReader(input), coreOptions())
	env, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	if env.ID != "a" || !bytes.Contains(env.Data, []byte("900719925474099312345")) {
		t.Fatalf("changed data: %s", env.Data)
	}
	env.Extra = map[string]json.RawMessage{"future": json.RawMessage(`{"v":900719925474099312345}`)}
	env.Runs = []Run{{Name: "previous", Command: "judge", Extra: map[string]json.RawMessage{"extension": json.RawMessage(`{"n":900719925474099312345}`)}}}
	wire, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Envelope
	if err = json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded.Extra["future"], env.Extra["future"]) || !bytes.Equal(decoded.Runs[0].Extra["extension"], env.Runs[0].Extra["extension"]) {
		t.Fatalf("extensions lost: %s", wire)
	}
}
func TestReadersRespectAdaptersAndReservedFormat(t *testing.T) {
	for _, tt := range []struct {
		name, input, adapter string
		count                int
		bad                  bool
	}{
		{"json blank lines", "\n  \n{\"x\":1}\n", "jsonl", 1, false},
		{"text empty", "\nhello\n", "text", 2, false},
		{"number", "900719925474099312345\n", "jsonl", 1, false},
		{"reserved version", `{"typesafe_cli":2,"id":"a","data":"a","runs":[]}`, "jsonl", 0, true},
		{"reserved missing", `{"typesafe_cli":1}`, "jsonl", 0, true},
		{"duplicate input member", `{"state":{"a":1,"a":2}}`, "jsonl", 0, true},
		{"bad json", `{"unfinished":`, "jsonl", 0, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			o := coreOptions()
			o.Input = tt.adapter
			r := NewRecordReader(strings.NewReader(tt.input), o)
			count := 0
			for {
				_, err := r.Next()
				if errors.Is(err, io.EOF) {
					if tt.bad {
						t.Fatal("wanted error")
					}
					break
				}
				if err != nil {
					if !tt.bad {
						t.Fatal(err)
					}
					break
				}
				count++
			}
			if count != tt.count {
				t.Fatalf("count=%d", count)
			}
		})
	}
}
func TestRecordAndCollectionLimits(t *testing.T) {
	o := coreOptions()
	o.MaxRecordBytes = 3
	r := NewRecordReader(strings.NewReader(`"abc"`), o)
	if _, err := r.Next(); err == nil {
		t.Fatal("record limit ignored")
	}
	o = coreOptions()
	o.MaxRecords = 1
	if _, err := ReadRecords(strings.NewReader("\"a\"\n\"b\"\n"), o); err == nil {
		t.Fatal("count limit ignored")
	}
	o = coreOptions()
	o.MaxBytes = 4
	if _, err := ReadRecords(strings.NewReader("\"a\"\n\n"), o); err == nil {
		t.Fatal("raw input byte limit ignored")
	}
	o = coreOptions()
	if _, err := ReadRecords(strings.NewReader("{\"id\":\"same\"}\n{\"id\":\"same\"}\n"), o); err == nil {
		t.Fatal("duplicate collection IDs accepted")
	}
}
func TestConfigRejectsDuplicateMembersAndLimits(t *testing.T) {
	for _, body := range []string{`{"question":{"instructions":"a","instructions":"b"}}`, `{"a":1,"a":2}`} {
		path := coreFile(t, "config.json", body)
		if _, err := ReadJSONFile(path, ConfigMaxBytes); err == nil {
			t.Fatal("duplicate config accepted")
		}
	}
	path := coreFile(t, "config.json", `{"a":1}`)
	if _, err := ReadJSONFile(path, 3); err == nil {
		t.Fatal("oversized config accepted")
	}
	if _, err := ReadJSONFile(path, 0); err == nil {
		t.Fatal("zero cap accepted")
	}
}
func TestSelectedRunDoesNotFallBackAndNamesStayUnique(t *testing.T) {
	e := Envelope{ID: "a", Runs: []Run{{Name: "good", Command: "judge"}, {Name: "failed", Command: "judge", Error: &RecordError{Kind: "http", Message: "failed"}}}}
	got, err := e.SelectedRun("")
	if err != nil || got.Name != "failed" {
		t.Fatal("fell back")
	}
	if _, err = e.SelectedRun("missing"); err == nil {
		t.Fatal("missing selection accepted")
	}
	if err = e.AppendRun(Run{Name: "good", Command: "gate"}); err == nil {
		t.Fatal("duplicate name accepted")
	}
}
func TestBasicMixedJudgmentAndDefaultRequestedModel(t *testing.T) {
	server := decidetest.NewServer(t)
	t.Setenv("TYPESAFE_DEFAULT_MODEL", "  jev-preview  ")
	file := coreFile(t, "questions.json", `{"relevant":{"type":"noul","instructions":"Relevant?"},"team":{"type":"choice","instructions":"Which?","criteria":{"a":"A","b":"B"}},"urgency":{"type":"score","instructions":"How?","criteria":["later","now"]}}`)
	a, out, diagnostics := coreApp(t, "{\"id\":\"a\",\"state\":{\"number\":900719925474099312345},\"label\":\"true\"}\n", server)
	if code := a.Run(t.Context(), []string{"judge", "--questions", file, "--state-field", "state"}); code != 0 {
		t.Fatalf("code=%d %s", code, diagnostics)
	}
	envs := coreRecords(t, out)
	r := envs[0].Runs[0]
	if r.RequestedModel != "jev-preview" || len(r.Questions) != 3 || r.RequestID == "" {
		t.Fatalf("metadata=%+v", r)
	}
	recorded := server.Requests()
	if len(recorded) != 1 || !bytes.Contains(recorded[0].Body, []byte("900719925474099312345")) {
		t.Fatalf("number lost: %+v", recorded)
	}
	if strings.Contains(string(recorded[0].Body), `"label"`) {
		t.Fatal("state-field sent label")
	}
	if _, err := r.ValidatedResponse(); err != nil {
		t.Fatal(err)
	}
}
func TestBasicLiveResultAndRejectionSemantics(t *testing.T) {
	server := decidetest.NewServer(t)
	server.Answer("match", decidetest.NoulAnswer(.25))
	for _, tt := range []struct {
		args        []string
		code, count int
	}{
		{[]string{"grep", "--question", "Relevant?", "--threshold", ".5"}, 1, 0},
		{[]string{"grep", "--question", "Relevant?", "--threshold", ".5", "--keep-all"}, 1, 1},
		{[]string{"grep", "--question", "Relevant?", "--threshold", ".2"}, 0, 1},
		{[]string{"grep", "--question", "Relevant?"}, 2, 0},
		{[]string{"grep", "--question", "Relevant?", "--threshold", "NaN"}, 2, 0},
	} {
		a, out, diagnostics := coreApp(t, "\"hello\"\n", server)
		if code := a.Run(t.Context(), tt.args); code != tt.code {
			t.Fatalf("%v code=%d %s", tt.args, code, diagnostics)
		}
		if count := len(coreRecords(t, out)); count != tt.count {
			t.Fatalf("%v count=%d", tt.args, count)
		}
	}
	taxonomy := coreFile(t, "taxonomy.json", `{"instructions":"Which team?","criteria":{"a":{"description":"First"},"b":"Second"}}`)
	a, out, diagnostics := coreApp(t, "\"hello\"\n", server)
	if code := a.Run(t.Context(), []string{"label", "--taxonomy", taxonomy}); code != 0 {
		t.Fatalf("label=%d %s", code, diagnostics)
	}
	if !bytes.Contains(coreRecords(t, out)[0].Runs[0].Result, []byte(`"label"`)) {
		t.Fatal("label missing")
	}
	rubric := coreFile(t, "rubric.json", `{"quality":{"instructions":"Quality?","criteria":["low","high"]}}`)
	a, out, diagnostics = coreApp(t, "\"hello\"\n", server)
	if code := a.Run(t.Context(), []string{"score", "--rubric", rubric}); code != 0 {
		t.Fatalf("score=%d %s", code, diagnostics)
	}
	if len(coreRecords(t, out)[0].Runs[0].Response) == 0 {
		t.Fatal("score response missing")
	}
	questions := coreFile(t, "question.json", `{"match":{"type":"noul","instructions":"Relevant?"}}`)
	policy := coreFile(t, "policy.json", `{"type":"bands","input":"match","measure":"noul","polarity":"high_is_safe","allow":0.8,"review":0.4}`)
	a, out, diagnostics = coreApp(t, "\"hello\"\n", server)
	if code := a.Run(t.Context(), []string{"check", "--questions", questions, "--policy", policy}); code != 1 {
		t.Fatalf("check=%d %s", code, diagnostics)
	}
	if !bytes.Contains(coreRecords(t, out)[0].Runs[0].Result, []byte(`"escalate"`)) {
		t.Fatalf("decision=%s", out)
	}
}
func TestReflectedCredentialsAreRedactedFromRunMetadata(t *testing.T) {
	const key = "synthetic-key-redaction-test"
	questions := coreFile(t, "questions.json", `{"match":{"type":"noul","instructions":"Match?"}}`)
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized} {
		for _, padded := range []bool{false, true} {
			name := http.StatusText(status)
			if padded {
				name += "/padded-key"
			}
			t.Run(name, func(t *testing.T) {
				envKey := key
				if padded {
					envKey = "  " + key + "  "
				}
				t.Setenv("TYPESAFE_API_KEY", envKey)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer "+key {
						t.Error("SDK did not send the expected synthetic credential")
					}
					w.Header().Set("x-typesafe-request-id", "req-"+key+"-suffix")
					w.WriteHeader(status)
					if status == http.StatusOK {
						io.WriteString(w, `{"model":"resolved-test","answers":{"match":{"type":"noul","noul":0.9}}}`)
					} else {
						json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "Rejected " + key}})
					}
				}))
				defer server.Close()
				t.Setenv("TYPESAFE_BASE_URL", server.URL)
				a, out, diagnostics := coreApp(t, "\"hello\"\n", nil)
				wantExit := 0
				if status != http.StatusOK {
					wantExit = 2
				}
				if code := a.Run(t.Context(), []string{"judge", "--questions", questions}); code != wantExit {
					t.Fatalf("exit=%d, want %d", code, wantExit)
				}
				if strings.Contains(out.String(), key) || strings.Contains(diagnostics.String(), key) {
					t.Fatal("credential appeared in CLI output")
				}
				run := coreRecords(t, out)[0].Runs[0]
				const wantID = "req-[REDACTED]-suffix"
				if run.RequestID != wantID {
					t.Fatal("run request ID was not redacted")
				}
				if status != http.StatusOK && (run.Error == nil || run.Error.RequestID != wantID) {
					t.Fatal("error request ID was not redacted")
				}
			})
		}
	}
}

func TestRawValidationFailuresRemainVerifiable(t *testing.T) {
	body := `{"model":"resolved-test","answers":{"match":{"type":"noul"},"good":{"type":"noul","noul":0.9}},"usage":{"input_tokens":1,"output_tokens":0},"future":{"n":900719925474099312345}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-typesafe-request-id", "raw-test")
		io.WriteString(w, body)
	}))
	defer server.Close()
	client, err := decide.NewClient(decide.WithoutEnvironment(), decide.WithAPIKey("test-key-00000000"), decide.WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	questions := coreFile(t, "questions.json", `{"match":{"type":"noul","instructions":"Match?"},"good":{"type":"noul","instructions":"Good?"}}`)
	a, out, diagnostics := coreApp(t, "\"hello\"\n", nil)
	a.NewClient = func() (*decide.Client, error) { return client, nil }
	if code := a.Run(t.Context(), []string{"judge", "--questions", questions}); code != 2 {
		t.Fatalf("code=%d %s", code, diagnostics)
	}
	saved := coreRecords(t, out)[0].Runs[0]
	if string(saved.Response) != body {
		t.Fatalf("changed wire evidence: %s", saved.Response)
	}
	resp, err := saved.ValidatedResponse()
	if err == nil || resp.Invalid["match"] == nil || resp.Invalid["good"] != nil {
		t.Fatalf("validation lost: %+v %v", resp, err)
	}
	saved.Error = nil
	saved.Invalid = nil
	resp, err = saved.ValidatedResponse()
	if err == nil || resp.Invalid["match"] == nil {
		t.Fatal("tampered validation metadata bypassed")
	}
	saved.Response = json.RawMessage(`{"model":"m","answers":{"match":{"type":"noul","noul":0.9},"good":{"type":"noul","noul":0.9}}}`)
	saved.Error = &RecordError{Kind: "validation", Message: "whole-run validation"}
	resp, err = saved.ValidatedResponse()
	if err == nil || len(resp.Invalid) != 2 {
		t.Fatal("whole-run validation error bypassed")
	}
}
func TestRunnerOrderAndWriteFailureStopsNextChunk(t *testing.T) {
	server := decidetest.NewServer(t)
	release := make(chan struct{})
	var once sync.Once
	server.Respond(func(req *decide.Request) (*decide.Response, error) {
		if req.State == "first" {
			<-release
		}
		if req.State == "second" {
			once.Do(func() { close(release) })
		}
		return &decide.Response{Model: "resolved", Answers: map[string]decide.Answer{"match": decidetest.NoulAnswer(.9)}}, nil
	})
	questions := coreFile(t, "questions.json", `{"match":{"type":"noul","instructions":"Match?"}}`)
	a, out, diagnostics := coreApp(t, "\"first\"\n\"second\"\n\"third\"\n", server)
	if code := a.Run(t.Context(), []string{"judge", "--questions", questions, "--chunk-size", "2", "--workers", "2"}); code != 0 {
		t.Fatalf("code=%d %s", code, diagnostics)
	}
	envs := coreRecords(t, out)
	if string(envs[0].Data) != `"first"` || string(envs[1].Data) != `"second"` {
		t.Fatal("unordered output")
	}
	server.Reset()
	a, _, _ = coreApp(t, "\"first\"\n\"second\"\n\"third\"\n", server)
	a.Out = coreFailWriter{}
	if code := a.Run(t.Context(), []string{"judge", "--questions", questions, "--chunk-size", "2"}); code != 2 {
		t.Fatalf("write failure code=%d", code)
	}
	if len(server.Requests()) != 2 {
		t.Fatalf("continued after write failure: %d", len(server.Requests()))
	}
}

type coreFailWriter struct{}

func (coreFailWriter) Write([]byte) (int, error) { return 0, errors.New("closed pipe") }
func TestRunnerEmptyInputAndLocalFailureAvoidClient(t *testing.T) {
	file := coreFile(t, "questions.json", `{"match":{"type":"noul","instructions":"Match?"}}`)
	calls := 0
	for _, input := range []string{"", "123\n"} {
		a, out, _ := coreApp(t, input, nil)
		a.NewClient = func() (*decide.Client, error) { calls++; return nil, errors.New("unexpected client construction") }
		code := a.Run(t.Context(), []string{"judge", "--questions", file})
		if input == "" && code != 0 {
			t.Fatalf("empty code=%d", code)
		}
		if input != "" && (code != 2 || len(coreRecords(t, out)) != 1) {
			t.Fatalf("invalid-state code=%d", code)
		}
	}
	if calls != 0 {
		t.Fatalf("client calls=%d", calls)
	}
}
func TestRunnerDuplicateNamePreflightsBeforeCall(t *testing.T) {
	server := decidetest.NewServer(t)
	file := coreFile(t, "questions.json", `{"match":{"type":"noul","instructions":"Match?"}}`)
	a, _, _ := coreApp(t, `{"typesafe_cli":1,"id":"a","data":"hello","runs":[{"name":"judge","command":"judge"}]}`, server)
	if code := a.Run(t.Context(), []string{"judge", "--questions", file}); code != 2 || len(server.Requests()) != 0 {
		t.Fatalf("code=%d sent=%d", code, len(server.Requests()))
	}
}
func TestCancellationAndHelp(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	a, _, _ := coreApp(t, "", nil)
	if code := a.Run(ctx, []string{"judge"}); code != 130 {
		t.Fatalf("cancel=%d", code)
	}
	for _, command := range []string{"judge", "grep", "label", "score", "check"} {
		a, out, diagnostics := coreApp(t, "", nil)
		if code := a.Run(t.Context(), []string{command, "--help"}); code != 0 || out.Len() == 0 || diagnostics.Len() != 0 {
			t.Fatalf("help %s=%d stdout=%s stderr=%s", command, code, out, diagnostics)
		}
	}
}

func TestUnknownFlagsKeepStdoutEmpty(t *testing.T) {
	a, out, diagnostics := coreApp(t, "", nil)
	if code := a.Run(t.Context(), []string{"judge", "--invented"}); code != 2 || out.Len() != 0 || diagnostics.Len() == 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out, diagnostics)
	}
}
func TestMissingInstructionsDoNotCallAPI(t *testing.T) {
	server := decidetest.NewServer(t)
	path := coreFile(t, "bad.json", `{"match":{"type":"noul","instrutions":"Meaning lost"}}`)
	a, _, _ := coreApp(t, "\"hello\"\n", server)
	if code := a.Run(t.Context(), []string{"judge", "--questions", path}); code != 2 || len(server.Requests()) != 0 {
		t.Fatalf("code=%d sent=%d", code, len(server.Requests()))
	}
}
