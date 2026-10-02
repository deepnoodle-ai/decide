package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/x/rank"
)

func hasLimits(d definition) bool {
	if d.Pattern.Type != "funnel" {
		return false
	}
	for _, s := range d.Pattern.Stages {
		if s.Limit > 0 {
			return true
		}
	}
	return false
}

// A ranked stage limit requires a barrier. Only these collection funnels use
// bounded in-memory ranking; ordinary mapping/funnels stay streaming.
func (e *executor) runLimitedFunnel(ctx context.Context, factory func() (*decide.Client, error), retryFailed, retryUncertain bool) {
	max := e.d.Pattern.MaxRecords
	if max == 0 {
		max = e.d.Options.MaxRecords
	}
	if e.s.Items > max {
		e.fail(fmt.Errorf("limited funnel exceeds %d records; raise max-records explicitly", max))
		return
	}
	full := e.d.Pattern.Stages
	e.singleStage = true
	defer func() { e.d.Pattern.Stages = full; e.singleStage = false }()
	for stageIndex, st := range full {
		if ctx.Err() != nil {
			return
		}
		e.d.Pattern.Stages = full[stageIndex : stageIndex+1]
		queue := make(chan int)
		var wg sync.WaitGroup
		for w := 0; w < e.d.Options.Workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range queue {
					if ctx.Err() != nil {
						continue
					}
					var old Result
					err := readJSON(itemPath(e.s.Path, "results", i), &old)
					if err == nil {
						if (stageIndex > 0 && old.NextStage < stageIndex) || old.Status == "complete" || old.Status == "dropped" || old.NextStage > stageIndex || (old.Status == "failed" && !retryFailed) || (old.Status == "uncertain" && !retryUncertain) {
							continue
						}
					} else if !os.IsNotExist(err) {
						e.fail(err)
						continue
					}

					var rec input
					if err = readJSON(itemPath(e.s.Path, "inputs", i), &rec); err != nil {
						e.fail(err)
						continue
					}
					r := e.process(ctx, i, rec, old)
					r.NextStage = stageIndex
					if r.Status == "complete" {
						r.Status = "staged"
						r.NextStage = stageIndex + 1
					}
					if err = e.storeResult(r); err != nil {
						e.fail(err)
					}
					if r.Status == "failed" && e.d.Options.OnError == "stop" {
						e.fail(errors.New("execution stopped after an item failed"))
					}
				}
			}()
		}
	feed:
		for i := 0; i < e.s.Items; i++ {
			select {
			case queue <- i:
			case <-ctx.Done():
				break feed
			}
		}
		close(queue)
		wg.Wait()
		if ctx.Err() != nil {
			return
		}
		eligible := []Result{}
		var collectionBytes int64
		obs := []rank.Observation{}
		for i := 0; i < e.s.Items; i++ {
			var r Result
			if readJSON(itemPath(e.s.Path, "results", i), &r) != nil || r.Status != "staged" || r.NextStage != stageIndex+1 {
				continue
			}
			size := int64(len(mustJSON(r)))
			if size > e.d.Options.MaxCollectionBytes-collectionBytes {
				e.fail(fmt.Errorf("funnel stage %s collection exceeds %d bytes; raise max-collection-bytes explicitly", st.Name, e.d.Options.MaxCollectionBytes))
				return
			}
			collectionBytes += size
			eligible = append(eligible, r)
			if st.Limit > 0 {
				r.Status = "complete"
				resp, err := savedResponse(r)
				var o rank.Observation
				if err == nil {
					o, err = rank.FromResponse(resp, st.Answer)
				}
				if err != nil {
					e.fail(fmt.Errorf("stage %s limit requires valid Noul answer %q: %w", st.Name, st.Answer, safeError(err)))
					return
				}
				obs = append(obs, o)
			}
		}
		keep := map[int]bool{}
		if st.Limit > 0 {
			order, err := rank.Rerank(eligible, obs, func(i int, r Result) string { return r.ID })
			if err != nil {
				e.fail(err)
				return
			}
			for n, en := range order.Entries {
				if n < st.Limit {
					keep[en.Item.Index] = true
				}
			}
		}
		for _, r := range eligible {
			if st.Limit > 0 && !keep[r.Index] {
				r.Status = "dropped"
				r.Stages[len(r.Stages)-1].Result = mustJSON(map[string]any{"kept": false, "limit": st.Limit})
			} else if stageIndex == len(full)-1 {
				r.Status = "complete"
			}
			if err := e.storeResult(r); err != nil {
				e.fail(err)
				return
			}
		}
	}
}
