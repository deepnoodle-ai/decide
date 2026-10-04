package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// Trust is how long a model version seen in a response is trusted to be
// what a model name resolves to. After that, a run's first request is sent
// to learn the version again.
const Trust = time.Hour

// refresh is how old the time a version was last seen may get before a
// response that names it again records it.
const refresh = 5 * time.Minute

// Scope is the part of a cache for requests to one provider, at one
// address, that name one model. It tracks which model version that name
// resolves to, since answers are kept by version. It is safe for
// concurrent use.
//
// A model name may be an alias, such as jev-latest, or an exact version,
// such as jev-1.13.0. Nothing in a name says which, so the scope learns it
// from responses: when a response names the same model the request named,
// the name is exact, it can't move to another version, and its record
// never goes stale. An alias's version is trusted for an hour after a
// response named it.
type Scope struct {
	c        *Cache
	provider string
	address  string
	name     string // the hash of provider, address and model name
	model    string
	read     bool

	wake     chan struct{} // closed when a probe ends
	mu       sync.Mutex
	version  string // the version answers are looked up by; empty until known
	probing  bool
	recorded time.Time // when the version was last recorded
}

// Scope returns the scope for a provider, its address and a model name.
// With read false, Lookup finds nothing, but answers are still kept.
func (c *Cache) Scope(provider, address, model string, read bool) *Scope {
	h := sha256.New()
	field(h, []byte("name"))
	field(h, []byte(provider))
	field(h, []byte(address))
	field(h, []byte(model))
	s := &Scope{c: c, provider: provider, address: address, name: hex.EncodeToString(h.Sum(nil)),
		model: model, read: read, wake: make(chan struct{})}
	if v, ok := c.version(s.name); ok && (v.model == model || now().Sub(v.seen) < Trust) {
		s.version, s.recorded = v.model, v.seen
	}
	return s
}

// LastVersion is the version the model name last resolved to, however long
// ago, and whether it is still trusted.
func (s *Scope) LastVersion() (version string, trusted bool) {
	v, ok := s.c.version(s.name)
	if !ok {
		return "", false
	}
	return v.model, v.model == s.model || now().Sub(v.seen) < Trust
}

// Key is the key of the answer to a question about an item from a model
// version. q is the question as sent, with its type and options.
func (s *Scope) Key(version string, it Item, question string, q []byte) Key {
	h := sha256.New()
	field(h, []byte("answer"))
	field(h, []byte(s.provider))
	field(h, []byte(s.address))
	field(h, []byte(version))
	field(h, it[:])
	field(h, []byte(question))
	field(h, q)
	var k Key
	copy(k[:], h.Sum(nil))
	return k
}

// Begin returns the version to look answers up by. When no trusted
// version is known, the first caller gets probe true and must send its
// request whole, then pass the response's model to End; other callers
// wait for it. With reads off, Begin returns no version and no probe.
func (s *Scope) Begin(ctx context.Context) (version string, probe bool, err error) {
	if !s.read {
		return "", false, nil
	}
	for {
		s.mu.Lock()
		if s.version != "" {
			v := s.version
			s.mu.Unlock()
			return v, false, nil
		}
		if !s.probing {
			s.probing = true
			s.mu.Unlock()
			return "", true, nil
		}
		wake := s.wake
		s.mu.Unlock()
		select {
		case <-wake:
		case <-ctx.Done():
			return "", false, ctx.Err()
		}
	}
}

// End records the model a response named, or "" when the request failed
// or named none. From then on, answers are looked up by that version. It
// ends a probe that Begin started.
func (s *Scope) End(probe bool, model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if model != "" && (model != s.version || now().Sub(s.recorded) > refresh) {
		s.version, s.recorded = model, now()
		s.c.setVersion(s.name, version{model: model, seen: s.recorded})
	}
	if probe {
		s.probing = false
		close(s.wake)
		s.wake = make(chan struct{})
	}
}

// Lookup returns the kept answers to questions about an item from a model
// version. questions maps each question's key to the question as sent.
func (s *Scope) Lookup(version string, it Item, questions map[string][]byte) map[string]json.RawMessage {
	if !s.read || version == "" {
		return nil
	}
	found := map[string]json.RawMessage{}
	for key, q := range questions {
		if a, ok := s.c.Get(s.Key(version, it, key, q)); ok {
			found[key] = a
		}
	}
	return found
}

// Has reports whether every question about an item has a kept answer
// from a model version.
func (s *Scope) Has(version string, it Item, questions map[string][]byte) bool {
	for key, q := range questions {
		if !s.c.Has(s.Key(version, it, key, q)) {
			return false
		}
	}
	return true
}

// Store keeps the answers to questions about an item from a model version.
func (s *Scope) Store(version string, it Item, questions map[string][]byte, answers map[string]json.RawMessage) {
	if version == "" {
		return
	}
	keep := map[Key]json.RawMessage{}
	for key, a := range answers {
		if q, ok := questions[key]; ok {
			keep[s.Key(version, it, key, q)] = a
		}
	}
	s.c.Put(keep)
}
