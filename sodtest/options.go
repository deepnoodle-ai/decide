package sodtest

import (
	"math"
	"strconv"
	"time"

	"github.com/deepnoodle-ai/sod"
)

// Option configures a Server.
type Option func(*Server)

// WithAPIKey sets the expected bearer key. Default "test-key-00000000".
func WithAPIKey(key string) Option { return func(s *Server) { s.apiKey = key } }

// WithResolvedModel sets the model name the server reports. Default
// "jev-1.13.0".
func WithResolvedModel(name string) Option { return func(s *Server) { s.resolved = name } }

// WithModels sets what GET /v1/models returns. Default: jev-latest and
// jev-preview.
func WithModels(models ...sod.Model) Option {
	return func(s *Server) { s.models = append([]sod.Model{}, models...) }
}

// fault is one queued failure response.
type fault struct {
	status    int
	headers   map[string]string
	body      []byte
	delay     time.Duration
	closeConn bool
}

// FaultOption configures a fault queued with FailNext.
type FaultOption func(*fault)

// RetryAfter sets Retry-After in whole seconds, rounded up.
func RetryAfter(d time.Duration) FaultOption {
	return func(f *fault) {
		f.headers["Retry-After"] = strconv.Itoa(int(math.Ceil(d.Seconds())))
	}
}

// RetryAfterMS sets retry-after-ms.
func RetryAfterMS(ms int) FaultOption {
	return func(f *fault) { f.headers["retry-after-ms"] = strconv.Itoa(ms) }
}

// FaultBody sets the response body. Default: a plausible body for the
// status.
func FaultBody(json []byte) FaultOption {
	return func(f *fault) { f.body = append([]byte(nil), json...) }
}

// FaultDelay waits d before writing the fault.
func FaultDelay(d time.Duration) FaultOption {
	return func(f *fault) { f.delay = d }
}
