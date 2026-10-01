package pick_test

import (
	"encoding/json"
	"testing"

	"github.com/deepnoodle-ai/decide"
)

// criteria returns the option keys and descriptions of q in order.
func criteria(q *decide.ChoiceQuestion) (keys []string, descs []any) {
	for _, o := range q.Criteria {
		keys = append(keys, o.Key)
		descs = append(descs, o.Description)
	}
	return keys, descs
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
