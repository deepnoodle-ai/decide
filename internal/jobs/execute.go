package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/backend"
	"github.com/deepnoodle-ai/decide/cloudflare"
	"github.com/deepnoodle-ai/decide/internal/dataset"
)

var errBudget = errors.New("request ceiling reached")

type executor struct {
	d           definition
	s           Summary
	client      *decide.Client
	mu          sync.Mutex
	next        time.Time
	cancel      context.CancelFunc
	callback    func(Result) error
	fatal       error
	singleStage bool
	factory     func() (*decide.Client, error)
	clientOnce  sync.Once
	clientErr   error
}

func defaultClient(o Options) (*decide.Client, error) {
	cfg := backend.Config{Provider: backend.Provider(o.Provider), Model: o.Model, BaseURL: o.BaseURL, AccountID: o.AccountID}
	if o.Provider == "cloudflare" {
		cfg.APIKey = os.Getenv("CLOUDFLARE_AUTH_TOKEN")
	} else {
		cfg.APIKey = os.Getenv("TYPESAFE_API_KEY")
	}
	return backend.NewClient(cfg, decide.WithMaxRetries(0))
}

func Run(ctx context.Context, o Options, in io.Reader, fn func(Result) error) (Summary, error) {
	o, e := normalize(o)
	if e != nil {
		return Summary{}, e
	}
	d, e := resolve(o)
	if e != nil {
		return Summary{}, e
	}
	s, e := newRun(d)
	if e != nil {
		return s, e
	}
	unlock, e := lock(s.Path)
	if e != nil {
		return s, e
	}
	defer unlock()
	if collection(d) {
		return runCollection(ctx, d, s, fn)
	}
	e = spool(ctx, d, &s, in)
	if e != nil {
		s.Status = "preparation-incomplete"
		saveSummary(&s)
		return s, fmt.Errorf("preparation incomplete; restart from original sources: %w", safeError(e))
	}
	s.Status = "prepared"
	if e = saveSummary(&s); e != nil {
		return s, e
	}
	return execute(ctx, d, s, fn, false, false)
}

func spoolItem(d definition, s *Summary, it dataset.Item) error {
	p, e := prepared(it, d)
	if e != nil {
		return e
	}
	rec := input{Prepared: p}
	for _, im := range it.Images {
		digest := imageDigest(im.Data)
		if e = os.WriteFile(filepath.Join(s.Path, "assets", digest), im.Data, 0600); e != nil {
			return e
		}
		rec.Assets = append(rec.Assets, asset{digest, im.ContentType})
	}
	rec.Prepared.Item.Images = nil
	if e = writeJSON(itemPath(s.Path, "inputs", s.Items), rec); e == nil {
		s.Items++
	}
	return e
}

func Resume(ctx context.Context, id string, o Options, retryFailed, retryUncertain bool, fn func(Result) error) (Summary, error) {
	s, e := Show(id, o.RunDir)
	if e != nil {
		return s, e
	}
	s.Path = pathFor(id, o.RunDir)
	if s.Status == "preparing" || s.Status == "preparation-incomplete" {
		return s, errors.New("preparation incomplete: the input snapshot is missing; restart from the original sources")
	}
	unlock, e := lock(s.Path)
	if e != nil {
		return s, e
	}
	defer unlock()
	var d definition
	if e = readJSON(filepath.Join(s.Path, "config.json"), &d); e != nil {
		return s, e
	}
	if d.Version != 1 {
		return s, errors.New("unsupported run configuration version")
	}
	if e = validateConnection(d.Options); e != nil {
		return s, e
	}
	d.Options.NewClient = o.NewClient
	if o.Workers > 0 {
		d.Options.Workers = o.Workers
	}
	if o.MaxRequests > 0 && d.Options.MaxRequests > 0 && o.MaxRequests > d.Options.MaxRequests {
		d.Options.MaxRequests = o.MaxRequests
		if e = writeJSON(filepath.Join(s.Path, "config.json"), d); e != nil {
			return s, e
		}
	}
	if d.Options.Snapshot == "refs" {
		if e = verifyRefs(ctx, s); e != nil {
			return s, e
		}
	}
	if collection(d) {
		return s, errors.New("saved-evidence transformations complete locally; restart the transformation to retry")
	}
	return execute(ctx, d, s, fn, retryFailed, retryUncertain)
}

