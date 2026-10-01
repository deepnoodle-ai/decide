package pick_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/deepnoodle-ai/sod"
	"github.com/deepnoodle-ai/sod/x/pick"
)

func items(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("item-%03d", i)
	}
	return out
}

func TestNewCap(t *testing.T) {
	tests := []struct {
		name        string
		n           int
		opts        []pick.Option
		wantOptions int                     // on success
		wantErr     *pick.TooManyItemsError // on failure
	}{
		{name: "254 items fit under 255", n: 254, wantOptions: 255},
		{name: "255 items do not", n: 255, wantErr: &pick.TooManyItemsError{Items: 255, Options: 256, Max: 255}},
		{name: "MaxOptions(3) with 2 items", n: 2, opts: []pick.Option{pick.MaxOptions(3)}, wantOptions: 3},
		{name: "MaxOptions(3) with 3 items", n: 3, opts: []pick.Option{pick.MaxOptions(3)}, wantErr: &pick.TooManyItemsError{Items: 3, Options: 4, Max: 3}},
		{name: "last MaxOptions wins", n: 3, opts: []pick.Option{pick.MaxOptions(3), pick.MaxOptions(4)}, wantOptions: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := pick.New(items(tt.n), tt.opts...)
			if tt.wantErr != nil {
				te, ok := errors.AsType[*pick.TooManyItemsError](err)
				if !ok || !errors.Is(err, pick.ErrTooManyItems) {
					t.Fatalf("err = %v, want *TooManyItemsError", err)
				}
				if *te != *tt.wantErr {
					t.Errorf("got %+v, want %+v", *te, *tt.wantErr)
				}
				if !strings.Contains(err.Error(), "narrow in two stages") ||
					!strings.Contains(err.Error(), "pre_parsed_value_extraction_cookbook") {
					t.Errorf("error text lacks the workaround: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := len(p.Question("Q?").Criteria); got != tt.wantOptions {
				t.Errorf("options = %d, want %d", got, tt.wantOptions)
			}
			if p.Len() != tt.n {
				t.Errorf("Len = %d, want %d", p.Len(), tt.n)
			}
		})
	}
}

func TestNewErrors(t *testing.T) {
	tests := []struct {
		name  string
		items []string
		opts  []pick.Option
		want  error
	}{
		{name: "zero items", items: []string{}, want: pick.ErrNoItems},
		{name: "nil items", items: nil, want: pick.ErrNoItems},
		{name: "MaxOptions(1)", items: []string{"a"}, opts: []pick.Option{pick.MaxOptions(1)}, want: pick.ErrInvalidOption},
		{name: "MaxKeyLen(7)", items: []string{"a"}, opts: []pick.Option{pick.MaxKeyLen(7)}, want: pick.ErrInvalidOption},
		{name: "empty abstain key", items: []string{"a"}, opts: []pick.Option{pick.Abstain("", "x")}, want: pick.ErrInvalidOption},
		{name: "invalid UTF-8 abstain key", items: []string{"a"}, opts: []pick.Option{pick.Abstain("n\xff", "x")}, want: pick.ErrInvalidOption},
		{name: "Describe of wrong T", items: []string{"a"}, opts: []pick.Option{pick.Describe(func(int) any { return nil })}, want: pick.ErrInvalidOption},
		{name: "KeyFunc of wrong T", items: []string{"a"}, opts: []pick.Option{pick.KeyFunc(func(int, email) string { return "k" })}, want: pick.ErrInvalidOption},
		{name: "nil Describe", items: []string{"a"}, opts: []pick.Option{pick.Describe[string](nil)}, want: pick.ErrInvalidOption},
		{name: "nil KeyFunc", items: []string{"a"}, opts: []pick.Option{pick.KeyFunc[string](nil)}, want: pick.ErrInvalidOption},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := pick.New(tt.items, tt.opts...)
			if !errors.Is(err, tt.want) || p != nil {
				t.Fatalf("New = %v, %v; want nil, %v", p, err, tt.want)
			}
		})
	}
}

