package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/deepnoodle-ai/decide/x/rank"
)

func (a *App) runRank(ctx context.Context, args []string) int {
	fs := a.flags("rank")
	o := BindCommon(fs, "rank", false, true)
	answer := fs.String("answer", "", "saved Noul question key to rank by")
	if code, ok := a.parse(fs, args, o); !ok {
		return code
	}
	if *answer == "" {
		return a.fail(fmt.Errorf("rank: --answer is required"))
	}
	if ctx.Err() != nil {
		return 130
	}
	records, err := ReadRecords(a.In, *o)
	if err != nil {
		return a.fail(err)
	}
	observations := make([]rank.Observation, len(records))
	sources := make([]string, len(records))
	for i := range records {
		run, err := records[i].SelectedRun(o.Run)
		if err != nil {
			return a.fail(fmt.Errorf("rank: record %q: %w", records[i].ID, err))
		}
		resp, err := run.ValidatedResponse()
		if err != nil {
			return a.fail(fmt.Errorf("rank: record %q: %w", records[i].ID, err))
		}
		observations[i], err = rank.FromResponse(resp, *answer)
		if err != nil {
			return a.fail(fmt.Errorf("rank: record %q: %w", records[i].ID, err))
		}
		sources[i] = run.Name
		if err := records[i].AppendRun(Run{Name: o.As, Command: "rank"}); err != nil {
			return a.fail(err)
		}
	}
	order, err := rank.Rerank(records, observations, func(_ int, _ Envelope) string { return "" })
	if err != nil {
		return a.fail(err)
	}
	for position, entry := range order.Entries {
		if ctx.Err() != nil {
			return 130
		}
		e := entry.Item
		result, err := json.Marshal(struct {
			Position  int     `json:"position"`
			Noul      float64 `json:"noul"`
			SourceRun string  `json:"source_run"`
		}{position + 1, entry.Value, sources[entry.Index]})
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
