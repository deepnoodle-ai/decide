package runs

import (
	"testing"

	"github.com/deepnoodle-ai/decide"
)

func TestOnlyMediaRequestsWaitLonger(t *testing.T) {
	if opts := callOptions(decide.NewRequest("text")); len(opts) != 0 {
		t.Fatalf("a text request got %d call options", len(opts))
	}
	for _, key := range []string{"images", "media"} {
		req := decide.NewRequest("", decide.WithRequestExtra(key, []string{"x"}))
		if len(callOptions(req)) != 1 {
			t.Errorf("a request with %s keeps the default timeout", key)
		}
	}
}
