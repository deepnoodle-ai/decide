package cloudflare

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

	"github.com/deepnoodle-ai/sod"
)

const (
	defaultBaseURL = "https://api.cloudflare.com/client/v4"
	maxBodyBytes   = 32 << 20
	redacted       = "[REDACTED]"
)

// Config configures a Workers AI transport. It reads no environment variables.
type Config struct {
	AccountID  string
	APIToken   string
	BaseURL    string       // default https://api.cloudflare.com/client/v4
	HTTPClient *http.Client // default http.DefaultTransport, no client timeout
	UserAgent  string       // optional product token, before sod-go/version
}

// String describes the config with its API token redacted.
func (c Config) String() string {
	return redactString(fmt.Sprintf("cloudflare.Config{AccountID:%q APIToken:%s BaseURL:%q}", c.AccountID, redacted, c.BaseURL), c.APIToken)
}

// GoString describes the config with its API token redacted.
func (c Config) GoString() string { return c.String() }

// LogValue describes the config with its API token redacted.
func (c Config) LogValue() slog.Value {
	return slog.StringValue(c.String())
}

// Transport performs one Workers AI attempt. Its configuration is immutable
// after construction, and it is safe for concurrent use.
type Transport struct {
	accountID, apiToken, baseURL, userAgent string
	hc                                      *http.Client
}

// NewTransport validates cfg and returns a transport. AccountID must contain
// only ASCII letters, digits, underscores, or hyphens. BaseURL must be an
// absolute HTTP(S) URL without user information, a query, or a fragment.
func NewTransport(cfg Config) (*Transport, error) {
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("%w: cloudflare requires APIToken", sod.ErrNoAPIKey)
	}
	if !identifier(cfg.AccountID, false) {
		return nil, fmt.Errorf("%w: cloudflare requires a valid AccountID", sod.ErrInvalidRequest)
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" {
		return nil, fmt.Errorf("%w: cloudflare BaseURL must be an absolute HTTP(S) URL without credentials, query, or fragment", sod.ErrInvalidRequest)
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Transport: http.DefaultTransport}
	}
	ua := "sod-go/" + sod.Version
	if cfg.UserAgent != "" {
		ua = cfg.UserAgent + " " + ua
	}
	return &Transport{accountID: cfg.AccountID, apiToken: cfg.APIToken,
		baseURL: strings.TrimRight(base, "/"), userAgent: ua, hc: hc}, nil
}

// String describes the transport with its API token redacted.
func (t Transport) String() string {
	return redactString(fmt.Sprintf("cloudflare.Transport{account_id:%q base_url:%q api_token:%s}", t.accountID, t.baseURL, redacted), t.apiToken)
}

// GoString describes the transport with its API token redacted.
func (t Transport) GoString() string { return t.String() }

// LogValue describes the transport with its API token redacted.
func (t Transport) LogValue() slog.Value { return slog.StringValue(t.String()) }

// ListModels returns errors.ErrUnsupported. This adapter does not advertise
// model availability for an account or synthesize a model catalog.
func (t *Transport) ListModels(context.Context) (*sod.ModelList, error) {
	return nil, fmt.Errorf("%w: cloudflare model listing", errors.ErrUnsupported)
}

