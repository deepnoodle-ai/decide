package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/deepnoodle-ai/decide/internal/catalog"
	"github.com/deepnoodle-ai/decide/internal/dataset"
	"github.com/deepnoodle-ai/decide/internal/jobs"
	"github.com/deepnoodle-ai/wonton/tui"
)

func (s *screen) currentOptions() jobs.Options {
	o := cloneOptions(s.options)
	if s.skill != nil {
		v := *s.skill
		v.Questions = make(map[string]json.RawMessage, len(s.skill.Questions))
		for k, q := range s.skill.Questions {
			v.Questions[k] = append(json.RawMessage(nil), q...)
		}
		o.SkillDefinition = &v
	}
	return o
}
func (s *screen) preview() tui.Cmd {
	o := s.currentOptions()
	n := sampleSize(o)
	if s.randomSample {
		o.Sources.Sample = n
	} else {
		o.Sources.Sample = 0
		if o.Sources.Limit == 0 || o.Sources.Limit > n {
			o.Sources.Limit = n
		}
	}
	s.status = "Preparing " + s.sampleLabel() + ". No model calls."
	return s.background(func(ctx context.Context) update {
		var prepared []jobs.Prepared
		var bytes int64
		_, err := jobs.Plan(ctx, o, nil, func(p jobs.Prepared) error {
			bytes += int64(len(p.State) + len(p.Item.Data))
			for _, img := range p.Item.Images {
				bytes += int64(len(img.Data))
			}
			if bytes > 64<<20 {
				return fmt.Errorf("sample exceeds 64 MiB workbench memory budget; select fewer items or smaller inputs")
			}
			if len(prepared) < 200 {
				prepared = append(prepared, p)
			}
			return nil
		})
		return update{kind: "preview", prepared: prepared, err: err}
	})
}
func sampleSize(o jobs.Options) int {
	if o.Sources.Sample > 0 {
		return min(o.Sources.Sample, 200)
	}
	return 5
}
func (s *screen) run(full bool) []tui.Cmd {
	if s.skill == nil && s.options.Pattern == "" {
		s.problem = errNoSkill.Error()
		return nil
	}
	if !full && len(s.sample) == 0 {
		s.problem = "preview your sample with p before running it; sample execution sends model requests"
		return nil
	}
	o := s.currentOptions()
	sample := append([]jobs.Prepared(nil), s.sample...)
	runtime := s.runtime
	s.results = nil
	s.filter = ""
	s.totalResults = 0
	s.tab = 3
	s.index = 0
	s.detail = false
	mode := "sample"
	if full {
		mode = "full"
		o.Sources.Sample = 0
	} else {
		if s.randomSample {
			o.Sources.Sample = sampleSize(o)
		} else {
			o.Sources.Sample = 0
			if o.Sources.Limit == 0 || o.Sources.Limit > sampleSize(o) {
				o.Sources.Limit = sampleSize(o)
			}
		}
	}
	s.status = "Running " + mode + " judgments. Esc cancels; evidence stays on disk."
	cmd := s.background(func(ctx context.Context) update {
		var results []jobs.Result
		callback := func(r jobs.Result) error {
			results = appendWindow(results, r)
			if runtime != nil {
				display := r
				if resultBytes(display) > 1<<20 {
					display = compactResult(display)
				}
				runtime.SendEvent(update{kind: "result", result: &display})
			}
			return ctx.Err()
		}
		var sum jobs.Summary
		var err error
		if full {
			sum, err = jobs.Run(ctx, o, nil, callback)
		} else {
			sum, err = jobs.RunPrepared(ctx, o, sample, callback)
		}
		var page *evidencePage
		if sum.Path != "" && ctx.Err() == nil {
			p, _, e := loadEvidencePage(ctx, sum.Path, o.RunDir, "", 0)
			if e == nil {
				page = p
				results = p.Results
			}
		}
		return update{kind: "run", summary: sum, results: results, page: page, err: err, message: mode, options: o}
	})
	if !full {
		s.expectedOutcomes = len(sample)
	}
	return []tui.Cmd{cmd}
}

