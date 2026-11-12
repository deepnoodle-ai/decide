package decide_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/deepnoodle-ai/decide"
)

type receiptState struct {
	Store   string
	Receipt []byte `json:"receipt"`
	Hidden  []byte `json:"-"`
	private []byte
}

type nested struct {
	Items []*receiptState `json:"items"`
}

type chain struct {
	Next  *chain
	Bytes []byte `json:"bytes"`
}

func TestBytesAtAnyDepth(t *testing.T) {
	bad := map[string]any{
		"state":                     []byte("x"),
		"state.receipt":             receiptState{Receipt: []byte("x")},
		"state.items[1].receipt":    &nested{Items: []*receiptState{{}, {Receipt: []byte("x")}}},
		"state.a.b[0]":              map[string]any{"a": map[string]any{"b": [1][]byte{[]byte("x")}}},
		"state.list[2]":             map[string]any{"list": []any{1, "two", []byte("x")}},
		"state[0].Store":            []struct{ Store any }{{Store: []byte("x")}},
		"state.wrapped.inner.bytes": map[string]any{"wrapped": map[string]map[string][]byte{"inner": {"bytes": []byte("x")}}},
		"state.Next.bytes":          &chain{Next: &chain{Bytes: []byte("x")}},
	}
	for want, state := range bad {
		err := noulRequest(func(r *decide.Request) { r.State = state }).Validate()
		if !errors.Is(err, decide.ErrInvalidRequest) ||
			!strings.Contains(err.Error(), want+" is []byte, which would be sent as base64 text; attach files with decide.WithMedia") {
			t.Errorf("%s: err = %v", want, err)
		}
	}
	// A cycle ends the walk; encoding/json then reports it.
	loop := &chain{}
	loop.Next = loop
	if err := noulRequest(func(r *decide.Request) { r.State = loop }).Validate(); err == nil || strings.Contains(err.Error(), "[]byte") {
		t.Errorf("cycle: %v", err)
	}
	for _, state := range []any{
		json.RawMessage(`{"a":1}`),
		receiptState{Store: "Acme", Hidden: []byte("x"), private: []byte("x")},
		[]int{1, 2, 3},
		[4]byte{1, 2, 3, 4},
		time.Now(),
	} {
		if err := noulRequest(func(r *decide.Request) { r.State = state }).Validate(); err != nil {
			t.Errorf("%T: %v", state, err)
		}
	}
	req := decide.NewRequest("x")
	req.Questions["n"] = decide.Noul(map[string]any{"ask": map[string]any{"deep": []byte("q")}})
	if err := req.Validate(); err == nil || !strings.Contains(err.Error(), `question "n" instructions.ask.deep is []byte`) {
		t.Errorf("instructions: %v", err)
	}
}

func TestReservedExtraKeys(t *testing.T) {
	for _, k := range []string{"images", "media", "videos", "assets"} {
		err := noulRequest(decide.WithRequestExtra(k, []string{"x"})).Validate()
		want := `Extra key "` + k + `" is reserved; attach files with decide.WithMedia`
		if !errors.Is(err, decide.ErrInvalidRequest) || !strings.HasSuffix(err.Error(), want) {
			t.Errorf("%s: err = %v", k, err)
		}
	}
	if err := noulRequest(decide.WithRequestExtra("store", false)).Validate(); err != nil {
		t.Error(err)
	}
}
