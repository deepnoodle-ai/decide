package pick

import "fmt"

// Defaults. The Choice cap includes the abstain option.
const (
	defaultAbstainKey         = "none"
	defaultAbstainDescription = "None of these is the requested value."
	defaultMaxOptions         = 255
	defaultMaxKeyLen          = 64
)

// Option configures New. It is not generic, so Abstain and MaxOptions need
// no type argument at the call site. The last occurrence of an option wins.
type Option interface{ apply(*settings) }

type settings struct {
	describe    any // func(T) any; New checks the type
	keyFunc     any // func(int, T) string; New checks the type
	abstainKey  string
	abstainDesc any
	maxOptions  int
	maxKeyLen   int
}

type optionFunc func(*settings)

func (f optionFunc) apply(s *settings) { f(s) }

// Describe sets each candidate's option description, a JSON value (string,
// object, array, or nil for null). The model sees it next to the key.
//
// T is inferred from f and must match the Picker's item type, or New
// returns an error wrapping ErrInvalidOption.
func Describe[T any](f func(T) any) Option {
	return optionFunc(func(s *settings) { s.describe = f })
}

// KeyFunc sets each candidate's key from its index and value. Keys are used
// verbatim, except that a key already taken gets a " (2)", " (3)", ...
// suffix. An empty or invalid UTF-8 key makes New return a *KeyError.
//
// T is inferred from f and must match the Picker's item type, or New
// returns an error wrapping ErrInvalidOption.
func KeyFunc[T any](f func(int, T) string) Option {
	return optionFunc(func(s *settings) { s.keyFunc = f })
}

// Abstain overrides the abstain key and description. Defaults: "none",
// "None of these is the requested value." The key must be non-empty valid
// UTF-8; it is never renamed, and a candidate that collides with it is.
func Abstain(key string, description any) Option {
	return optionFunc(func(s *settings) { s.abstainKey, s.abstainDesc = key, description })
}

// MaxOptions sets the option cap, abstain included. Default 255, Jev 1.13's
// documented limit. n must be >= 2.
func MaxOptions(n int) Option {
	return optionFunc(func(s *settings) { s.maxOptions = n })
}

// MaxKeyLen sets the longest key derived from an item's text, in runes.
// Longer text gets a positional key instead of a truncated one. Default 64.
// n must be >= 8.
func MaxKeyLen(n int) Option {
	return optionFunc(func(s *settings) { s.maxKeyLen = n })
}

func newSettings(opts []Option) settings {
	s := settings{
		abstainKey:  defaultAbstainKey,
		abstainDesc: defaultAbstainDescription,
		maxOptions:  defaultMaxOptions,
		maxKeyLen:   defaultMaxKeyLen,
	}
	for _, o := range opts {
		if o != nil {
			o.apply(&s)
		}
	}
	return s
}

// funcs checks the settings and asserts the generic options to T.
func funcs[T any](s *settings) (describe func(T) any, keyFunc func(int, T) string, err error) {
	if s.maxOptions < 2 {
		return nil, nil, fmt.Errorf("%w: MaxOptions(%d): must be >= 2", ErrInvalidOption, s.maxOptions)
	}
	if s.maxKeyLen < 8 {
		return nil, nil, fmt.Errorf("%w: MaxKeyLen(%d): must be >= 8", ErrInvalidOption, s.maxKeyLen)
	}
	if err := checkKey(s.abstainKey); err != "" {
		return nil, nil, fmt.Errorf("%w: abstain key %q: %s", ErrInvalidOption, s.abstainKey, err)
	}
	if s.describe != nil {
		f, ok := s.describe.(func(T) any)
		if !ok {
			return nil, nil, fmt.Errorf("%w: Describe takes %T, want func(%s) any", ErrInvalidOption, s.describe, typeName[T]())
		}
		if f == nil {
			return nil, nil, fmt.Errorf("%w: Describe with nil function", ErrInvalidOption)
		}
		describe = f
	}
	if s.keyFunc != nil {
		f, ok := s.keyFunc.(func(int, T) string)
		if !ok {
			return nil, nil, fmt.Errorf("%w: KeyFunc takes %T, want func(int, %s) string", ErrInvalidOption, s.keyFunc, typeName[T]())
		}
		if f == nil {
			return nil, nil, fmt.Errorf("%w: KeyFunc with nil function", ErrInvalidOption)
		}
		keyFunc = f
	}
	return describe, keyFunc, nil
}

func typeName[T any]() string { return fmt.Sprintf("%T", (*T)(nil))[1:] }
