package pick

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// deriveKeys returns one key and one option description per item. It is a
// pure function of its inputs: no map iteration order, randomness, or clock
// affects the result.
func deriveKeys[T any](items []T, s *settings, describe func(T) any, keyFunc func(int, T) string) ([]string, []any, error) {
	keys := make([]string, len(items))
	descs := make([]any, len(items))
	used := map[string]bool{s.abstainKey: true} // abstain is reserved first
	for i, item := range items {
		text, hasText := itemText(item)
		var base string
		var desc any
		switch {
		case keyFunc != nil:
			base = keyFunc(i, item)
			if reason := checkKey(base); reason != "" {
				return nil, nil, &KeyError{Index: i, Key: base, Reason: reason}
			}
			if hasText && text != base {
				desc = text // a caller key such as "c1" still shows the model the item
			}
		case hasText:
			if k := sanitize(text); k != "" && utf8.RuneCountInString(k) <= s.maxKeyLen {
				base = k // the span itself is the key; null description
			} else {
				base, desc = positional(i), text
			}
		case describe == nil:
			return nil, nil, fmt.Errorf("%w: item %d has no text; set Describe or KeyFunc", ErrInvalidOption, i)
		default:
			base = positional(i)
		}
		if describe != nil {
			desc = describe(item)
		}
		keys[i] = claim(base, used)
		descs[i] = desc
	}
	return keys, descs, nil
}

// itemText returns the item's text: String() for a fmt.Stringer, else the
// value of any type whose kind is string (so type Email string works).
func itemText(item any) (string, bool) {
	if s, ok := item.(fmt.Stringer); ok {
		return s.String(), true
	}
	if v := reflect.ValueOf(item); v.Kind() == reflect.String {
		return v.String(), true
	}
	return "", false
}

// sanitize replaces invalid UTF-8 with U+FFFD, collapses each run of space
// or control runes to one U+0020, and trims both ends. Case, punctuation,
// and symbols are kept.
func sanitize(s string) string {
	s = strings.ToValidUTF8(s, "�")
	var b strings.Builder
	b.Grow(len(s))
	gap := false
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			gap = true
			continue
		}
		if gap && b.Len() > 0 {
			b.WriteByte(' ')
		}
		gap = false
		b.WriteRune(r)
	}
	return b.String()
}

func positional(i int) string { return "c" + strconv.Itoa(i+1) }

// claim returns base, or the first free "base (n)" for n = 2, 3, ...,
// and marks it used. Comparison is exact, byte for byte.
func claim(base string, used map[string]bool) string {
	k := base
	for n := 2; used[k]; n++ {
		k = base + " (" + strconv.Itoa(n) + ")"
	}
	used[k] = true
	return k
}

// checkKey returns why k cannot be an option key, or "".
func checkKey(k string) string {
	switch {
	case k == "":
		return "empty"
	case !utf8.ValidString(k):
		return "invalid UTF-8"
	}
	return ""
}
