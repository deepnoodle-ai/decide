package decide

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
)

// Transport performs exactly one attempt of each API call. It must not
// retry, must be safe for concurrent use, and must not mutate req. req.Model
// is always set. Non-2xx outcomes return *APIError. Answers must be
// normalized to this package's answer types; dialect quirks stay inside.
//
// The interface is frozen: it will never gain a method, because that would
// break every implementation outside this package. A new endpoint arrives as
// an optional interface that the client type-asserts, returning an error
// wrapping errors.ErrUnsupported when the transport lacks it.
type Transport interface {
	SystemOne(ctx context.Context, req *Request) (*Response, error)
	ListModels(ctx context.Context) (*ModelList, error)
}

type attemptKey struct{}

// Attempt returns the zero-based attempt number the client is on, for
// transports that want to send a retry-count header. 0 outside the client.
func Attempt(ctx context.Context) int {
	n, _ := ctx.Value(attemptKey{}).(int)
	return n
}

func withAttempt(ctx context.Context, n int) context.Context {
	return context.WithValue(ctx, attemptKey{}, n)
}

const (
	defaultBaseURL  = "https://api.typesafe.ai"
	requestIDHeader = "x-typesafe-request-id"
	maxBodyBytes    = 32 << 20
	redacted        = "[REDACTED]"
)

// HTTPTransportConfig configures an HTTPTransport.
type HTTPTransportConfig struct {
	APIKey     string
	BaseURL    string       // default https://api.typesafe.ai
	HTTPClient *http.Client // default: new client with http.DefaultTransport, no Timeout
	UserAgent  string       // product token prepended to the SDK token
}

// String describes the config with the API key redacted.
func (c HTTPTransportConfig) String() string {
	return fmt.Sprintf("decide.HTTPTransportConfig{APIKey:%s BaseURL:%q UserAgent:%q}",
		keyState(c.APIKey), c.BaseURL, c.UserAgent)
}

// GoString describes the config with the API key redacted, for %#v.
func (c HTTPTransportConfig) GoString() string { return c.String() }

// LogValue implements slog.LogValuer with the API key redacted.
func (c HTTPTransportConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("api_key", keyState(c.APIKey)),
		slog.String("base_url", c.BaseURL),
		slog.String("user_agent", c.UserAgent),
	)
}

// HTTPTransport speaks TypeSafe's native HTTP API. It is safe for
// concurrent use.
type HTTPTransport struct {
	apiKey    string
	baseURL   string
	hc        *http.Client
	userAgent string
}

// NewHTTPTransport returns a transport for the native API. A missing API key
// is an error wrapping ErrNoAPIKey.
func NewHTTPTransport(cfg HTTPTransportConfig) (*HTTPTransport, error) {
	if cfg.APIKey == "" {
		return nil, ErrNoAPIKey
	}
	base, err := normalizeBaseURL(cmp.Or(cfg.BaseURL, defaultBaseURL))
	if err != nil {
		return nil, err
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Transport: http.DefaultTransport}
	}
	ua := "decide-go/" + Version
	if cfg.UserAgent != "" {
		ua = cfg.UserAgent + " " + ua
	}
	return &HTTPTransport{apiKey: cfg.APIKey, baseURL: base, hc: hc, userAgent: ua}, nil
}

// normalizeBaseURL requires an absolute http or https URL and removes
// trailing slashes, so a base with a path prefix works for compatible
// servers.
func normalizeBaseURL(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("decide: base URL %q must be an absolute http or https URL", s)
	}
	return strings.TrimRight(s, "/"), nil
}

// String describes the transport with the API key redacted. String,
// GoString, and LogValue have value receivers so that both HTTPTransport and
// *HTTPTransport print safely, including with %+v.
func (t HTTPTransport) String() string {
	return fmt.Sprintf("decide.HTTPTransport{base_url: %s, api_key: %s}", t.baseURL, keyState(t.apiKey))
}

// GoString describes the transport with the API key redacted, for %#v.
func (t HTTPTransport) GoString() string { return t.String() }

// LogValue implements slog.LogValuer with the API key redacted.
func (t HTTPTransport) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("base_url", t.baseURL),
		slog.String("api_key", keyState(t.apiKey)),
	)
}

