// Package runs saves each evaluation of a dataset so it can be viewed again
// or resumed after an interruption.
//
// A run is a directory in DECIDE_HOME/runs:
//
//	run.json       what was asked, of what, and how far it got
//	inputs.jsonl   every item, saved before any model calls
//	results.jsonl  one line per answered or failed item
//	images/        image items
//
// Resuming a run evaluates the items that have no successful result, using
// the saved inputs and questions.
//
// With an answer cache, set with UseCache, a run asks only the questions
// whose answers the cache doesn't hold.
package runs

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/cloudflare"
	"github.com/deepnoodle-ai/decide/internal/cache"
	"github.com/deepnoodle-ai/decide/internal/source"
	"github.com/deepnoodle-ai/decide/internal/template"
)

// Status values.
const (
	Running     = "running"
	Complete    = "complete"    // every item answered
	Partial     = "partial"     // finished, but some items failed
	Interrupted = "interrupted" // stopped before every item was tried
)

// Run describes one evaluation of a dataset with a template.
type Run struct {
	ID       string             `json:"id"`
	Template *template.Template `json:"template"` // with parameters applied
	Params   map[string]string  `json:"params,omitempty"`
	Provider string             `json:"provider"`
	Model    string             `json:"model"`
	Sources  []string           `json:"sources"`
	Created  time.Time          `json:"created"`
	Updated  time.Time          `json:"updated"`
	Status   string             `json:"status"`
	Total    int                `json:"total"` // requests: one per item, or one per part of a large item
	Items    int                `json:"items,omitempty"`
	Complete int                `json:"complete"`
	Failed   int                `json:"failed"`
	NoCache  bool               `json:"no_cache,omitempty"` // ask every question, though answers are still kept

	Dir string `json:"-"`

	inputs *os.File
	cache  *cache.Cache
	source cache.Source
}

// Result is the outcome for one item.
type Result struct {
	Index     int                        `json:"index"`
	Source    string                     `json:"source"`
	Part      *Part                      `json:"part,omitempty"` // set when the result is for one part of an item
	Input     json.RawMessage            `json:"input,omitempty"`
	Status    string                     `json:"status"` // "complete" or "failed"
	Answers   map[string]json.RawMessage `json:"answers,omitempty"`
	Model     string                     `json:"model,omitempty"`
	RequestID string                     `json:"request_id,omitempty"`
	Cached    []string                   `json:"cached,omitempty"` // the questions whose answers came from the cache
	Error     string                     `json:"error,omitempty"`
}

// Part says which part of an item a result is for. The parts of an item
// have consecutive indexes.
type Part struct {
	N     int    `json:"n"`               // from 1
	Of    int    `json:"of"`              // how many parts the item has
	Lines string `json:"lines,omitempty"` // the file's lines in this part, such as "120-260"; empty for a record
	Size  int    `json:"size,omitempty"`  // the size of its text
}

type input struct {
	Index       int             `json:"index"`
	Source      string          `json:"source"`
	Part        *Part           `json:"part,omitempty"`
	Value       json.RawMessage `json:"input,omitempty"`
	State       json.RawMessage `json:"state"`
	Image       string          `json:"image,omitempty"`
	ContentType string          `json:"content_type,omitempty"`
}

// UseCache makes Execute look answers from a source up in a cache, unless
// the run was made with NoCache, and keep the answers it gets there.
func (r *Run) UseCache(c *cache.Cache, src cache.Source) { r.cache, r.source = c, src }

// Root is the directory that holds all runs.
func Root() string { return filepath.Join(template.Home(), "runs") }

