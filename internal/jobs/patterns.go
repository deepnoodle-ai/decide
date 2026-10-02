package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/internal/catalog"
	"github.com/deepnoodle-ai/decide/patterns/compact"
	"github.com/deepnoodle-ai/decide/patterns/gate"
	"github.com/deepnoodle-ai/decide/patterns/heads"
	"github.com/deepnoodle-ai/decide/patterns/rank"
)

func collection(d definition) bool {
	return d.Pattern.Type == "gate" || d.Pattern.Type == "rank" || d.Pattern.Type == "rank-pack"
}
func headsRequest(state json.RawMessage, d definition) (*decide.Request, *heads.Binding, error) {
	q, err := decide.DecodeQuestion(d.Pattern.Selector)
	if err != nil {
		return nil, nil, err
	}
	sel, ok := q.(*decide.ChoiceQuestion)
	if !ok {
		return nil, nil, errors.New("heads selector must be Choice")
	}
	branches := []heads.Branch{}
	keys := []string{}
	for key := range d.Pattern.Branches {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		qs, e := catalog.Questions(d.Skills[d.Pattern.Branches[key]])
		if e != nil {
			return nil, nil, e
		}
		branches = append(branches, heads.Branch{Option: key, Questions: qs})
	}
	p, e := heads.New(sel, branches...)
	if e != nil {
		return nil, nil, e
	}
	req := decide.NewRequest(state, decide.WithRequestModel(d.Options.Model))
	b, e := p.Attach(req, "selector")
	return req, b, e
}
func (e *executor) process(ctx context.Context, i int, rec input, old Result) Result {
	r := Result{Version: 1, Assets: rec.Assets, ID: rec.Prepared.Item.ID, Source: rec.Prepared.Item.Source, Data: rec.Prepared.Item.Data, Index: i, Status: "complete"}
	if e.singleStage {
		r.Stages = append([]Evidence(nil), old.Stages...)
	}
	if e.d.Pattern.Type == "heads" {
		req, b, err := headsRequest(rec.Prepared.State, e.d)
		if err == nil {
			err = attachAssets(req, e.s.Path, rec.Assets)
		}
		if err != nil {
			r.Status = "failed"
			r.Error = safeError(err).Error()
			return r
		}
		var resp *decide.Response
		var ev Evidence
		status := "complete"
		recovered := false
		for _, saved := range old.Stages {
			if saved.Name == "heads" && len(saved.Response) > 0 && saved.Error == "" {
				ev = saved
				resp, err = decide.DecodeResponse(saved.Response, req)
				recovered = true
				break
			}
		}
		if !recovered {
			resp, ev, status, err = e.call(ctx, req, i, "heads", r)
		}
		if resp != nil && (err == nil || errors.Is(err, decide.ErrInvalidAnswer)) {
			out, re := b.Read(resp)
			if re == nil {
				ev.Result = mustJSON(map[string]any{"branch": out.Branch, "selector": out.Selector, "answers": out.Answers})
				err = nil
				ev.Error = ""
				status = "complete"
			} else {
				err = re
				status = "failed"
			}
		}
		r.Stages = append(r.Stages, ev)
		r.Status = status
		if err != nil {
			r.Error = safeError(err).Error()
		}
		return r
	}
	stages := []catalog.Stage{{Name: "map", Skill: e.d.Options.Skill, Model: e.d.Options.Model}}
	if e.d.Pattern.Type == "funnel" {
		stages = e.d.Pattern.Stages
	}
	for _, st := range stages {
		skill := e.d.Skill
		if e.d.Pattern.Type == "funnel" {
			skill = e.d.Skills[st.Skill]
		}
		var prior *Evidence
		for k := range old.Stages {
			if old.Stages[k].Name == st.Name && len(old.Stages[k].Response) > 0 && old.Stages[k].Error == "" {
				prior = &old.Stages[k]
				break
			}
		}
		var resp *decide.Response
		var ev Evidence
		var err error
		status := "complete"
		state := stateFor(rec.Prepared.Item, skill)
		if len(r.Stages) > 0 {
			state = mustJSON(map[string]any{"input": json.RawMessage(state), "stages": r.Stages})
		}
		qs, err := catalog.Questions(skill)
		if err != nil {
			r.Status = "failed"
			r.Error = safeError(err).Error()
			return r
		}
		model := st.Model
		if model == "" {
			model = e.d.Options.Model
		}
		req := &decide.Request{State: state, Questions: qs, Model: model}
		if prior != nil {
			ev = *prior
			resp, err = decide.DecodeResponse(ev.Response, req)
			if err == nil {
				err = validateSaved(req, resp)
			}
		} else {
			if err = attachAssets(req, e.s.Path, rec.Assets); err == nil {
				resp, ev, status, err = e.call(ctx, req, i, st.Name, r)
			}
		}
		if err != nil {
			if status == "pending" {
				r.Status = "pending"
			} else {
				r.Status = status
				if r.Status == "complete" {
					r.Status = "failed"
				}
			}
			r.Error = safeError(err).Error()
			if status != "pending" {
				r.Stages = append(r.Stages, ev)
			}
			return r
		}
		ev.Error = ""
		r.Stages = append(r.Stages, ev)
		policy := st.Policy
		if len(policy) == 0 {
			policy = skill.Policy
		}
		if len(policy) > 0 {
			rule, err := gate.DecodeRule(policy)
			if err != nil {
				r.Status = "failed"
				r.Error = safeError(err).Error()
				return r
			}
			decision := rule.Evaluate(gate.FromResponse(resp))
			r.Stages[len(r.Stages)-1].Result = mustJSON(decision)
			if decision.Outcome != gate.Allow {
				r.Status = "dropped"
				return r
			}
		}
		if st.Threshold != nil {
			obs, err := rank.FromResponse(resp, st.Answer)
			if err != nil {
				r.Status = "failed"
				r.Error = safeError(err).Error()
				return r
			}
			if obs.Noul < *st.Threshold {
				r.Status = "dropped"
				r.Stages[len(r.Stages)-1].Result = mustJSON(map[string]any{"kept": false, "threshold": *st.Threshold})
				return r
			}
		}
	}
	return r
}
func validateSaved(req *decide.Request, resp *decide.Response) error {
	for key, q := range req.Questions {
		a := resp.Answers[key]
		if a == nil {
			return fmt.Errorf("saved answer %q is missing", key)
		}
		v, ok := q.(interface{ ValidateAnswer(decide.Answer) error })
		if !ok {
			return fmt.Errorf("saved question %q cannot validate answers", key)
		}
		if err := v.ValidateAnswer(a); err != nil {
			return err
		}
	}
	for key := range resp.Answers {
		if req.Questions[key] == nil {
			return fmt.Errorf("unexpected saved answer %q", key)
		}
	}
	return nil
}
func savedResponse(r Result) (*decide.Response, error) {
	if r.Status != "complete" {
		return nil, errors.New("source item does not have successful evidence")
	}
	// Local transformations append result-only stages. Read the newest model
	// evidence without mistaking its derived ranking or policy for an answer.
	for i := len(r.Stages) - 1; i >= 0; i-- {
		ev := r.Stages[i]
		if len(ev.Response) == 0 || len(ev.Questions) == 0 {
			continue
		}
		if ev.Error != "" {
			return nil, errors.New("latest model evidence contains an error")
		}
		req := &decide.Request{State: ev.State, Questions: map[string]decide.Question{}}
		for key, b := range ev.Questions {
			q, e := decide.DecodeQuestion(b)
			if e != nil {
				return nil, e
			}
			req.Questions[key] = q
		}
		if e := req.Validate(); e != nil {
			return nil, e
		}
		resp, e := decide.DecodeResponse(ev.Response, req)
		if e == nil {
			e = validateSaved(req, resp)
		}
		return resp, e
	}
	return nil, errors.New("source item has no model question/response evidence")
}
func runCollection(ctx context.Context, d definition, s Summary, fn func(Result) error) (Summary, error) {
	p := d.Pattern
	if p.Run == "" {
		if len(d.Options.Sources.Sources) != 1 {
			return s, errors.New("saved-evidence pattern requires one run ID or evidence file operand")
		}
		p.Run = d.Options.Sources.Sources[0]
	}
	max := p.MaxRecords
	if max == 0 {
		max = d.Options.MaxRecords
	}
	items := []Result{}
	var collectionBytes int64
	e := readEvidence(p.Run, d.Options.RunDir, func(r Result) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(items) >= max {
			return fmt.Errorf("collection exceeds %d records; raise max-records explicitly", max)
		}
		size := int64(len(mustJSON(r)))
		if size > d.Options.MaxCollectionBytes-collectionBytes {
			return fmt.Errorf("collection exceeds %d bytes; raise max-collection-bytes explicitly", d.Options.MaxCollectionBytes)
		}
		collectionBytes += size
		items = append(items, r)
		return nil
	})
	if e != nil {
		s.Status = "preparation-incomplete"
		saveSummary(&s)
		return s, e
	}
	s.Items = len(items)
	if p.Type == "gate" {
		rule, e := gate.DecodeRule(p.Policy)
		if e != nil {
			return s, e
		}
		for i := range items {
			r := &items[i]
			r.Index = i
			resp, e := savedResponse(*r)
			if e != nil {
				r.Status = "failed"
				r.Error = safeError(e).Error()
				continue
			}
			out := rule.Evaluate(gate.FromResponse(resp))
			r.Stages = append(r.Stages, Evidence{Name: "gate", Result: mustJSON(out)})
			if out.Outcome != gate.Allow {
				r.Status = "dropped"
			}
		}
	} else {
		obs := make([]rank.Observation, len(items))
		for i := range items {
			resp, e := savedResponse(items[i])
			if e == nil {
				obs[i], e = rank.FromResponse(resp, p.Answer)
			}
			if e != nil {
				s.Status = "preparation-incomplete"
				saveSummary(&s)
				return s, fmt.Errorf("ranking item %s: %w", items[i].ID, safeError(e))
			}
		}
		keys := make([]string, len(items))
		for i := range items {
			keys[i] = items[i].ID
		}
		order, e := rank.Rerank(items, obs, func(i int, r Result) string { return keys[i] })
		if e != nil {
			return s, e
		}
		ordered := make([]Result, len(items))
		for i, entry := range order.Entries {
			ordered[i] = entry.Item
			ordered[i].Index = i
			ordered[i].Stages = append(ordered[i].Stages, Evidence{Name: "rank", Result: mustJSON(map[string]any{"position": i + 1, "value": entry.Value})})
		}
		items = ordered
		if p.Type == "rank-pack" {
			segs := make([]compact.Segment, len(items))
			scores := make([]compact.Scores, len(items))
			for i := range items {
				var text string
				if json.Unmarshal(items[i].Data, &text) != nil {
					return s, errors.New("rank-pack requires original string data")
				}
				segs[i] = compact.Segment{Text: text, Size: len(text)}
				scores[i] = compact.Scores{Needed: order.Entries[i].Value}
			}
			selection, e := compact.Select(segs, scores, compact.Rule{Budget: p.BudgetBytes})
			if e != nil {
				return s, e
			}
			for i, decision := range selection {
				items[i].Stages = append(items[i].Stages, Evidence{Name: "pack", Result: mustJSON(decision)})
				if decision.Action == compact.Drop {
					items[i].Status = "dropped"
				}
			}
		}
	}
	for _, r := range items {
		r, e = cleanValue(r)
		if e != nil {
			break
		}
		if e = ctx.Err(); e != nil {
			break
		}
		if e = writeJSON(itemPath(s.Path, "results", r.Index), r); e != nil {
			break
		}
		switch r.Status {
		case "complete":
			s.Completed++
		case "dropped":
			s.Dropped++
		case "failed":
			s.Failed++
		}
		if fn != nil {
			if e = fn(r); e != nil {
				break
			}
		}
	}
	s.Status = "complete"
	if e != nil {
		s.Status = "stopped"
	} else if s.Failed > 0 {
		s.Status = "partial"
	}
	se := saveSummary(&s)
	if e == nil {
		e = se
	}
	return s, e
}
