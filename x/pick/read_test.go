package pick_test

import (
	"errors"
	"math"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/x/pick"
)

func TestRead(t *testing.T) {
	p, err := pick.New([]string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		probs map[string]float64
		// choice defaults to the argmax when empty.
		choice string
		want   pick.Result[string] // Answer is not compared
	}{
		{
			name:  "candidate chosen",
			probs: map[string]float64{"a": 0.1, "b": 0.7, "c": 0.15, "none": 0.05},
			want:  pick.Result[string]{Item: "b", Index: 1, Picked: true, Key: "b", P: 0.7, AbstainP: 0.05, RunnerUp: "c", Margin: 0.7 - 0.15},
		},
		{
			name:  "abstain chosen",
			probs: map[string]float64{"a": 0.3, "b": 0.05, "c": 0.05, "none": 0.6},
			want:  pick.Result[string]{Index: -1, Key: "none", P: 0.6, AbstainP: 0.6, RunnerUp: "a", Margin: 0.6 - 0.3},
		},
		{
			name:  "runner-up is abstain",
			probs: map[string]float64{"a": 0.5, "b": 0.05, "c": 0.05, "none": 0.4},
			want:  pick.Result[string]{Item: "a", Index: 0, Picked: true, Key: "a", P: 0.5, AbstainP: 0.4, RunnerUp: "none", Margin: 0.5 - 0.4},
		},
		{
			name:  "runner-up tie broken by key",
			probs: map[string]float64{"a": 0.5, "c": 0.2, "b": 0.2, "none": 0.1},
			want:  pick.Result[string]{Item: "a", Index: 0, Picked: true, Key: "a", P: 0.5, AbstainP: 0.1, RunnerUp: "b", Margin: 0.5 - 0.2},
		},
		{
			name:  "only the chosen key present",
			probs: map[string]float64{"c": 1},
			want:  pick.Result[string]{Item: "c", Index: 2, Picked: true, Key: "c", P: 1, AbstainP: 0, RunnerUp: "", Margin: 1},
		},
		{
			name:  "P and AbstainP exactly as received",
			probs: map[string]float64{"a": 0.123456789012345678, "b": 0.0000001, "c": 0.0, "none": 0.876543110987654},
			want:  pick.Result[string]{Index: -1, Key: "none", P: 0.876543110987654, AbstainP: 0.876543110987654, RunnerUp: "a", Margin: 0.876543110987654 - 0.123456789012345678},
		},
		{
			// The client allows argmax ties within 1e-6. Ranked() puts "a"
			// first, so Margin from the chosen "b" is slightly negative.
			name:   "tie within 1e-6 with another key ranked first",
			probs:  map[string]float64{"a": 0.5, "b": 0.4999995, "c": 0.0000005, "none": 0},
			choice: "b",
			want:   pick.Result[string]{Item: "b", Index: 1, Picked: true, Key: "b", P: 0.4999995, AbstainP: 0, RunnerUp: "a", Margin: 0.4999995 - 0.5},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := answer(tt.probs, tt.choice)
			r, err := p.Read(a)
			if err != nil {
				t.Fatal(err)
			}
			if r.Answer != a {
				t.Error("Answer is not the answer passed in")
			}
			got := r
			got.Answer, got.Margin = nil, 0
			want := tt.want
			want.Margin = 0
			if got != want {
				t.Errorf("got  %+v\nwant %+v", got, want)
			}
			if math.Abs(r.Margin-tt.want.Margin) > 1e-12 {
				t.Errorf("Margin = %v, want %v", r.Margin, tt.want.Margin)
			}
		})
	}
}

func TestReadMarginAgreesOnStrictArgmax(t *testing.T) {
	p, err := pick.New([]string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	for _, probs := range []map[string]float64{
		{"a": 0.6, "b": 0.3, "c": 0.05, "none": 0.05},
		{"a": 0.01, "b": 0.01, "c": 0.01, "none": 0.97},
		{"a": 0.26, "b": 0.25, "c": 0.25, "none": 0.24},
	} {
		a := answer(probs, "")
		r, err := p.Read(a)
		if err != nil {
			t.Fatal(err)
		}
		if r.Margin != a.Margin() {
			t.Errorf("%v: Margin = %v, a.Margin() = %v", probs, r.Margin, a.Margin())
		}
		if r.Answer.Confidence != a.Confidence {
			t.Error("confidence did not pass through")
		}
	}
}

func TestReadErrors(t *testing.T) {
	p, err := pick.New([]string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("unmapped choice", func(t *testing.T) {
		_, err := p.Read(&decide.ChoiceAnswer{Choice: "zzz", Probabilities: map[string]float64{"zzz": 1}})
		ue, ok := errors.AsType[*pick.UnmappedChoiceError](err)
		if !ok || !errors.Is(err, pick.ErrUnmappedChoice) || ue.Choice != "zzz" {
			t.Fatalf("err = %v, want *UnmappedChoiceError for zzz", err)
		}
	})
	t.Run("empty choice is unmapped, not abstain", func(t *testing.T) {
		if _, err := p.Read(&decide.ChoiceAnswer{}); !errors.Is(err, pick.ErrUnmappedChoice) {
			t.Fatalf("err = %v, want ErrUnmappedChoice", err)
		}
	})
	t.Run("nil answer", func(t *testing.T) {
		r, err := p.Read(nil)
		if !errors.Is(err, decide.ErrInvalidAnswer) || r.Picked {
			t.Fatalf("Read(nil) = %+v, %v", r, err)
		}
	})
}

func TestItem(t *testing.T) {
	p, err := pick.New([]string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		key       string
		wantItem  string
		wantIndex int
		wantOK    bool
	}{
		{"b", "b", 1, true},
		{"none", "", -1, false},
		{"unknown", "", -1, false},
		{"", "", -1, false},
	}
	for _, tt := range tests {
		item, index, ok := p.Item(tt.key)
		if item != tt.wantItem || index != tt.wantIndex || ok != tt.wantOK {
			t.Errorf("Item(%q) = %q, %d, %v; want %q, %d, %v", tt.key, item, index, ok, tt.wantItem, tt.wantIndex, tt.wantOK)
		}
	}
}

// answer builds a choice answer. An empty choice means the argmax.
func answer(probs map[string]float64, choice string) *decide.ChoiceAnswer {
	a := &decide.ChoiceAnswer{Probabilities: probs, Confidence: 0.42}
	if choice != "" {
		a.Choice = choice
		return a
	}
	a.Choice = a.Ranked()[0].Key
	return a
}