func execute(ctx context.Context, d definition, s Summary, fn func(Result) error, retryFailed, retryUncertain bool) (Summary, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ex := &executor{d: d, s: s, cancel: cancel, callback: fn}
	ex.s.Status = "running"
	if e := ex.reconcile(); e != nil {
		return ex.s, e
	}
	if e := saveSummary(&ex.s); e != nil {
		return ex.s, e
	}
	factory := d.Options.NewClient
	if factory == nil {
		factory = func() (*decide.Client, error) { return defaultClient(d.Options) }
	}
	// Empty datasets and recovered successful attempts need no credentials.
	ex.factory = factory
	if hasLimits(d) {
		ex.runLimitedFunnel(ctx, factory, retryFailed, retryUncertain)
	} else {
		queue := make(chan int)
		var wg sync.WaitGroup
		for w := 0; w < d.Options.Workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range queue {
					if ctx.Err() != nil {
						continue
					}
					var old Result
					e := readJSON(itemPath(s.Path, "results", i), &old)
					if e == nil {
						if old.Status == "complete" || old.Status == "dropped" || (old.Status == "failed" && !retryFailed) || (old.Status == "uncertain" && !retryUncertain) {
							continue
						}
					} else if !os.IsNotExist(e) {
						ex.fail(e)
						continue
					}

					var rec input
					if e = readJSON(itemPath(s.Path, "inputs", i), &rec); e != nil {
						ex.fail(e)
						continue
					}
					r := ex.process(ctx, i, rec, old)
					if e = ex.storeResult(r); e != nil {
						ex.fail(e)
					}
					if r.Status == "failed" && d.Options.OnError == "stop" {
						ex.fail(errors.New("execution stopped after an item failed"))
					}
				}
			}()
		}
	loop:
		for i := 0; i < s.Items; i++ {
			select {
			case queue <- i:
			case <-ctx.Done():
				break loop
			}
		}
		close(queue)
		wg.Wait()

	}
	ex.mu.Lock()
	if ex.fatal != nil {
		ex.s.Status = "stopped"
	} else if ctx.Err() != nil {
		ex.s.Status = "stopped"
	} else if ex.s.Completed+ex.s.Dropped+ex.s.Failed+ex.s.Uncertain < ex.s.Items {
		ex.s.Status = "limited"
	} else if ex.s.Failed+ex.s.Uncertain > 0 {
		ex.s.Status = "partial"
	} else {
		ex.s.Status = "complete"
	}
	e := saveSummary(&ex.s)
	result := ex.s
	fatal := ex.fatal
	ex.mu.Unlock()
	if d.Options.Order == "input" && fn != nil {
		if err := ReadResults(s.Path, "", fn); err != nil && fatal == nil {
			fatal = err
		}
	}
	if fatal != nil {
		return result, fatal
	}
	if e != nil {
		return result, e
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, nil
}

func (e *executor) fail(err error) {
	e.mu.Lock()
	if e.fatal == nil {
		e.fatal = err
	}
	e.mu.Unlock()
	e.cancel()
}

func (e *executor) reconcile() error {
	e.s.Completed = 0
	e.s.Failed = 0
	e.s.Dropped = 0
	e.s.Uncertain = 0
	e.s.InputTokens = 0
	e.s.OutputTokens = 0
	e.s.Requests = 0

	checkpoints := filepath.Join(e.s.Path, "last-attempts")
	if err := os.MkdirAll(checkpoints, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(e.s.Path, "attempts.jsonl"), os.O_RDONLY, 0600)
	if err == nil {
		dec := json.NewDecoder(f)
		var offset int64
		for {
			var a attempt
			err = dec.Decode(&a)
			if err == io.EOF {
				break
			}
			if err == io.ErrUnexpectedEOF {
				f.Close()
				if err = os.Truncate(filepath.Join(e.s.Path, "attempts.jsonl"), offset); err != nil {
					return err
				}
				break
			}
			if err != nil {
				f.Close()
				return err
			}
			offset = dec.InputOffset()
			if a.Status == "started" {
				e.s.Requests++
			}
			if a.Status == "finished" {
				e.s.InputTokens += a.Evidence.InputTokens
				e.s.OutputTokens += a.Evidence.OutputTokens
			}
			if err = writeJSON(itemPath(e.s.Path, "last-attempts", a.Index), a); err != nil {
				f.Close()
				return err
			}
		}
		f.Close()
	} else if !os.IsNotExist(err) {
		return err
	}
	for i := 0; i < e.s.Items; i++ {
		var r Result
		if readJSON(itemPath(e.s.Path, "results", i), &r) != nil || r.Status != "uncertain" {
			continue
		}
		var latest attempt
		err = readJSON(itemPath(e.s.Path, "last-attempts", i), &latest)
		if os.IsNotExist(err) {
			r.Status = "pending"
			r.Error = ""
		} else if err != nil {
			return err
		} else if latest.Status == "finished" && latest.Evidence.Error == "" {
			r.Status = "pending"
			r.Error = ""
			r.Stages = append(r.Stages, latest.Evidence)
		} else if latest.Status == "finished" && latest.Evidence.Error != "transport outcome unknown; explicit retry required" {
			r.Status = "failed"
			r.Error = latest.Evidence.Error
			r.Stages = append(r.Stages, latest.Evidence)
		}
		if err = writeJSON(itemPath(e.s.Path, "results", i), r); err != nil {
			return err
		}
	}

	return ReadResults(e.s.Path, "", func(r Result) error { e.count(r, 1); return nil })
}

