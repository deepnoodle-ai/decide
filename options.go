package decide

import (
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// ClientOption configures a Client. Options that receive invalid values make
// NewClient return an error.
type ClientOption func(*clientConfig) error

type clientConfig struct {
	apiKey     *string
	baseURL    *string
	model      *string
	httpClient *http.Client
	userAgent  *string
	transport  Transport
	transSet   bool
	logger     *slog.Logger
	logBodies  bool
	maxRetries int
	initial    time.Duration
	max        time.Duration
	timeout    time.Duration
	validate   bool
	noEnv      bool
}

func defaultConfig() clientConfig {
	return clientConfig{
		maxRetries: 2,
		initial:    500 * time.Millisecond,
		max:        5 * time.Second,
		timeout:    10 * time.Second,
		validate:   true,
	}
}

// WithAPIKey sets the API key. It overrides TYPESAFE_API_KEY. An empty key
// makes NewClient return ErrNoAPIKey.
func WithAPIKey(key string) ClientOption {
	return func(c *clientConfig) error { c.apiKey = &key; return nil }
}

// WithBaseURL sets the API base URL. It overrides TYPESAFE_BASE_URL. It must
// be an absolute http or https URL; a path prefix is kept.
func WithBaseURL(url string) ClientOption {
	return func(c *clientConfig) error { c.baseURL = &url; return nil }
}

// WithModel sets the default model, used when Request.Model is empty. It
// overrides TYPESAFE_DEFAULT_MODEL.
func WithModel(name string) ClientOption {
	return func(c *clientConfig) error {
		if name == "" {
			return errors.New("decide: WithModel with empty name")
		}
		c.model = &name
		return nil
	}
}

// WithHTTPClient sets the *http.Client used by the native transport. Its
// Timeout, if any, applies in addition to WithAttemptTimeout.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *clientConfig) error {
		if hc == nil {
			return errors.New("decide: WithHTTPClient(nil)")
		}
		c.httpClient = hc
		return nil
	}
}

// WithTransport replaces the native HTTP transport, for gateways and
// compatible servers. It cannot be combined with WithAPIKey, WithBaseURL,
// WithHTTPClient, or WithUserAgent, which configure the native transport.
// With it, TYPESAFE_API_KEY and TYPESAFE_BASE_URL are not read.
func WithTransport(t Transport) ClientOption {
	return func(c *clientConfig) error {
		if t == nil {
			return errors.New("decide: WithTransport(nil)")
		}
		c.transport, c.transSet = t, true
		return nil
	}
}

// WithLogger sets the logger. It overrides TYPESAFE_LOG_LEVEL. The default
// discards everything.
func WithLogger(l *slog.Logger) ClientOption {
	return func(c *clientConfig) error {
		if l == nil {
			return errors.New("decide: WithLogger(nil)")
		}
		c.logger = l
		return nil
	}
}

// WithLogBodies adds request and response bodies to Debug records. Default
// false. Headers are never logged.
func WithLogBodies(on bool) ClientOption {
	return func(c *clientConfig) error { c.logBodies = on; return nil }
}

// WithMaxRetries sets how many times a failed attempt is retried. n >= 0;
// default 2.
func WithMaxRetries(n int) ClientOption {
	return func(c *clientConfig) error {
		if n < 0 {
			return errors.New("decide: WithMaxRetries must be >= 0")
		}
		c.maxRetries = n
		return nil
	}
}

// WithRetryBackoff sets the initial and maximum backoff. Defaults 500ms, 5s.
func WithRetryBackoff(initial, max time.Duration) ClientOption {
	return func(c *clientConfig) error {
		if initial < 0 || initial > max {
			return errors.New("decide: WithRetryBackoff needs 0 <= initial <= max")
		}
		c.initial, c.max = initial, max
		return nil
	}
}

// WithAttemptTimeout sets the timeout for each attempt, which covers reading
// the body. Default 10s; 0 disables it. It bounds each attempt only; the
// overall bound for a call, including retries and sleeps, comes from ctx.
func WithAttemptTimeout(d time.Duration) ClientOption {
	return func(c *clientConfig) error {
		if d < 0 {
			return errors.New("decide: WithAttemptTimeout must be >= 0")
		}
		c.timeout = d
		return nil
	}
}

// WithUserAgent prepends a product token such as "myapp/1.2" to the
// User-Agent header.
func WithUserAgent(product string) ClientOption {
	return func(c *clientConfig) error { c.userAgent = &product; return nil }
}

// WithValidation turns answer validation on or off. Default true.
func WithValidation(on bool) ClientOption {
	return func(c *clientConfig) error { c.validate = on; return nil }
}

// WithoutEnvironment makes NewClient read no TYPESAFE_* environment
// variables, so only options and defaults apply.
func WithoutEnvironment() ClientOption {
	return func(c *clientConfig) error { c.noEnv = true; return nil }
}

// CallOption overrides a client setting for one SystemOne call. Invalid
// values make SystemOne return an error wrapping ErrInvalidRequest.
type CallOption func(*callConfig)

type callConfig struct {
	maxRetries     *int
	attemptTimeout *time.Duration
	validate       *bool
}

// WithCallMaxRetries sets the retry count for one call. n >= 0.
func WithCallMaxRetries(n int) CallOption {
	return func(c *callConfig) { c.maxRetries = &n }
}

// WithCallAttemptTimeout sets the per-attempt timeout for one call. d >= 0;
// 0 disables it.
func WithCallAttemptTimeout(d time.Duration) CallOption {
	return func(c *callConfig) { c.attemptTimeout = &d }
}

// WithCallValidation turns answer validation on or off for one call.
func WithCallValidation(on bool) CallOption {
	return func(c *callConfig) { c.validate = &on }
}