// SystemOne sends one request to the model-specific Workers AI route.
// It does not retry or mutate req. req.Model must be clef or clef-flash.
// Response.Raw contains the redacted complete Workers AI envelope.
// Response.Extra["cloudflare"] contains envelope metadata without result.
// CF-Ray, when supplied, remains in Header; RequestID is not synthesized.
func (t *Transport) SystemOne(ctx context.Context, req *sod.Request) (*sod.Response, error) {
	if err := validate(req); err != nil {
		return nil, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("%w: cloudflare request: %w", sod.ErrInvalidRequest, err)
	}
	if len(body) > maxRequestBytes {
		return nil, fmt.Errorf("%w: cloudflare request exceeds 13 MiB", sod.ErrInvalidRequest)
	}
	endpoint := t.baseURL + "/accounts/" + t.accountID + "/ai/run/@cf/cloudflare/" + req.Model
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Authorization", "Bearer "+t.apiToken)
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
		return nil, fmt.Errorf("%w: cloudflare response exceeds %d bytes", sod.ErrDecode, maxBodyBytes)
	}
	raw = t.redact(raw)
	header := hresp.Header.Clone()
	for k, vs := range header {
		for i, v := range vs {
			header[k][i] = strings.ReplaceAll(v, t.apiToken, redacted)
		}
	}
	if hresp.StatusCode < 200 || hresp.StatusCode > 299 {
		return nil, providerError(hresp.StatusCode, header, raw, true)
	}
	var envelope struct {
		Success *bool           `json:"success"`
		Result  json.RawMessage `json:"result"`
		Errors  []ErrorDetail   `json:"errors"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Success == nil {
		return nil, fmt.Errorf("%w: cloudflare response has an invalid envelope", sod.ErrDecode)
	}
	if !*envelope.Success || len(envelope.Errors) != 0 {
		return nil, providerError(hresp.StatusCode, header, raw, false)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Result, &result); err != nil || result == nil {
		return nil, fmt.Errorf("%w: cloudflare result must be an object", sod.ErrDecode)
	}
	if !object(result["answers"]) {
		return nil, fmt.Errorf("%w: cloudflare result.answers must be an object", sod.ErrDecode)
	}
	resp, err := sod.DecodeResponse(envelope.Result, req)
	if err != nil {
		return nil, fmt.Errorf("%w: cloudflare result: %w", sod.ErrDecode, err)
	}
	var metadata map[string]json.RawMessage
	_ = json.Unmarshal(raw, &metadata) // envelope was already decoded successfully
	delete(metadata, "result")
	meta, _ := json.Marshal(metadata)
	if resp.Extra == nil {
		resp.Extra = make(map[string]json.RawMessage)
	}
	// Preserve a provider result's own field if it uses our metadata key.
	if _, collision := resp.Extra["cloudflare"]; !collision {
		resp.Extra["cloudflare"] = meta
	}
	resp.Raw, resp.Header = raw, header
	return resp, nil
}

func object(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '{'
}

func (t *Transport) redact(raw []byte) []byte {
	// Inspect decoded string tokens so alternate JSON escapes cannot reveal
	// the token after error/answer decoding. Copy every other byte verbatim,
	// including number text and object member order.
	// Match encoding/json's acceptance of duplicate names, invalid UTF-8,
	// and unpaired surrogates so scanning cannot stop before later secrets.
	decoder := jsontext.NewDecoder(bytes.NewReader(raw), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	var out bytes.Buffer
	last := 0
	for {
		before := int(decoder.InputOffset())
		tok, err := decoder.ReadToken()
		if err != nil {
			break
		}
		if tok.Kind() != '"' || !strings.Contains(tok.String(), t.apiToken) {
			continue
		}
		end := int(decoder.InputOffset())
		start := before + bytes.IndexByte(raw[before:end], '"')
		out.Write(raw[last:start])
		encoded, _ := json.Marshal(strings.ReplaceAll(tok.String(), t.apiToken, redacted))
		out.Write(encoded)
		last = end
	}
	if last != 0 {
		out.Write(raw[last:])
		raw = out.Bytes()
	}
	// Non-JSON bodies and malformed suffixes still receive literal redaction.
	raw = bytes.ReplaceAll(raw, []byte(t.apiToken), []byte(redacted))
	encoded, _ := json.Marshal(t.apiToken)
	if len(encoded) > 2 {
		raw = bytes.ReplaceAll(raw, encoded[1:len(encoded)-1], []byte(redacted))
	}
	return raw
}

func redactString(s, token string) string {
	if token == "" {
		return s
	}
	s = strings.ReplaceAll(s, token, redacted)
	encoded, _ := json.Marshal(token)
	return strings.ReplaceAll(s, string(encoded[1:len(encoded)-1]), redacted)
}

// Keep the underlying error available for cancellation and retry decisions,
// but never format a network error that reflects the authorization token.
type transportError struct {
	cause   error
	message string
}

func (e *transportError) Error() string    { return e.message }
func (e *transportError) GoString() string { return e.message }
func (e *transportError) Unwrap() error    { return e.cause }

func (t *Transport) safeError(err error) error {
	message := redactString(err.Error(), t.apiToken)
	if message == err.Error() {
		return err
	}
	return &transportError{cause: err, message: message}
}

var _ sod.Transport = (*Transport)(nil)