func (e *executor) count(r Result, n int) {
	switch r.Status {
	case "complete":
		e.s.Completed += n
	case "failed":
		e.s.Failed += n
	case "dropped":
		e.s.Dropped += n
	case "uncertain":
		e.s.Uncertain += n
	}
}

func (e *executor) storeResult(r Result) error {
	var err error
	r, err = cleanValue(r)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var old Result
	if readJSON(itemPath(e.s.Path, "results", r.Index), &old) == nil {
		e.count(old, -1)
	}
	if err := writeJSON(itemPath(e.s.Path, "results", r.Index), r); err != nil {
		return err
	}
	e.count(r, 1)
	if err := saveSummary(&e.s); err != nil {
		return err
	}
	if e.d.Options.Order == "completion" && e.callback != nil && r.Status != "staged" && r.Status != "pending" {
		return e.callback(r)
	}
	return nil
}

func (e *executor) admit(ctx context.Context, index int, stage string, number int, r Result) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.d.Options.MaxRequests > 0 && e.s.Requests >= e.d.Options.MaxRequests {
		return errBudget
	}
	if e.d.Options.RateLimit > 0 {
		wait := time.Until(e.next)
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		e.next = time.Now().Add(time.Duration(float64(time.Second) / e.d.Options.RateLimit))
	}
	var old Result
	if readJSON(itemPath(e.s.Path, "results", index), &old) == nil {
		e.count(old, -1)
	}
	r.Status = "uncertain"
	r.Error = "request submitted without a recorded outcome; retry requires --retry-uncertain"
	if err := writeJSON(itemPath(e.s.Path, "results", index), r); err != nil {
		return err
	}
	if err := appendAttempt(e.s.Path, attempt{Index: index, Stage: stage, Number: number, Status: "started", Time: time.Now().UTC()}); err != nil {
		return err
	}
	e.s.Requests++
	e.count(r, 1)
	return saveSummary(&e.s)
}

