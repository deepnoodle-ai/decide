package jobs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/deepnoodle-ai/decide/internal/catalog"
)

func patternFile(t *testing.T, p catalog.Pattern) string {
	t.Helper()
	p.Version = 1
	p.Description = "Test pattern"
	if p.Name == "" {
		p.Name = "test"
	}
	b, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "pattern.json")
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	return path
}

func TestHeadsSelectedBranchAndEvidence(t *testing.T) {
	o := testOptions(t)
	o.Skill = ""
	o.SkillDefinition = nil
	o.Pattern = "builtin/heads"
	o.NewClient = serverClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&req)
		var qs map[string]json.RawMessage
		json.Unmarshal(req["questions"], &qs)
		answers := map[string]any{}
		for key := range qs {
			if key == "selector" {
				answers[key] = map[string]any{"type": "choice", "choice": "prose", "probabilities": map[string]float64{"prose": 0.8, "code": 0.2}, "confidence": 0.8}
			} else if strings.Contains(key, "prose") {
				answers[key] = map[string]any{"type": "noul", "noul": 0.9}
			} else {
				answers[key] = map[string]any{"type": "noul", "noul": 0.5}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"model": "fake", "answers": answers})
	})
	s, e := Run(context.Background(), o, strings.NewReader("\"hello\"\n"), nil)
	if e != nil || s.Completed != 1 {
		t.Fatalf("%+v %v", s, e)
	}
	ReadResults(s.ID, o.RunDir, func(r Result) error {
		if !strings.Contains(string(r.Stages[0].Result), `"branch":"prose"`) {
			t.Errorf("%+v", r)
		}
		return nil
	})
}

func TestFunnelDropsAndCarriesEvidence(t *testing.T) {
	o := testOptions(t)
	o.Skill = ""
	o.SkillDefinition = nil
	o.Pattern = "builtin/funnel"
	var calls atomic.Int32
	o.NewClient = serverClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req struct {
			State     json.RawMessage
			Questions map[string]json.RawMessage
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Questions["relevant"] != nil {
			score := 0.9
			if strings.Contains(string(req.State), "skip") {
				score = 0.1
			}
			json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"relevant": map[string]any{"type": "noul", "noul": score}}})
			return
		}
		if !strings.Contains(string(req.State), `"stages"`) {
			t.Error("missing prior stage state")
		}
		io.WriteString(w, `{"answers":{"risk":{"type":"noul","noul":0.1},"maintainability":{"type":"score","score":2,"legend":{"0":"a","1":"b","2":"c","3":"d","4":"e"},"probabilities":{"0":0,"1":0,"2":1,"3":0,"4":0},"confidence":1}}}`)
	})
	s, e := Run(context.Background(), o, strings.NewReader("\"skip\"\n\"keep\"\n"), nil)
	if e != nil || s.Dropped != 1 || s.Completed != 1 || calls.Load() != 3 {
		t.Fatalf("%+v %v calls %d", s, e, calls.Load())
	}
	s, e = Resume(context.Background(), s.ID, o, false, false, nil)
	if e != nil || calls.Load() != 3 {
		t.Fatalf("%+v %v calls %d", s, e, calls.Load())
	}
}

func TestLimitedFunnelRanksBeforeNextStage(t *testing.T) {
	o := testOptions(t)
	o.Skill = ""
	o.SkillDefinition = nil
	o.Pattern = patternFile(t, catalog.Pattern{Type: "funnel", Stages: []catalog.Stage{{Name: "screen", Skill: "builtin/relevance", Answer: "relevant", Limit: 1}, {Name: "review", Skill: "builtin/relevance"}}})
	var calls atomic.Int32
	o.NewClient = serverClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req struct{ State json.RawMessage }
		json.NewDecoder(r.Body).Decode(&req)
		score := 0.1
		if strings.Contains(string(req.State), "winner") {
			score = 0.9
		}
		json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"relevant": map[string]any{"type": "noul", "noul": score}}})
	})
	var output []Result
	s, e := Run(context.Background(), o, strings.NewReader("\"loser\"\n\"winner\"\n"), func(r Result) error { output = append(output, r); return nil })
	if e != nil || s.Dropped != 1 || s.Completed != 1 || calls.Load() != 3 {
		t.Fatalf("%+v %v calls %d", s, e, calls.Load())
	}
	for _, r := range output {
		if r.Status == "staged" {
			t.Fatal("intermediate outcome emitted")
		}
		if r.Status == "complete" && len(r.Stages) != 2 {
			t.Fatalf("%+v", r)
		}
	}
}

