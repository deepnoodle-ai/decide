package cli

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"flag"
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
	"github.com/deepnoodle-ai/decide/internal/workbench"
	"golang.org/x/term"
)

type repeated []string

func (r *repeated) String() string     { return strings.Join(*r, ", ") }
func (r *repeated) Set(v string) error { *r = append(*r, v); return nil }

func bindSources(fs *flag.FlagSet, o *dataset.Options) {
	fs.Var((*repeated)(&o.Include), "include", "file glob relative to each source root (repeatable)")
	fs.Var((*repeated)(&o.Exclude), "exclude", "excluded file glob; exclusions win (repeatable)")
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

func bindExecution(fs *flag.FlagSet, o *jobs.Options, params *repeated) {
	bindSources(fs, &o.Sources)
	fs.StringVar(&o.Skill, "skill", o.Skill, "reusable skill name (explore or alternative to skill operand)")
	fs.StringVar(&o.Pattern, "pattern", o.Pattern, "configured pattern name or JSON file")
	fs.Var(params, "param", "typed skill parameter NAME=VALUE (repeatable)")
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
	fs.StringVar(&o.Output, "output", o.Output, "write result JSONL to a new file")
	fs.StringVar(&o.Progress, "progress", "auto", "stderr summaries: auto, plain, or none")
}

// parseOperands retains flag package validation while allowing normal CLI
// spelling: skill/source operands followed by options. Values never use a shell.
func parseOperands(fs *flag.FlagSet, args []string) ([]string, error) {
	var flags, operands []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			operands = append(operands, args[i+1:]...)
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			operands = append(operands, arg)
			continue
		}
		name := strings.TrimLeft(arg, "-")
		name, _, inline := strings.Cut(name, "=")
		f := fs.Lookup(name)
		flags = append(flags, arg)
		if f == nil {
			if name == "h" || name == "help" {
				continue
			}
			continue
		}
		isBool := false
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok {
			isBool = b.IsBoolFlag()
		}
		if !inline && !isBool {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--%s needs a value", name)
			}
			i++
			flags = append(flags, args[i])
		}
	}
	usage := fs.Usage
	fs.Usage = func() {}
	err := fs.Parse(flags)
	fs.Usage = usage
	if err != nil {
		if errors.Is(err, flag.ErrHelp) && usage != nil {
			usage()
		}
		return nil, err
	}
	return operands, nil
}

func markSourceFlags(fs *flag.FlagSet, o *dataset.Options) {
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "items" {
			o.ItemsSet = true
		}
	})
}

func envOptions() jobs.Options {
	o := jobs.DefaultOptions()
	o.Provider = os.Getenv("DECIDE_PROVIDER")
	o.Model = os.Getenv("DECIDE_MODEL")
	o.Profile = os.Getenv("DECIDE_PROFILE")
	if d := os.Getenv("DECIDE_RUNS_DIR"); d != "" {
		o.RunDir = d
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
	if o.Profile != "" {
		path := os.Getenv("DECIDE_CONFIG")
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
			o.Model = os.Getenv("TYPESAFE_DEFAULT_MODEL")
		}
		if o.BaseURL == "" {
			o.BaseURL = os.Getenv("TYPESAFE_BASE_URL")
		}
	} else if o.Provider == "cloudflare" {
		if o.BaseURL == "" {
			o.BaseURL = os.Getenv("CLOUDFLARE_BASE_URL")
		}
		if o.AccountID == "" {
			o.AccountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
		}
	} else {
		return fmt.Errorf("provider %q must be typesafe or cloudflare", o.Provider)
	}
	return nil
}

