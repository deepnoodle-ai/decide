package gate_test

import (
	"testing"

	"github.com/deepnoodle-ai/decide/patterns/gate"
)

func TestOutcome(t *testing.T) {
	for _, tc := range []struct {
		o    gate.Outcome
		name string
	}{{gate.Allow, "allow"}, {gate.Review, "review"}, {gate.Escalate, "escalate"}} {
		if tc.o.String() != tc.name {
			t.Errorf("%d: %q", tc.o, tc.o.String())
		}
		b, err := tc.o.MarshalText()
		if err != nil || string(b) != tc.name {
			t.Errorf("MarshalText %d: %q %v", tc.o, b, err)
		}
		var back gate.Outcome
		if err := back.UnmarshalText(b); err != nil || back != tc.o {
			t.Errorf("UnmarshalText %q: %v %v", b, back, err)
		}
	}
	if s := gate.Outcome(0).String(); s != "Outcome(0)" {
		t.Errorf("zero: %q", s)
	}
	for _, o := range []gate.Outcome{0, 4, 255} {
		if _, err := o.MarshalText(); err == nil {
			t.Errorf("MarshalText(%d) succeeded", o)
		}
	}
	for _, s := range []string{"", "Allow", "ALLOW", " allow", "alow", "0", "1"} {
		var o gate.Outcome
		if err := o.UnmarshalText([]byte(s)); err == nil {
			t.Errorf("UnmarshalText(%q) succeeded", s)
		}
	}
}

func TestWorse(t *testing.T) {
	for _, tc := range []struct{ a, b, want gate.Outcome }{
		{gate.Allow, gate.Allow, gate.Allow},
		{gate.Allow, gate.Review, gate.Review},
		{gate.Escalate, gate.Review, gate.Escalate},
		{0, gate.Allow, gate.Escalate},
		{gate.Allow, 0, gate.Escalate},
		{0, 0, gate.Escalate},
		{9, gate.Allow, gate.Escalate},
	} {
		if got := gate.Worse(tc.a, tc.b); got != tc.want {
			t.Errorf("Worse(%d, %d) = %s, want %s", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestKindString(t *testing.T) {
	want := []string{"missing", "failed", "abstained", "noul", "choice", "score"}
	for i, w := range want {
		if s := gate.Kind(i).String(); s != w {
			t.Errorf("Kind(%d) = %q", i, s)
		}
	}
	if s := gate.Kind(42).String(); s != "Kind(42)" {
		t.Errorf("%q", s)
	}
}

func TestInputs(t *testing.T) {
	in := gate.NewInputs(noul("b", 0.1), noul("a", 0.2))
	if got := in.Get("zzz"); got.Kind != gate.KindMissing || got.Name != "zzz" {
		t.Errorf("absent: %+v", got)
	}
	var names []string
	for k, v := range in.All() {
		if k != v.Name {
			t.Errorf("key %q name %q", k, v.Name)
		}
		names = append(names, k)
	}
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Errorf("All order: %v", names)
	}
	for range in.All() {
		break // early stop must not panic
	}
	var zero gate.Inputs
	if zero.Get("x").Kind != gate.KindMissing {
		t.Error("zero Inputs")
	}
	for _, bad := range [][]gate.Input{{noul("", 0.5)}, {noul("a", 0.1), noul("a", 0.2)}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("NewInputs(%v) did not panic", bad)
				}
			}()
			gate.NewInputs(bad...)
		}()
	}
}
