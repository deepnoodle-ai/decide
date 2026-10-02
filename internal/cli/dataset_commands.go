package cli

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/deepnoodle-ai/decide/internal/catalog"
	"github.com/deepnoodle-ai/decide/internal/dataset"
	"github.com/deepnoodle-ai/decide/internal/jobs"
	"github.com/deepnoodle-ai/wonton/env"
	"golang.org/x/term"
)

func bindSources(fs *commandBinding, o *dataset.Options) {
	fs.StringsVar(&o.Include, "include", "file glob relative to each source root (repeatable)")
	fs.StringsVar(&o.Exclude, "exclude", "excluded file glob; exclusions win (repeatable)")
	fs.StringVar(&o.Format, "format", o.Format, "auto, json, jsonl, text, lines, or image")
	fs.StringVar(&o.Items, "items", o.Items, "JSON pointer to array; empty string expands root array")
	fs.StringVar(&o.State, "state", o.State, "JSON pointer selecting model state")
	fs.StringVar(&o.IDField, "id-field", o.IDField, "JSON pointer selecting a record identifier")
	fs.StringVar(&o.Manifest, "sources", o.Manifest, "source manifest JSON file")
	fs.BoolVar(&o.NoIgnore, "no-ignore", o.NoIgnore, "include files excluded by ignore files")
	fs.BoolVar(&o.FollowSymlinks, "follow-symlinks", o.FollowSymlinks, "follow symbolic links")
	fs.IntVar(&o.Limit, "limit", o.Limit, "first N selected items; zero means no item limit")
	fs.IntVar(&o.Sample, "sample", o.Sample, "seeded sample size; scans the selected sources")
	fs.Int64Var(&o.Seed, "seed", o.Seed, "sample random seed")
	fs.Int64Var(&o.MaxItemBytes, "max-item-bytes", o.MaxItemBytes, "maximum bytes in one source item")
	fs.Int64Var(&o.MaxSourceBytes, "max-source-bytes", o.MaxSourceBytes, "maximum bytes read from one source")
}

func bindExecution(fs *commandBinding, o *jobs.Options, params *[]string) {
	bindSources(fs, &o.Sources)
	fs.StringVar(&o.Skill, "skill", o.Skill, "reusable skill name (alternative to skill operand)")
	fs.StringVar(&o.Pattern, "pattern", o.Pattern, "configured pattern name or JSON file")
	fs.StringsVar(params, "param", "typed skill parameter NAME=VALUE (repeatable)")
	fs.StringVar(&o.Provider, "provider", o.Provider, "typesafe or cloudflare")
	fs.StringVar(&o.Model, "model", o.Model, "exact model ID; otherwise provider default")
	fs.StringVar(&o.Profile, "profile", o.Profile, "named connection profile in settings.json")
	fs.StringVar(&o.BaseURL, "base-url", o.BaseURL, "provider API endpoint override")
	fs.StringVar(&o.AccountID, "account-id", o.AccountID, "Cloudflare account ID")
	fs.IntVar(&o.Workers, "workers", o.Workers, "maximum simultaneous requests")
	fs.Float64Var(&o.RateLimit, "rate-limit", o.RateLimit, "maximum request attempts per second; zero is unlimited")
	fs.DurationVar(&o.RequestTimeout, "request-timeout", o.RequestTimeout, "timeout for one provider attempt")
	fs.IntVar(&o.Retries, "retries", o.Retries, "additional attempts for retryable HTTP responses")
	fs.IntVar(&o.MaxRequests, "max-requests", o.MaxRequests, "maximum provider attempts including retries; zero is unlimited")
	fs.IntVar(&o.MaxRecords, "max-records", o.MaxRecords, "maximum items for collection patterns")
	fs.Int64Var(&o.MaxCollectionBytes, "max-collection-bytes", o.MaxCollectionBytes, "maximum retained bytes for collection patterns")
	fs.StringVar(&o.Order, "order", o.Order, "completion or input output order")
	fs.StringVar(&o.OnError, "on-error", o.OnError, "continue or stop")
	fs.StringVar(&o.Snapshot, "snapshot", o.Snapshot, "copy or refs; refs verifies local sources on resume")
	fs.StringVar(&o.RunDir, "run-dir", o.RunDir, "durable run directory")
	fs.StringVar(&o.Output, "output", o.Output, "write full result JSONL to a new file")
	fs.BoolVar(&o.JSONL, "jsonl", false, "emit full result JSONL to stdout (run)")
	fs.BoolVar(&o.Details, "details", false, "show confidence and probability distributions (run)")
	fs.StringVar(&o.Color, "color", "auto", "auto, always, or never (human run output)")
	fs.StringVar(&o.Progress, "progress", "auto", "stderr summaries: auto, plain, or none")
}

