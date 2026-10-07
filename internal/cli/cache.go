package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/internal/cache"
	"github.com/deepnoodle-ai/decide/internal/runs"
	"github.com/deepnoodle-ai/decide/internal/source"
	"github.com/deepnoodle-ai/decide/internal/template"
	"github.com/deepnoodle-ai/wonton/cli"
)

// openCache opens the answer cache, and names the source of a run's
// answers: the provider, its address and the model name.
func openCache(c *cli.Context, provider, model string) (*cache.Cache, cache.Source) {
	cc := cache.Open(filepath.Join(template.Home(), "cache"), func(msg string) {
		fmt.Fprintln(c.Stderr(), dim(clean(msg)))
	})
	return cc, cache.Source{Provider: provider, Address: address(provider), Model: model}
}

// address is where requests to a provider go, as connect chooses it: the
// base URL, empty for the provider's default, and for Cloudflare the
// account.
func address(provider string) string {
	base := func(name string) string { return strings.TrimRight(strings.TrimSpace(os.Getenv(name)), "/") }
	switch provider {
	case "cloudflare":
		return strings.TrimSpace(os.Getenv("CLOUDFLARE_ACCOUNT_ID")) + " " + base("CLOUDFLARE_BASE_URL")
	case "openai":
		return base("OPENAI_BASE_URL")
	}
	return base("TYPESAFE_BASE_URL")
}

// cacheCount counts, for a dry run, the answers the cache holds and the
// answers left to ask. An answer to an item judged in parts is in the
// cache when every part's is.
type cacheCount struct {
	cache    *cache.Cache
	source   cache.Source
	sent     map[string][]byte
	decoded  map[string]decide.Question
	cached   int
	toAsk    int
	disabled bool // --no-cache: nothing is looked up
}

func newCacheCount(cc *cache.Cache, src cache.Source, s *template.Template, read bool) (*cacheCount, error) {
	questions, err := s.Decode()
	if err != nil {
		return nil, err
	}
	n := &cacheCount{cache: cc, source: src, sent: map[string][]byte{}, decoded: questions, disabled: !read}
	for key, q := range questions {
		if n.sent[key], err = json.Marshal(q); err != nil {
			return nil, err
		}
	}
	return n, nil
}

func (n *cacheCount) add(it source.Item) {
	if n.disabled {
		n.toAsk += len(n.sent)
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
	items := make([]cache.Item, 0, len(states))
	for _, st := range states {
		b, err := json.Marshal(st)
		if err != nil {
			n.toAsk += len(n.sent)
			return
		}
		items = append(items, cache.ItemOf(b, contentType, image))
	}
	// Count an answer only if the run would use it: present and valid
	// for every part.
	hits := map[string]int{}
	for _, ci := range items {
		for key, a := range n.cache.Lookup(n.source, ci, n.sent) {
			if runs.ValidAnswer(n.decoded[key], a) {
				hits[key]++
			}
		}
	}
	for key := range n.sent {
		if hits[key] == len(items) {
			n.cached++
		} else {
			n.toAsk++
		}
	}
}
