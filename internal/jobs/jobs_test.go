package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/internal/catalog"
	"github.com/deepnoodle-ai/decide/internal/dataset"
)

func testOptions(t *testing.T) Options {
	t.Helper()
	o := DefaultOptions()
	o.RunDir = t.TempDir()
	o.Skill = "edited"
	o.SkillDefinition = &catalog.Skill{Version: 1, Name: "edited", Description: "Test judgment", State: "value", Inputs: []string{"json"}, Questions: map[string]json.RawMessage{"yes": json.RawMessage(`{"type":"noul","instructions":"Is this good?"}`)}}
	o.Sources.Sources = []string{"-"}
	o.Sources.Format = "jsonl"
	return o
}

func serverClient(t *testing.T, handler http.HandlerFunc) func() (*decide.Client, error) {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	return func() (*decide.Client, error) {
		return decide.NewClient(decide.WithAPIKey("test"), decide.WithBaseURL(s.URL), decide.WithMaxRetries(5), decide.WithoutEnvironment())
	}
}

func okay(w http.ResponseWriter) {
	io.WriteString(w, `{"model":"test-model","answers":{"yes":{"type":"noul","noul":0.8}},"usage":{"input_tokens":3,"output_tokens":1}}`)
}

func TestPlanAndPreparationBeforeCalls(t *testing.T) {
	o := testOptions(t)
	var calls atomic.Int32
	o.NewClient = serverClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); okay(w) })
	var plans []Prepared
	s, e := Plan(context.Background(), o, strings.NewReader("{\"x\":1}\n"), func(p Prepared) error { plans = append(plans, p); return nil })
	if e != nil || s.Items != 1 || len(plans) != 1 || calls.Load() != 0 {
		t.Fatalf("plan %+v %v %d", s, e, calls.Load())
	}
	s, e = Run(context.Background(), o, strings.NewReader("{\"x\":1}\ninvalid\n"), nil)
	if e == nil || s.Status != "preparation-incomplete" || calls.Load() != 0 {
		t.Fatalf("run %+v %v calls %d", s, e, calls.Load())
	}
	if _, e = Resume(context.Background(), s.ID, o, false, false, nil); e == nil || !strings.Contains(e.Error(), "snapshot") {
		t.Fatalf("resume %v", e)
	}
}

func TestRunBudgetOrderResumeFrozen(t *testing.T) {
	o := testOptions(t)
	o.Workers = 2
	o.MaxRequests = 2
	o.Order = "input"
	var calls atomic.Int32
	o.NewClient = serverClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); okay(w) })
	var out []Result
	s, e := Run(context.Background(), o, strings.NewReader("1\n2\n3\n"), func(r Result) error { out = append(out, r); return nil })
	if e != nil || s.Requests != 2 || s.Completed != 2 || s.Status != "limited" {
		t.Fatalf("%+v %v", s, e)
	}
	for i := 1; i < len(out); i++ {
		if out[i].Index < out[i-1].Index {
			t.Fatal("unordered")
		}
	}
	o.MaxRequests = 0
	o.Skill = "different"
	s, e = Resume(context.Background(), s.ID, o, false, false, nil)
	if e != nil || s.Requests != 2 || calls.Load() != 2 {
		t.Fatalf("frozen %+v %v calls=%d", s, e, calls.Load())
	}
	var b bytes.Buffer
	if e = Export(s.ID, o.RunDir, &b); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(b.String(), "test-model") {
		t.Fatal(b.String())
	}
}

func TestRetriesCountEachAttempt(t *testing.T) {
	o := testOptions(t)
	o.Retries = 2
	o.MaxRequests = 2
	var calls atomic.Int32
	o.NewClient = serverClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(503)
		io.WriteString(w, "secret body")
	})
	s, e := Run(context.Background(), o, strings.NewReader("1\n"), nil)
	if e != nil || s.Requests != 2 || calls.Load() != 2 || s.Status != "limited" {
		t.Fatalf("%+v %v calls=%d", s, e, calls.Load())
	}
	b, _ := os.ReadFile(filepath.Join(s.Path, "attempts.jsonl"))
	if bytes.Contains(b, []byte("secret body")) {
		t.Fatal(string(b))
	}
}

type unknownTransport struct{ calls atomic.Int32 }

func (t *unknownTransport) SystemOne(context.Context, *decide.Request) (*decide.Response, error) {
	t.calls.Add(1)
	return nil, errors.New("connection lost after submission")
}

func (t *unknownTransport) ListModels(context.Context) (*decide.ModelList, error) {
	return nil, errors.New("unused")
}

