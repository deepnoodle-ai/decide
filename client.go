package decide

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// Client calls the TypeSafe API. It is safe for concurrent use: every field
// is set by NewClient and only read afterwards, and retry jitter uses the
// math/rand/v2 top-level functions, which are safe for concurrent use.
type Client struct {
	Models *ModelsService // set by NewClient, never reassigned

	transport      Transport
	model          string
	logger         *slog.Logger
	logBodies      bool
	maxRetries     int
	initialBackoff time.Duration
	maxBackoff     time.Duration
	attemptTimeout time.Duration
	validate       bool
}

const defaultModel = "jev-latest"

// Version is the version of this SDK. It is sent in the User-Agent and
// X-TypeSafe-SDK headers.
const Version = "0.0.0"

// NewClient returns a client. Each setting resolves from its option, then
// its environment variable (unless WithoutEnvironment), then the default:
//
//	API key        TYPESAFE_API_KEY        none: ErrNoAPIKey
//	Base URL       TYPESAFE_BASE_URL       https://api.typesafe.ai
//	Default model  TYPESAFE_DEFAULT_MODEL  jev-latest
//	Log level      TYPESAFE_LOG_LEVEL      silent
//
// Environment values are trimmed; blank means unset. TYPESAFE_LOG_LEVEL
// accepts debug, info, warn, error, or off, and is ignored when WithLogger
// is given.
func NewClient(opts ...ClientOption) (*Client, error) {
	cfg := defaultConfig()
	for _, o := range opts {
		if err := o(&cfg); err != nil {
			return nil, err
		}
	}
	env := func(name string) string {
		if cfg.noEnv {
			return ""
		}
		return strings.TrimSpace(os.Getenv(name))
	}
	c := &Client{
		logger:         cfg.logger,
		logBodies:      cfg.logBodies,
		maxRetries:     cfg.maxRetries,
		initialBackoff: cfg.initial,
		maxBackoff:     cfg.max,
		attemptTimeout: cfg.timeout,
		validate:       cfg.validate,
	}
	c.Models = &ModelsService{c: c}

	if cfg.model != nil {
		c.model = *cfg.model
	} else {
		c.model = cmp.Or(env("TYPESAFE_DEFAULT_MODEL"), defaultModel)
	}

	if c.logger == nil {
		l, err := loggerForLevel(env("TYPESAFE_LOG_LEVEL"))
		if err != nil {
			return nil, err
		}
		c.logger = l
	}

	if cfg.transSet {
		if cfg.apiKey != nil || cfg.baseURL != nil || cfg.httpClient != nil || cfg.userAgent != nil {
			return nil, errors.New("decide: WithTransport cannot be combined with WithAPIKey, WithBaseURL, WithHTTPClient, or WithUserAgent")
		}
		c.transport = cfg.transport
		return c, nil
	}

	var tc HTTPTransportConfig
	if cfg.apiKey != nil {
		tc.APIKey = *cfg.apiKey
	} else {
		tc.APIKey = env("TYPESAFE_API_KEY")
	}
	if tc.APIKey == "" {
		return nil, ErrNoAPIKey
	}
	if cfg.baseURL != nil {
		tc.BaseURL = *cfg.baseURL
		if tc.BaseURL == "" {
			return nil, errors.New("decide: WithBaseURL with empty URL")
		}
	} else {
		tc.BaseURL = env("TYPESAFE_BASE_URL")
	}
	tc.HTTPClient = cfg.httpClient
	if cfg.userAgent != nil {
		tc.UserAgent = *cfg.userAgent
	}
	t, err := NewHTTPTransport(tc)
	if err != nil {
		return nil, err
	}
	c.transport = t
	return c, nil
}

func loggerForLevel(v string) (*slog.Logger, error) {
	var level slog.Level
	switch strings.ToLower(v) {
	case "", "off":
		return slog.New(slog.DiscardHandler), nil
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		return nil, fmt.Errorf("decide: TYPESAFE_LOG_LEVEL %q must be debug, info, warn, error, or off", v)
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})), nil
}

// DefaultModel returns the model used when a request's Model is empty.
func (c *Client) DefaultModel() string { return c.model }

// String describes the client. It never includes the API key.
func (c *Client) String() string {
	return fmt.Sprintf("decide.Client{model: %s, transport: %s, max_retries: %d, attempt_timeout: %s, validate: %t}",
		c.model, describeTransport(c.transport), c.maxRetries, c.attemptTimeout, c.validate)
}

// GoString describes the client for %#v. It never includes the API key.
func (c *Client) GoString() string { return c.String() }

// LogValue implements slog.LogValuer. It never includes the API key.
func (c *Client) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("model", c.model),
		slog.String("transport", describeTransport(c.transport)),
		slog.Int("max_retries", c.maxRetries),
		slog.Duration("attempt_timeout", c.attemptTimeout),
		slog.Bool("validate", c.validate),
	)
}

