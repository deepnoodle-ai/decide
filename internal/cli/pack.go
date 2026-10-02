package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/deepnoodle-ai/decide/patterns/compact"
	"github.com/deepnoodle-ai/decide/patterns/rank"
)

func (a *App) runPack(ctx context.Context, args []string) int {
	fs := a.flags("pack")
	o := BindCommon(fs, "pack", false, true)
	answer := fs.String("answer", "", "saved Noul key indicating whether source text is needed")
	budget := fs.Int("budget-bytes", 0, "positive budget for selected UTF-8 string bytes")
	if code, ok := a.parse(fs, args, o); !ok {
		return code
	}
	if *answer == "" || *budget < 1 {
		return a.fail(fmt.Errorf("pack: --answer and positive --budget-bytes are required"))
	}
	if ctx.Err() != nil {
		return 130
	}
	records, err := ReadRecords(a.In, *o)
	if err != nil {
		return a.fail(err)
	}
	segments := make([]compact.Segment, len(records))
	scores := make([]compact.Scores, len(records))
	sources := make([]string, len(records))
	for i := range records {
		var text string
		if err := json.Unmarshal(records[i].Data, &text); err != nil || !bytes.HasPrefix(bytes.TrimSpace(records[i].Data), []byte(`"`)) {
			return a.fail(fmt.Errorf("pack: record %q: data must be a string", records[i].ID))
		}
		run, err := records[i].SelectedRun(o.Run)
		if err != nil {
			return a.fail(fmt.Errorf("pack: record %q: %w", records[i].ID, err))
		}
		resp, err := run.ValidatedResponse()
		if err != nil {
			return a.fail(fmt.Errorf("pack: record %q: %w", records[i].ID, err))
		}
		observation, err := rank.FromResponse(resp, *answer)
		if err != nil {
			return a.fail(fmt.Errorf("pack: record %q: %w", records[i].ID, err))
		}
		segments[i] = compact.Segment{Text: text, Size: len(text)}
		scores[i] = compact.Scores{Needed: observation.Noul}
		sources[i] = run.Name
		if err := records[i].AppendRun(Run{Name: o.As, Command: "pack"}); err != nil {
			return a.fail(err)
		}
	}
	decisions, err := compact.Select(segments, scores, compact.Rule{Budget: *budget})
	if err != nil {
		return a.fail(err)
	}
	for _, decision := range decisions {
		if ctx.Err() != nil {
			return 130
		}
		e := records[decision.Index]
		result, err := json.Marshal(struct {
			Action    compact.Action `json:"action"`
			Selected  bool           `json:"selected"`
			SizeBytes int            `json:"size_bytes"`
			Cause     compact.Cause  `json:"cause"`
			SourceRun string         `json:"source_run"`
		}{decision.Action, decision.Action == compact.Keep,
			segments[decision.Index].Size, decision.Cause, sources[decision.Index]})
		if err != nil {
			return a.fail(err)
		}
		e.Runs[len(e.Runs)-1].Result = result
		if err := WriteRecord(a.Out, e); err != nil {
			return a.fail(err)
		}
	}
	return 0
}