func markSourceFlags(fs *commandBinding, o *dataset.Options) {
	o.ItemsSet = fs.context.IsSet("items")
}

type environment struct {
	Provider            string `env:"DECIDE_PROVIDER"`
	Model               string `env:"DECIDE_MODEL"`
	Profile             string `env:"DECIDE_PROFILE"`
	RunDir              string `env:"DECIDE_RUNS_DIR"`
	Config              string `env:"DECIDE_CONFIG"`
	TypeSafeModel       string `env:"TYPESAFE_DEFAULT_MODEL"`
	TypeSafeBaseURL     string `env:"TYPESAFE_BASE_URL"`
	CloudflareBaseURL   string `env:"CLOUDFLARE_BASE_URL"`
	CloudflareAccountID string `env:"CLOUDFLARE_ACCOUNT_ID"`
}

func envOptions() jobs.Options {
	// String-only configuration has no conversions or required credentials.
	config, _ := env.Parse[environment]()
	o := jobs.DefaultOptions()
	o.Provider, o.Model, o.Profile = config.Provider, config.Model, config.Profile
	if config.RunDir != "" {
		o.RunDir = config.RunDir
	}
	return o
}

type connectionProfile struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	BaseURL   string `json:"base_url"`
	AccountID string `json:"account_id"`
}
type settingsFile struct {
	Profiles map[string]connectionProfile `json:"profiles"`
}

func resolveConnection(o *jobs.Options) error {
	config, err := env.Parse[environment]()
	if err != nil {
		return err
	}
	if o.Profile != "" {
		path := config.Config
		if path == "" {
			path = filepath.Join(catalog.Home(), "settings.json")
		}
		raw, e := ReadJSONFile(path, ConfigMaxBytes)
		if e != nil {
			return e
		}
		var settings settingsFile
		if e = jsonv2.Unmarshal(raw, &settings, jsonv2.RejectUnknownMembers(true)); e != nil {
			return e
		}
		p, ok := settings.Profiles[o.Profile]
		if !ok {
			return fmt.Errorf("profile %q not found in %s", o.Profile, path)
		}
		if o.Provider == "" {
			o.Provider = p.Provider
		}
		if o.Model == "" {
			o.Model = p.Model
		}
		if o.BaseURL == "" {
			o.BaseURL = p.BaseURL
		}
		if o.AccountID == "" {
			o.AccountID = p.AccountID
		}
	}
	if o.Provider == "" {
		o.Provider = "typesafe"
	}
	if o.Provider == "typesafe" {
		if o.Model == "" {
			o.Model = config.TypeSafeModel
		}
		if o.BaseURL == "" {
			o.BaseURL = config.TypeSafeBaseURL
		}
	} else if o.Provider == "cloudflare" {
		if o.BaseURL == "" {
			o.BaseURL = config.CloudflareBaseURL
		}
		if o.AccountID == "" {
			o.AccountID = config.CloudflareAccountID
		}
	} else {
		return fmt.Errorf("provider %q must be typesafe or cloudflare", o.Provider)
	}
	return nil
}

