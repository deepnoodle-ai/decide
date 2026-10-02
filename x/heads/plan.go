package heads

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"

	"github.com/deepnoodle-ai/decide"
)

// Branch holds the questions to ask if Option is selected. Questions may
// be empty. Each local question name must be non-empty.
type Branch struct {
	Option    string
	Questions map[string]decide.Question
}

// Plan holds a Choice selector and exactly one branch for each option.
// It is safe for concurrent use if callers do not mutate question objects
// or JSON values passed to New.
type Plan struct {
	selector *decide.ChoiceQuestion
	branches map[string]map[string]decide.Question
}

// New checks selector and branch coverage before returning an immutable
// plan. It copies maps and the selector's option slice, not question objects
// or nested JSON values.
func New(selector *decide.ChoiceQuestion, branches ...Branch) (*Plan, error) {
	if selector == nil {
		return nil, fmt.Errorf("heads: nil selector: %w", decide.ErrInvalidRequest)
	}
	check := decide.NewRequest("preflight")
	check.Questions["selector"] = selector
	if err := check.Validate(); err != nil {
		return nil, fmt.Errorf("heads: selector: %w", err)
	}
	options := make(map[string]bool, len(selector.Criteria))
	for _, option := range selector.Criteria {
		options[option.Key] = true
	}
	byOption := make(map[string]map[string]decide.Question, len(branches))
	for _, branch := range branches {
		if !options[branch.Option] {
			return nil, fmt.Errorf("heads: unknown branch %q: %w", branch.Option, decide.ErrInvalidRequest)
		}
		if _, exists := byOption[branch.Option]; exists {
			return nil, fmt.Errorf("heads: duplicate branch %q: %w", branch.Option, decide.ErrInvalidRequest)
		}
		questions := maps.Clone(branch.Questions)
		for local, question := range questions {
			if local == "" || nilValue(question) {
				return nil, fmt.Errorf("heads: branch %q has empty name or nil question: %w", branch.Option, decide.ErrInvalidRequest)
			}
		}
		byOption[branch.Option] = questions
	}
	for option := range options {
		if _, exists := byOption[option]; !exists {
			return nil, fmt.Errorf("heads: missing branch %q: %w", option, decide.ErrInvalidRequest)
		}
	}
	copySelector := *selector
	copySelector.Criteria = slices.Clone(selector.Criteria)
	copySelector.Extra = maps.Clone(selector.Extra)
	return &Plan{selector: &copySelector, branches: byOption}, nil
}

// Binding records the keys and questions attached to a request. It is
// safe for concurrent Read calls if its question objects remain unchanged.
type Binding struct {
	selectorKey string
	byBranch    map[string]map[string]string // branch -> local -> wire key
	owned       map[string]ownedQuestion
	request     *decide.Request
}

type ownedQuestion struct {
	branch string
	local  string
	q      decide.Question
}

// Attach adds a plan to req. It preflights every key; an error leaves req
// unchanged. key is the selector's wire key and names the plan's namespace.
func (p *Plan) Attach(req *decide.Request, key string) (*Binding, error) {
	if p == nil || req == nil || req.Questions == nil || key == "" {
		return nil, fmt.Errorf("heads: nil plan/request/questions or empty key: %w", decide.ErrInvalidRequest)
	}
	owned := map[string]ownedQuestion{
		key: {local: "$selector", q: p.selector},
	}
	byBranch := make(map[string]map[string]string, len(p.branches))
	for _, option := range slices.Sorted(maps.Keys(p.branches)) {
		byBranch[option] = make(map[string]string, len(p.branches[option]))
		for _, local := range slices.Sorted(maps.Keys(p.branches[option])) {
			wireKey := questionKey(key, option, local)
			if _, exists := owned[wireKey]; exists {
				return nil, fmt.Errorf("heads: generated key collision %q: %w", wireKey, decide.ErrInvalidRequest)
			}
			owned[wireKey] = ownedQuestion{branch: option, local: local, q: p.branches[option][local]}
			byBranch[option][local] = wireKey
		}
	}
	for wireKey := range owned {
		if _, exists := req.Questions[wireKey]; exists {
			return nil, fmt.Errorf("heads: question key %q already exists: %w", wireKey, decide.ErrInvalidRequest)
		}
	}
	for wireKey, entry := range owned {
		req.Questions[wireKey] = entry.q
	}
	return &Binding{
		selectorKey: key,
		byBranch:    byBranch,
		owned:       owned,
		request:     req,
	}, nil
}

func questionKey(selectorKey, option, local string) string {
	return selectorKey + "." + strconv.Itoa(len(option)) + ":" + option + "." + local
}

// SelectorKey returns the selector's wire key.
func (b *Binding) SelectorKey() string { return b.selectorKey }

// Key returns a branch question's wire key. Most callers need only its
// local name; this is useful for test fixtures.
func (b *Binding) Key(branch, local string) (string, bool) {
	locals, ok := b.byBranch[branch]
	if !ok {
		return "", false
	}
	key, ok := locals[local]
	return key, ok
}

func nilValue(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}