func (a *App) datasetOptions(command string, args []string) (jobs.Options, int, bool) {
	o := envOptions()
	fs := a.flags(command)
	var params repeated
	bindExecution(fs, &o, &params)
	operands, e := parseOperands(fs, args)
	if e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return o, 0, false
		}
		return o, a.fail(e), false
	}
	markSourceFlags(fs, &o.Sources)
	if command != "explore" && o.Skill == "" && o.Pattern == "" {
		if len(operands) == 0 {
			return o, a.fail(errors.New("choose a skill: decide skills list; then decide " + command + " SKILL SOURCES...")), false
		}
		o.Skill = operands[0]
		operands = operands[1:]
	}
	o.Sources.Sources = operands
	if o.Progress != "auto" && o.Progress != "plain" && o.Progress != "none" {
		return o, a.fail(errors.New("progress must be auto, plain, or none")), false
	}
	o.Params, e = catalog.ParameterValues(params)
	if e == nil {
		e = resolveConnection(&o)
	}
	if e != nil {
		return o, a.fail(e), false
	}
	if len(operands) == 0 && o.Sources.Manifest == "" && command != "explore" {
		if f, ok := a.In.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
			return o, a.fail(errors.New("choose files, a directory, a URL, or '-' for stdin; try decide explore")), false
		}
	}
	o.NewClient = a.NewClient
	return o, 0, true
}

func (a *App) runDataset(ctx context.Context, command string, args []string) int {
	o, status, ok := a.datasetOptions(command, args)
	if !ok {
		return status
	}
	if command == "explore" {
		if e := workbench.Explore(ctx, o); e != nil {
			return a.offlineFailure(e)
		}
		return 0
	}
	if command == "plan" {
		summary, e := jobs.Plan(ctx, o, a.In, func(p jobs.Prepared) error { return writeJSON(a.Out, p) })
		if e != nil {
			return a.offlineFailure(e)
		}
		if o.Progress != "none" {
			fmt.Fprintf(a.Err, "Ready to try: %d items. No model calls.\n", summary.Items)
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
	summary, e := jobs.Run(ctx, o, a.In, func(r jobs.Result) error { return writeJSON(w, r) })
	if o.Progress != "none" {
		fmt.Fprintf(a.Err, "Run %s: %s · %d complete · %d failed · %d dropped · %d uncertain · %d requests\n", summary.ID, summary.Status, summary.Completed, summary.Failed, summary.Dropped, summary.Uncertain, summary.Requests)
	}
	if summary.ID != "" && o.Progress != "none" {
		fmt.Fprintf(a.Err, "Evidence: %s\nNext: decide inspect %s\n", summary.Path, summary.ID)
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

func (a *App) runSources(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return a.fail(errors.New("sources requires list or preview"))
	}
	op := args[0]
	if op == "--help" || op == "-h" {
		fmt.Fprintln(a.Out, "Usage: decide sources list|preview SOURCES... [options]\nFiles, directories, URLs, or '-' for stdin. Preview defaults to five items.")
		return 0
	}
	if op != "list" && op != "preview" {
		return a.fail(fmt.Errorf("unknown sources command %q", op))
	}
	o := dataset.DefaultOptions()
	if op == "preview" {
		o.Limit = 5
	}
	fs := a.flags("sources " + op)
	bindSources(fs, &o)
	operands, e := parseOperands(fs, args[1:])
	if e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return 0
		}
		return a.fail(e)
	}
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
}

func (a *App) runLibrary(ctx context.Context, kind string, args []string) int {
	if len(args) == 0 {
		args = []string{"list"}
	}
	op := args[0]
	if op == "--help" || op == "-h" {
		fmt.Fprintf(a.Out, "Usage: decide %s list|show NAME [--json]\n", kind)
		if kind == "skills" {
			fmt.Fprintln(a.Out, "       decide skills new NAME [--from builtin/code-risk]\n       decide skills edit|validate|test NAME [--live]\nMake a little judgment your own. Built-in skills are read-only.")
		}
		return 0
	}
	fs := a.flags(kind + " " + op)
	asJSON := fs.Bool("json", false, "machine-readable library metadata")
	from := fs.String("from", "", "copy a skill as the starting point")
	live := fs.Bool("live", false, "qualify examples with actual model calls")
	operands, e := parseOperands(fs, args[1:])
	if e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return 0
		}
		return a.fail(e)
	}
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
			fmt.Fprint(a.Out, "Your judgment shelf\n\n")
			for _, s := range items {
				fmt.Fprintf(a.Out, "  %-28s %s\n", s.Name, s.Description)
			}
			fmt.Fprintln(a.Out, "\nTry: decide explore . --skill builtin/code-risk")
			return 0
		}
		items, e := catalog.ListPatterns()
		if e != nil {
			return a.fail(e)
		}
		if *asJSON {
			return a.offlineFailure(writeJSON(a.Out, items))
		}
		fmt.Fprint(a.Out, "Ways to put judgments to work\n\n")
		for _, p := range items {
			fmt.Fprintf(a.Out, "  %-28s %s\n", p.Name, p.Description)
		}
		fmt.Fprintln(a.Out, "\nShow a pattern, save its JSON, then run --pattern FILE.")
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
		fmt.Fprintf(a.Out, "A fresh judgment, ready to shape: %s\nTry: decide skills edit %s\n", p, name)
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
		fmt.Fprintln(a.Out, "Saved and validated. Ready for a sample.")
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
		fmt.Fprintf(a.Out, "%s is ready: %d typed questions, %d examples.\n", name, len(s.Questions), len(s.Examples))
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
			return a.fail(errors.New("this skill has no examples; choose image sources with run or explore"))
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
}