// describeTransport prints a transport only through its own String method,
// which is the transport author's promise that it is safe to print. Any other
// transport is shown by type alone, because %v of a struct would print every
// field, including a key a custom transport holds.
func describeTransport(t Transport) string {
	if s, ok := t.(fmt.Stringer); ok {
		return s.String()
	}
	return fmt.Sprintf("%T", t)
}

// SystemOne sends req to POST /v1/systemone and returns the response.
//
// A nil or invalid request, an invalid CallOption, or a request that cannot
// be encoded returns an error wrapping ErrInvalidRequest without a network
// call. If req.Model is empty the client's default model is sent; req itself
// is never modified. Transient failures are retried.
//
// When validation is on (the default) and an answer does not match its
// question, SystemOne returns a non-nil response AND an
// *InvalidAnswersError, and resp.Invalid names the bad keys. Callers who
// treat any error as fatal stay safe; callers who want partial results use
// resp.Invalid or the handles, which fail only for the affected keys.
//
// If ctx ends, the error satisfies errors.Is(err, ctx.Err()) and, when an
// attempt had already failed, errors.As still reaches that attempt's
// *APIError.
func (c *Client) SystemOne(ctx context.Context, req *Request, opts ...CallOption) (*Response, error) {
	if req == nil {
		return nil, invalidRequest("request is nil")
	}
	var cc callConfig
	for _, o := range opts {
		o(&cc)
	}
	p := retryPolicy{maxRetries: c.maxRetries, attemptTimeout: c.attemptTimeout}
	validate := c.validate
	if cc.maxRetries != nil {
		if *cc.maxRetries < 0 {
			return nil, invalidRequest("WithCallMaxRetries must be >= 0")
		}
		p.maxRetries = *cc.maxRetries
	}
	if cc.attemptTimeout != nil {
		if *cc.attemptTimeout < 0 {
			return nil, invalidRequest("WithCallAttemptTimeout must be >= 0")
		}
		p.attemptTimeout = *cc.attemptTimeout
	}
	if cc.validate != nil {
		validate = *cc.validate
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	r := *req
	r.Model = cmp.Or(r.Model, c.model)
	// Encoding once up front catches unmarshalable values before any network
	// call. The bytes feed WithLogBodies; HTTPTransport encodes on its own.
	body, err := r.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	resp, err := withRetry(ctx, c, p, "systemone",
		func(ctx context.Context) (*Response, error) { return c.transport.SystemOne(ctx, &r) },
		func(attempt int, d time.Duration, resp *Response, err error) {
			c.logSystemOne(ctx, &r, body, attempt, d, resp, err)
		})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("%w: transport returned a nil response", ErrDecode)
	}
	if resp.Answers == nil {
		resp.Answers = map[string]Answer{}
	}
	if validate {
		invalid, verr := validateResponse(&r, resp)
		if verr != nil {
			resp.Invalid = invalid
			return resp, verr
		}
	}
	return resp, nil
}

func (c *Client) logSystemOne(ctx context.Context, r *Request, reqBody []byte, attempt int, d time.Duration, resp *Response, err error) {
	if !c.logger.Enabled(ctx, slog.LevelDebug) {
		return
	}
	var requestID string
	var respBody []byte
	if resp != nil {
		requestID, respBody = resp.RequestID, resp.Raw
	}
	attrs := []slog.Attr{
		slog.String("op", "systemone"),
		slog.Int("attempt", attempt),
		slog.Duration("duration", d),
		slog.String("model", r.Model),
		slog.Int("questions", len(r.Questions)),
	}
	attrs = append(attrs, outcomeAttrs(err, requestID)...)
	if resp != nil {
		attrs = append(attrs,
			slog.String("resolved_model", resp.Model),
			slog.Int("input_tokens", resp.Usage.InputTokens),
			slog.Int("output_tokens", resp.Usage.OutputTokens),
		)
	}
	if ae, ok := errors.AsType[*APIError](err); ok {
		respBody = ae.Body
	}
	if c.logBodies {
		attrs = append(attrs, slog.String("request_body", string(reqBody)), slog.String("response_body", string(respBody)))
	}
	c.logger.LogAttrs(ctx, slog.LevelDebug, "decide request", attrs...)
}

// outcomeAttrs returns status, request_id, and error for one attempt, each
// at most once even when a transport returns both a result and an error.
// status is the API status for an *APIError and 0 for a transport error; on
// success it is omitted, because transports do not report which 2xx they
// received. request_id comes from the *APIError, else from the result.
func outcomeAttrs(err error, requestID string) []slog.Attr {
	if err == nil {
		return []slog.Attr{slog.String("request_id", requestID)}
	}
	status := 0
	if ae, ok := errors.AsType[*APIError](err); ok {
		status, requestID = ae.StatusCode, ae.RequestID
	}
	return []slog.Attr{slog.Int("status", status), slog.String("request_id", requestID), slog.String("error", err.Error())}
}
