package sod_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"

	"github.com/deepnoodle-ai/sod"
)

// stubTransport is a Transport driven by functions, for tests that do not
// need HTTP.
type stubTransport struct {
	systemOne  func(ctx context.Context, req *sod.Request) (*sod.Response, error)
	listModels func(ctx context.Context) (*sod.ModelList, error)
}

func (s stubTransport) SystemOne(ctx context.Context, req *sod.Request) (*sod.Response, error) {
	return s.systemOne(ctx, req)
}

func (s stubTransport) ListModels(ctx context.Context) (*sod.ModelList, error) {
	return s.listModels(ctx)
}

// answering returns a stub transport whose response carries answers.
func answering(answers map[string]sod.Answer) stubTransport {
	return stubTransport{systemOne: func(context.Context, *sod.Request) (*sod.Response, error) {
		return &sod.Response{Model: "jev-1.13.0", Answers: answers}, nil
	}}
}

// decoding returns a stub transport that decodes body against the request,
// as HTTPTransport does.
func decoding(body string) stubTransport {
	return stubTransport{systemOne: func(_ context.Context, req *sod.Request) (*sod.Response, error) {
		return sod.DecodeResponse([]byte(body), req)
	}}
}

func stubClient(t *testing.T, tr sod.Transport, opts ...sod.ClientOption) *sod.Client {
	t.Helper()
	c, err := sod.NewClient(append([]sod.ClientOption{sod.WithTransport(tr)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// jsonEqual compares two JSON documents semantically.
func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatalf("unmarshal %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return bytes.Equal(xb, yb)
}

func compact(t *testing.T, b []byte) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		t.Fatalf("compact: %v", err)
	}
	return buf.String()
}

// logBuffer is a goroutine-safe buffer for capturing slog output.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// debugLogger returns a Debug-level text logger writing to a new buffer.
func debugLogger() (*slog.Logger, *logBuffer) {
	b := &logBuffer{}
	return slog.New(slog.NewTextHandler(b, &slog.HandlerOptions{Level: slog.LevelDebug})), b
}