func TestUncertainRequiresExplicitRetry(t *testing.T) {
	o := testOptions(t)
	o.Retries = 9
	tr := &unknownTransport{}
	o.NewClient = func() (*decide.Client, error) {
		return decide.NewClient(decide.WithTransport(tr), decide.WithoutEnvironment())
	}
	s, e := Run(context.Background(), o, strings.NewReader("1\n"), nil)
	if e != nil || s.Uncertain != 1 || tr.calls.Load() != 1 {
		t.Fatalf("%+v %v", s, e)
	}
	s, e = Resume(context.Background(), s.ID, o, false, false, nil)
	if e != nil || tr.calls.Load() != 1 {
		t.Fatalf("%+v %v", s, e)
	}
	s, e = Resume(context.Background(), s.ID, o, false, true, nil)
	if e != nil || tr.calls.Load() != 2 || s.Uncertain != 1 {
		t.Fatalf("%+v %v calls %d", s, e, tr.calls.Load())
	}
}

func TestSnapshotRefsAndCopy(t *testing.T) {
	for _, snapshot := range []string{"refs", "copy"} {
		t.Run(snapshot, func(t *testing.T) {
			o := testOptions(t)
			o.Snapshot = snapshot
			path := filepath.Join(t.TempDir(), "data.jsonl")
			os.WriteFile(path, []byte("1\n"), 0600)
			o.Sources.Sources = []string{path}
			o.NewClient = serverClient(t, func(w http.ResponseWriter, r *http.Request) { okay(w) })
			s, e := Run(context.Background(), o, nil, nil)
			if e != nil {
				t.Fatal(e)
			}
			os.WriteFile(path, []byte("2\n"), 0600)
			_, e = Resume(context.Background(), s.ID, o, false, false, nil)
			if snapshot == "refs" && e == nil {
				t.Fatal("changed reference accepted")
			}
			if snapshot == "copy" && e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestImagesDeduplicatedAndCapability(t *testing.T) {
	o := testOptions(t)
	o.SkillDefinition.Inputs = []string{"image"}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var b bytes.Buffer
	png.Encode(&b, img)
	it := dataset.Item{ID: "img", Data: json.RawMessage(`{"path":"a.png"}`), Images: []dataset.Image{{ContentType: "image/png", Data: b.Bytes()}}}
	_, e := RunPrepared(context.Background(), o, []Prepared{{Item: it}}, nil)
	if e == nil || !strings.Contains(e.Error(), "cloudflare") {
		t.Fatalf("%v", e)
	}
	o.Provider = "cloudflare"
	o.SkillDefinition.Inputs = []string{"image"}
	var calls atomic.Int32
	o.NewClient = serverClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if !bytes.Contains(body, []byte(`"images"`)) {
			t.Error(string(body))
		}
		okay(w)
	})
	s, e := RunPrepared(context.Background(), o, []Prepared{{Item: it}, {Item: it}}, nil)
	if e != nil || calls.Load() != 2 {
		t.Fatalf("%+v %v", s, e)
	}
	assets, _ := os.ReadDir(filepath.Join(s.Path, "assets"))
	if len(assets) != 1 {
		t.Fatalf("assets %d", len(assets))
	}
	for i := 0; i < 2; i++ {
		body, _ := os.ReadFile(itemPath(s.Path, "inputs", i))
		if bytes.Contains(body, []byte("base64")) {
			t.Fatal(string(body))
		}
	}
}

func TestConcurrencyAndCancellation(t *testing.T) {
	o := testOptions(t)
	o.Workers = 2
	var active, peak atomic.Int32
	o.NewClient = serverClient(t, func(w http.ResponseWriter, r *http.Request) {
		a := active.Add(1)
		defer active.Add(-1)
		for {
			p := peak.Load()
			if a <= p || peak.CompareAndSwap(p, a) {
				break
			}
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(20 * time.Millisecond):
			okay(w)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	s, e := Run(ctx, o, strings.NewReader("1\n2\n3\n4\n5\n"), func(r Result) error { cancel(); return nil })
	if !errors.Is(e, context.Canceled) || s.Completed == 5 || peak.Load() > 2 {
		t.Fatalf("%+v %v peak %d", s, e, peak.Load())
	}
}

func TestLockAndRedaction(t *testing.T) {
	o := testOptions(t)
	secret := "a\"b\\secret"
	t.Setenv("TYPESAFE_API_KEY", secret)
	o.NewClient = serverClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401); fmt.Fprintf(w, "bad %s", secret) })
	s, e := Run(context.Background(), o, strings.NewReader("1\n"), nil)
	if e != nil {
		t.Fatal(e)
	}
	unlock, e := lock(s.Path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Resume(context.Background(), s.ID, o, true, true, nil); e == nil {
		t.Fatal("second lock succeeded")
	}
	unlock()
	if _, e = Resume(context.Background(), s.ID, o, false, false, nil); e != nil {
		t.Fatal(e)
	}
	filepath.WalkDir(s.Path, func(path string, d os.DirEntry, e error) error {
		if !d.IsDir() {
			b, _ := os.ReadFile(path)
			if bytes.Contains(b, []byte(secret)) {
				t.Error(path)
			}
		}
		return nil
	})
}

func TestRecoveredFinishedAttemptNeverResubmits(t *testing.T) {
	o := testOptions(t)
	var calls atomic.Int32
	o.NewClient = serverClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); okay(w) })
	s, e := Run(context.Background(), o, strings.NewReader("1\n"), nil)
	if e != nil {
		t.Fatal(e)
	}
	var r Result
	readJSON(itemPath(s.Path, "results", 0), &r)
	r.Status = "uncertain"
	r.Stages = nil
	r.Error = "unrecorded"
	writeJSON(itemPath(s.Path, "results", 0), r)
	f, e := os.OpenFile(filepath.Join(s.Path, "attempts.jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	f.WriteString(`{"index":`)
	f.Close()
	s, e = Resume(context.Background(), s.ID, o, false, false, nil)
	if e != nil || s.Completed != 1 || s.Requests != 1 || calls.Load() != 1 {
		t.Fatalf("%+v %v calls %d", s, e, calls.Load())
	}
}

func TestGlobalPatternParametersAndInputValidation(t *testing.T) {
	o := testOptions(t)
	o.Skill = ""
	o.SkillDefinition = nil
	o.Pattern = "builtin/funnel"
	o.Params = map[string]string{"question": "Is this about databases?"}
	s, e := Plan(context.Background(), o, strings.NewReader("\"hello\"\n"), nil)
	if e != nil || s.Items != 1 {
		t.Fatalf("%+v %v", s, e)
	}
	o.Params["undeclared"] = "bad"
	if _, e = Plan(context.Background(), o, strings.NewReader("1\n"), nil); e == nil {
		t.Fatal("unknown global parameter accepted")
	}
	o = testOptions(t)
	o.SkillDefinition.Inputs = []string{"image"}
	if _, e = Plan(context.Background(), o, strings.NewReader("1\n"), nil); e == nil {
		t.Fatal("image skill accepted json")
	}
	o = testOptions(t)
	o.SkillDefinition.Questions = nil
	if _, e = Run(context.Background(), o, strings.NewReader("1\n"), nil); e == nil {
		t.Fatal("invalid edited skill accepted")
	}
}

func TestCredentialsNeverReachCallbacksOrFrozenRequests(t *testing.T) {
	o := testOptions(t)
	secret := "synthetic-\"key\\with<&>escape"
	t.Setenv("TYPESAFE_API_KEY", secret)
	o.Skill = "builtin/code-risk"
	o.SkillDefinition = nil
	o.Params = map[string]string{"focus": secret}
	o.NewClient = serverClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var value any
		json.Unmarshal(body, &value)
		decoded, _ := json.Marshal(value)
		encodedSecret, _ := json.Marshal(secret)
		if bytes.Contains(body, []byte(secret)) || bytes.Contains(decoded, encodedSecret[1:len(encodedSecret)-1]) {
			t.Error("credential reached provider request")
		}
		w.WriteHeader(401)
	})
	check := func(v any) {
		t.Helper()
		b, _ := json.Marshal(v)
		escaped, _ := json.Marshal(secret)
		if bytes.Contains(b, []byte(secret)) || bytes.Contains(b, escaped[1:len(escaped)-1]) {
			t.Fatalf("credential escaped redaction: %s", b)
		}
	}
	_, e := Plan(context.Background(), o, strings.NewReader("\"func foo(){}\"\n"), func(p Prepared) error { check(p); return nil })
	if e != nil {
		t.Fatal(e)
	}
	s, e := Run(context.Background(), o, strings.NewReader("\"func foo(){}\"\n"), func(r Result) error { check(r); return nil })
	if e != nil {
		t.Fatal(e)
	}
	check(s)
	filepath.WalkDir(s.Path, func(path string, d os.DirEntry, e error) error {
		if !d.IsDir() {
			b, _ := os.ReadFile(path)
			escaped, _ := json.Marshal(secret)
			if bytes.Contains(b, []byte(secret)) || bytes.Contains(b, escaped[1:len(escaped)-1]) {
				t.Error(path)
			}
		}
		return nil
	})
}

