// Package backend selects a decision provider during decide.Client construction.
// Requests, typed handles, retries, and answer validation use the same decide
// API for every provider.
//
// Config supplies connection settings explicitly. NewClient reads no
// environment variables, so a TypeSafe environment cannot affect Cloudflare
// or vice versa. Applications can load configuration from their own source.
package backend

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/cloudflare"
	"github.com/deepnoodle-ai/decide/openai"
)

// Provider identifies the service used by a client.
type Provider string

const (
	// TypeSafe uses the TypeSafe System One API. The default model is jev-latest.
	TypeSafe Provider = "typesafe"
	// Cloudflare uses Workers AI. The default model is clef.
	Cloudflare Provider = "cloudflare"
	// OpenAI uses the Decisions API, in beta. The default model is gpt-6-luna.
	OpenAI Provider = "openai"
)

// Config selects a provider and its connection settings. APIKey is a
// TypeSafe API key, Cloudflare Workers AI API token, or OpenAI API key,
// depending on Provider.
// AccountID is required only for Cloudflare. Provider must be explicit.
type Config struct {
	Provider   Provider
	APIKey     string
	AccountID  string
	Model      string       // empty: "jev-latest", "clef", or "gpt-6-luna" by provider
	BaseURL    string       // empty: provider default
	HTTPClient *http.Client // nil: provider default
	UserAgent  string
}

// String describes cfg with its API key redacted.
func (c Config) String() string {
	s := fmt.Sprintf("backend.Config{Provider:%q APIKey:[REDACTED] AccountID:%q Model:%q BaseURL:%q}", c.Provider, c.AccountID, c.Model, c.BaseURL)
	if c.APIKey != "" {
		s = strings.ReplaceAll(s, c.APIKey, "[REDACTED]")
	}
	return s
}

// GoString describes cfg with its API key redacted.
func (c Config) GoString() string { return c.String() }

// LogValue describes cfg with its API key redacted.
func (c Config) LogValue() slog.Value { return slog.StringValue(c.String()) }

// NewClient builds a client for cfg.Provider. Changing Config selects the
// backend without changing request code. It reads no environment variables.
// opts configure shared client behavior, such as logging, retries, timeouts,
// and validation. Set connection settings through cfg, not through opts.
// WithModel can override cfg.Model. The selected transport cannot be replaced
// through opts; options that configure native HTTP settings return an error.
func NewClient(cfg Config, opts ...decide.ClientOption) (*decide.Client, error) {
	var transport decide.Transport
	var err error
	model := cfg.Model
	switch cfg.Provider {
	case TypeSafe:
		if cfg.AccountID != "" {
			return nil, fmt.Errorf("%w: backend AccountID is only for Cloudflare", decide.ErrInvalidRequest)
		}
		transport, err = decide.NewHTTPTransport(decide.HTTPTransportConfig{
			APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, HTTPClient: cfg.HTTPClient, UserAgent: cfg.UserAgent,
		})
		if model == "" {
			model = "jev-latest"
		}
	case Cloudflare:
		transport, err = cloudflare.NewTransport(cloudflare.Config{
			APIToken: cfg.APIKey, AccountID: cfg.AccountID, BaseURL: cfg.BaseURL,
			HTTPClient: cfg.HTTPClient, UserAgent: cfg.UserAgent,
		})
		if model == "" {
			model = "clef"
		}
	case OpenAI:
		if cfg.AccountID != "" {
			return nil, fmt.Errorf("%w: backend AccountID is only for Cloudflare", decide.ErrInvalidRequest)
		}
		transport, err = openai.NewTransport(openai.Config{
			APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, HTTPClient: cfg.HTTPClient, UserAgent: cfg.UserAgent,
		})
		if model == "" {
			model = "gpt-6-luna"
		}
	default:
		return nil, fmt.Errorf("%w: backend Provider must be typesafe, cloudflare, or openai", decide.ErrInvalidRequest)
	}
	if err != nil {
		return nil, err
	}
	all := []decide.ClientOption{decide.WithModel(model)}
	all = append(all, opts...)
	all = append(all, decide.WithoutEnvironment(), decide.WithTransport(transport))
	return decide.NewClient(all...)
}