// Create starts a new run. Add its items with Add, then call Ready.
func Create(r *Run) (*Run, error) {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	r.ID = now.Local().Format("20060102-150405") + "-" + hex.EncodeToString(b)
	// Items are saved in a hidden directory, which Ready renames, so an
	// interrupted read never leaves a run that looks finished.
	r.Dir = filepath.Join(Root(), ".new-"+r.ID)
	r.Created, r.Updated, r.Status = now, now, Running
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(r.Dir, "inputs.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	r.inputs = f
	return r, r.save()
}

// Add saves one item to the run, as one input per part when it has parts.
func (r *Run) Add(it source.Item) error {
	r.Items++
	if len(it.Parts) == 0 {
		return r.add(input{Index: r.Total, Source: it.Label, Value: it.Value, State: it.State}, it)
	}
	for i, p := range it.Parts {
		in := input{Index: r.Total, Source: it.Label, Value: it.Value, State: p.State,
			Part: &Part{N: i + 1, Of: len(it.Parts), Lines: p.Lines, Size: p.Size}}
		if err := r.add(in, it); err != nil {
			return err
		}
	}
	return nil
}

func (r *Run) add(in input, it source.Item) error {
	if it.Image != nil {
		in.Image = filepath.Join("images", fmt.Sprintf("%d%s", r.Total, filepath.Ext(it.Label)))
		in.ContentType = it.Image.ContentType
		if err := os.MkdirAll(filepath.Join(r.Dir, "images"), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(r.Dir, in.Image), it.Image.Data, 0o600); err != nil {
			return err
		}
	}
	line, err := json.Marshal(in)
	if err != nil {
		return err
	}
	if _, err := r.inputs.Write(append(line, '\n')); err != nil {
		return err
	}
	r.Total++
	return nil
}

// Ready finishes adding items.
func (r *Run) Ready() error {
	if err := r.inputs.Close(); err != nil {
		return err
	}
	if err := r.save(); err != nil {
		return err
	}
	dir := filepath.Join(Root(), r.ID)
	if err := os.Rename(r.Dir, dir); err != nil {
		return err
	}
	r.Dir = dir
	return nil
}

// Discard deletes a run that was never ready, such as one whose inputs
// could not be read.
func (r *Run) Discard() {
	if r.inputs != nil {
		r.inputs.Close()
	}
	os.RemoveAll(r.Dir)
}

func (r *Run) save() error {
	r.Updated = time.Now().UTC()
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(r.Dir, ".run.json.tmp")
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(r.Dir, "run.json"))
}

// ErrNotFound reports a run ID that matches no run.
var ErrNotFound = errors.New("run not found")

