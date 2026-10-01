package pick_test

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/sod/x/pick"
)

type email string

type span struct{ text string }

func (s span) String() string { return s.text }

type record struct{ ID int }

func TestKeysFromStrings(t *testing.T) {
	long := strings.Repeat("é", 64) // 64 runes, 128 bytes: the limit counts runes
	tests := []struct {
		name      string
		items     []string
		opts      []pick.Option
		wantKeys  []string
		wantDescs []any
	}{
		{
			name:      "plain spans kept verbatim",
			items:     []string{"dana.personal@gmail.com", "(415) 555-0177", "$1,315.50"},
			wantKeys:  []string{"dana.personal@gmail.com", "(415) 555-0177", "$1,315.50"},
			wantDescs: []any{nil, nil, nil},
		},
		{
			name:      "newline and tab runs collapse to one space",
			items:     []string{"a\n\tb", "x \r\n\v y", "p\x00\x01q"},
			wantKeys:  []string{"a b", "x y", "p q"},
			wantDescs: []any{nil, nil, nil},
		},
		{
			name:      "leading and trailing space trimmed",
			items:     []string{"  a  ", "\tb\n"},
			wantKeys:  []string{"a", "b"},
			wantDescs: []any{nil, nil},
		},
		{
			name:      "invalid UTF-8 replaced",
			items:     []string{"a\xffb", "\xc3"},
			wantKeys:  []string{"a\uFFFDb", "\uFFFD"},
			wantDescs: []any{nil, nil},
		},
		{
			name:      "empty and blank strings get positional keys",
			items:     []string{"a", "", " \n "},
			wantKeys:  []string{"a", "c2", "c3"},
			wantDescs: []any{nil, "", " \n "},
		},
		{
			name:      "exactly MaxKeyLen runes kept, one more is positional with original text",
			items:     []string{"abcdefgh", "abcdefghi", "ab\ncdefghi"},
			opts:      []pick.Option{pick.MaxKeyLen(8)},
			wantKeys:  []string{"abcdefgh", "c2", "c3"},
			wantDescs: []any{nil, "abcdefghi", "ab\ncdefghi"},
		},
		{
			name:      "default MaxKeyLen counts runes",
			items:     []string{long, long + "é"},
			wantKeys:  []string{long, "c2"},
			wantDescs: []any{nil, long + "é"},
		},
		{
			name:      "duplicates get numbered suffixes",
			items:     []string{"x", "x", "y", "x"},
			wantKeys:  []string{"x", "x (2)", "y", "x (3)"},
			wantDescs: []any{nil, nil, nil, nil},
		},
		{
			name:      "item equal to the abstain key is renamed",
			items:     []string{"none", "a"},
			wantKeys:  []string{"none (2)", "a"},
			wantDescs: []any{nil, nil},
		},
		{
			name:      "item c2 collides with a positional c2",
			items:     []string{"c2", ""},
			wantKeys:  []string{"c2", "c2 (2)"},
			wantDescs: []any{nil, ""},
		},
		{
			name:      "positional c1 collides with a later item c1",
			items:     []string{"", "c1"},
			wantKeys:  []string{"c1", "c1 (2)"},
			wantDescs: []any{"", nil},
		},
		{
			name:      "suffix collides with a literal suffixed item",
			items:     []string{"x", "x (2)", "x"},
			wantKeys:  []string{"x", "x (2)", "x (3)"},
			wantDescs: []any{nil, nil, nil},
		},
		{
			name:      "custom abstain key collision",
			items:     []string{"ask_user", "none"},
			opts:      []pick.Option{pick.Abstain("ask_user", "Ask the user.")},
			wantKeys:  []string{"ask_user (2)", "none"},
			wantDescs: []any{nil, nil},
		},
		{
			name:      "KeyFunc keys verbatim, text becomes the description",
			items:     []string{"alpha", "beta", "  gamma\n"},
			opts:      []pick.Option{pick.KeyFunc(func(i int, s string) string { return " K" + strconv.Itoa(i) + "\t" })},
			wantKeys:  []string{" K0\t", " K1\t", " K2\t"},
			wantDescs: []any{"alpha", "beta", "  gamma\n"},
		},
		{
			name:      "KeyFunc equal to the text leaves a null description",
			items:     []string{"a", "b"},
			opts:      []pick.Option{pick.KeyFunc(func(_ int, s string) string { return s })},
			wantKeys:  []string{"a", "b"},
			wantDescs: []any{nil, nil},
		},
		{
			name:      "KeyFunc keys still yield to abstain and to each other",
			items:     []string{"a", "b", "c"},
			opts:      []pick.Option{pick.KeyFunc(func(i int, _ string) string { return []string{"none", "k", "k"}[i] })},
			wantKeys:  []string{"none (2)", "k", "k (2)"},
			wantDescs: []any{"a", "b", "c"},
		},
		{
			name:      "Describe overrides every description",
			items:     []string{"a", "", "abcdefghi"},
			opts:      []pick.Option{pick.MaxKeyLen(8), pick.Describe(func(s string) any { return map[string]any{"len": len(s)} })},
			wantKeys:  []string{"a", "c2", "c3"},
			wantDescs: []any{map[string]any{"len": 1}, map[string]any{"len": 0}, map[string]any{"len": 9}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := pick.New(tt.items, tt.opts...)
			if err != nil {
				t.Fatal(err)
			}
			if got := p.Keys(); !slices.Equal(got, tt.wantKeys) {
				t.Errorf("keys = %q, want %q", got, tt.wantKeys)
			}
			keys, descs := criteria(p.Question("Q?"))
			if last := keys[len(keys)-1]; last != p.AbstainKey() {
				t.Errorf("last option = %q, want abstain %q", last, p.AbstainKey())
			}
			if got, want := mustJSON(t, descs[:len(descs)-1]), mustJSON(t, tt.wantDescs); got != want {
				t.Errorf("descriptions = %s, want %s", got, want)
			}
			for i, k := range tt.wantKeys {
				item, idx, ok := p.Item(k)
				if !ok || idx != i || item != tt.items[i] {
					t.Errorf("Item(%q) = %q, %d, %v; want %q, %d, true", k, item, idx, ok, tt.items[i], i)
				}
			}
		})
	}
}