func TestCredentialEndpointsRejectedBeforeArtifacts(t *testing.T) {
	for _, base := range []string{"https://api.example/?api_key=synthetic-credential-123", "https://user:password@api.example/", "https://api.example/#secret"} {
		o := testOptions(t)
		o.BaseURL = base
		s, e := Run(context.Background(), o, strings.NewReader(""), nil)
		if e == nil || s.ID != "" {
			t.Fatalf("%+v %v", s, e)
		}
		files, _ := os.ReadDir(o.RunDir)
		if len(files) != 0 {
			t.Fatal("invalid endpoint created artifacts")
		}
	}
}

func TestLegacyEvidenceFidelityAndMalformedRecords(t *testing.T) {
	valid := `{"typesafe_cli":1,"id":"r1","data":"hello","runs":[{"name":"judge","command":"judge","state":"hello","questions":{"yes":{"type":"noul","instructions":"Good?"}},"response":{"answers":{"yes":{"type":"noul","noul":0.9}}},"invalid":{"yes":{"kind":"invalid_answer","message":"original diagnostic"}}}]}`
	file := filepath.Join(t.TempDir(), "legacy.jsonl")
	os.WriteFile(file, []byte(valid+"\n"), 0600)
	var out []Result
	if e := ReadEvidence(file, "", func(r Result) error { out = append(out, r); return nil }); e != nil {
		t.Fatal(e)
	}
	if len(out) != 1 || len(out[0].Stages[0].Invalid) != 1 {
		t.Fatalf("%+v", out)
	}
	for _, bad := range []string{`{"typesafe_cli":2,"id":"r1","data":1,"runs":[]}`, `{"typesafe_cli":1,"id":"r1","data":1,"runs":[{"name":"a","command":"judge"},{"name":"a","command":"judge"}]}`, `{"typesafe_cli":1,"id":"r1","data":1,"runs":[{"name":"a"}]}`, `{"typesafe_cli":1,"id":"r1","id":"shadow","data":1,"runs":[]}`} {
		os.WriteFile(file, []byte(bad+"\n"), 0600)
		if e := ReadEvidence(file, "", func(Result) error { return nil }); e == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestUnknownArtifactVersionRejected(t *testing.T) {
	o := testOptions(t)
	o.NewClient = serverClient(t, func(w http.ResponseWriter, r *http.Request) { okay(w) })
	s, e := Run(context.Background(), o, strings.NewReader("1\n"), nil)
	if e != nil {
		t.Fatal(e)
	}
	var r Result
	readJSON(itemPath(s.Path, "results", 0), &r)
	if r.Version != 1 {
		t.Fatal(r)
	}
	r.Version = 99
	writeJSON(itemPath(s.Path, "results", 0), r)
	if e = ReadResults(s.ID, o.RunDir, func(Result) error { return nil }); e == nil {
		t.Fatal("unknown result accepted")
	}
	s.Version = 99
	writeJSON(filepath.Join(s.Path, "summary.json"), s)
	if _, e = Show(s.ID, o.RunDir); e == nil {
		t.Fatal("unknown summary accepted")
	}
}

func TestEnvironmentEndpointRejectedBeforeArtifacts(t *testing.T) {
	for _, provider := range []string{"typesafe", "cloudflare"} {
		t.Run(provider, func(t *testing.T) {
			o := testOptions(t)
			o.Provider = provider
			name := "TYPESAFE_BASE_URL"
			if provider == "cloudflare" {
				name = "CLOUDFLARE_BASE_URL"
			}
			t.Setenv(name, "https://api.example/?api_key=synthetic-credential-123")
			s, e := Run(context.Background(), o, strings.NewReader(""), nil)
			if e == nil || s.ID != "" {
				t.Fatalf("%+v %v", s, e)
			}
			files, _ := os.ReadDir(o.RunDir)
			if len(files) != 0 {
				t.Fatal("invalid endpoint created artifacts")
			}
		})
	}
}

func TestResumeUsesFrozenEnvironmentEndpointAndModel(t *testing.T) {
	o := testOptions(t)
	o.Snapshot = "copy"
	o.MaxRequests = 1
	o.Workers = 1
	var originalCalls, newCalls atomic.Int32
	original := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originalCalls.Add(1)
		var req struct{ Model string }
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "frozen-model" {
			t.Errorf("model changed: %s", req.Model)
		}
		okay(w)
	}))
	defer original.Close()
	changed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { newCalls.Add(1); okay(w) }))
	defer changed.Close()
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	t.Setenv("TYPESAFE_BASE_URL", original.URL)
	t.Setenv("TYPESAFE_DEFAULT_MODEL", "frozen-model")
	s, e := Run(context.Background(), o, strings.NewReader("1\n2\n"), nil)
	if e != nil || s.Completed != 1 {
		t.Fatalf("%+v %v", s, e)
	}
	t.Setenv("TYPESAFE_BASE_URL", changed.URL)
	t.Setenv("TYPESAFE_DEFAULT_MODEL", "changed-model")
	o.MaxRequests = 2
	s, e = Resume(context.Background(), s.ID, o, false, false, nil)
	if e != nil || s.Completed != 2 || originalCalls.Load() != 2 || newCalls.Load() != 0 {
		t.Fatalf("%+v %v original %d new %d", s, e, originalCalls.Load(), newCalls.Load())
	}
}

