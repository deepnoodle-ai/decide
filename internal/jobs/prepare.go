package jobs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/cloudflare"
	"github.com/deepnoodle-ai/decide/internal/catalog"
	"github.com/deepnoodle-ai/decide/internal/dataset"
)

func normalize(o Options) (Options, error) {
	if o.Provider == "" {
		o.Provider = "typesafe"
	}
	if o.Workers == 0 {
		o.Workers = 4
	}
	if o.Order == "" {
		o.Order = "completion"
	}
	if o.OnError == "" {
		o.OnError = "continue"
	}
	if o.Snapshot == "" {
		o.Snapshot = "refs"
	}
	if o.MaxCollectionBytes == 0 {
		o.MaxCollectionBytes = 64 << 20
	}
	if o.MaxRecords == 0 {
		o.MaxRecords = 10000
	}
	if o.Model == "" {
		if o.Provider == "cloudflare" {
			o.Model = "clef"
		} else {
			o.Model = os.Getenv("TYPESAFE_DEFAULT_MODEL")
			if o.Model == "" {
				o.Model = "jev-latest"
			}
		}
	}
	if o.BaseURL == "" {
		if o.Provider == "cloudflare" {
			o.BaseURL = strings.TrimSpace(os.Getenv("CLOUDFLARE_BASE_URL"))
			if o.BaseURL == "" {
				o.BaseURL = "https://api.cloudflare.com/client/v4"
			}
		} else {
			o.BaseURL = strings.TrimSpace(os.Getenv("TYPESAFE_BASE_URL"))
			if o.BaseURL == "" {
				o.BaseURL = "https://api.typesafe.ai"
			}
		}
	}
	if o.Provider == "cloudflare" && o.AccountID == "" {
		o.AccountID = strings.TrimSpace(os.Getenv("CLOUDFLARE_ACCOUNT_ID"))
	}
	if e := validateConnection(o); e != nil {
		return o, e
	}
	if e := dataset.ValidateSources(o.Sources); e != nil {
		return o, e
	}
	if o.Workers < 1 || o.Retries < 0 || o.MaxRequests < 0 || o.MaxRecords < 1 || o.MaxCollectionBytes < 1 || o.RateLimit < 0 || o.RequestTimeout < 0 {
		return o, errors.New("workers and collection limit must be positive; retries, request limit, rate and timeout must be nonnegative")
	}
	if o.Provider != "typesafe" && o.Provider != "cloudflare" {
		return o, errors.New("provider must be typesafe or cloudflare")
	}
	if o.Order != "input" && o.Order != "completion" {
		return o, errors.New("order must be input or completion")
	}
	if o.OnError != "stop" && o.OnError != "continue" {
		return o, errors.New("on-error must be stop or continue")
	}
	if o.Snapshot != "refs" && o.Snapshot != "copy" {
		return o, errors.New("snapshot must be refs or copy")
	}
	return o, nil
}

func resolve(o Options) (definition, error) {
	d := definition{Version: 1, Options: o, Skills: map[string]catalog.Skill{}}
	known := map[string]bool{}
	load := func(name string) (catalog.Skill, error) {
		s, e := catalog.LoadSkill(name)
		if d.Options.SkillDefinition != nil && name == d.Options.Skill {
			s = *d.Options.SkillDefinition
			e = nil
		}
		if e != nil {
			return s, e
		}
		if e = catalog.ValidateSkill(s); e != nil {
			return s, e
		}
		values := map[string]string{}
		for key := range s.Parameters {
			known[key] = true
			if value, ok := o.Params[key]; ok {
				values[key] = value
			}
		}
		s, e = catalog.ResolveParameters(s, values)
		if e == nil {
			d.Skills[name] = s
		}
		return s, e
	}
	if o.Pattern != "" {
		p, e := catalog.LoadPattern(o.Pattern)
		if e != nil {
			return d, e
		}
		d.Pattern = p
		switch p.Type {
		case "map":
			s, e := load(p.Skill)
			if e != nil {
				return d, e
			}
			d.Skill = s
		case "heads":
			for _, n := range p.Branches {
				if _, e := load(n); e != nil {
					return d, e
				}
			}
		case "funnel":
			for _, st := range p.Stages {
				if _, e := load(st.Skill); e != nil {
					return d, e
				}
			}
		case "gate", "rank", "rank-pack":
			return d, nil
		default:
			return d, fmt.Errorf("unsupported pattern %q", p.Type)
		}
	} else {
		if o.Skill == "" {
			return d, errors.New("choose a skill or --pattern")
		}
		s, e := load(o.Skill)
		if e != nil {
			return d, e
		}
		d.Skill = s
		d.Pattern.Type = "map"
	}
	for key := range o.Params {
		if !known[key] {
			return d, fmt.Errorf("unknown parameter %q across selected skills", key)
		}
	}
	factory := d.Options.NewClient
	clean, e := cleanValue(d)
	if e != nil {
		return d, e
	}
	clean.Options.NewClient = factory
	return clean, nil
}

