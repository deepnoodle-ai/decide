package pick

import (
	"iter"
	"slices"

	"github.com/deepnoodle-ai/sod"
)

// Picker holds a fixed, ordered candidate list and the option keys derived
// for it. It is immutable after New and safe for concurrent use. One Picker
// can back any number of questions.
type Picker[T any] struct {
	items       []T
	keys        []string
	descs       []any
	index       map[string]int // candidate key to item index
	abstainKey  string
	abstainDesc any
}

// New copies items, derives keys, and checks the cap. All configuration
// errors surface here, so Question and Ask never fail.
//
// Keys, by default, are each item's text (String() for a fmt.Stringer, or
// the value of a string-kinded type) with whitespace and control runs
// collapsed to one space. Text that is empty, or longer than MaxKeyLen
// runes, gets a positional key "c1", "c2", ... and is sent as the
// description instead. A key already in use, including the abstain key,
// gets " (2)", " (3)", ... appended. Whenever the final key differs from
// the item's text, the original text becomes its description, unless
// Describe supplies one. Duplicate items are kept: each maps
// back to its own index, and probability splits between them.
//
// Errors: ErrNoItems for an empty slice, *TooManyItemsError when
// len(items)+1 exceeds MaxOptions, *KeyError for a bad KeyFunc key, and an
// error wrapping ErrInvalidOption for a bad option or an item with no text
// and neither Describe nor KeyFunc.
func New[T any](items []T, opts ...Option) (*Picker[T], error) {
	s := newSettings(opts)
	describe, keyFunc, err := funcs[T](&s)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrNoItems
	}
	if n := len(items) + 1; n > s.maxOptions {
		return nil, &TooManyItemsError{Items: len(items), Options: n, Max: s.maxOptions}
	}
	items = slices.Clone(items)
	keys, descs, err := deriveKeys(items, &s, describe, keyFunc)
	if err != nil {
		return nil, err
	}
	index := make(map[string]int, len(keys))
	for i, k := range keys {
		index[k] = i
	}
	return &Picker[T]{
		items:       items,
		keys:        keys,
		descs:       descs,
		index:       index,
		abstainKey:  s.abstainKey,
		abstainDesc: s.abstainDesc,
	}, nil
}

// Question builds a new *sod.ChoiceQuestion on each call: one option
// per item in item order, then the abstain option last. Extra is nil.
// Mutating the returned question's fields or Criteria slice does not affect
// the Picker; description values themselves (a map returned by Describe,
// say) are shared, not deep-copied.
func (p *Picker[T]) Question(instructions any) *sod.ChoiceQuestion {
	opts := make([]sod.ChoiceOption, 0, len(p.keys)+1)
	for i, k := range p.keys {
		opts = append(opts, sod.ChoiceOption{Key: k, Description: p.descs[i]})
	}
	opts = append(opts, sod.ChoiceOption{Key: p.abstainKey, Description: p.abstainDesc})
	return sod.Choice(instructions, opts...)
}

// Ask is sod.Ask(req, key, p.Question(instructions)) plus a handle that
// maps the answer back to an item. It panics under the same conditions as
// sod.Ask: nil request, empty key, or a key already present.
func (p *Picker[T]) Ask(req *sod.Request, key string, instructions any) Handle[T] {
	return Handle[T]{h: sod.Ask(req, key, p.Question(instructions)), p: p}
}

// Len returns the number of candidates, excluding abstain.
func (p *Picker[T]) Len() int { return len(p.items) }

// Keys returns the candidate keys in item order, abstain excluded. The
// slice is a copy.
func (p *Picker[T]) Keys() []string { return slices.Clone(p.keys) }

// AbstainKey returns the abstain option's key.
func (p *Picker[T]) AbstainKey() string { return p.abstainKey }

// All yields (key, item) in item order, abstain excluded.
func (p *Picker[T]) All() iter.Seq2[string, T] {
	return func(yield func(string, T) bool) {
		for i, k := range p.keys {
			if !yield(k, p.items[i]) {
				return
			}
		}
	}
}

// Item returns the candidate for an option key and its index. The abstain
// key and unknown keys return ok == false. Use it to walk a.Ranked()
// without a side map.
func (p *Picker[T]) Item(key string) (item T, index int, ok bool) {
	i, ok := p.index[key]
	if !ok {
		return item, -1, false
	}
	return p.items[i], i, true
}
