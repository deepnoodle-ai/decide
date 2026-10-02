package dataset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	ignore "github.com/sabhiram/go-gitignore"
)

var errLimit = errors.New("dataset limit reached")

// Walk enumerates inputs deterministically and calls yield serially. JSONL is
// streamed; whole-file formats are bounded by MaxItemBytes. Sampling scans the
// selected inputs and retains only Sample items on disk, not in memory.
func Walk(ctx context.Context, opts Options, stdin io.Reader, yield func(Item) error) error {
	if opts.Limit < 0 || opts.Sample < 0 {
		return fmt.Errorf("limit and sample must be nonnegative")
	}
	if opts.MaxItemBytes == 0 {
		opts.MaxItemBytes = DefaultOptions().MaxItemBytes
	}
	if opts.MaxSourceBytes == 0 {
		opts.MaxSourceBytes = DefaultOptions().MaxSourceBytes
	}
	if opts.MaxItemBytes < 1 || opts.MaxSourceBytes < 1 || opts.MaxItemBytes > math.MaxInt64-2 || opts.MaxSourceBytes > math.MaxInt64-2 {
		return fmt.Errorf("byte limits must be positive and below %d", int64(math.MaxInt64-1))
	}
	for _, pattern := range append(append([]string{}, opts.Include...), opts.Exclude...) {
		if !doublestar.ValidatePattern(pattern) {
			return fmt.Errorf("invalid glob %q", pattern)
		}
	}
	sources, err := resolveSources(opts)
	if err != nil {
		return err
	}
	for _, src := range sources {
		if err := validateSource(src); err != nil {
			return err
		}
	}
	if len(sources) == 0 {
		return fmt.Errorf("no sources selected; supply a path, URL, or - for stdin")
	}
	count := 0
	emit := func(item Item) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := yield(item); err != nil {
			return err
		}
		count++
		if opts.Limit > 0 && count >= opts.Limit {
			return errLimit
		}
		return nil
	}
	var finish func() error
	var cleanup func()
	if opts.Sample > 0 {
		emit, finish, cleanup, err = sampler(ctx, opts, yield)
		if err != nil {
			return err
		}
		defer cleanup()
	}
	seenByConfig := make(map[string]map[string]bool)
	for _, src := range sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		selection, marshalErr := json.Marshal(src.options)
		if marshalErr != nil {
			return marshalErr
		}
		key := digest(selection)
		seen := seenByConfig[key]
		if seen == nil {
			seen = make(map[string]bool)
			seenByConfig[key] = seen
		}
		err = walkSource(ctx, src.path, src.options, stdin, seen, emit)
		if errors.Is(err, errLimit) {
			err = nil
			break
		}
		if err != nil {
			return err
		}
	}
	if finish != nil {
		return finish()
	}
	return nil
}

type configuredSource struct {
	path    string
	options Options
}

func resolveSources(opts Options) ([]configuredSource, error) {
	var out []configuredSource
	for _, path := range opts.Sources {
		out = append(out, configuredSource{path, opts})
	}
	if opts.Manifest != "" {
		f, err := openRegularFile(opts.Manifest)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return nil, err
		}
		if info.Size() > opts.MaxItemBytes {
			return nil, fmt.Errorf("source manifest exceeds --max-item-bytes (%d)", opts.MaxItemBytes)
		}
		var m manifest
		dec := json.NewDecoder(io.LimitReader(f, opts.MaxItemBytes+1))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&m); err != nil {
			return nil, fmt.Errorf("source manifest: %w", err)
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("source manifest must contain one JSON document")
		}
		if m.Version != 1 {
			return nil, fmt.Errorf("source manifest version must be 1")
		}
		for _, s := range m.Sources {
			if (s.Path == "") == (s.URL == "") {
				return nil, fmt.Errorf("manifest source must supply exactly one of path or url")
			}
			o := opts
			if s.Include != nil {
				o.Include = s.Include
			}
			if s.Exclude != nil {
				o.Exclude = s.Exclude
			}
			if s.Format != "" {
				o.Format = s.Format
			}
			if s.Items != nil {
				o.Items = *s.Items
				o.ItemsSet = true
			}
			if s.State != "" {
				o.State = s.State
			}
			if s.IDField != "" {
				o.IDField = s.IDField
			}
			p := s.URL
			if s.Path != "" {
				p = s.Path
				if p != "-" && !filepath.IsAbs(p) {
					p = filepath.Join(filepath.Dir(opts.Manifest), p)
				}
			}
			out = append(out, configuredSource{p, o})
		}
	}
	return out, nil
}

