package jobs

import (
	"errors"
	"strings"
	"testing"
)

func TestEvidenceAcceptsOnlyCurrentRunResults(t *testing.T) {
	for _, invalid := range []string{
		`{"typesafe_cli":1,"id":"r1","data":"hello","runs":[]}`,
		`{"decide_run":2,"id":"r1","status":"complete"}`,
		`{"decide_run":1,"id":"r1","id":"shadow","status":"complete"}`,
		`{"decide_run":1,"status":"complete"}`,
	} {
		if err := DecodeEvidence(strings.NewReader(invalid), nil); err == nil {
			t.Fatalf("accepted %s", invalid)
		}
	}
	stopped := errors.New("stop reading")
	err := DecodeEvidence(strings.NewReader(`{"decide_run":1,"id":"r1","status":"complete"}`), func(Result) error { return stopped })
	if !errors.Is(err, stopped) {
		t.Fatalf("callback error lost: %v", err)
	}
}