func TestCloudflareConnectionDefaultsFrozen(t *testing.T) {
	o := testOptions(t)
	o.Provider = "cloudflare"
	t.Setenv("TYPESAFE_BASE_URL", "https://wrong.example")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "original-account")
	normalized, e := normalize(o)
	if e != nil {
		t.Fatal(e)
	}
	if normalized.BaseURL != "https://api.cloudflare.com/client/v4" || normalized.AccountID != "original-account" || normalized.Model != "clef" {
		t.Fatalf("%+v", normalized)
	}
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "changed-account")
	t.Setenv("CLOUDFLARE_BASE_URL", "https://changed.example")
	normalizedAgain, e := normalize(normalized)
	if e != nil || normalizedAgain.AccountID != "original-account" || normalizedAgain.BaseURL != normalized.BaseURL {
		t.Fatalf("%+v %v", normalizedAgain, e)
	}
}

func TestExportAndDecodeEvidenceAboveInputItemLimit(t *testing.T) {
	o := testOptions(t)
	o.SkillDefinition.State = "file"
	o.SkillDefinition.Inputs = []string{"text"}
	file := filepath.Join(t.TempDir(), "large.txt")
	// Each item is below the default 16 MiB limit, but original data plus
	// prepared file state produces a saved record above that limit.
	content := strings.Repeat("x", 10<<20)
	if err := os.WriteFile(file, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	o.Sources.Sources = []string{file}
	o.Sources.Format = "text"
	o.NewClient = serverClient(t, func(w http.ResponseWriter, r *http.Request) { okay(w) })
	sum, err := Run(t.Context(), o, nil, nil)
	if err != nil || sum.Completed != 1 {
		t.Fatalf("run: %+v %v", sum, err)
	}
	var exported bytes.Buffer
	if err := Export(sum.ID, o.RunDir, &exported); err != nil {
		t.Fatal(err)
	}
	if exported.Len() <= 16<<20 {
		t.Fatalf("test record too small: %d", exported.Len())
	}
	count := 0
	if err := DecodeEvidence(&exported, func(r Result) error {
		count++
		var data string
		if err := json.Unmarshal(r.Data, &data); err != nil || data != content {
			t.Fatalf("original data did not round trip: %v", err)
		}
		var state struct {
			Content string `json:"content"`
		}
		if len(r.Stages) != 1 {
			t.Fatalf("stages=%d", len(r.Stages))
		}
		if err := json.Unmarshal(r.Stages[0].State, &state); err != nil || state.Content != content {
			t.Fatalf("prepared state did not round trip: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("decoded=%d", count)
	}
}

func TestDecodeEvidenceRejectsTruncatedAndDuplicateRecords(t *testing.T) {
	for _, text := range []string{
		`{"decide_run":1,"id":"a","status":"complete"`,
		`{"decide_run":1,"id":"a","id":"b","status":"complete"}`,
	} {
		if err := DecodeEvidence(strings.NewReader(text), nil); err == nil {
			t.Fatalf("accepted invalid evidence: %s", text)
		}
	}
	stop := errors.New("stop after first record")
	if err := DecodeEvidence(strings.NewReader("{\"decide_run\":1,\"id\":\"a\",\"status\":\"complete\"}\ninvalid"), func(Result) error { return stop }); !errors.Is(err, stop) {
		t.Fatalf("decoder read past callback stop: %v", err)
	}
}