func stateFor(it dataset.Item, s catalog.Skill) json.RawMessage {
	state := it.Data
	if len(it.State) > 0 {
		state = it.State
	}
	if s.State == "file" {
		var content any
		if json.Unmarshal(state, &content) != nil {
			content = string(state)
		}
		b, _ := json.Marshal(map[string]any{"path": it.Source.Path, "language": strings.TrimPrefix(filepath.Ext(it.Source.Path), "."), "content": content})
		return b
	}
	return state
}

func prepared(it dataset.Item, d definition) (Prepared, error) {
	s := d.Skill
	if d.Pattern.Type == "funnel" {
		s = d.Skills[d.Pattern.Stages[0].Skill]
	}
	if d.Pattern.Type == "heads" {
		s = catalog.Skill{State: "value"}
		for _, branch := range d.Skills {
			if branch.State == "file" {
				s.State = "file"
			}
		}
	}
	for _, selected := range d.Skills {
		if e := checkInput(it, selected); e != nil {
			return Prepared{}, e
		}
	}
	p := Prepared{Item: it, State: stateFor(it, s), Questions: s.Questions}
	images := p.Item.Images
	p.Item.Images = nil
	clean, e := cleanValue(p)
	if e != nil {
		return p, e
	}
	p = clean
	p.Item.Images = images
	if d.Pattern.Type == "heads" {
		req, _, e := headsRequest(p.State, d)
		if e != nil {
			return p, e
		}
		p.Questions = rawQuestions(req.Questions)
	}
	qs := map[string]decide.Question{}
	for k, b := range p.Questions {
		q, e := decide.DecodeQuestion(b)
		if e != nil {
			return p, e
		}
		qs[k] = q
	}
	req := &decide.Request{State: p.State, Questions: qs, Model: d.Options.Model}
	if e := req.Validate(); e != nil {
		return p, e
	}
	if len(it.Images) > 0 {
		if d.Options.Provider != "cloudflare" {
			return p, errors.New("images require --provider cloudflare")
		}
		images := []cloudflare.Image{}
		for _, im := range it.Images {
			c, e := cloudflare.NewImage(im.ContentType, im.Data)
			if e != nil {
				return p, e
			}
			images = append(images, c)
		}
		if e := cloudflare.SetImages(req, images...); e != nil {
			return p, e
		}
	}
	return p, nil
}

func imageDigest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

func rawQuestions(qs map[string]decide.Question) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for k, q := range qs {
		b, _ := q.MarshalJSON()
		out[k] = b
	}
	return out
}

func Plan(ctx context.Context, o Options, in io.Reader, fn func(Prepared) error) (Summary, error) {
	o, e := normalize(o)
	if e != nil {
		return Summary{}, e
	}
	d, e := resolve(o)
	if e != nil {
		return Summary{}, e
	}
	s := Summary{Version: 1, Status: "planned", Skill: d.Options.Skill, Pattern: d.Options.Pattern, Provider: d.Options.Provider, Model: d.Options.Model}
	if collection(d) {
		return s, errors.New("saved-evidence patterns use run; source planning is unavailable")
	}
	e = dataset.Walk(ctx, o.Sources, in, func(it dataset.Item) error {
		p, e := prepared(it, d)
		if e != nil {
			return e
		}
		s.Items++
		if fn != nil {
			return fn(p)
		}
		return nil
	})
	return s, e
}