func TestKeysFromOtherTypes(t *testing.T) {
	t.Run("fmt.Stringer", func(t *testing.T) {
		p, err := pick.New([]span{{"x\ny"}, {""}})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := p.Keys(), []string{"x y", "c2"}; !slices.Equal(got, want) {
			t.Errorf("keys = %q, want %q", got, want)
		}
	})
	t.Run("named string type", func(t *testing.T) {
		p, err := pick.New([]email{"a@b.co", "c@d.co"})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := p.Keys(), []string{"a@b.co", "c@d.co"}; !slices.Equal(got, want) {
			t.Errorf("keys = %q, want %q", got, want)
		}
		if item, _, _ := p.Item("c@d.co"); item != "c@d.co" {
			t.Errorf("Item = %q", item)
		}
	})
	t.Run("struct without Describe or KeyFunc errors", func(t *testing.T) {
		_, err := pick.New([]record{{1}, {2}})
		if !errors.Is(err, pick.ErrInvalidOption) || !strings.Contains(err.Error(), "item 0 has no text") {
			t.Fatalf("err = %v, want ErrInvalidOption naming item 0", err)
		}
	})
	t.Run("struct with Describe gets positional keys", func(t *testing.T) {
		p, err := pick.New([]record{{7}, {9}}, pick.Describe(func(r record) any { return map[string]int{"id": r.ID} }))
		if err != nil {
			t.Fatal(err)
		}
		keys, descs := criteria(p.Question(nil))
		if want := []string{"c1", "c2", "none"}; !slices.Equal(keys, want) {
			t.Errorf("keys = %q, want %q", keys, want)
		}
		if got := mustJSON(t, descs[:2]); got != `[{"id":7},{"id":9}]` {
			t.Errorf("descriptions = %s", got)
		}
	})
	t.Run("struct with KeyFunc has null descriptions", func(t *testing.T) {
		p, err := pick.New([]record{{7}, {9}}, pick.KeyFunc(func(_ int, r record) string { return "id-" + strconv.Itoa(r.ID) }))
		if err != nil {
			t.Fatal(err)
		}
		keys, descs := criteria(p.Question(nil))
		if want := []string{"id-7", "id-9", "none"}; !slices.Equal(keys, want) {
			t.Errorf("keys = %q, want %q", keys, want)
		}
		if descs[0] != nil || descs[1] != nil {
			t.Errorf("descriptions = %v, want nulls", descs)
		}
	})
}

func TestKeyFuncErrors(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		wantReason string
	}{
		{"empty", "", "empty"},
		{"invalid UTF-8", "k\xff", "invalid UTF-8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pick.New([]string{"a", "b", "c"}, pick.KeyFunc(func(i int, s string) string {
				if i == 2 {
					return tt.key
				}
				return s
			}))
			ke, ok := errors.AsType[*pick.KeyError](err)
			if !ok || !errors.Is(err, pick.ErrInvalidKey) {
				t.Fatalf("err = %v, want *KeyError", err)
			}
			if ke.Index != 2 || ke.Key != tt.key || ke.Reason != tt.wantReason {
				t.Errorf("KeyError = %+v", *ke)
			}
		})
	}
}

func TestKeysDeterministic(t *testing.T) {
	items := []string{"b", "a", "none", "", "a", "c1", "x\ny", strings.Repeat("z", 70), "a (2)"}
	var first string
	for i := range 100 {
		p, err := pick.New(items)
		if err != nil {
			t.Fatal(err)
		}
		got := mustJSON(t, p.Question("Q?"))
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("run %d:\n%s\nwant\n%s", i, got, first)
		}
	}
}

// TestKeysPinned pins keys that depend on the unicode tables. If a Go
// release moves one, the change is noticed here rather than in production.
func TestKeysPinned(t *testing.T) {
	items := []string{
		"\u00a0lead\u00a0nbsp",   // NO-BREAK SPACE is a space
		"em\u2003space",          // EM SPACE
		"line\u2028sep\u0085nel", // LINE SEPARATOR, NEXT LINE
		"zw\u200bsp",             // ZERO WIDTH SPACE is a format rune, kept
		"soft\u00adhyphen",       // SOFT HYPHEN is a format rune, kept
		"esc\x1b[0m",             // ESC is a control rune
		"ＦＵＬＬ　width",             // IDEOGRAPHIC SPACE; fullwidth letters kept
	}
	want := []string{
		"lead nbsp",
		"em space",
		"line sep nel",
		"zw\u200bsp",
		"soft\u00adhyphen",
		"esc [0m",
		"ＦＵＬＬ width",
	}
	p, err := pick.New(items)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Keys(); !slices.Equal(got, want) {
		t.Errorf("keys = %q, want %q", got, want)
	}
}
