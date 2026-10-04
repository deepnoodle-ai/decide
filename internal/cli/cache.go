package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deepnoodle-ai/decide/internal/cache"
	"github.com/deepnoodle-ai/decide/internal/source"
	"github.com/deepnoodle-ai/decide/internal/template"
	"github.com/deepnoodle-ai/wonton/cli"
)

// openCache opens the answer cache for requests to a provider and model.
// It returns nil for Cloudflare: Workers AI names no model version, so
// decide could not tell when a cached answer went stale. With read false,
// answers are kept but not looked up.
func openCache(c *cli.Context, provider, model string, read bool) (*cache.Cache, *cache.Scope) {
	if provider != "typesafe" {
		return nil, nil
	}
	cc := cache.Open(filepath.Join(template.Home(), "cache"), func(msg string) {
		fmt.Fprintln(c.Stderr(), dim(clean(msg)))
	})
	return cc, cc.Scope(provider, address(provider), model, read)
}

// address is where requests to a provider go, as connect chooses it. An
// empty address is the provider's default.
func address(provider string) string {
	if provider == "typesafe" {
		return strings.TrimRight(strings.TrimSpace(os.Getenv("TYPESAFE_BASE_URL")), "/")
	}
	return ""
}

// cacheCount counts, for a dry run, the items whose every answer is in
// the cache.
type cacheCount struct {
	scope    *cache.Scope
	version  string
	sent     map[string][]byte
	cached   int
	disabled bool // --no-cache: nothing is looked up
}

func newCacheCount(scope *cache.Scope, s *template.Template, read bool) (*cacheCount, error) {
	questions, err := s.Decode()
	if err != nil {
		return nil, err
	}
	n := &cacheCount{scope: scope, sent: map[string][]byte{}, disabled: !read}
	for key, q := range questions {
		if n.sent[key], err = json.Marshal(q); err != nil {
			return nil, err
		}
	}
	// A version no longer trusted is still the best guess: a run checks it
	// with its first request.
	n.version, _ = scope.LastVersion()
	return n, nil
}

func (n *cacheCount) add(it source.Item) {
	if n.disabled || n.version == "" {
		return
	}
	var contentType string
	var image []byte
	if it.Image != nil {
		contentType, image = it.Image.ContentType, it.Image.Data
	}
	states := []json.RawMessage{it.State}
	if len(it.Parts) > 0 {
		states = states[:0]
		for _, p := range it.Parts {
			states = append(states, p.State)
		}
	}
	for _, st := range states {
		b, err := json.Marshal(st)
		if err != nil || !n.scope.Has(n.version, cache.ItemOf(b, contentType, image), n.sent) {
			return
		}
	}
	n.cached++
}
