package cli

import (
	"bytes"
	"errors"
	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func coreApp(t *testing.T, input string, server *decidetest.Server) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out, diagnostics := &bytes.Buffer{}, &bytes.Buffer{}
	a := &App{In: strings.NewReader(input), Out: out, Err: diagnostics}
	if server != nil {
		a.NewClient = func() (*decide.Client, error) { return server.NewClient() }
	}
	return a, out, diagnostics
}

func TestJSONConfigurationIsBoundedAndStrict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	for _, input := range []string{`{"profiles":{},"profiles":{}}`, `{"profiles":`} {
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadJSONFile(path, ConfigMaxBytes); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	if err := os.WriteFile(path, []byte(`{"profiles":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadJSONFile(path, 5); err == nil {
		t.Fatal("accepted oversized configuration")
	}
}

func TestDiagnosticsRedactCredentials(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", `synthetic-secret-"quoted"`)
	a, _, diagnostics := coreApp(t, "", nil)
	if a.fail(errors.New(`bad synthetic-secret-"quoted" and synthetic-secret-\"quoted\"`)) != 2 {
		t.Fatal("incorrect exit status")
	}
	if strings.Contains(diagnostics.String(), "synthetic-secret") || !strings.Contains(diagnostics.String(), "[REDACTED]") {
		t.Fatal("credential redaction failed")
	}
}