func (s *screen) submit(value string) {
	mode := s.edit
	if s.busy && (mode == "filter" || mode == "export") {
		s.problem = "wait for current work or cancel it with Esc before filtering or exporting"
		return
	}
	s.problem = ""
	switch mode {
	case "sample size":
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 1 || n > 200 {
			s.problem = "choose 1–200 items for your tasting flight"
			return
		}
		s.options.Sources.Sample = n
		s.sample, s.prepared = nil, nil
		s.status = fmt.Sprintf("Flight size: %d. p prepares your next experiment.", n)
	case "items pointer", "state pointer", "id pointer":
		if mode == "items pointer" && value == "-" {
			s.options.Sources.Items, s.options.Sources.ItemsSet = "", false
			s.sample, s.prepared = nil, nil
			s.status = "Whole-document mode restored. p prepares a fresh preview."
			s.edit = ""
			return
		}
		if value != "" && !strings.HasPrefix(value, "/") {
			s.problem = "JSON pointers start with /; empty selects the root"
			return
		}
		d := s.options.Sources
		switch mode {
		case "items pointer":
			d.Items, d.ItemsSet = value, true
		case "state pointer":
			d.State = value
		case "id pointer":
			d.IDField = value
		}
		if err := dataset.ValidateSources(d); err != nil {
			s.problem = err.Error()
			return
		}
		s.options.Sources = d
		s.sample, s.prepared = nil, nil
		s.status = "Mapping updated. p follows your new data trail."
	case "add source":
		source := strings.TrimSpace(value)
		if source == "" {
			s.problem = "enter a file, directory, or HTTP URL"
			return
		}
		if source == "-" {
			s.problem = "interactive stdin is reserved for keyboard input; use a file or URL"
			return
		}
		s.options.Sources.Sources = append(s.options.Sources.Sources, source)
		s.tab = 0
		s.index = len(s.options.Sources.Sources) - 1
		s.sample = nil
		s.prepared = nil
		s.status = "Source added. Add another with a, or p previews your selection."
	case "include", "exclude":
		pattern := strings.TrimSpace(value)
		if pattern == "" {
			s.problem = "enter a glob such as **/*.go"
			return
		}
		if mode == "include" {
			s.options.Sources.Include = append(s.options.Sources.Include, pattern)
		} else {
			s.options.Sources.Exclude = append(s.options.Sources.Exclude, pattern)
		}
		s.sample = nil
		s.prepared = nil
		s.status = "Selection rule added. p previews matching sources; A edits all rules."
	case "parameter":
		name, v, ok := strings.Cut(value, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			s.problem = "enter NAME=VALUE, for example focus=authorization"
			return
		}
		if s.skill == nil {
			s.problem = errNoSkill.Error()
			return
		}
		params := map[string]string{}
		for k, v := range s.options.Params {
			params[k] = v
		}
		params[name] = v
		if _, err := catalog.ResolveParameters(*s.skill, params); err != nil {
			s.problem = err.Error()
			return
		}
		s.options.Params = params
		s.status = "Parameter updated. s reruns the same sample for comparison."

	case "sources":
		var v dataset.Options
		if err := strictJSON(value, &v); err != nil {
			s.problem = err.Error()
			return
		}
		if len(v.Sources) == 0 && v.Manifest == "" {
			s.problem = "provide Sources or a Manifest"
			return
		}
		for _, source := range v.Sources {
			if source == "-" {
				s.problem = "interactive stdin is reserved for keyboard input; use a file or URL"
				return
			}
		}
		s.options.Sources = v
		s.sample = nil
		s.prepared = nil
		s.status = "Sources updated. p prepares a fresh sample."
	case "execution":
		var v jobs.Options
		if err := strictJSON(value, &v); err != nil {
			s.problem = err.Error()
			return
		}
		if v.Workers < 1 || v.RequestTimeout <= 0 {
			s.problem = "Workers and RequestTimeout must be positive (duration in nanoseconds)"
			return
		}
		v.Sources = s.options.Sources
		v.Skill = s.options.Skill
		v.Params = s.options.Params
		v.NewClient = s.options.NewClient
		s.options = v
		s.status = "Execution settings updated."
	case "parameters":
		var v map[string]string
		if err := strictJSON(value, &v); err != nil {
			s.problem = err.Error()
			return
		}
		if s.skill == nil {
			s.problem = errNoSkill.Error()
			return
		}
		if _, err := catalog.ResolveParameters(*s.skill, v); err != nil {
			s.problem = err.Error()
			return
		}
		s.options.Params = v
		s.status = "Parameters updated. Sample identities stay fixed for comparison."
	case "questions":
		var q map[string]json.RawMessage
		if err := strictJSON(value, &q); err != nil {
			s.problem = err.Error()
			return
		}
		v := *s.skill
		v.Questions = q
		if err := catalog.ValidateSkill(v); err != nil {
			s.problem = err.Error()
			return
		}
		s.skill = &v
		s.status = "Questions updated. s reruns the same sample."
	case "full":
		if value != "run" {
			s.problem = "type run to start the full selection, or Esc to return"
			return
		}
		s.edit = ""
		s.pending = s.run(true)
		return
	case "filter":
		target := s.target
		if target == "" {
			target = s.summary.Path
		}
		if target != "" {
			s.pending = s.loadPage(0, value, nil)
		}
	case "export":
		if strings.TrimSpace(value) == "" {
			s.problem = "enter a new directory for evidence and reproducible configuration"
			return
		}
		path := value
		o := s.currentOptions()
		if s.lastOptions != nil {
			o = *s.lastOptions
		}
		sum := s.summary
		target := s.target
		results := append([]jobs.Result(nil), s.results...)
		s.pending = []tui.Cmd{s.background(func(ctx context.Context) update {
			err := exportBundle(ctx, path, o, sum, target, results)
			return update{kind: "export", err: err, message: "Notebook packed! Evidence, skill, source selection, and command saved to " + path}
		})}
	}
	s.edit = ""
}
func strictJSON(text string, v any) error {
	d := json.NewDecoder(strings.NewReader(text))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("configuration must contain exactly one JSON value")
	}
	return nil
}