func (e *executor) call(ctx context.Context, req *decide.Request, index int, name string, r Result) (*decide.Response, Evidence, string, error) {
	ev := Evidence{Name: name, Model: req.Model, State: mustJSON(req.State), Questions: rawQuestions(req.Questions)}
	e.clientOnce.Do(func() { e.client, e.clientErr = e.factory() })
	if e.clientErr != nil {
		err := safeError(e.clientErr)
		e.fail(err)
		return nil, ev, "pending", err
	}
	for n := 0; n <= e.d.Options.Retries; n++ {
		ev = Evidence{Name: name, Model: req.Model, State: mustJSON(req.State), Questions: rawQuestions(req.Questions)}
		if err := e.admit(ctx, index, name, n, r); err != nil {
			return nil, ev, "pending", err
		}
		callctx := ctx
		cancel := func() {}
		if e.d.Options.RequestTimeout > 0 {
			callctx, cancel = context.WithTimeout(ctx, e.d.Options.RequestTimeout)
		}
		resp, err := e.client.SystemOne(callctx, req, decide.WithCallMaxRetries(0))
		cancel()
		if resp != nil {
			ev.Model = resp.Model
			ev.RequestID = sanitize(resp.RequestID)
			ev.Response = safeJSONResponse(resp)
			ev.InputTokens = resp.Usage.InputTokens
			ev.OutputTokens = resp.Usage.OutputTokens
		}
		if err != nil {
			ev.Error = safeError(err).Error()
			if resp == nil {
				var ae *decide.APIError
				if !errors.As(err, &ae) && !errors.Is(err, decide.ErrInvalidRequest) && !errors.Is(err, decide.ErrDecode) {
					ev.Error = "transport outcome unknown; explicit retry required"
				}
			}
		}
		e.mu.Lock()
		pe := appendAttempt(e.s.Path, attempt{Index: index, Stage: name, Number: n, Status: "finished", Time: time.Now().UTC(), Evidence: ev})
		e.s.InputTokens += ev.InputTokens
		e.s.OutputTokens += ev.OutputTokens
		e.mu.Unlock()
		if pe != nil {
			return resp, ev, "uncertain", pe
		}
		if err == nil {
			return resp, ev, "complete", nil
		}
		var ae *decide.APIError
		if errors.As(err, &ae) {
			if (ae.StatusCode == 429 || ae.StatusCode >= 500) && n < e.d.Options.Retries {
				delay := time.Duration(math.Min(float64(n+1), 5)) * 100 * time.Millisecond
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return resp, ev, "failed", ctx.Err()
				case <-timer.C:
				}
				continue
			}
			return resp, ev, "failed", err
		}
		if errors.Is(err, decide.ErrInvalidAnswer) || errors.Is(err, decide.ErrInvalidRequest) || errors.Is(err, decide.ErrDecode) {
			return resp, ev, "failed", err
		}
		ev.Error = "transport outcome unknown; explicit retry required"
		return resp, ev, "uncertain", errors.New(ev.Error)
	}
	panic("unreachable")
}

func attachAssets(req *decide.Request, path string, assets []asset) error {
	if len(assets) == 0 {
		return nil
	}
	ims := []cloudflare.Image{}
	for _, a := range assets {
		if !validDigest(a.Digest) {
			return errors.New("invalid image asset digest")
		}
		b, e := os.ReadFile(filepath.Join(path, "assets", a.Digest))
		if e != nil {
			return e
		}
		if imageDigest(b) != a.Digest {
			return errors.New("image asset fingerprint changed")
		}
		im, e := cloudflare.NewImage(a.ContentType, b)
		if e != nil {
			return e
		}
		ims = append(ims, im)
	}
	return cloudflare.SetImages(req, ims...)
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// Persist only typed response fields; unmodeled provider fields can echo headers,
// authorization values or raw requests. Never persist provider error bodies.
func safeJSONResponse(r *decide.Response) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"model": sanitize(r.Model), "answers": r.Answers, "usage": map[string]int{"input_tokens": r.Usage.InputTokens, "output_tokens": r.Usage.OutputTokens}})
	return json.RawMessage(sanitize(string(b)))
}

var userInfoPattern = regexp.MustCompile(`(?i)(https?://)[^/\s"<>]+@`)

func sanitize(s string) string {
	s = userInfoPattern.ReplaceAllString(s, "${1}[REDACTED]@")
	for _, key := range []string{"TYPESAFE_API_KEY", "CLOUDFLARE_AUTH_TOKEN"} {
		secret := os.Getenv(key)
		if secret == "" {
			continue
		}
		s = strings.ReplaceAll(s, secret, "[REDACTED]")
		b, _ := json.Marshal(secret)
		if len(b) > 2 {
			s = strings.ReplaceAll(s, string(b[1:len(b)-1]), "[REDACTED]")
		}
	}
	return s
}

func safeError(err error) error {
	if err == nil {
		return nil
	}
	var ae *decide.APIError
	if errors.As(err, &ae) {
		return fmt.Errorf("provider HTTP %d", ae.StatusCode)
	}
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, decide.ErrInvalidRequest):
		return errors.New("invalid decision request")
	case errors.Is(err, decide.ErrInvalidAnswer):
		return errors.New("provider returned invalid answers")
	case errors.Is(err, decide.ErrDecode):
		return errors.New("provider response could not be decoded")
	}
	return errors.New(sanitize(err.Error()))
}

func cleanValue[T any](v T) (T, error) {
	var clean T
	b, err := json.Marshal(v)
	if err != nil {
		return clean, err
	}
	err = json.Unmarshal([]byte(sanitize(string(b))), &clean)
	return clean, err
}

func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