func walkSource(ctx context.Context, path string, opts Options, stdin io.Reader, seen map[string]bool, emit func(Item) error) error {
	for _, pattern := range append(append([]string{}, opts.Include...), opts.Exclude...) {
		if !doublestar.ValidatePattern(pattern) {
			return fmt.Errorf("invalid glob %q", pattern)
		}
	}
	if path == "-" {
		if seen[path] {
			return nil
		}
		seen[path] = true
		if stdin == nil {
			return fmt.Errorf("stdin source requires a reader")
		}
		return readSource(ctx, stdin, "stdin:", "", opts, emit)
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		u, err := publicURL(path)
		if err != nil {
			return err
		}
		if seen[path] {
			return nil
		}
		seen[path] = true
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
		if err != nil {
			return fmt.Errorf("invalid source URL")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("fetch source %s: request failed", u.Host+u.Path)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("fetch source %s: HTTP %d", u.Host+u.Path, resp.StatusCode)
		}
		if resp.ContentLength > opts.MaxSourceBytes {
			return fmt.Errorf("source exceeds --max-source-bytes (%d)", opts.MaxSourceBytes)
		}
		if opts.Format == "" || opts.Format == "auto" {
			opts.Format = detectFormat(u.Path, resp.Header.Get("Content-Type"))
		}
		return readSource(ctx, resp.Body, path, "", opts, emit)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if !opts.FollowSymlinks {
			return nil
		}
		info, err = os.Stat(absolute)
		if err != nil {
			return err
		}
	}
	if seen[absolute] || covered(absolute, seen) {
		return nil
	}
	if !info.IsDir() {
		err := readFile(ctx, absolute, filepath.Base(absolute), opts, seen, emit)
		if err == nil {
			seen[absolute] = true
		}
		return err
	}
	visited := map[string]bool{}
	rules, err := ancestorRules(absolute, opts)
	if err != nil {
		return err
	}
	err = walkDirectory(ctx, absolute, absolute, opts, rules, visited, seen, emit)
	if err == nil {
		seen[absolute+string(filepath.Separator)] = true
	}
	return err
}

type ignoreRule struct {
	root    string
	matcher *ignore.GitIgnore
	negate  bool
}