func newRun(d definition) (Summary, error) {
	b := make([]byte, 6)
	if _, e := rand.Read(b); e != nil {
		return Summary{}, e
	}
	id := time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b)
	path, e := filepath.Abs(filepath.Join(runRoot(d.Options.RunDir), id))
	if e != nil {
		return Summary{}, e
	}
	for _, p := range []string{path, filepath.Join(path, "inputs"), filepath.Join(path, "results"), filepath.Join(path, "assets")} {
		if e = os.MkdirAll(p, 0700); e != nil {
			return Summary{}, e
		}
	}
	s := Summary{Version: 1, ID: id, Path: path, Status: "preparing", Skill: d.Options.Skill, Pattern: d.Options.Pattern, Provider: d.Options.Provider, Model: d.Options.Model, Started: time.Now().UTC()}
	if e = writeJSON(filepath.Join(path, "config.json"), d); e != nil {
		return s, e
	}
	e = saveSummary(&s)
	return s, e
}

func spool(ctx context.Context, d definition, s *Summary, in io.Reader) error {
	return dataset.Walk(ctx, d.Options.Sources, in, func(it dataset.Item) error {
		p, e := prepared(it, d)
		if e != nil {
			return e
		}
		rec := input{Prepared: p}
		for _, im := range it.Images {
			sum := sha256.Sum256(im.Data)
			digest := hex.EncodeToString(sum[:])
			assetPath := filepath.Join(s.Path, "assets", digest)
			if e = os.WriteFile(assetPath, im.Data, 0600); e != nil {
				return e
			}
			rec.Assets = append(rec.Assets, asset{digest, im.ContentType})
		}
		rec.Prepared.Item.Images = nil
		if e = writeJSON(itemPath(s.Path, "inputs", s.Items), rec); e != nil {
			return e
		}
		s.Items++
		return nil
	})
}

func verifyRefs(ctx context.Context, s Summary) error {
	seen := map[string]bool{}
	for i := 0; i < s.Items; i++ {
		var rec input
		if e := readJSON(itemPath(s.Path, "inputs", i), &rec); e != nil {
			return e
		}
		src := rec.Prepared.Item.Source
		u, e := url.Parse(src.URI)
		if e != nil {
			return errors.New("invalid recorded source")
		}
		if u.Scheme != "file" {
			continue
		}
		if seen[src.URI] {
			continue
		}
		seen[src.URI] = true
		f, e := os.Open(u.Path)
		if e != nil {
			return fmt.Errorf("referenced input is unavailable: %s", src.Path)
		}
		h := sha256.New()
		buf := make([]byte, 128<<10)
		for {
			if e = ctx.Err(); e != nil {
				f.Close()
				return e
			}
			n, re := f.Read(buf)
			if n > 0 {
				h.Write(buf[:n])
			}
			if re == io.EOF {
				break
			}
			if re != nil {
				f.Close()
				return re
			}
		}
		f.Close()
		if hex.EncodeToString(h.Sum(nil)) != src.Digest {
			return fmt.Errorf("referenced input changed: %s; start a new run or use copy snapshots", src.Path)
		}
	}
	return nil
}

func checkInput(it dataset.Item, s catalog.Skill) error {
	kind := it.Source.Format
	if len(it.Images) > 0 {
		kind = "image"
	}
	state := it.Data
	if len(it.State) > 0 {
		state = it.State
	}
	var text string
	isText := json.Unmarshal(state, &text) == nil
	for _, input := range s.Inputs {
		if input == kind {
			return nil
		}
		if input == "text" && isText && kind != "image" {
			return nil
		}
		if input == "json" && kind != "image" && json.Valid(state) {
			return nil
		}
	}
	return fmt.Errorf("skill %s accepts %s; source %s supplies %s", s.Name, strings.Join(s.Inputs, ", "), it.Source.Path, kind)
}

func validateConnection(o Options) error {
	u, e := url.Parse(o.BaseURL)
	if e != nil || u == nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return errors.New("base URL must be an absolute HTTP(S) URL without userinfo, query parameters, or fragments")
	}
	if o.Provider == "typesafe" && o.AccountID != "" {
		return errors.New("account ID is only supported by cloudflare")
	}
	return nil
}
