//go:build live

// The recall measurement calls the real API. Run with:
//
//	TYPESAFE_API_KEY=... go test -tags live -run Live -v ./patterns/compact
package compact_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/patterns/compact"
)

type fixture struct {
	Source   string `json:"source"`
	Focus    string `json:"focus"`
	Segments []struct {
		Text   string `json:"text"`
		Needed bool   `json:"needed"`
	} `json:"segments"`
}

// TestLiveRecall logs, for each labeled fixture and budget, the share of
// needed segments kept by scored selection and by keeping the most recent
// segments through the same Select. Sizes are byte lengths. It asserts only
// that the calls succeed; the numbers are for the PR, not a gate.
func TestLiveRecall(t *testing.T) {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		t.Skip("TYPESAFE_API_KEY is not set")
	}
	client, err := decide.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob("testdata/*.json")
	for _, file := range files {
		var f fixture
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &f); err != nil {
			t.Fatal(err)
		}
		segs := make([]compact.Segment, len(f.Segments))
		recency := make([]compact.Scores, len(f.Segments))
		total := 0
		for i, s := range f.Segments {
			segs[i] = compact.Segment{Text: s.Text, Size: len(s.Text)}
			recency[i] = compact.Scores{Needed: float64(i+1) / float64(len(segs))}
			total += len(s.Text)
		}
		for _, share := range []float64{0.4, 0.6} {
			rule := compact.Rule{Budget: int(share * float64(total))}
			res, err := compact.Compact(context.Background(), client, segs,
				compact.Config{Focus: f.Focus, Rule: rule, Workers: 2})
			if err != nil {
				t.Fatal(err)
			}
			base, err := compact.Select(segs, recency, rule)
			if err != nil {
				t.Fatal(err)
			}
			scored, wasted := recall(f, res.Decisions)
			recent, recentWasted := recall(f, base)
			t.Logf("%s budget %.0f%%: scored recall %s (%d unneeded kept), recency recall %s (%d unneeded kept), model %v",
				filepath.Base(file), share*100, scored, wasted, recent, recentWasted, res.Models)
			if share == 0.6 {
				for _, d := range res.Decisions {
					t.Logf("  %2d needed=%-5v p=%.2f %s", d.Index, f.Segments[d.Index].Needed, d.Scores.Needed, d.Action)
				}
			}
		}
	}
}

func recall(f fixture, ds []compact.Decision) (string, int) {
	kept, needed, wasted := 0, 0, 0
	for _, d := range ds {
		n := f.Segments[d.Index].Needed
		if n {
			needed++
		}
		if d.Action != compact.Drop {
			if n {
				kept++
			} else {
				wasted++
			}
		}
	}
	return fmt.Sprintf("%d/%d", kept, needed), wasted
}