func (a *App) runCommand() *commandBinding {
	o := envOptions()
	fs := a.flags("run")
	var params []string
	bindExecution(fs, &o, &params)
	plan := fs.Bool("plan", false, "preview prepared inputs and questions without model calls or saved runs")
	return fs.run(func(ctx context.Context, operands []string) int {
		markSourceFlags(fs, &o.Sources)
		if o.Skill == "" && o.Pattern == "" {
			if len(operands) == 0 {
				return a.fail(errors.New("choose a skill: decide skills list; then decide run SKILL SOURCES..."))
			}
			o.Skill = operands[0]
			operands = operands[1:]
		}
		o.Sources.Sources = operands
		if o.Progress != "auto" && o.Progress != "plain" && o.Progress != "none" {
			return a.fail(errors.New("progress must be auto, plain, or none"))
		}
		if o.Color != "auto" && o.Color != "always" && o.Color != "never" {
			return a.fail(errors.New("color must be auto, always, or never"))
		}
		if o.JSONL && o.Details {
			return a.fail(errors.New("choose --details or --jsonl, not both"))
		}
		var e error
		o.Params, e = catalog.ParameterValues(params)
		if e == nil {
			e = resolveConnection(&o)
		}
		if e != nil {
			return a.fail(e)
		}
		if len(operands) == 0 && o.Sources.Manifest == "" {
			if f, ok := a.In.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
				return a.fail(errors.New("choose files, a directory, a URL, or '-' for stdin"))
			}
		}
		o.NewClient = a.NewClient
		return a.executeDataset(ctx, o, *plan)

	})
}

func (a *App) executeDataset(ctx context.Context, o jobs.Options, plan bool) int {
	if plan {
		summary, e := jobs.Plan(ctx, o, a.In, func(p jobs.Prepared) error { return writeJSON(a.Out, p) })
		if e != nil {
			return a.offlineFailure(e)
		}
		if o.Progress != "none" {
			fmt.Fprintf(a.Err, "Prepared %d items. No model calls.\n", summary.Items)
		}
		return 0
	}
	w := a.Out
	var file *os.File
	if o.Output != "" {
		var e error
		file, e = os.OpenFile(o.Output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return a.fail(e)
		}
		defer file.Close()
		w = file
	}
	printer := newResultPrinter(a.Out, o.Color, o.Details)
	summary, e := jobs.Run(ctx, o, a.In, func(r jobs.Result) error {
		if file != nil || o.JSONL {
			if err := writeJSON(w, r); err != nil {
				return err
			}
		}
		if !o.JSONL {
			return printer.result(r)
		}
		return nil
	})
	if o.Progress != "none" && o.JSONL {
		fmt.Fprintf(a.Err, "Run %s: %s · %d complete · %d failed · %d dropped · %d uncertain · %d requests\n", summary.ID, summary.Status, summary.Completed, summary.Failed, summary.Dropped, summary.Uncertain, summary.Requests)
	}
	if summary.ID != "" && o.Progress != "none" && o.JSONL {
		fmt.Fprintf(a.Err, "Evidence: %s\nView results: decide runs view %s\n", summary.Path, summary.ID)
	}
	if !o.JSONL && summary.ID != "" {
		if err := printer.summary(summary); err != nil {
			return a.fail(err)
		}
	}
	if e != nil {
		return a.offlineFailure(e)
	}
	if file != nil {
		if e := file.Close(); e != nil {
			return a.fail(e)
		}
	}
	if summary.Failed > 0 || summary.Uncertain > 0 || summary.Status != "complete" {
		return 2
	}
	return 0
}

