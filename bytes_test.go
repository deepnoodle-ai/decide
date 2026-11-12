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

// thumbMeta is unexported, but encoding/json promotes the fields of an
// embedded struct, so Thumb is written as base64.
type thumbMeta struct{ Thumb []byte }

type embeddedItem struct {
	thumbMeta
	Name string
}

type embeddedPointer struct {
	*thumbMeta
	Name string
}

type ExportedMeta struct{ Thumb []byte }

type taggedEmbed struct {
	ExportedMeta `json:"meta"`
}

// blob is an unexported non-struct type; encoding/json skips it embedded.
type blob []byte

type embeddedBlob struct {
	blob
	Name string
}

func TestBytesInEmbeddedStructs(t *testing.T) {
	if b, _ := json.Marshal(embeddedItem{thumbMeta{[]byte("abc")}, "x"}); !strings.Contains(string(b), `"Thumb":"YWJj"`) {
		t.Fatalf("encoding/json wrote %s", b)
	}
	bad := []struct {
		state any
		want  string
	}{
		{embeddedItem{thumbMeta: thumbMeta{Thumb: []byte("abc")}, Name: "x"}, "state.Thumb"},
		{&embeddedPointer{thumbMeta: &thumbMeta{Thumb: []byte("abc")}}, "state.Thumb"},
		{taggedEmbed{ExportedMeta{Thumb: []byte("abc")}}, "state.meta.Thumb"},
	}
	for _, c := range bad {
		err := noulRequest(func(r *decide.Request) { r.State = c.state }).Validate()
		if !errors.Is(err, decide.ErrInvalidRequest) || !strings.Contains(err.Error(), c.want+" is []byte") {
			t.Errorf("%T: err = %v, want %s", c.state, err, c.want)
		}
	}
	for _, state := range []any{
		embeddedPointer{Name: "nil pointer"},
		embeddedBlob{blob: blob("x"), Name: "x"},
	} {
		if err := noulRequest(func(r *decide.Request) { r.State = state }).Validate(); err != nil {
			t.Errorf("%T: %v", state, err)
		}
	}
}

// signedBlob and hexBytes hold bytes but write their own JSON or text, so
// encoding/json never writes those bytes as base64.
type signedBlob struct{ Sig []byte }

func (s signedBlob) MarshalJSON() ([]byte, error) { return []byte(`"signed"`), nil }

type hexBytes struct{ B []byte }

func (h *hexBytes) MarshalText() ([]byte, error) { return []byte("abc"), nil }

func TestBytesSkippedWhereJSONWritesNone(t *testing.T) {
	for _, state := range []any{
		receiptState{Store: "nil bytes"},
		map[string]any{"bytes": []byte(nil)},
		receiptState{Store: "tagged -", Hidden: []byte("x")},
		receiptState{Store: "unexported", private: []byte("x")},
		signedBlob{Sig: []byte("x")},
		map[string]any{"sig": signedBlob{Sig: []byte("x")}},
		&hexBytes{B: []byte("x")},
		map[string]any{"hex": &hexBytes{B: []byte("x")}},
	} {
		if err := noulRequest(func(r *decide.Request) { r.State = state }).Validate(); err != nil {
			t.Errorf("%T %v: %v", state, state, err)
		}
		if b, err := json.Marshal(state); err != nil || strings.Contains(string(b), `"eA=="`) {
			t.Errorf("%T: encoding/json wrote %s, %v", state, b, err)
		}
	}
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

// TestBytesInCycles checks that a slice or map that holds itself ends the
// walk: without a visit record, each path is walked to the depth limit,
// about 2^64 calls for two self-references.
func TestBytesInCycles(t *testing.T) {
	s := []any{nil, nil}
	s[0], s[1] = s, s
	m := map[string]any{}
	m["a"], m["b"] = m, m
	withBytes := map[string]any{}
	withBytes["a"], withBytes["b"], withBytes["c"] = withBytes, withBytes, []byte("x")
	cases := []struct {
		name  string
		state any
		want  string
	}{
		{"slice", s, "encountered a cycle"},
		{"map", m, "encountered a cycle"},
		{"map with bytes", withBytes, "state.c is []byte"},
	}
	for _, c := range cases {
		done := make(chan error, 1)
		go func() { done <- noulRequest(func(r *decide.Request) { r.State = c.state }).Validate() }()
		select {
		case err := <-done:
			if !errors.Is(err, decide.ErrInvalidRequest) || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: Validate did not return", c.name)
		}
	}
}

// TestBytesReachedTwice checks that a map first reached at the depth limit
// is walked again when a shorter path reaches it.
func TestBytesReachedTwice(t *testing.T) {
	shared := map[string]any{"b": []byte("x")}
	for n := range 40 {
		var deep any = shared
		for range n {
			deep = []any{deep}
		}
		state := []any{deep, shared}
		if err := noulRequest(func(r *decide.Request) { r.State = state }).Validate(); err == nil || !strings.Contains(err.Error(), " is []byte") {
			t.Errorf("%d wrappers: err = %v", n, err)
		}
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

func TestBytesInExtra(t *testing.T) {
	bad := map[string]any{
		`Extra "photo"`:        []byte("x"),
		`Extra "meta".thumb`:   map[string]any{"thumb": []byte("x")},
		`Extra "list"[1].Data`: []any{"a", struct{ Data []byte }{[]byte("x")}},
	}
	for want, v := range bad {
		key, _, _ := strings.Cut(strings.TrimPrefix(want, `Extra "`), `"`)
		err := noulRequest(decide.WithRequestExtra(key, v)).Validate()
		if !errors.Is(err, decide.ErrInvalidRequest) ||
			!strings.Contains(err.Error(), want+" is []byte, which would be sent as base64 text; attach files with decide.WithMedia") {
			t.Errorf("%s: err = %v", want, err)
		}
	}
	for _, v := range []any{json.RawMessage(`{"a":1}`), []byte(nil), "text"} {
		if err := noulRequest(decide.WithRequestExtra("ok", v)).Validate(); err != nil {
			t.Errorf("%T: %v", v, err)
		}
	}
}