func TestQuestion(t *testing.T) {
	p, err := pick.New([]string{"b", "a", "c"})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("abstain last, item order kept in JSON", func(t *testing.T) {
		got := mustJSON(t, p.Question("Which one?"))
		want := `{"type":"choice","instructions":"Which one?","criteria":{"b":null,"a":null,"c":null,"none":"None of these is the requested value."}}`
		if got != want {
			t.Errorf("got  %s\nwant %s", got, want)
		}
	})
	t.Run("instructions are a JSON value", func(t *testing.T) {
		q := p.Question(map[string]any{"role": "receipt"})
		if got := mustJSON(t, q.Instructions); got != `{"role":"receipt"}` {
			t.Errorf("instructions = %s", got)
		}
		if q.Extra != nil {
			t.Errorf("Extra = %v, want nil", q.Extra)
		}
	})
	t.Run("custom abstain", func(t *testing.T) {
		p, err := pick.New([]string{"a"}, pick.Abstain("ask_user", nil))
		if err != nil {
			t.Fatal(err)
		}
		if got := mustJSON(t, p.Question(nil).Criteria); got != `[{"Key":"a","Description":null},{"Key":"ask_user","Description":null}]` {
			t.Errorf("criteria = %s", got)
		}
	})
	t.Run("mutating a returned question leaves the next one unchanged", func(t *testing.T) {
		want := mustJSON(t, p.Question("Q?"))
		q := p.Question("Q?")
		q.Instructions = "changed"
		q.Criteria[0] = sod.ChoiceOption{Key: "zzz", Description: "x"}
		q.Criteria = append(q.Criteria[:1], sod.Option("extra"))
		q.Extra = map[string]any{"k": 1}
		if got := mustJSON(t, p.Question("Q?")); got != want {
			t.Errorf("got  %s\nwant %s", got, want)
		}
		if got := p.Keys(); !slices.Equal(got, []string{"b", "a", "c"}) {
			t.Errorf("Keys = %q", got)
		}
	})
	t.Run("Keys returns a copy", func(t *testing.T) {
		p.Keys()[0] = "mutated"
		if p.Keys()[0] != "b" {
			t.Error("Keys aliases the picker's slice")
		}
	})
	t.Run("New copies items", func(t *testing.T) {
		in := []string{"x", "y"}
		p, err := pick.New(in)
		if err != nil {
			t.Fatal(err)
		}
		in[0] = "mutated"
		if item, _, _ := p.Item("x"); item != "x" {
			t.Errorf("Item = %q, want x", item)
		}
	})
}

func TestAskPanics(t *testing.T) {
	p, err := pick.New([]string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	dup := sod.NewRequest("s")
	p.Ask(dup, "k", "Q?")
	tests := []struct {
		name string
		req  *sod.Request
		key  string
	}{
		{"duplicate request key", dup, "k"},
		{"nil request", nil, "k"},
		{"empty key", sod.NewRequest("s"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("Ask did not panic")
				}
			}()
			p.Ask(tt.req, tt.key, "Q?")
		})
	}
	t.Run("handle key and question", func(t *testing.T) {
		req := sod.NewRequest("s")
		h := p.Ask(req, "which", "Which one?")
		if h.Key() != "which" {
			t.Errorf("Key = %q", h.Key())
		}
		if got, want := mustJSON(t, req.Questions["which"]), mustJSON(t, p.Question("Which one?")); got != want {
			t.Errorf("question = %s, want %s", got, want)
		}
	})
}

func TestConcurrentUse(t *testing.T) {
	p, err := pick.New(items(50))
	if err != nil {
		t.Fatal(err)
	}
	want := mustJSON(t, p.Question("Q?"))
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Go(func() {
			for i := range 50 {
				q := p.Question("Q?")
				if b, err := q.MarshalJSON(); err != nil || string(b) != want {
					t.Errorf("goroutine %d: question changed", g)
					return
				}
				q.Criteria[0].Key = "mutated"
				key := p.Keys()[i]
				a := &sod.ChoiceAnswer{Choice: key, Probabilities: map[string]float64{key: 0.9, "none": 0.1}}
				r, err := p.Read(a)
				if err != nil || r.Index != i {
					t.Errorf("goroutine %d: Read = %+v, %v", g, r, err)
					return
				}
				for range p.Ranked(a) {
				}
				for range p.All() {
				}
			}
		})
	}
	wg.Wait()
}