func (a *App) sourcesCommand(op string) *commandBinding {
	o := dataset.DefaultOptions()
	if op == "preview" {
		o.Limit = 5
	}
	fs := a.flags("sources " + op)
	bindSources(fs, &o)
	return fs.run(func(ctx context.Context, operands []string) int {
		var e error
		markSourceFlags(fs, &o)
		o.Sources = operands
		if len(operands) == 0 && o.Manifest == "" {
			if f, ok := a.In.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
				return a.fail(errors.New("sources requires a path, URL, or '-' for stdin"))
			}
		}
		e = dataset.Walk(ctx, o, a.In, func(item dataset.Item) error {
			if op == "list" {
				return writeJSON(a.Out, struct {
					ID     string         `json:"id"`
					Source dataset.Source `json:"source"`
				}{item.ID, item.Source})
			}
			return writeJSON(a.Out, struct {
				ID     string          `json:"id"`
				Source dataset.Source  `json:"source"`
				Data   json.RawMessage `json:"data"`
				Images int             `json:"images,omitempty"`
			}{item.ID, item.Source, item.Data, len(item.Images)})
		})
		if e != nil {
			return a.offlineFailure(e)
		}
		return 0
	})
}

func (a *App) libraryCommand(kind, op string) *commandBinding {
	fs := a.flags(kind + " " + op)
	asJSON := fs.Bool("json", false, "machine-readable library metadata")
	from, live := new(string), new(bool)
	if kind == "skills" && op == "new" {
		fs.StringVar(from, "from", "", "copy a skill as the starting point")
	}
	if kind == "skills" && op == "test" {
		fs.BoolVar(live, "live", false, "run example inputs through a model and save results")
	}
	return fs.run(func(ctx context.Context, operands []string) int {
		var e error
		if op == "list" {
			if len(operands) > 0 {
				return a.fail(errors.New("list takes no operands"))
			}
			if kind == "skills" {
				items, e := catalog.ListSkills()
				if e != nil {
					return a.fail(e)
				}
				if *asJSON {
					return a.offlineFailure(writeJSON(a.Out, items))
				}
				fmt.Fprint(a.Out, "Available skills\n\n")
				for _, s := range items {
					fmt.Fprintf(a.Out, "  %-28s %s\n", s.Name, s.Description)
				}
				fmt.Fprintln(a.Out, "\nRun a skill: decide run code-risk . --include '**/*.go'")
				return 0
			}
			items, e := catalog.ListPatterns()
			if e != nil {
				return a.fail(e)
			}
			if *asJSON {
				return a.offlineFailure(writeJSON(a.Out, items))
			}
			fmt.Fprint(a.Out, "Available patterns\n\n")
			for _, p := range items {
				fmt.Fprintf(a.Out, "  %-28s %s\n", p.Name, p.Description)
			}
			fmt.Fprintln(a.Out, "\nRun a pattern: decide run --pattern NAME SOURCES...")
			return 0
		}
		if len(operands) != 1 {
			return a.fail(fmt.Errorf("%s %s requires one name", kind, op))
		}
		name := operands[0]
		if kind == "patterns" {
			if op != "show" {
				return a.fail(fmt.Errorf("patterns supports list and show"))
			}
			p, e := catalog.LoadPattern(name)
			if e != nil {
				return a.fail(e)
			}
			return a.offlineFailure(writePrettyJSON(a.Out, p))
		}
		if op == "new" {
			p, e := catalog.CreateSkill(name, *from)
			if e != nil {
				return a.fail(e)
			}
			fmt.Fprintf(a.Out, "Created skill: %s\nEdit: decide skills edit %s\n", p, name)
			return 0
		}
		if op == "edit" {
			p, e := catalog.SkillPath(name)
			if e != nil {
				return a.fail(e)
			}
			editor := os.Getenv("VISUAL")
			if editor == "" {
				editor = os.Getenv("EDITOR")
			}
			if editor == "" {
				return a.fail(fmt.Errorf("set VISUAL or EDITOR, or edit %s directly", p))
			}
			cmd := exec.CommandContext(ctx, "sh", "-c", editor+` "$1"`, "decide-editor", p)
			cmd.Stdin = a.In
			cmd.Stdout = a.Out
			cmd.Stderr = a.Err
			if e := cmd.Run(); e != nil {
				return a.fail(e)
			}
			if _, e := catalog.LoadSkill(p); e != nil {
				return a.fail(e)
			}
			fmt.Fprintln(a.Out, "Skill saved and validated.")
			return 0
		}
		s, e := catalog.LoadSkill(name)
		if e != nil {
			return a.fail(e)
		}
		switch op {
		case "show":
			if *asJSON {
				return a.offlineFailure(writePrettyJSON(a.Out, s))
			}
			fmt.Fprintf(a.Out, "%s\n%s\n\n", s.Name, s.Description)
			fmt.Fprint(a.Out, s.Documentation)
			fmt.Fprintf(a.Out, "\nInputs: %s\n", strings.Join(s.Inputs, ", "))
			for n, p := range s.Parameters {
				fmt.Fprintf(a.Out, "Parameter %s (%s): %s; default %s\n", n, p.Type, p.Description, catalog.FormatParameter(p))
			}
			return 0
		case "validate":
			fmt.Fprintf(a.Out, "%s: configuration valid; %d typed questions, %d examples.\n", name, len(s.Questions), len(s.Examples))
			return 0
		case "test":
			resolved, e := catalog.ResolveParameters(s, nil)
			if e != nil {
				return a.fail(e)
			}
			if _, e = catalog.Questions(resolved); e != nil {
				return a.fail(e)
			}
			if *live && len(s.Examples) == 0 {
				return a.fail(errors.New("this skill has no examples; choose image sources with run"))
			}
			var input strings.Builder
			for _, ex := range s.Examples {
				input.Write(ex)
				input.WriteByte('\n')
			}
			o := envOptions()
			o.Skill = name
			o.Sources.Sources = []string{"-"}
			o.Sources.Format = "jsonl"
			o.NewClient = a.NewClient
			if !*live {
				if len(s.Examples) > 0 {
					if _, e := jobs.Plan(ctx, o, strings.NewReader(input.String()), nil); e != nil {
						return a.offlineFailure(e)
					}
				}
				fmt.Fprintf(a.Out, "%s: definitions and %d examples validated offline. No model calls.\n", name, len(s.Examples))
				return 0
			}
			if e := resolveConnection(&o); e != nil {
				return a.fail(e)
			}
			summary, e := jobs.Run(ctx, o, strings.NewReader(input.String()), func(r jobs.Result) error { return writeJSON(a.Out, r) })
			if e != nil {
				return a.offlineFailure(e)
			}
			if summary.Failed > 0 || summary.Uncertain > 0 {
				return 2
			}
			return 0
		default:
			return a.fail(fmt.Errorf("unknown skills command %q", op))
		}
	})
}

