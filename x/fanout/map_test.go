package fanout_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/x/fanout"
)

func request(_ context.Context, _ int, state string) (*decide.Request, error) {
	r := decide.NewRequest(state)
	decide.Ask(r, "relevant", &decide.NoulQuestion{
		Instructions: "Is this relevant?",
	})
	return r, nil
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for concurrent work")
		var zero T
		return zero
	}
}

func TestMapOrderedAndBounded(t *testing.T) {
	srv := decidetest.NewServer(t)
	client := srv.Client(t)
	started := make(chan int, 4)
	finished := make(chan int, 4)
	gates := [4]chan struct{}{}
	for i := range gates {
		gates[i] = make(chan struct{})
	}
	var active, maximum atomic.Int32
	srv.Respond(func(req *decide.Request) (*decide.Response, error) {
		name, ok := req.State.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected state type %T", req.State)
		}
		i, err := strconv.Atoi(name)
		if err != nil {
			return nil, err
		}
		n := active.Add(1)
		for old := maximum.Load(); n > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, n) {
				break
			}
		}
		started <- i
		<-gates[i]
		active.Add(-1)
		finished <- i
		return &decide.Response{
			Answers: map[string]decide.Answer{
				"relevant": decidetest.NoulAnswer(float64(i) / 10),
			},
			Usage: decide.Usage{InputTokens: 100 + i},
		}, nil
	})

	type outcome struct {
		results []fanout.Result
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		r, err := fanout.Map(context.Background(), client,
			[]string{"0", "1", "2", "3"}, 2, request)
		done <- outcome{r, err}
	}()

	a, b := receive(t, started), receive(t, started)
	if a == b {
		t.Fatalf("duplicate start for item %d", a)
	}
	if maximum.Load() != 2 {
		t.Fatalf("maximum in flight = %d, want 2", maximum.Load())
	}
	// Free the second arrival first, then each new request as it arrives.
	close(gates[b])
	receive(t, finished)
	c := receive(t, started)
	close(gates[c])
	receive(t, finished)
	d := receive(t, started)
	close(gates[d])
	receive(t, finished)
	close(gates[a])
	receive(t, finished)
	out := receive(t, done)
	if out.err != nil {
		t.Fatal(out.err)
	}
	if len(out.results) != 4 || maximum.Load() != 2 {
		t.Fatalf("results=%d maximum=%d", len(out.results), maximum.Load())
	}
	for i, r := range out.results {
		if r.Index != i || !r.Started || r.Err != nil || r.Response == nil {
			t.Fatalf("slot %d: %+v", i, r)
		}
		if r.Response.Model != "jev-1.13.0" || r.Response.RequestID == "" ||
			r.Response.Usage.InputTokens != 100+i {
			t.Fatalf("slot %d metadata: %+v", i, r.Response)
		}
		if got := r.Response.Answers["relevant"].(*decide.NoulAnswer).Noul; got != float64(i)/10 {
			t.Fatalf("slot %d noul=%v", i, got)
		}
	}
	records := srv.Requests()
	if len(records) != 4 {
		t.Fatalf("recorded %d calls, want 4", len(records))
	}
	states := make([]string, 0, len(records))
	for _, rec := range records {
		states = append(states, rec.Request.State.(string))
	}
	slices.Sort(states)
	if !slices.Equal(states, []string{"0", "1", "2", "3"}) {
		t.Fatalf("recorded states = %v", states)
	}
}