func writePrettyJSON(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, redact(string(b))+"\n")
	return err
}

func (a *App) runRuns(ctx context.Context, args []string) int {
	if len(args) == 0 {
		args = []string{"list"}
	}
	op := args[0]
	if op == "--help" || op == "-h" {
		fmt.Fprintln(a.Out, "Usage: decide runs list|show|watch|resume|export [ID] [options]\nRecorded judgments are a notebook you can reopen.")
		return 0
	}
	o := envOptions()
	fs := a.flags("runs " + op)
	asJSON := fs.Bool("json", false, "structured summaries")
	retryFailed := fs.Bool("retry-failed", false, "retry known failed items")
	retryUncertain := fs.Bool("retry-uncertain", false, "explicitly resubmit uncertain attempts; may duplicate provider work")
	fs.StringVar(&o.RunDir, "run-dir", o.RunDir, "durable run directory")
	fs.StringVar(&o.Output, "output", "", "export to a new file")
	fs.IntVar(&o.Workers, "workers", o.Workers, "resume request concurrency")
	fs.IntVar(&o.MaxRequests, "max-requests", o.MaxRequests, "resume total request ceiling")
	operands, e := parseOperands(fs, args[1:])
	if e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return 0
		}
		return a.fail(e)
	}
	if op == "list" {
		if len(operands) > 0 {
			return a.fail(errors.New("runs list takes no ID"))
		}
		items, e := jobs.List(o.RunDir)
		if e != nil {
			return a.fail(e)
		}
		if *asJSON {
			return a.offlineFailure(writeJSON(a.Out, items))
		}
		if len(items) == 0 {
			fmt.Fprintln(a.Out, "Your notebook is empty. Try: decide run builtin/code-risk . --include '**/*.go' --sample 5")
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
	case "show", "watch":
		for {
			s, e := jobs.Show(id, o.RunDir)
			if e != nil {
				return a.fail(e)
			}
			if *asJSON {
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
		o.NewClient = a.NewClient // Only explicitly supplied execution overrides are forwarded.
		visited := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
		if !visited["workers"] {
			o.Workers = 0
		}
		if !visited["max-requests"] {
			o.MaxRequests = 0
		}
		s, e := jobs.Resume(ctx, id, o, *retryFailed, *retryUncertain, func(r jobs.Result) error { return writeJSON(a.Out, r) })
		fmt.Fprintf(a.Err, "Run %s: %s · %d complete · %d failed · %d uncertain\n", s.ID, s.Status, s.Completed, s.Failed, s.Uncertain)
		if e != nil {
			return a.offlineFailure(e)
		}
		if s.Failed > 0 || s.Uncertain > 0 || s.Status != "complete" {
			return 2
		}
		return 0
	default:
		return a.fail(fmt.Errorf("unknown runs command %q", op))
	}
}

func (a *App) runInspect(ctx context.Context, args []string) int {
	fs := a.flags("inspect")
	dir := fs.String("run-dir", envOptions().RunDir, "durable run directory")
	operands, e := parseOperands(fs, args)
	if e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return 0
		}
		return a.fail(e)
	}
	if len(operands) != 1 {
		return a.fail(errors.New("inspect requires a run ID, run directory, or saved JSONL file"))
	}
	if e := workbench.Inspect(ctx, operands[0], *dir); e != nil {
		return a.offlineFailure(e)
	}
	return 0
}