func walkDirectory(ctx context.Context, root, dir string, opts Options, rules []ignoreRule, visited, seen map[string]bool, emit func(Item) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if covered(dir, seen) {
		return nil
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	if visited[real] {
		return nil
	}
	visited[real] = true
	defer delete(visited, real)
	if !opts.NoIgnore {
		local, err := directoryRules(dir, opts.MaxItemBytes)
		if err != nil {
			return err
		}
		rules = append(append([]ignoreRule{}, rules...), local...)
	}
	entries, err := directoryNames(ctx, dir)
	if err != nil {
		return err
	}
	defer entries.Close()
	for {
		name, ok := entries.Next()
		if !ok {
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		p := filepath.Join(dir, name)
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if name == ".git" || name == ".decide" {
			continue
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if !opts.FollowSymlinks {
				continue
			}
			info, err = os.Stat(p)
			if err != nil {
				return err
			}
		}
		ignored := false
		if !opts.NoIgnore {
			for _, r := range rules {
				rr, _ := filepath.Rel(r.root, p)
				rr = filepath.ToSlash(rr)
				if info.IsDir() {
					rr += "/"
				}
				if r.matcher.MatchesPath(rr) {
					ignored = !r.negate
				}
			}
		}
		if ignored {
			continue
		}
		if info.IsDir() {
			if matches(opts.Exclude, rel) || matches(opts.Exclude, rel+"/") {
				continue
			}
			if err := walkDirectory(ctx, root, p, opts, rules, visited, seen, emit); err != nil {
				return err
			}
			continue
		}
		// Devices, sockets and pipes are not dataset items. In particular,
		// opening a FIFO for reading can wait indefinitely for a writer.
		if !info.Mode().IsRegular() {
			continue
		}
		if len(opts.Include) > 0 && !matches(opts.Include, rel) {
			continue
		}
		if matches(opts.Exclude, rel) {
			continue
		}
		if err := readFile(ctx, p, rel, opts, seen, emit); err != nil {
			return err
		}
	}
	return entries.err
}

func matches(patterns []string, path string) bool {
	for _, p := range patterns {
		matched, _ := doublestar.Match(p, path)
		if matched {
			return true
		}
	}
	return false
}

func readFile(ctx context.Context, path, relative string, opts Options, seen map[string]bool, emit func(Item) error) error {
	if seen[path] || covered(path, seen) {
		return nil
	}
	f, err := openRegularFile(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source %s is not a regular file", path)
	}
	if info.Size() > opts.MaxSourceBytes {
		return fmt.Errorf("source %s exceeds --max-source-bytes (%d)", path, opts.MaxSourceBytes)
	}
	h := sha256.New()
	if _, err := io.Copy(h, &contextReader{ctx: ctx, r: f, max: opts.MaxSourceBytes}); err != nil {
		return err
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	return readSourceDigest(ctx, f, uri, relative, hex.EncodeToString(h.Sum(nil)), opts, emit)
}

// sampler keeps the sampled payloads on disk. Only slot identities stay in memory.
func sampler(ctx context.Context, opts Options, yield func(Item) error) (func(Item) error, func() error, func(), error) {
	dir, err := os.MkdirTemp("", "decide-sample-")
	if err != nil {
		return nil, nil, nil, err
	}
	rng := rand.New(rand.NewSource(opts.Seed))
	var n int64
	indices := make([]int64, opts.Sample)
	emit := func(item Item) error {
		n++
		slot := n - 1
		if n > int64(opts.Sample) {
			slot = rng.Int63n(n)
			if slot >= int64(opts.Sample) {
				return nil
			}
		}
		data, err := json.Marshal(item)
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(dir, fmt.Sprint(slot)), data, 0600); err != nil {
			return err
		}
		indices[slot] = n
		return nil
	}
	finish := func() error {
		slots := make([]int, 0, opts.Sample)
		for i, v := range indices {
			if v > 0 {
				slots = append(slots, i)
			}
		}
		sort.Slice(slots, func(i, j int) bool { return indices[slots[i]] < indices[slots[j]] })
		if opts.Limit > 0 && len(slots) > opts.Limit {
			slots = slots[:opts.Limit]
		}
		for _, slot := range slots {
			if err := ctx.Err(); err != nil {
				return err
			}
			data, err := os.ReadFile(filepath.Join(dir, fmt.Sprint(slot)))
			if err != nil {
				return err
			}
			var item Item
			if err = json.Unmarshal(data, &item); err != nil {
				return err
			}
			if err = yield(item); err != nil {
				return err
			}
		}
		return nil
	}
	return emit, finish, func() { _ = os.RemoveAll(dir) }, nil
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func directoryRules(dir string, max int64) ([]ignoreRule, error) {
	var out []ignoreRule
	for _, name := range []string{".gitignore", ".ignore", ".decideignore"} {
		f, err := openRegularFile(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(f, max+1))
		_ = f.Close()
		if readErr != nil {
			return nil, readErr
		}
		if int64(len(data)) > max {
			return nil, fmt.Errorf("ignore file exceeds --max-item-bytes")
		}
		for _, line := range strings.Split(string(data), "\n") {
			negate := strings.HasPrefix(line, "!")
			if negate {
				line = line[1:]
			}
			out = append(out, ignoreRule{root: dir, matcher: ignore.CompileIgnoreLines(line), negate: negate})
		}
	}
	return out, nil
}

func ancestorRules(root string, opts Options) ([]ignoreRule, error) {
	if opts.NoIgnore {
		return nil, nil
	}
	// Parent rules apply when selecting a subtree inside a repository. Stop at
	// its nearest repository boundary, so unrelated home-directory rules do not
	// become implicit policy for independent datasets.
	var dirs []string
	for dir := filepath.Dir(root); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			var out []ignoreRule
			for i := len(dirs) - 1; i >= 0; i-- {
				rules, err := directoryRules(dirs[i], opts.MaxItemBytes)
				if err != nil {
					return nil, err
				}
				out = append(out, rules...)
			}
			return out, nil
		}
	}
	return nil, nil
}

func covered(path string, seen map[string]bool) bool {
	for source := range seen {
		if strings.HasSuffix(source, string(filepath.Separator)) && strings.HasPrefix(path+string(filepath.Separator), source) {
			return true
		}
	}
	return false
}

// ValidateSources validates declarations before callers persist a run's options.
// It reads an optional manifest but never consumes source data or contacts URLs.
func ValidateSources(opts Options) error {
	if opts.MaxItemBytes == 0 {
		opts.MaxItemBytes = DefaultOptions().MaxItemBytes
	}
	if opts.MaxSourceBytes == 0 {
		opts.MaxSourceBytes = DefaultOptions().MaxSourceBytes
	}
	if opts.MaxItemBytes < 1 || opts.MaxSourceBytes < 1 || opts.MaxItemBytes > math.MaxInt64-2 || opts.MaxSourceBytes > math.MaxInt64-2 {
		return fmt.Errorf("byte limits must be positive and below %d", int64(math.MaxInt64-1))
	}
	sources, err := resolveSources(opts)
	if err != nil {
		return err
	}
	for _, source := range sources {
		if err := validateSource(source); err != nil {
			return err
		}
	}
	return nil
}

func validateSource(source configuredSource) error {
	switch source.options.Format {
	case "", "auto", "json", "jsonl", "text", "lines", "image":
	default:
		return fmt.Errorf("unknown source format %q", source.options.Format)
	}
	for _, pointer := range []string{source.options.Items, source.options.State, source.options.IDField} {
		if pointer != "" && !strings.HasPrefix(pointer, "/") {
			return fmt.Errorf("JSON pointer must start with /")
		}
	}

	for _, pattern := range append(append([]string{}, source.options.Include...), source.options.Exclude...) {
		if !doublestar.ValidatePattern(pattern) {
			return fmt.Errorf("invalid glob %q", pattern)
		}
	}
	if strings.HasPrefix(source.path, "http://") || strings.HasPrefix(source.path, "https://") {
		_, err := publicURL(source.path)
		return err
	}
	return nil
}

func publicURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid source URL")
	}
	if u.User != nil {
		return nil, fmt.Errorf("source URLs must not contain credentials")
	}
	if u.Fragment != "" {
		return nil, fmt.Errorf("source URLs must not contain fragments")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("invalid source URL query")
	}
	for key := range query {
		normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
		switch normalized {
		case "key", "apikey", "token", "accesstoken", "refreshtoken", "idtoken", "authtoken", "auth", "authorization", "password", "passwd", "secret", "clientsecret", "credential", "credentials", "signature", "sig", "bearer":
			return nil, fmt.Errorf("source URLs must not contain credential query parameters")
		}
		if strings.HasPrefix(normalized, "xamz") || strings.HasPrefix(normalized, "xgoog") {
			return nil, fmt.Errorf("source URLs must not contain signed credential query parameters")
		}
	}
	return u, nil
}