// Open finds a run by ID or by a unique prefix of its ID.
func Open(id string) (*Run, error) {
	if id == "" || strings.HasPrefix(id, ".") || strings.ContainsAny(id, `/\`) {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	if r, err := load(filepath.Join(Root(), id)); err == nil {
		return r, nil
	}
	entries, err := os.ReadDir(Root())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	var matches []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && strings.HasPrefix(e.Name(), id) {
			matches = append(matches, e.Name())
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("%w: %q", ErrNotFound, id)
	case 1:
		return load(filepath.Join(Root(), matches[0]))
	}
	return nil, fmt.Errorf("%q matches %d runs; use more of the ID", id, len(matches))
}

func load(dir string) (*Run, error) {
	data, err := os.ReadFile(filepath.Join(dir, "run.json"))
	if err != nil {
		return nil, err
	}
	var r Run
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	r.Dir = dir
	if r.Template == nil {
		return nil, fmt.Errorf("%s was saved by an older version of decide; delete it", dir)
	}
	r.Template.Normalize() // runs saved before input was "text" or "image"
	if r.Status != Running || !r.Active() {
		r.settle()
	}
	return &r, nil
}

// List returns all runs, newest first.
func List() ([]*Run, error) {
	entries, err := os.ReadDir(Root())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Run
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".new-") {
			// Remove runs whose items were never all read, after a crash.
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
				os.RemoveAll(filepath.Join(Root(), e.Name()))
			}
			continue
		}
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		r, err := load(filepath.Join(Root(), e.Name()))
		if err != nil {
			continue // not a run, or one still being created
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b *Run) int { return b.Created.Compare(a.Created) })
	return out, nil
}

// Latest returns the most recent run.
func Latest() (*Run, error) {
	all, err := List()
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, ErrNotFound
	}
	return all[0], nil
}

// Results returns the latest result for each item, in input order.
func (r *Run) Results() ([]Result, error) {
	latest := map[int]Result{}
	err := readLines(filepath.Join(r.Dir, "results.jsonl"), func(line []byte) error {
		var res Result
		if err := json.Unmarshal(line, &res); err != nil {
			return err
		}
		latest[res.Index] = res
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	out := make([]Result, 0, len(latest))
	for _, res := range latest {
		out = append(out, res)
	}
	slices.SortFunc(out, func(a, b Result) int { return a.Index - b.Index })
	return out, nil
}

// readLines calls fn for each line. A final line cut short by a crash is
// ignored.
func readLines(path string, fn func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	br := bufio.NewReader(f)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			if err := fn(line); err != nil {
				return err
			}
		}
		if err != nil {
			return nil
		}
	}
}

// FatalError stops a run instead of failing one item, for problems that
// would fail every item, such as a rejected API key.
type FatalError struct{ Err error }

func (e *FatalError) Error() string { return e.Err.Error() }
func (e *FatalError) Unwrap() error { return e.Err }

// RepeatLimit is how many items in a row may fail with the same error
// before a run stops, since that error will likely fail every item.
const RepeatLimit = 5

// RepeatedError stops a run whose items keep failing the same way, such as
// with an unknown model name or an exhausted quota.
type RepeatedError struct {
	Err   string
	Count int
}

func (e *RepeatedError) Error() string {
	return fmt.Sprintf("%d items in a row failed with: %s", e.Count, e.Err)
}

// Execute evaluates every item without a successful result, with up to
// workers requests at a time. It calls fn for each new result, one at a
// time, in input order: a result that finishes early waits for the items
// before it, and workers stay at most a few items ahead of the earliest
// unfinished one. Once fn returns an error, Execute stops calling it.
func (r *Run) Execute(ctx context.Context, client *decide.Client, workers int, fn func(Result) error) error {
	unlock, err := lock(r.Dir)
	if err != nil {
		return err
	}
	defer unlock()
	decoded, err := r.Template.Decode()
	if err != nil {
		return err
	}
	questions := questionSet{decoded: decoded, sent: map[string][]byte{}}
	for key, q := range decoded {
		if questions.sent[key], err = json.Marshal(q); err != nil {
			return err
		}
	}
	previous, err := r.Results()
	if err != nil {
		return err
	}
	status := map[int]string{} // guarded by mu once workers start
	done := map[int]bool{}     // read-only
	for _, res := range previous {
		status[res.Index] = res.Status
		done[res.Index] = res.Status == "complete"
	}
	r.Status = Running
	r.count(status)
	if err := r.save(); err != nil {
		return err
	}
	out, err := os.OpenFile(filepath.Join(r.Dir, "results.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu       sync.Mutex
		fatal    error
		lastErr  string // the error of the latest failures in a row
		streak   int    // items, not parts, that failed in a row
		lastItem = -1   // the item of the latest failure
		order    []int  // indexes sent to workers and not yet passed to fn
		waiting  = map[int]Result{}
		fnFailed bool
	)
	// ahead holds a slot for each index in order, so a slow item cannot
	// leave many finished results waiting with nothing on screen.
	ahead := make(chan struct{}, 2*max(workers, 1))
	// flush passes the waiting results to fn in input order. With all set,
	// it skips items that will never finish, such as after a cancel.
	flush := func(all bool) error {
		for len(order) > 0 {
			res, ok := waiting[order[0]]
			if !ok && !all {
				return nil
			}
			order = order[1:]
			<-ahead
			if !ok {
				continue
			}
			delete(waiting, res.Index)
			if fn != nil && !fnFailed {
				if err := fn(res); err != nil {
					fnFailed = true
					return err
				}
			}
		}
		return nil
	}
	stop := func(err error) {
		mu.Lock()
		if fatal == nil {
			fatal = err
		}
		mu.Unlock()
		cancel()
	}
	record := func(res Result) {
		mu.Lock()
		defer mu.Unlock()
		line, err := json.Marshal(res)
		if err == nil {
			_, err = out.Write(append(line, '\n'))
		}
		if err == nil {
			status[res.Index] = res.Status
			r.count(status)
			err = r.save()
		}
		if err == nil {
			waiting[res.Index] = res
			err = flush(false)
		}
		item := res.Index // the index of the item's first part
		if res.Part != nil {
			item -= res.Part.N - 1
		}
		switch {
		case res.Status == "complete":
			streak = 0
		case item == lastItem:
			// Another part of an item that already failed.
		case res.Error == lastErr:
			streak++
		default:
			lastErr, streak = res.Error, 1
		}
		if res.Status != "complete" {
			lastItem = item
		}
		if err == nil && streak >= RepeatLimit {
			err = &RepeatedError{Err: lastErr, Count: streak}
		}
		if err != nil && fatal == nil {
			fatal = err
			cancel()
		}
	}

	queue := make(chan input)
	var wg sync.WaitGroup
	for range max(workers, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for in := range queue {
				res, err := r.evaluate(ctx, client, questions, in)
				if ctx.Err() != nil {
					continue // canceled: leave the item for resume
				}
				var fe *FatalError
				if errors.As(err, &fe) {
					stop(fe)
					continue
				}
				record(res)
			}
		}()
	}
	readErr := readLines(filepath.Join(r.Dir, "inputs.jsonl"), func(line []byte) error {
		var in input
		if err := json.Unmarshal(line, &in); err != nil {
			return err
		}
		if done[in.Index] {
			return nil
		}
		select {
		case ahead <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		mu.Lock()
		order = append(order, in.Index)
		mu.Unlock()
		select {
		case queue <- in:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	close(queue)
	wg.Wait()
	if err := flush(true); err != nil && fatal == nil {
		fatal = err
	}

	r.settle()
	if err := r.save(); err != nil {
		return err
	}
	if fatal != nil {
		return fatal
	}
	if readErr != nil && !errors.Is(readErr, context.Canceled) {
		return readErr
	}
	return context.Cause(ctx)
}

// settle sets the status of a run that is not executing from how many
// items it answered.
func (r *Run) settle() {
	switch {
	case r.Complete == r.Total:
		r.Status = Complete
	case r.Complete+r.Failed == r.Total:
		r.Status = Partial
	default:
		r.Status = Interrupted
	}
}

func (r *Run) count(status map[int]string) {
	r.Complete, r.Failed = 0, 0
	for _, s := range status {
		switch s {
		case "complete":
			r.Complete++
		case "failed":
			r.Failed++
		}
	}
}

// questionSet is a run's questions, decoded and as sent.
type questionSet struct {
	decoded map[string]decide.Question
	sent    map[string][]byte
}

// evaluate answers one input. With a cache, it asks only the questions
// whose answers the cache doesn't hold, and sends nothing when it holds
// them all.
func (r *Run) evaluate(ctx context.Context, client *decide.Client, questions questionSet, in input) (Result, error) {
	res := Result{Index: in.Index, Source: in.Source, Part: in.Part, Input: in.Value, Status: "failed"}
	req := decide.NewRequest(in.State)
	var image []byte
	if in.Image != "" {
		data, err := os.ReadFile(filepath.Join(r.Dir, in.Image))
		if err == nil {
			var im cloudflare.Image
			if im, err = cloudflare.NewImage(in.ContentType, data); err == nil {
				err = cloudflare.SetImages(req, im)
			}
		}
		if err != nil {
			res.Error = err.Error()
			return res, nil
		}
		image = data
	}
	if r.cache == nil {
		req.Questions = questions.decoded
		return r.ask(ctx, client, req, res)
	}
	state, err := json.Marshal(req.State)
	if err != nil {
		res.Error = err.Error()
		return res, nil
	}
	it := cache.ItemOf(state, in.ContentType, image)
	cached := map[string]json.RawMessage{}
	if !r.NoCache {
		for key, a := range r.cache.Lookup(r.source, it, questions.sent) {
			if ValidAnswer(questions.decoded[key], a) {
				cached[key] = a
			}
		}
	}
	// A response that names a newer version than cached answers came from
	// makes them stale: those questions are asked again, once.
	var out Result
	answers := map[string]json.RawMessage{}
	for round := range 2 {
		req.Questions = map[string]decide.Question{}
		for key, q := range questions.decoded {
			_, hit := cached[key]
			if _, got := answers[key]; !hit && !got {
				req.Questions[key] = q
			}
		}
		if len(req.Questions) == 0 {
			break
		}
		o, err := r.ask(ctx, client, req, res)
		if err != nil {
			return o, err
		}
		if o.Status != "complete" {
			r.cache.Store(r.source, it, questions.sent, nil, o.Model)
			return o, nil
		}
		r.cache.Store(r.source, it, questions.sent, o.Answers, o.Model)
		if round == 0 {
			out = o
		}
		maps.Copy(answers, o.Answers)
		if round == 0 && o.Model != "" && len(cached) > 0 {
			sent := map[string][]byte{}
			for key := range cached {
				sent[key] = questions.sent[key]
			}
			still := r.cache.Lookup(r.source, it, sent)
			for key := range cached {
				if _, ok := still[key]; !ok {
					delete(cached, key)
				}
			}
		}
	}
	if len(answers) == 0 {
		res.Status, res.Answers, res.Cached = "complete", cached, slices.Sorted(maps.Keys(cached))
		return res, nil
	}
	maps.Copy(answers, cached)
	out.Answers = answers
	if len(cached) > 0 {
		out.Cached = slices.Sorted(maps.Keys(cached))
	}
	return out, nil
}

// ValidAnswer reports whether a kept answer is a valid answer to its
// question, as the client checks an answer it receives.
func ValidAnswer(q decide.Question, raw json.RawMessage) bool {
	a, err := decide.DecodeAnswer(raw)
	if err != nil || a.AnswerType() != q.QuestionType() {
		return false
	}
	if v, ok := q.(interface{ ValidateAnswer(decide.Answer) error }); ok {
		return v.ValidateAnswer(a) == nil
	}
	return true
}

// ask sends a request and records its answers in res.
func (r *Run) ask(ctx context.Context, client *decide.Client, req *decide.Request, res Result) (Result, error) {
	resp, err := client.SystemOne(ctx, req)
	if resp != nil {
		res.Model, res.RequestID = resp.Model, resp.RequestID
	}
	if err != nil {
		if errors.Is(err, decide.ErrAuth) {
			return res, &FatalError{err}
		}
		res.Error = err.Error()
		var api *decide.APIError
		if errors.As(err, &api) {
			res.Error = fmt.Sprintf("HTTP %d", api.StatusCode)
			if api.Message != "" {
				res.Error += ": " + api.Message
			}
			res.RequestID = api.RequestID
		}
		return res, nil
	}
	res.Answers = map[string]json.RawMessage{}
	for key, a := range resp.Answers {
		b, err := json.Marshal(a)
		if err != nil {
			res.Error = err.Error()
			return res, nil
		}
		res.Answers[key] = b
	}
	res.Status = "complete"
	return res, nil
}

// Pending reports how many items have no successful result.
func (r *Run) Pending() int { return r.Total - r.Complete }
