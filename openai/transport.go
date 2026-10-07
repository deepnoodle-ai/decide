package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/deepnoodle-ai/decide"
)

const (
	defaultBaseURL  = "https://api.openai.com/v1"
	requestIDHeader = "x-request-id"
	maxBodyBytes    = 32 << 20
	redacted        = "[REDACTED]"
)

// Config configures a Decisions API transport. It reads no environment
// variables.
type Config struct {
	APIKey     string
	BaseURL    string       // default https://api.openai.com/v1
	HTTPClient *http.Client // default http.DefaultTransport, no client timeout
	UserAgent  string       // optional product token, before decide-go/version
}

// String describes the config with its API key redacted.
func (c Config) String() string {
	return redactString(fmt.Sprintf("openai.Config{APIKey:%s BaseURL:%q}", redacted, c.BaseURL), c.APIKey)
}

// GoString describes the config with its API key redacted.
func (c Config) GoString() string { return c.String() }

// LogValue describes the config with its API key redacted.
func (c Config) LogValue() slog.Value { return slog.StringValue(c.String()) }

// Transport performs one Decisions API attempt. Its configuration is
// immutable after construction, and it is safe for concurrent use.
type Transport struct {
	apiKey, baseURL, userAgent string
	hc                         *http.Client
}

// NewTransport validates cfg and returns a transport. BaseURL replaces
// https://api.openai.com/v1, so requests go to BaseURL + "/decisions". It
// must be an absolute HTTP(S) URL without user information, a query, or a
// fragment.
func NewTransport(cfg Config) (*Transport, error) {
	if cfg.APIKey == "" {
		return nil, &transportError{cause: decide.ErrNoAPIKey,
			message: "decide: openai requires an API key: set openai.Config.APIKey"}
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" {
		return nil, fmt.Errorf("%w: openai BaseURL must be an absolute HTTP(S) URL without credentials, query, or fragment", decide.ErrInvalidRequest)
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Transport: http.DefaultTransport}
	}
	ua := "decide-go/" + decide.Version
	if cfg.UserAgent != "" {
		ua = cfg.UserAgent + " " + ua
	}
	return &Transport{apiKey: cfg.APIKey, baseURL: strings.TrimRight(base, "/"), userAgent: ua, hc: hc}, nil
}

// String describes the transport with its API key redacted.
func (t Transport) String() string {
	return redactString(fmt.Sprintf("openai.Transport{base_url:%q api_key:%s}", t.baseURL, redacted), t.apiKey)
}

// GoString describes the transport with its API key redacted.
func (t Transport) GoString() string { return t.String() }

// LogValue describes the transport with its API key redacted.
func (t Transport) LogValue() slog.Value { return slog.StringValue(t.String()) }

// ListModels returns errors.ErrUnsupported. OpenAI's model list names every
// model on the account, not the ones the Decisions API accepts.
func (t *Transport) ListModels(context.Context) (*decide.ModelList, error) {
	return nil, fmt.Errorf("%w: openai model listing", errors.ErrUnsupported)
}

// SystemOne sends one request to POST /v1/decisions. It does not retry or
// mutate req. Response.Raw contains the redacted Decisions API body, whose
// answers are a list; Response.Answers holds them by question key.
// Response.RequestID is OpenAI's x-request-id. Malformed or non-JSON
// response bodies are suppressed to protect credentials.
func (t *Transport) SystemOne(ctx context.Context, req *decide.Request) (*decide.Response, error) {
	body, err := encodeRequest(req)
	if err != nil {
		return nil, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/decisions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Authorization", "Bearer "+t.apiKey)
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "application/json")
	hreq.Header.Set("User-Agent", t.userAgent)
	hresp, err := t.hc.Do(hreq)
	if err != nil {
		return nil, t.safeError(err)
	}
	defer hresp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(hresp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, t.safeError(err)
	}
	if len(raw) > maxBodyBytes {
		return nil, fmt.Errorf("%w: openai response exceeds %d bytes", decide.ErrDecode, maxBodyBytes)
	}
	raw = t.redact(raw)
	header := hresp.Header.Clone()
	for k, vs := range header {
		for i, v := range vs {
			header[k][i] = redactString(v, t.apiKey)
		}
	}
	if hresp.StatusCode < 200 || hresp.StatusCode > 299 {
		return nil, apiError(hresp.StatusCode, header, raw)
	}
	native, err := decodeResponse(raw, req)
	if err != nil {
		return nil, err
	}
	resp, err := decide.DecodeResponse(native, req)
	if err != nil {
		return nil, fmt.Errorf("%w: openai response: %w", decide.ErrDecode, err)
	}
	resp.Raw, resp.Header = raw, header
	resp.RequestID = header.Get(requestIDHeader)
	return resp, nil
}

func (t *Transport) redact(raw []byte) []byte {
	// A structural parser cannot inspect strings beyond a syntax error.
	// Suppress the entire body instead of retaining potentially escaped secrets.
	if !json.Valid(raw) {
		return []byte("[REDACTED: invalid JSON response body]")
	}
	// Inspect decoded string tokens so alternate JSON escapes cannot reveal
	// the key. Copy every other byte verbatim, including number text and
	// object member order.
	decoder := jsontext.NewDecoder(bytes.NewReader(raw), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	var out bytes.Buffer
	last := 0
	for {
		before := int(decoder.InputOffset())
		tok, err := decoder.ReadToken()
		if err != nil {
			if err != io.EOF {
				return []byte("[REDACTED: invalid JSON response body]")
			}
			break
		}
		if tok.Kind() != '"' || !strings.Contains(tok.String(), t.apiKey) {
			continue
		}
		end := int(decoder.InputOffset())
		start := before + bytes.IndexByte(raw[before:end], '"')
		out.Write(raw[last:start])
		encoded, _ := json.Marshal(strings.ReplaceAll(tok.String(), t.apiKey, redacted))
		out.Write(encoded)
		last = end
	}
	if last != 0 {
		out.Write(raw[last:])
		raw = out.Bytes()
	}
	raw = bytes.ReplaceAll(raw, []byte(t.apiKey), []byte(redacted))
	encoded, _ := json.Marshal(t.apiKey)
	if len(encoded) > 2 {
		raw = bytes.ReplaceAll(raw, encoded[1:len(encoded)-1], []byte(redacted))
	}
	return raw
}

func redactString(s, key string) string {
	if key == "" {
		return s
	}
	s = strings.ReplaceAll(s, key, redacted)
	encoded, _ := json.Marshal(key)
	return strings.ReplaceAll(s, string(encoded[1:len(encoded)-1]), redacted)
}

// Keep the underlying error available for cancellation and retry decisions,
// but never format a network error that reflects the API key.
type transportError struct {
	cause   error
	message string
}

func (e *transportError) Error() string    { return e.message }
func (e *transportError) GoString() string { return e.message }
func (e *transportError) Unwrap() error    { return e.cause }

func (t *Transport) safeError(err error) error {
	message := redactString(err.Error(), t.apiKey)
	if message == err.Error() {
		return err
	}
	return &transportError{cause: err, message: message}
}

var _ decide.Transport = (*Transport)(nil)
