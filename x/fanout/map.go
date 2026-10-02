package fanout

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/deepnoodle-ai/decide"
)

// ErrInvalidConfig reports an invalid Map argument.
var ErrInvalidConfig = errors.New("fanout: invalid configuration")

// Builder creates one request for an item. Calls may run concurrently, up
// to the worker limit. The builder must honor ctx for prompt cancellation.
// Its returned request must remain unchanged while the client sends it.
type Builder[T any] func(ctx context.Context, index int, item T) (*decide.Request, error)

// Result is the outcome for one input item. Index is its input position.
// Started means its builder was invoked. Response and Err can both be set
// when the root client returns a response with an answer validation error.
type Result struct {
	Index    int
	Started  bool
	Response *decide.Response
	Err      error
}

// Map builds and sends one independent request per item, with at most
// workers builders or calls running concurrently. Results are in input order.
// A builder or call error affects only that item; later items can still run.
//
// Cancellation stops new work and returns all slots. Never-started items
// receive ctx.Err(); started items keep their response and error. Map waits
// for started work to return, so builders must honor ctx for a prompt exit.
// If cancellation affects an item, the top-level error matches ctx.Err().
// Configuration errors return nil results and wrap ErrInvalidConfig.
// The caller must not mutate items during Map.
func Map[T any](
	ctx context.Context,
	client *decide.Client,
	items []T,
	workers int,
	build Builder[T],
) ([]Result, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: context is nil", ErrInvalidConfig)
	}
	if client == nil {
		return nil, fmt.Errorf("%w: client is nil", ErrInvalidConfig)
	}
	if build == nil {
		return nil, fmt.Errorf("%w: builder is nil", ErrInvalidConfig)
	}
	if workers < 1 {
		return nil, fmt.Errorf("%w: workers must be positive", ErrInvalidConfig)
	}

	results := make([]Result, len(items))
	// Each index has one worker, and these flags are read only after wg.Wait.
	canceledWhileStarted := make([]bool, len(items))
	for i := range results {
		results[i].Index = i
	}
	if len(items) == 0 {
		return results, nil
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(workers, len(items)) {
		wg.Go(func() {
			for index := range jobs {
				if ctx.Err() != nil {
					continue
				}
				r := &results[index]
				r.Started = true
				req, err := build(ctx, index, items[index])
				if err != nil {
					r.Err = err
				} else if ctxErr := ctx.Err(); ctxErr != nil {
					r.Err = ctxErr
				} else {
					r.Response, r.Err = client.SystemOne(ctx, req)
				}
				canceledWhileStarted[index] = ctx.Err() != nil
			}
		})
	}

dispatch:
	for index := range items {
		select {
		case <-ctx.Done():
			break dispatch
		case jobs <- index:
		}
	}
	close(jobs)
	wg.Wait()

	ctxErr := ctx.Err()
	if ctxErr != nil {
		cancellationAffectedWork := false
		for i := range results {
			if !results[i].Started {
				results[i].Err = ctxErr
				cancellationAffectedWork = true
			}
			if canceledWhileStarted[i] {
				cancellationAffectedWork = true
			}
		}
		if cancellationAffectedWork {
			return results, ctxErr
		}
	}
	return results, nil
}
