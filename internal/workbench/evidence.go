package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/internal/jobs"
)

// loadEvidence shares the engine's bounded saved-evidence decoder and rechecks
// answers for presentation. The screen retains at most 200 records / 16 MiB.
func loadEvidence(ctx context.Context, target, dir, filter string) ([]jobs.Result, jobs.Summary, error) {
	var out []jobs.Result
	accept := func(r jobs.Result) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		r = checkedResult(r)
		if matches(r, filter) && len(out) < windowLimit {
			if resultsBytes(out)+resultBytes(r) <= 16<<20 {
				out = append(out, r)
			} else if len(out) == 0 {
				out = append(out, compactResult(r))
			}
		}
		return nil
	}
	sum, _ := jobs.Show(target, dir)
	err := jobs.ReadEvidence(target, dir, accept)
	return out, sum, err
}
func matches(r jobs.Result, filter string) bool {
	if filter == "" {
		return true
	}
	return strings.Contains(strings.ToLower(pretty(r)), strings.ToLower(filter))
}
func decodeEvidence(ctx context.Context, reader io.Reader, accept func(jobs.Result) error) error {
	return jobs.DecodeEvidence(reader, func(r jobs.Result) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return accept(checkedResult(r))
	})
}
func legacyResult(raw json.RawMessage) (jobs.Result, error) {
	var out jobs.Result
	err := decodeEvidence(context.Background(), strings.NewReader(string(raw)), func(r jobs.Result) error { out = r; return nil })
	return out, err
}
func transformation(e jobs.Evidence) bool {
	return len(e.Questions) == 0 && len(e.Response) == 0 && len(e.Result) > 0
}
func checkedResult(r jobs.Result) jobs.Result {
	r.Stages = append([]jobs.Evidence(nil), r.Stages...)
	for i, e := range r.Stages {
		if e.Error != "" && r.Status != "uncertain" {
			r.Status = "failed"
			r.Error = e.Error
		}
		err := error(nil)
		if len(e.Invalid) > 0 {
			err = fmt.Errorf("saved validation rejected %d answers", len(e.Invalid))
		} else if !transformation(e) {
			err = validateEvidence(e)
		}
		if err != nil && r.Status != "uncertain" {
			e.Error = "saved evidence validation: " + err.Error()
			r.Stages[i] = e
			r.Status = "failed"
			r.Error = e.Error
		}
	}
	return r
}
func validateEvidence(e jobs.Evidence) error {
	if len(e.Invalid) > 0 {
		return fmt.Errorf("saved validation rejected %d answers", len(e.Invalid))
	}
	req := decide.NewRequest(e.State)
	for name, raw := range e.Questions {
		q, err := decide.DecodeQuestion(raw)
		if err != nil {
			return err
		}
		switch q.(type) {
		case *decide.NoulQuestion, *decide.ChoiceQuestion, *decide.ScoreQuestion:
		default:
			return fmt.Errorf("unsupported saved question %q", name)
		}
		req.Questions[name] = q
	}
	if err := req.Validate(); err != nil {
		return err
	}
	if len(e.Response) == 0 {
		return fmt.Errorf("no saved response")
	}
	resp, err := decide.DecodeResponse(e.Response, req)
	if err != nil {
		return err
	}
	for k, q := range req.Questions {
		a := resp.Answers[k]
		if a == nil {
			return fmt.Errorf("missing answer %q", k)
		}
		if a.AnswerType() != q.QuestionType() {
			return fmt.Errorf("answer %q type mismatch", k)
		}
		if v, ok := q.(decide.AnswerValidator); ok {
			if err := v.ValidateAnswer(a); err != nil {
				return fmt.Errorf("answer %q: %w", k, err)
			}
		}
	}
	for k := range resp.Answers {
		if _, ok := req.Questions[k]; !ok {
			return fmt.Errorf("unexpected answer %q", k)
		}
	}
	return nil
}