func TestSavedRankAndPackAndGate(t *testing.T) {
	o := testOptions(t)
	o.Skill = "builtin/relevance"
	o.SkillDefinition = nil
	o.NewClient = serverClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct{ State string }
		json.NewDecoder(r.Body).Decode(&req)
		score := 0.2
		if req.State == "best" {
			score = 0.9
		}
		json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"relevant": map[string]any{"type": "noul", "noul": score}}})
	})
	base, e := Run(context.Background(), o, strings.NewReader("\"bad\"\n\"best\"\n"), nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"rank", "rank-pack", "gate"} {
		t.Run(kind, func(t *testing.T) {
			p := catalog.Pattern{Type: kind, Run: base.Path, Answer: "relevant", BudgetBytes: 4, MaxRecords: 10}
			if kind == "gate" {
				p.Policy = json.RawMessage(`{"type":"bands","input":"relevant","measure":"noul","polarity":"high_is_risky","allow":0.3,"review":0.7}`)
			}
			o.Pattern = patternFile(t, p)
			o.NewClient = nil
			var results []Result
			s, e := Run(context.Background(), o, nil, func(r Result) error { results = append(results, r); return nil })
			if e != nil || s.Items != 2 || s.Requests != 0 {
				t.Fatalf("%+v %v", s, e)
			}
			if kind == "rank" || kind == "rank-pack" {
				if string(results[0].Data) != `"best"` {
					t.Fatal(results)
				}
			}
			if kind == "rank-pack" && s.Dropped != 1 {
				t.Fatalf("%+v", s)
			}
			if kind == "gate" && s.Dropped != 1 {
				t.Fatalf("%+v", s)
			}
		})
	}
}

func TestRankThenPackReadsUnderlyingModelEvidence(t *testing.T) {
	o := testOptions(t)
	o.Skill = "builtin/relevance"
	o.SkillDefinition = nil
	o.NewClient = serverClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"answers":{"relevant":{"type":"noul","noul":0.9}}}`)
	})
	base, e := Run(context.Background(), o, strings.NewReader("\"first\"\n\"second\"\n"), nil)
	if e != nil {
		t.Fatal(e)
	}
	o.Pattern = patternFile(t, catalog.Pattern{Type: "rank", Run: base.Path, Answer: "relevant"})
	ranked, e := Run(context.Background(), o, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	o.Pattern = patternFile(t, catalog.Pattern{Type: "rank-pack", Run: ranked.Path, Answer: "relevant", BudgetBytes: 6})
	packed, e := Run(context.Background(), o, nil, nil)
	if e != nil || packed.Completed != 1 || packed.Dropped != 1 || packed.Requests != 0 {
		t.Fatalf("%+v %v", packed, e)
	}
}

func TestCollectionAndFunnelByteBounds(t *testing.T) {
	o := testOptions(t)
	o.Skill = "builtin/relevance"
	o.SkillDefinition = nil
	o.NewClient = serverClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"answers":{"relevant":{"type":"noul","noul":0.9}}}`)
	})
	base, e := Run(context.Background(), o, strings.NewReader("\"first\"\n\"second\"\n"), nil)
	if e != nil {
		t.Fatal(e)
	}
	o.MaxCollectionBytes = 1
	o.Pattern = patternFile(t, catalog.Pattern{Type: "rank", Run: base.Path, Answer: "relevant"})
	s, e := Run(context.Background(), o, nil, nil)
	if e == nil || !strings.Contains(e.Error(), "max-collection-bytes") || s.Requests != 0 {
		t.Fatalf("%+v %v", s, e)
	}
	o.Pattern = patternFile(t, catalog.Pattern{Type: "funnel", Stages: []catalog.Stage{{Name: "screen", Skill: "builtin/relevance", Answer: "relevant", Limit: 1}}})
	s, e = Run(context.Background(), o, strings.NewReader("\"first\"\n"), nil)
	if e == nil || !strings.Contains(e.Error(), "max-collection-bytes") {
		t.Fatalf("%+v %v", s, e)
	}
}
