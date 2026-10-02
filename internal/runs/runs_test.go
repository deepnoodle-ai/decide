package runs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/internal/skill"
	"github.com/deepnoodle-ai/decide/internal/source"
)

func newRun(t *testing.T, texts ...string) *Run {
	t.Helper()
	t.Setenv("DECIDE_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	s, err := skill.Load("sentiment")
	if err != nil {
		t.Fatal(err)
	}
	r, err := Create(&Run{Skill: s, Provider: "typesafe", Model: "jev-latest", Sources: []string{"stdin"}})
	if err != nil {
		t.Fatal(err)
	}
	err = source.Walk(context.Background(), nil, strings.NewReader(strings.Join(texts, "\n")), source.Options{Input: skill.Record}, r.Add)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Ready(); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestExecuteAndResume(t *testing.T) {
	r := newRun(t, "great", "awful", "fine")
	server := decidetest.NewServer(t)
	client := server.Client(t)
	server.FailNext(422) // the first item fails validation
	var seen []Result
	err := r.Execute(context.Background(), client, 1, func(res Result) error {
		seen = append(seen, res)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 || seen[0].Status != "failed" || seen[0].Error == "" || seen[1].Status != "complete" {
		t.Fatalf("results = %+v", seen)
	}
	if r.Status != Partial || r.Complete != 2 || r.Failed != 1 {
		t.Fatalf("run = %s %d/%d", r.Status, r.Complete, r.Failed)
	}

	reopened, err := Open(r.ID[:12])
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Status != Partial || reopened.Pending() != 1 {
		t.Fatalf("reopened = %s, %d pending", reopened.Status, reopened.Pending())
	}
	seen = nil
	if err := reopened.Execute(context.Background(), client, 1, func(res Result) error {
		seen = append(seen, res)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0].Index != 0 || seen[0].Status != "complete" {
		t.Fatalf("resumed results = %+v", seen)
	}
	if reopened.Status != Complete {
		t.Fatalf("status after resume = %s", reopened.Status)
	}
	results, err := reopened.Results()
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 || results[0].Status != "complete" || string(results[0].Input) != `"great"` {
		t.Fatalf("latest results = %+v", results)
	}
	if got := len(server.Requests()); got != 4 {
		t.Fatalf("server saw %d requests, want 4", got)
	}
}

func TestResultsArriveInInputOrder(t *testing.T) {
	r := newRun(t, "slow", "b", "c", "d")
	server := decidetest.NewServer(t)
	server.Respond(func(req *decide.Request) (*decide.Response, error) {
		if fmt.Sprint(req.State) == "slow" {
			time.Sleep(50 * time.Millisecond)
		}
		q := req.Questions["sentiment"].(*decide.ChoiceQuestion)
		return &decide.Response{Answers: map[string]decide.Answer{"sentiment": decidetest.ChoiceFor(q, 0.8, 0.1, 0.1)}}, nil
	})
	var order []int
	err := r.Execute(context.Background(), server.Client(t), 4, func(res Result) error {
		order = append(order, res.Index)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(order, []int{0, 1, 2, 3}) {
		t.Fatalf("order = %v", order)
	}
}

func TestCancelDeliversWaitingResults(t *testing.T) {
	r := newRun(t, "slow", "b", "c", "d", "e")
	server := decidetest.NewServer(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) }) // runs before the server closes
	server.Respond(func(req *decide.Request) (*decide.Response, error) {
		if fmt.Sprint(req.State) == "slow" {
			<-release
		}
		q := req.Questions["sentiment"].(*decide.ChoiceQuestion)
		return &decide.Response{Answers: map[string]decide.Answer{"sentiment": decidetest.ChoiceFor(q, 0.8, 0.1, 0.1)}}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Cancel once b, c, and d are saved behind the slow item.
		for {
			data, _ := os.ReadFile(filepath.Join(r.Dir, "results.jsonl"))
			if bytes.Count(data, []byte("\n")) == 3 {
				cancel()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	var order []int
	err := r.Execute(ctx, server.Client(t), 2, func(res Result) error {
		order = append(order, res.Index)
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	// Two workers may run at most four items ahead, so e never starts.
	if !slices.Equal(order, []int{1, 2, 3}) || r.Complete != 3 {
		t.Fatalf("order = %v, complete = %d", order, r.Complete)
	}
}

func TestOutputErrorStopsCallingFn(t *testing.T) {
	r := newRun(t, "a", "b", "c", "d")
	server := decidetest.NewServer(t)
	broken := errors.New("broken pipe")
	calls := 0
	err := r.Execute(context.Background(), server.Client(t), 2, func(Result) error {
		calls++
		return broken
	})
	if !errors.Is(err, broken) || calls != 1 {
		t.Fatalf("err = %v after %d calls", err, calls)
	}
}

func TestRejectedKeyStopsTheRun(t *testing.T) {
	r := newRun(t, "a", "b", "c")
	server := decidetest.NewServer(t, decidetest.WithAPIKey("right"))
	client, err := server.NewClient(decide.WithAPIKey("wrong"))
	if err != nil {
		t.Fatal(err)
	}
	err = r.Execute(context.Background(), client, 1, nil)
	var fatal *FatalError
	if !errors.As(err, &fatal) {
		t.Fatalf("err = %v, want FatalError", err)
	}
	if r.Status != Interrupted || r.Failed != 0 {
		t.Fatalf("run = %s with %d failed", r.Status, r.Failed)
	}
}

func TestCanceledRunIsInterrupted(t *testing.T) {
	r := newRun(t, "a", "b")
	server := decidetest.NewServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Execute(ctx, server.Client(t), 2, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if r.Status != Interrupted || r.Pending() != 2 {
		t.Fatalf("run = %s, %d pending", r.Status, r.Pending())
	}
}

func TestListAndLatest(t *testing.T) {
	first := newRun(t, "a")
	if _, err := Open("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open(nope) = %v", err)
	}
	latest, err := Latest()
	if err != nil || latest.ID != first.ID {
		t.Fatalf("Latest = %v, %v", latest, err)
	}
	if latest.Status != Interrupted {
		t.Fatalf("an unexecuted run has status %s", latest.Status)
	}
	all, err := List()
	if err != nil || len(all) != 1 {
		t.Fatalf("List = %d runs, %v", len(all), err)
	}
}

func TestRepeatedFailuresStopTheRun(t *testing.T) {
	r := newRun(t, "a", "b", "c", "d", "e", "f", "g")
	server := decidetest.NewServer(t)
	for range 10 {
		server.FailNext(404)
	}
	err := r.Execute(context.Background(), server.Client(t), 1, nil)
	var repeated *RepeatedError
	if !errors.As(err, &repeated) || repeated.Count != RepeatLimit {
		t.Fatalf("err = %v, want RepeatedError", err)
	}
	if r.Status != Interrupted || r.Failed != RepeatLimit {
		t.Fatalf("run = %s with %d failed", r.Status, r.Failed)
	}
}

func TestUnfinishedRunsAreHidden(t *testing.T) {
	t.Setenv("DECIDE_HOME", t.TempDir())
	s, _ := skill.Load("sentiment")
	r, err := Create(&Run{Skill: s})
	if err != nil {
		t.Fatal(err)
	}
	if all, _ := List(); len(all) != 0 {
		t.Fatalf("List shows %d unfinished runs", len(all))
	}
	if _, err := Open(r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open(unfinished) = %v", err)
	}
	r.Discard()
}
