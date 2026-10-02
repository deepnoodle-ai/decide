package cli

import (
	"bytes"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

func TestFailedStateFieldDoesNotSaveInputAsState(t *testing.T) {
	server := decidetest.NewServer(t)
	file := coreFile(t, "questions.json", `{"q":{"type":"noul","instructions":"Relevant?"}}`)
	a, out, diagnostics := coreApp(t, `{"id":"failed","label":true,"extra":"preserved"}`, server)
	a.NewClient = func() (*decide.Client, error) {
		t.Error("failed build constructed a client")
		return server.NewClient()
	}
	if code := a.Run(t.Context(), []string{"judge", "--questions", file, "--state-field", "state"}); code != 2 {
		t.Fatalf("exit = %d, diagnostics = %s", code, diagnostics)
	}
	records := coreRecords(t, out)
	if len(records) != 1 || len(records[0].Runs) != 1 {
		t.Fatalf("output = %s", out)
	}
	run := records[0].Runs[0]
	if run.Error == nil || len(run.State) != 0 || len(run.Questions) != 0 || len(run.Response) != 0 {
		t.Fatalf("failed build fabricated model context: %+v", run)
	}
	if !bytes.Contains(records[0].Data, []byte(`"label":true`)) {
		t.Fatalf("source data changed: %s", records[0].Data)
	}
	if n := len(server.Requests()); n != 0 {
		t.Fatalf("failed build sent %d requests", n)
	}
}
