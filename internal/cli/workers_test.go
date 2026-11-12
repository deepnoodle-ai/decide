package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

// peakServer is a fake server that records the most requests it held at
// once. Each request waits long enough for the others to arrive.
type peakServer struct {
	fake           *decidetest.Server
	url            string
	mu             sync.Mutex
	now, peak      int
	failFirstImage bool
}

func newPeakServer(t *testing.T) *peakServer {
	t.Helper()
	p := &peakServer{fake: decidetest.NewServer(t, decidetest.AcceptMedia())}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.now++
		p.peak = max(p.peak, p.now)
		p.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
		p.fake.Handler().ServeHTTP(w, r)
		p.mu.Lock()
		p.now--
		p.mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	p.url = srv.URL
	return p
}

// run runs the CLI against the server and returns the peak since the last
// run.
func (p *peakServer) run(t *testing.T, args ...string) (output, int) {
	t.Helper()
	p.mu.Lock()
	p.peak = 0
	p.mu.Unlock()
	var stdout, stderr bytes.Buffer
	app := &App{
		Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		NewClient: func(provider, model string) (*decide.Client, error) {
			return p.fake.NewClientFor(p.url, nil, decide.WithModel(model))
		},
	}
	code := app.Run(context.Background(), args)
	p.mu.Lock()
	defer p.mu.Unlock()
	return output{stdout.String(), stderr.String(), code}, p.peak
}

// R-11: a run that holds a media file sends at most 2 requests at once by
// default, on run and on runs resume, whatever the template reads. A run
// of text sends 4.
func TestMixedRunDefaultsToTwoWorkers(t *testing.T) {
	h := setup(t)
	p := newPeakServer(t)
	h.write("notes.txt", "a\nb\nc\nd\ne\nf\ng\nh\n")
	h.write("shot.png", pngOf(t, 2, 2))

	out, peak := p.run(t, "run", "sentiment", "notes.txt", "--no-cache")
	if out.code != 0 || peak != 4 {
		t.Fatalf("text: exit %d, peak %d, want 4: %s", out.code, peak, out.stderr)
	}
	out, peak = p.run(t, "run", "sentiment", "notes.txt", "shot.png", "--provider", "gemini", "--no-cache")
	if out.code != 0 || peak != 2 {
		t.Fatalf("mixed: exit %d, peak %d, want 2: %s", out.code, peak, out.stderr)
	}
	out, peak = p.run(t, "run", "sentiment", "notes.txt", "shot.png", "--provider", "gemini", "--no-cache", "--workers", "4")
	if out.code != 0 || peak != 4 {
		t.Fatalf("--workers 4: exit %d, peak %d, want 4: %s", out.code, peak, out.stderr)
	}

	// Resume: the first requests fail, so several items are left, and the
	// resumed run of a text template with an image sends two at once.
	for range 9 {
		p.fake.FailNext(422)
	}
	out, _ = p.run(t, "run", "sentiment", "notes.txt", "shot.png", "--provider", "gemini", "--no-cache")
	if out.code != 1 {
		t.Fatalf("failing run: exit %d: %s", out.code, out.stderr)
	}
	p.fake.Reset() // the faults the run did not use
	out, peak = p.run(t, "runs", "resume")
	if out.code != 0 || peak != 2 {
		t.Fatalf("resume: exit %d, peak %d, want 2: %s", out.code, peak, out.stderr)
	}
}