func TestMapConcurrentBuilders(t *testing.T) {
	srv := decidetest.NewServer(t)
	client := srv.Client(t)
	started := make(chan int, 3)
	release := make(chan struct{})
	build := func(ctx context.Context, i int, state string) (*decide.Request, error) {
		started <- i
		select {
		case <-release:
			return request(ctx, i, state)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	type outcome struct {
		results []fanout.Result
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		r, err := fanout.Map(context.Background(), client,
			[]string{"a", "b", "c"}, 2, build)
		done <- outcome{r, err}
	}()
	receive(t, started)
	receive(t, started)
	select {
	case third := <-started:
		t.Fatalf("builder %d started above worker bound", third)
	default:
	}
	close(release)
	out := receive(t, done)
	if out.err != nil || len(out.results) != 3 {
		t.Fatalf("results=%v err=%v", out.results, out.err)
	}
	for _, r := range out.results {
		if !r.Started || r.Err != nil {
			t.Fatalf("slot %+v", r)
		}
	}
}

func TestMapBuildFailureAndValidation(t *testing.T) {
	srv := decidetest.NewServer(t)
	client := srv.Client(t)
	srv.Respond(func(req *decide.Request) (*decide.Response, error) {
		p := 0.7
		if req.State == "invalid" {
			p = 1.5
		}
		return &decide.Response{Answers: map[string]decide.Answer{
			"relevant": decidetest.NoulAnswer(p),
			"usable":   decidetest.NoulAnswer(0.8),
		}}, nil
	})
	buildErr := errors.New("build failed")
	var built [3]int
	build := func(ctx context.Context, i int, state string) (*decide.Request, error) {
		built[i]++
		if state == "build-error" {
			return nil, buildErr
		}
		r, _ := request(ctx, i, state)
		decide.Ask(r, "usable", &decide.NoulQuestion{
			Instructions: "Does this contain usable evidence?",
		})
		return r, nil
	}
	results, err := fanout.Map(context.Background(), client,
		[]string{"build-error", "invalid", "good"}, 1, build)
	if err != nil || len(results) != 3 || built != [3]int{1, 1, 1} {
		t.Fatalf("results=%v built=%v err=%v", results, built, err)
	}
	if !errors.Is(results[0].Err, buildErr) || results[0].Response != nil {
		t.Fatalf("build failure: %+v", results[0])
	}
	var invalid *decide.InvalidAnswersError
	if !errors.As(results[1].Err, &invalid) || results[1].Response == nil ||
		results[1].Response.Invalid["relevant"] == nil {
		t.Fatalf("validation failure: %+v", results[1])
	}
	if results[2].Err != nil || results[2].Response == nil ||
		len(results[2].Response.Answers) != 2 {
		t.Fatalf("later success: %+v", results[2])
	}
	if len(srv.Requests()) != 2 {
		t.Fatalf("recorded %d calls, want 2", len(srv.Requests()))
	}
}

func TestMapRetryBuildsOnce(t *testing.T) {
	srv := decidetest.NewServer(t)
	client := srv.Client(t)
	srv.FailNext(429, decidetest.RetryAfterMS(1))
	built := 0
	build := func(ctx context.Context, i int, state string) (*decide.Request, error) {
		built++
		return request(ctx, i, state)
	}
	results, err := fanout.Map(context.Background(), client,
		[]string{"one"}, 1, build)
	if err != nil || len(results) != 1 || results[0].Err != nil ||
		built != 1 || len(srv.Requests()) != 2 {
		t.Fatalf("results=%v built=%d requests=%d err=%v",
			results, built, len(srv.Requests()), err)
	}
}

func TestMapMultipleQuestionsPerTicket(t *testing.T) {
	srv := decidetest.NewServer(t)
	client := srv.Client(t)
	build := func(ctx context.Context, i int, ticket string) (*decide.Request, error) {
		r := decide.NewRequest(ticket)
		decide.Ask(r, "urgent", &decide.NoulQuestion{
			Instructions: "Does the ticket describe an urgent issue?",
		})
		decide.Ask(r, "refund", &decide.NoulQuestion{
			Instructions: "Does the ticket request a refund?",
		})
		return r, nil
	}
	items := []string{"Payouts have failed for three days.", "Please refund my order."}
	results, err := fanout.Map(context.Background(), client, items, 2, build)
	if err != nil || len(results) != len(items) {
		t.Fatalf("results=%v err=%v", results, err)
	}
	for i, r := range results {
		if r.Err != nil || r.Response == nil || len(r.Response.Answers) != 2 ||
			r.Index != i {
			t.Fatalf("slot %d: %+v", i, r)
		}
	}
	records := srv.Requests()
	if len(records) != len(items) {
		t.Fatalf("recorded %d calls, want %d", len(records), len(items))
	}
	states := []string{records[0].Request.State.(string), records[1].Request.State.(string)}
	slices.Sort(states)
	sortedItems := slices.Clone(items)
	slices.Sort(sortedItems)
	if !slices.Equal(states, sortedItems) {
		t.Fatalf("recorded states = %v", states)
	}
	for _, rec := range records {
		if len(rec.Request.Questions) != 2 {
			t.Fatalf("question count for %v = %d", rec.Request.State,
				len(rec.Request.Questions))
		}
	}
}

func TestMapCancellationPreservesStartedOutcomes(t *testing.T) {
	srv := decidetest.NewServer(t)
	client := srv.Client(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	buildErr := errors.New("started builder failed")
	built := make([]int, 0, 3)
	build := func(ctx context.Context, i int, state string) (*decide.Request, error) {
		built = append(built, i) // one worker
		if i == 1 {
			cancel()
			return nil, buildErr
		}
		return request(ctx, i, state)
	}
	results, err := fanout.Map(ctx, client,
		[]string{"done", "build-error", "never-started"}, 1, build)
	if !errors.Is(err, context.Canceled) || len(results) != 3 {
		t.Fatalf("results=%v err=%v", results, err)
	}
	if !slices.Equal(built, []int{0, 1}) {
		t.Fatalf("builders invoked for %v", built)
	}
	if !results[0].Started || results[0].Response == nil || results[0].Err != nil {
		t.Fatalf("completed slot lost: %+v", results[0])
	}
	if !results[1].Started || !errors.Is(results[1].Err, buildErr) {
		t.Fatalf("started error replaced: %+v", results[1])
	}
	if results[2].Started || !errors.Is(results[2].Err, context.Canceled) {
		t.Fatalf("never-started slot: %+v", results[2])
	}
}

func TestMapCanceledBeforeStart(t *testing.T) {
	srv := decidetest.NewServer(t)
	client := srv.Client(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	built := 0
	build := func(ctx context.Context, i int, state string) (*decide.Request, error) {
		built++
		return request(ctx, i, state)
	}
	results, err := fanout.Map(ctx, client, []string{"a", "b"}, 2, build)
	if !errors.Is(err, context.Canceled) || built != 0 || len(srv.Requests()) != 0 {
		t.Fatalf("results=%v built=%d calls=%d err=%v",
			results, built, len(srv.Requests()), err)
	}
	for _, r := range results {
		if r.Started || !errors.Is(r.Err, context.Canceled) {
			t.Fatalf("slot %+v", r)
		}
	}
}

func TestMapCancellationDuringCall(t *testing.T) {
	srv := decidetest.NewServer(t)
	client := srv.Client(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	// Keep the server blocked until the client observes cancellation. Releasing
	// it immediately after cancel would let a successful response win the race.
	defer close(release)
	srv.Respond(func(req *decide.Request) (*decide.Response, error) {
		close(entered)
		<-release
		return &decide.Response{Answers: map[string]decide.Answer{
			"relevant": decidetest.NoulAnswer(0.5),
		}}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		results []fanout.Result
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		r, err := fanout.Map(ctx, client, []string{"started", "waiting"}, 1, request)
		done <- outcome{r, err}
	}()
	receive(t, entered)
	cancel()
	out := receive(t, done)
	if !errors.Is(out.err, context.Canceled) || len(out.results) != 2 {
		t.Fatalf("results=%v err=%v", out.results, out.err)
	}
	if !out.results[0].Started || !errors.Is(out.results[0].Err, context.Canceled) {
		t.Fatalf("in-flight call: %+v", out.results[0])
	}
	if out.results[1].Started || !errors.Is(out.results[1].Err, context.Canceled) {
		t.Fatalf("never-started item: %+v", out.results[1])
	}
	if len(srv.Requests()) != 1 {
		t.Fatalf("recorded %d calls, want 1", len(srv.Requests()))
	}
}

// This transport deliberately ignores cancellation so the root client can
// validate a late response after the caller cancels the collection.
type lateResponseTransport struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (tr *lateResponseTransport) SystemOne(
	_ context.Context,
	_ *decide.Request,
) (*decide.Response, error) {
	tr.calls.Add(1)
	close(tr.entered)
	<-tr.release
	return &decide.Response{Answers: map[string]decide.Answer{
		"relevant": decidetest.NoulAnswer(1.5),
	}}, nil
}

func (*lateResponseTransport) ListModels(context.Context) (*decide.ModelList, error) {
	return &decide.ModelList{}, nil
}

func TestMapCancellationKeepsLateResponseAndValidationError(t *testing.T) {
	tr := &lateResponseTransport{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	client, err := decide.NewClient(
		decide.WithoutEnvironment(),
		decide.WithTransport(tr),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		results []fanout.Result
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		r, err := fanout.Map(ctx, client, []string{"late", "unstarted"}, 1, request)
		done <- outcome{r, err}
	}()
	receive(t, tr.entered)
	cancel()
	close(tr.release)
	out := receive(t, done)
	if !errors.Is(out.err, context.Canceled) || len(out.results) != 2 {
		t.Fatalf("results=%v err=%v", out.results, out.err)
	}
	started := out.results[0]
	var invalid *decide.InvalidAnswersError
	if !started.Started || started.Response == nil ||
		!errors.As(started.Err, &invalid) ||
		started.Response.Invalid["relevant"] == nil {
		t.Fatalf("late validation outcome was replaced: %+v", started)
	}
	if errors.Is(started.Err, context.Canceled) {
		t.Fatalf("started error replaced by cancellation: %v", started.Err)
	}
	unstarted := out.results[1]
	if unstarted.Started || unstarted.Response != nil ||
		!errors.Is(unstarted.Err, context.Canceled) {
		t.Fatalf("unstarted slot: %+v", unstarted)
	}
	if tr.calls.Load() != 1 {
		t.Fatalf("transport calls = %d, want 1", tr.calls.Load())
	}
}

func TestMapCanceledSingleItemKeepsLateValidationError(t *testing.T) {
	tr := &lateResponseTransport{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	client, err := decide.NewClient(
		decide.WithoutEnvironment(),
		decide.WithTransport(tr),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		results []fanout.Result
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		r, err := fanout.Map(ctx, client, []string{"late"}, 1, request)
		done <- outcome{r, err}
	}()
	receive(t, tr.entered)
	cancel()
	close(tr.release)
	out := receive(t, done)
	if !errors.Is(out.err, context.Canceled) || len(out.results) != 1 {
		t.Fatalf("results=%v err=%v", out.results, out.err)
	}
	r := out.results[0]
	var invalid *decide.InvalidAnswersError
	if !r.Started || r.Response == nil || !errors.As(r.Err, &invalid) ||
		r.Response.Invalid["relevant"] == nil {
		t.Fatalf("late result lost: %+v", r)
	}
	if errors.Is(r.Err, context.Canceled) || tr.calls.Load() != 1 {
		t.Fatalf("item error=%v calls=%d", r.Err, tr.calls.Load())
	}
}

func TestMapInvalidArgumentsAndEmpty(t *testing.T) {
	srv := decidetest.NewServer(t)
	client := srv.Client(t)
	tests := []struct {
		name    string
		ctx     context.Context
		client  *decide.Client
		workers int
		build   fanout.Builder[string]
	}{
		{"nil context", nil, client, 1, request},
		{"nil client", context.Background(), nil, 1, request},
		{"zero workers", context.Background(), client, 0, request},
		{"nil builder", context.Background(), client, 1, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := fanout.Map(tt.ctx, tt.client,
				[]string{"item"}, tt.workers, tt.build)
			if r != nil || !errors.Is(err, fanout.ErrInvalidConfig) {
				t.Fatalf("results=%v err=%v", r, err)
			}
		})
	}
	r, err := fanout.Map(context.Background(), client, []string{}, 1, request)
	if err != nil || r == nil || len(r) != 0 || len(srv.Requests()) != 0 {
		t.Fatalf("empty results=%v calls=%d err=%v", r, len(srv.Requests()), err)
	}
}