func exportBundle(ctx context.Context, path string, o jobs.Options, sum jobs.Summary, target string, results []jobs.Result) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var frozen *frozenDefinition
	var freezeErr error
	o, frozen, freezeErr = frozenExport(path, o, sum)
	if freezeErr != nil {
		return freezeErr
	}
	// Reject configuration exports containing credentials rather than silently
	// changing the judgment that a reproducible command would execute.
	if containsCredential(pretty(o) + pretty(o.SkillDefinition) + command(o, path) + path) {
		return fmt.Errorf("export configuration contains a configured provider credential; remove it from parameters, questions, or sources before exporting")
	}
	if o.SkillDefinition != nil && containsCredential(o.SkillDefinition.Documentation) {
		return fmt.Errorf("skill documentation contains a configured provider credential; remove it before exporting")
	}
	if err := os.Mkdir(path, 0700); err != nil {
		return fmt.Errorf("create export directory: %w", err)
	}
	if err := writeFrozen(path, frozen); err != nil {
		return err
	}
	write := func(name string, v any) error {
		return os.WriteFile(filepath.Join(path, name), []byte(pretty(v)+"\n"), 0600)
	}
	if err := write("experiment.json", o); err != nil {
		return err
	}
	if err := write("sources.json", o.Sources); err != nil {
		return err
	}
	if o.SkillDefinition != nil {
		if err := os.Mkdir(filepath.Join(path, "skill"), 0700); err != nil {
			return err
		}
		if err := write("skill/skill.json", o.SkillDefinition); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(path, "skill", "SKILL.md"), []byte(o.SkillDefinition.Documentation), 0600); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(filepath.Join(path, "evidence.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	encode := func(r jobs.Result) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(f, redactCredentials(string(b)))
		return err
	}
	if sum.Path != "" {
		err = jobs.ReadResults(sum.Path, o.RunDir, encode)
	} else if target != "" {
		var source *os.File
		source, err = os.Open(target)
		if err == nil {
			err = decodeEvidence(ctx, source, encode)
			_ = source.Close()
		}
	} else {
		for _, r := range results {
			if err = encode(r); err != nil {
				break
			}
		}
	}
	if err != nil {
		return err
	}
	cmd := command(o, path)
	cwd, _ := os.Getwd()
	cmd = "cd " + quote(cwd) + " || exit\n" + cmd
	return os.WriteFile(filepath.Join(path, "command.sh"), []byte("#!/bin/sh\n# Replay against original sources; recorded inputs remain in the run artifact.\n"+cmd+"\n"), 0700)
}
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func command(o jobs.Options, path string) string {
	abs, _ := filepath.Abs(path)
	args := []string{"decide", "run"}
	if o.Pattern != "" {
		args = append(args, "--pattern", quote(o.Pattern))
	} else if o.SkillDefinition != nil {
		args = append(args, quote(filepath.Join(abs, "skill")))
	} else {
		args = append(args, quote(o.Skill))
	}
	flag := func(name, value string) {
		if value != "" {
			args = append(args, "--"+name, quote(value))
		}
	}
	flag("provider", o.Provider)
	flag("model", o.Model)
	flag("profile", o.Profile)
	flag("base-url", o.BaseURL)
	flag("account-id", o.AccountID)
	flag("max-collection-bytes", fmt.Sprint(o.MaxCollectionBytes))
	flag("workers", fmt.Sprint(o.Workers))
	flag("request-timeout", o.RequestTimeout.String())
	flag("retries", fmt.Sprint(o.Retries))
	flag("max-requests", fmt.Sprint(o.MaxRequests))
	flag("max-records", fmt.Sprint(o.MaxRecords))
	flag("rate-limit", fmt.Sprint(o.RateLimit))
	flag("snapshot", o.Snapshot)
	flag("order", o.Order)
	flag("on-error", o.OnError)
	for _, k := range sortedKeys(o.Params) {
		flag("param", k+"="+o.Params[k])
	}
	d := o.Sources
	for _, v := range d.Include {
		flag("include", v)
	}
	for _, v := range d.Exclude {
		flag("exclude", v)
	}
	flag("format", d.Format)
	if d.ItemsSet || d.Items != "" {
		args = append(args, "--items", quote(d.Items))
	}
	flag("state", d.State)
	flag("id-field", d.IDField)
	flag("sources", d.Manifest)
	flag("max-item-bytes", fmt.Sprint(d.MaxItemBytes))
	flag("max-source-bytes", fmt.Sprint(d.MaxSourceBytes))
	flag("limit", fmt.Sprint(d.Limit))
	flag("sample", fmt.Sprint(d.Sample))
	flag("seed", fmt.Sprint(d.Seed))
	if d.NoIgnore {
		args = append(args, "--no-ignore")
	}
	if d.FollowSymlinks {
		args = append(args, "--follow-symlinks")
	}
	args = append(args, "--")
	for _, v := range d.Sources {
		args = append(args, quote(v))
	}
	return strings.Join(args, " ")
}