func writePrettyJSON(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, redact(string(b))+"\n")
	return err
}

func (a *App) runsCommand(op string) *commandBinding {
	o := envOptions()
	fs := a.flags("runs " + op)
	var asJSON, retryFailed, retryUncertain bool
	fs.StringVar(&o.RunDir, "run-dir", o.RunDir, "saved run directory")
	switch op {
	case "list", "show", "watch":
		fs.BoolVar(&asJSON, "json", false, "structured summaries")
	case "view", "resume":
		fs.BoolVar(&o.JSONL, "jsonl", false, "emit full result JSONL")
		fs.BoolVar(&o.Details, "details", false, "show confidence and labeled distributions")
		fs.StringVar(&o.Color, "color", "auto", "auto, always, or never")
		if op == "resume" {
			fs.BoolVar(&retryFailed, "retry-failed", false, "retry known failed items")
			fs.BoolVar(&retryUncertain, "retry-uncertain", false, "explicitly resubmit uncertain attempts; may duplicate provider work")
			fs.IntVar(&o.Workers, "workers", o.Workers, "request concurrency")
			fs.IntVar(&o.MaxRequests, "max-requests", o.MaxRequests, "total request ceiling")
		}
	case "export":
		fs.StringVar(&o.Output, "output", "", "export to a new file")
	}
	return fs.run(func(ctx context.Context, operands []string) int {
		if op == "list" {
			if len(operands) > 0 {
				return a.fail(errors.New("runs list takes no ID"))
			}
			items, e := jobs.List(o.RunDir)
			if e != nil {
				return a.fail(e)
			}
			if asJSON {
				return a.offlineFailure(writeJSON(a.Out, items))
			}
			if len(items) == 0 {
				fmt.Fprintln(a.Out, "No saved runs. Create a run: decide run builtin/code-risk . --include '**/*.go' --sample 5")
				return 0
			}
			for _, s := range items {
				fmt.Fprintf(a.Out, "%-30s %-12s %5d complete %4d failed  %s\n", s.ID, s.Status, s.Completed, s.Failed, s.Skill)
			}
			return 0
		}
		if len(operands) != 1 {
			return a.fail(fmt.Errorf("runs %s requires one run ID or path", op))
		}
		id := operands[0]
		switch op {
		case "view":
			if o.Color != "auto" && o.Color != "always" && o.Color != "never" {
				return a.fail(errors.New("color must be auto, always, or never"))
			}
			if o.JSONL && o.Details {
				return a.fail(errors.New("choose --details or --jsonl, not both"))
			}
			printer := newResultPrinter(a.Out, o.Color, o.Details)
			err := jobs.ReadEvidence(id, o.RunDir, func(r jobs.Result) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if o.JSONL {
					return writeJSON(a.Out, r)
				}
				return printer.result(r)
			})
			if err != nil {
				return a.offlineFailure(err)
			}
			if !o.JSONL {
				if sum, err := jobs.Show(id, o.RunDir); err == nil {
					return a.offlineFailure(printer.summary(sum))
				}
			}
			return 0
		case "show", "watch":
			for {
				s, e := jobs.Show(id, o.RunDir)
				if e != nil {
					return a.fail(e)
				}
				if asJSON {
					if e := writeJSON(a.Out, s); e != nil {
						return a.fail(e)
					}
				} else {
					fmt.Fprintf(a.Out, "%s · %s · %d/%d complete · %d failed · %d uncertain · %d requests\n", s.ID, s.Status, s.Completed, s.Items, s.Failed, s.Uncertain, s.Requests)
				}
				if op == "show" || (s.Status != "preparing" && s.Status != "prepared" && s.Status != "running") {
					return 0
				}
				select {
				case <-ctx.Done():
					return 130
				case <-time.After(time.Second):
				}
			}
		case "export":
			w := a.Out
			if o.Output != "" {
				f, e := os.OpenFile(o.Output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if e != nil {
					return a.fail(e)
				}
				defer f.Close()
				w = f
			}
			return a.offlineFailure(jobs.Export(id, o.RunDir, w))
		case "resume":
			if o.Color != "auto" && o.Color != "always" && o.Color != "never" {
				return a.fail(errors.New("color must be auto, always, or never"))
			}
			if o.JSONL && o.Details {
				return a.fail(errors.New("choose --details or --jsonl, not both"))
			}
			printer := newResultPrinter(a.Out, o.Color, o.Details)
			o.NewClient = a.NewClient // Only explicitly supplied execution overrides are forwarded.
			visited := map[string]bool{}
			for _, flag := range fs.flags {
				visited[flag.GetName()] = fs.context.IsSet(flag.GetName())
			}
			if !visited["workers"] {
				o.Workers = 0
			}
			if !visited["max-requests"] {
				o.MaxRequests = 0
			}
			s, e := jobs.Resume(ctx, id, o, retryFailed, retryUncertain, func(r jobs.Result) error {
				if o.JSONL {
					return writeJSON(a.Out, r)
				}
				return printer.result(r)
			})
			if o.JSONL {
				fmt.Fprintf(a.Err, "Run %s: %s · %d complete · %d failed · %d uncertain\n", s.ID, s.Status, s.Completed, s.Failed, s.Uncertain)
			} else if s.ID != "" {
				if err := printer.summary(s); err != nil {
					return a.fail(err)
				}
			}
			if e != nil {
				return a.offlineFailure(e)
			}
			if s.Failed > 0 || s.Uncertain > 0 || s.Status != "complete" {
				return 2
			}
			return 0
		}
		return a.fail(fmt.Errorf("unknown runs command %q", op))
	})
}