func keyState(k string) string {
	if k == "" {
		return "unset"
	}
	return redacted
}

// SystemOne sends one POST /v1/systemone attempt. The body is decoded with
// DecodeResponse, so answers take the types of req's questions.
func (t *HTTPTransport) SystemOne(ctx context.Context, req *Request) (*Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	status, header, rb, err := t.do(ctx, http.MethodPost, "/v1/systemone", body)
	if err != nil {
		return nil, err
	}
	resp, err := DecodeResponse(rb, req)
	if err != nil {
		return nil, decodeError(status, header, err)
	}
	resp.RequestID = header.Get(requestIDHeader)
	resp.Header = header
	resp.Raw = rb
	return resp, nil
}

// ListModels sends one GET /v1/models attempt.
func (t *HTTPTransport) ListModels(ctx context.Context) (*ModelList, error) {
	status, header, rb, err := t.do(ctx, http.MethodGet, "/v1/models", nil)
	if err != nil {
		return nil, err
	}
	list := &ModelList{}
	if err := list.UnmarshalJSON(rb); err != nil {
		return nil, decodeError(status, header, err)
	}
	list.RequestID = header.Get(requestIDHeader)
	list.Header = header
	list.Raw = rb
	return list, nil
}

func decodeError(status int, header http.Header, err error) error {
	id := header.Get(requestIDHeader)
	if id != "" {
		return fmt.Errorf("%w: status %d (request_id=%s): %v", ErrDecode, status, id, err)
	}
	return fmt.Errorf("%w: status %d: %v", ErrDecode, status, err)
}

// do performs one request. It returns the status, headers, and redacted
// body for 2xx; an *APIError for other statuses; or a transport error. The
// body is read here so a per-attempt timeout on ctx covers it.
func (t *HTTPTransport) do(ctx context.Context, method, path string, body []byte) (int, http.Header, []byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	hreq, err := http.NewRequestWithContext(ctx, method, t.baseURL+path, rdr)
	if err != nil {
		return 0, nil, nil, err
	}
	h := hreq.Header
	h.Set("Authorization", "Bearer "+t.apiKey)
	h.Set("Accept", "application/json")
	if body != nil {
		h.Set("Content-Type", "application/json")
	}
	h.Set("User-Agent", t.userAgent)
	h.Set("X-TypeSafe-SDK", "decide-go/"+Version)
	h.Set("X-TypeSafe-Runtime", "go/"+runtime.Version()+" "+runtime.GOOS+"/"+runtime.GOARCH)
	if n := Attempt(ctx); n > 0 {
		h.Set("X-TypeSafe-Retry-Count", strconv.Itoa(n))
	}
	hresp, err := t.hc.Do(hreq)
	if err != nil {
		return 0, nil, nil, err
	}
	defer hresp.Body.Close()
	rb, err := io.ReadAll(io.LimitReader(hresp.Body, maxBodyBytes+1))
	if err != nil {
		return 0, nil, nil, err
	}
	if len(rb) > maxBodyBytes {
		return 0, nil, nil, fmt.Errorf("%w: body exceeds %d bytes", ErrDecode, maxBodyBytes)
	}
	// Redact every body, not only error bodies: 2xx bodies become
	// Response.Raw and response_body log attributes, which must not carry the
	// key either. A real response has no reason to contain it.
	rb = t.redact(rb)
	if hresp.StatusCode < 200 || hresp.StatusCode > 299 {
		return 0, nil, nil, newAPIError(hresp.StatusCode, hresp.Header, rb)
	}
	return hresp.StatusCode, hresp.Header, rb, nil
}

// minRedactLen is the shortest key that is substituted in bodies. Replacing
// a short or empty string would corrupt unrelated text, and such keys are
// not real TypeSafe keys.
const minRedactLen = 8

// redact replaces any occurrence of the API key in b.
func (t *HTTPTransport) redact(b []byte) []byte {
	if len(t.apiKey) < minRedactLen || !bytes.Contains(b, []byte(t.apiKey)) {
		return b
	}
	return bytes.ReplaceAll(b, []byte(t.apiKey), []byte(redacted))
}

var _ Transport = (*HTTPTransport)(nil)
