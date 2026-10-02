package heads

import (
	"errors"
	"maps"
	"reflect"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/patterns/pick"
)

func testPlan(t *testing.T) *Plan {
	t.Helper()
	p, err := New(
		decide.Choice("Which route?",
			decide.Option("bug"), decide.Option("billing")),
		Branch{Option: "bug", Questions: map[string]decide.Question{
			"severity": decide.Score("If this is a bug, how severe?", "low", "high"),
		}},
		Branch{Option: "billing", Questions: map[string]decide.Question{
			"refund": decide.Noul("If this is billing, is a refund requested?"),
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNewRejectsBadBranches(t *testing.T) {
	selector := decide.Choice("route", decide.Option("a"), decide.Option("b"))
	cases := []struct {
		name     string
		selector *decide.ChoiceQuestion
		branches []Branch
	}{
		{"nil selector", nil, nil},
		{"missing branch", selector, []Branch{{Option: "a"}}},
		{"extra branch", selector, []Branch{{Option: "a"}, {Option: "b"}, {Option: "c"}}},
		{"duplicate branch", selector, []Branch{{Option: "a"}, {Option: "a"}, {Option: "b"}}},
		{"empty local", selector, []Branch{{Option: "a", Questions: map[string]decide.Question{"": decide.Noul("x")}}, {Option: "b"}}},
		{"nil question", selector, []Branch{{Option: "a", Questions: map[string]decide.Question{"x": nil}}, {Option: "b"}}},
		{"duplicate option", decide.Choice("route", decide.Option("a"), decide.Option("a")), []Branch{{Option: "a"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.selector, tc.branches...)
			if !errors.Is(err, decide.ErrInvalidRequest) {
				t.Fatalf("got %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestAttachAtomicCollision(t *testing.T) {
	p := testPlan(t)
	first := decide.NewRequest("ticket")
	b, err := p.Attach(first, "route")
	if err != nil {
		t.Fatal(err)
	}
	lateKey, ok := b.Key("billing", "refund")
	if !ok {
		t.Fatal("missing key")
	}
	req := decide.NewRequest("ticket")
	req.Questions["common"] = decide.Noul("common")
	req.Questions[lateKey] = decide.Noul("occupied")
	before := maps.Clone(req.Questions)
	if _, err := p.Attach(req, "route"); err == nil {
		t.Fatal("expected collision")
	}
	if !reflect.DeepEqual(req.Questions, before) {
		t.Fatal("Attach mutated request on error")
	}
	if _, err := p.Attach(req, "common"); err == nil {
		t.Fatal("expected selector collision")
	}
	if !reflect.DeepEqual(req.Questions, before) {
		t.Fatal("selector collision mutated request")
	}
}

func TestKeysDeterministicAndUnambiguous(t *testing.T) {
	selector := decide.Choice("route", decide.Option("a.b"), decide.Option("a"))
	p, err := New(selector,
		Branch{Option: "a.b", Questions: map[string]decide.Question{"c": decide.Noul("x")}},
		Branch{Option: "a", Questions: map[string]decide.Question{"b.c": decide.Noul("y")}},
	)
	if err != nil {
		t.Fatal(err)
	}
	a, err := p.Attach(decide.NewRequest("state"), "r")
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.Attach(decide.NewRequest("state"), "r")
	if err != nil {
		t.Fatal(err)
	}
	ak, _ := a.Key("a.b", "c")
	bk, _ := a.Key("a", "b.c")
	if ak == bk {
		t.Fatalf("ambiguous keys: %q", ak)
	}
	ak2, _ := b.Key("a.b", "c")
	if ak != ak2 {
		t.Fatalf("unstable key: %q vs %q", ak, ak2)
	}
}

func TestPickSelectorIncludesAbstainBranch(t *testing.T) {
	picker, err := pick.New([]string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(picker.Question("Which item?"),
		Branch{Option: "a"}, Branch{Option: "b"},
		Branch{Option: picker.AbstainKey()},
	)
	if err != nil {
		t.Fatalf("pick selector with abstain branch: %v", err)
	}
}
