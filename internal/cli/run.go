package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/internal/runs"
	"github.com/deepnoodle-ai/decide/internal/skill"
	"github.com/deepnoodle-ai/decide/internal/source"
	"github.com/deepnoodle-ai/wonton/cli"
	"github.com/deepnoodle-ai/wonton/humanize"
)

const runHelp = `Run a skill: ask its questions about each item in your data, show the
answers, and save them as a run.

What counts as an item depends on the skill:
  file skills     each file in the folders you name (binary files are skipped)
  record skills   each line of a text or JSONL file, or each element of a
                  JSON array
  image skills    each image file

Folders are read recursively and skip files listed in .gitignore. With no
files named, decide reads from stdin.

Examples:
  decide run code-risk src --include '*.go' --limit 5
  decide run ticket-routing tickets.jsonl --field body
  decide run relevance notes.txt -p question="Is this about pricing?"
  echo "This is great" | decide run sentiment
  decide run code-risk . --dry-run`

func (a *App) addRun(app *cli.App) {
	app.Command("run").
		Description("Run a skill on your data").
		Long(runHelp).
		AddArg(&cli.Arg{Name: "skill", Description: "The skill to run; see them with: decide skills"}).
		AddArg(&cli.Arg{Name: "data", Description: "Files and folders to read (default: stdin)", Variadic: true}).
		Flags(
			cli.Strings("include", "i").Help("Only read files that match this pattern, like '*.go' (repeatable)"),
			cli.Strings("exclude", "x").Help("Skip files that match this pattern (repeatable)"),
			cli.Strings("param", "p").Help("Set a skill parameter, as name=value (repeatable)"),
			cli.String("field").Help("Ask about one field of each JSON record, like body or ticket.body"),
			cli.String("items").Help("Where the records are inside a JSON file, like data.tickets"),
			cli.Int("limit", "n").Help("Stop after this many items"),
			cli.Int("sample").Help("Pick this many items at random"),
			cli.Bool("dry-run").Help("Show what would be asked, without calling the model"),
			cli.Bool("details", "d").Help("Show the probability of every option"),
			cli.Bool("json").Help("Print results as JSON lines"),
			cli.String("provider").Env("DECIDE_PROVIDER").Enum("typesafe", "cloudflare").
				Help("Model provider: typesafe or cloudflare (default: typesafe; cloudflare for images)"),
			cli.String("model", "m").Env("DECIDE_MODEL").Help("Model name (default: the provider's default)"),
			cli.Int("workers").Default(4).Help("How many requests to send at once"),
			cli.Bool("yes", "y").Help(fmt.Sprintf("Don't ask before running more than %d items", confirmAbove)),
		).
		Run(a.run)
}

func (a *App) run(c *cli.Context) error {
	if c.NArg() == 0 {
		return cli.Error("Which skill should decide run?").
			Hint("See the skills with: decide skills\nThen run one, like: decide run sentiment reviews.txt")
	}
	s, err := loadSkill(c.Arg(0))
	if err != nil {
		return err
	}
	values, err := parseParams(c.Strings("param"))
	if err != nil {
		return err
	}
	resolved, err := s.Resolve(values)
	if err != nil {
		return paramError(err)
	}
	paths := c.Args()[1:]
	limit, sample, workers := c.Int("limit"), c.Int("sample"), c.Int("workers")
	switch {
	case limit < 0 || sample < 0:
		return cli.Error("--limit and --sample must be positive numbers")
	case limit > 0 && sample > 0:
		return cli.Error("Use --limit or --sample, not both").
			Hint("--limit takes the first items; --sample picks items at random.")
	case workers < 1:
		return cli.Error("--workers must be at least 1")
	case c.Bool("json") && c.Bool("details"):
		return cli.Error("Use --json or --details, not both").
			Hint("JSON output always includes every probability.")
	}
	if len(paths) == 0 && a.interactiveStdin() {
		return cli.Errorf("What should %s look at?", s.Name).
			Hint(fmt.Sprintf("Name files or folders: decide run %s %s\nOr pipe in text:      echo \"some text\" | decide run %s",
				s.Name, exampleData(s.Input), s.Name))
	}
	opts := source.Options{
		Input:   s.Input,
		Include: c.Strings("include"),
		Exclude: c.Strings("exclude"),
		Field:   c.String("field"),
		Items:   c.String("items"),
		Limit:   limit,
		Sample:  sample,
		Warn:    func(msg string) { fmt.Fprintln(c.Stderr(), dim(msg)) },
	}
	if c.Bool("dry-run") {
		return a.dryRun(c, resolved, paths, opts)
	}

	provider := c.String("provider")
	if provider == "" {
		provider = "typesafe"
		if s.Input == skill.Image {
			provider = "cloudflare"
		}
	}
	if s.Input == skill.Image && provider != "cloudflare" {
		return cli.Errorf("%s reads images, which need the cloudflare provider", s.Name).
			Hint("Run it with --provider cloudflare")
	}
	model := c.String("model")
	if model == "" {
		model = defaultModels[provider]
	}
	client, err := a.NewClient(provider, model)
	if err != nil {
		return err
	}

	run, err := runs.Create(&runs.Run{Skill: resolved, Params: values, Provider: provider, Model: model, Sources: displayPaths(paths)})
	if err != nil {
		return err
	}
	err = source.Walk(c.Context(), paths, c.Stdin(), opts, run.Add)
	if err == nil && run.Total == 0 {
		err = nothingFound(s, paths, opts)
	}
	if err == nil && run.Total > confirmAbove && !c.Bool("yes") && a.interactiveStdin() {
		if !a.confirm(c, fmt.Sprintf("Run %s on %s? Each one is a model request. [y/N] ", s.Name, count(run.Total, s.Input))) {
			run.Discard()
			fmt.Fprintln(c.Stderr(), "\nNothing was sent. To start small, add --limit 20.")
			if c.Context().Err() != nil {
				return cli.Exit(130)
			}
			return nil
		}
	}
	if err == nil {
		err = run.Ready()
	}
	if err != nil {
		run.Discard()
		return err
	}
	fmt.Fprintf(c.Stderr(), "%s\n\n", dim(fmt.Sprintf("Running %s on %s · %s %s",
		s.Name, count(run.Total, s.Input), provider, model)))
	return a.execute(c, run, client, workers)
}

// execute runs or resumes a run, printing each result and a summary.
func (a *App) execute(c *cli.Context, run *runs.Run, client *decide.Client, workers int) error {
	out := resultWriter(c, run.Skill)
	start := time.Now()
	err := run.Execute(c.Context(), client, workers, out)
	elapsed := time.Since(start)
	var fatal *runs.FatalError
	if errors.As(err, &fatal) {
		return cli.Errorf("The %s provider rejected the request: %s", run.Provider, friendlyError(fatal.Error())).
			Hint(credentialHint(run.Provider) + "\nThen continue with: decide runs resume " + run.ID)
	}
	var repeated *runs.RepeatedError
	if errors.As(err, &repeated) {
		summarize(c.Stderr(), run, elapsed)
		return cli.Errorf("Stopped because %s", friendlyError(repeated.Error())).
			Hint("Fix the problem, then resume as shown above.\nIf the model name is wrong, start a new run with --model.")
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	summarize(c.Stderr(), run, elapsed)
	switch {
	case c.Context().Err() != nil:
		return cli.Exit(130)
	case run.Status != runs.Complete:
		return cli.Exit(1)
	}
	return nil
}

// confirmAbove is the number of items above which run asks before
// starting, when someone is at the terminal.
const confirmAbove = 100

// confirm asks a yes-or-no question. Ctrl-C counts as no.
func (a *App) confirm(c *cli.Context, question string) bool {
	fmt.Fprint(c.Stderr(), question)
	answer := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(c.Stdin()).ReadString('\n')
		answer <- strings.ToLower(strings.TrimSpace(line))
	}()
	select {
	case got := <-answer:
		return got == "y" || got == "yes"
	case <-c.Context().Done():
		return false
	}
}

func credentialHint(provider string) string {
	if provider == "cloudflare" {
		return "Check CLOUDFLARE_AUTH_TOKEN and CLOUDFLARE_ACCOUNT_ID."
	}
	return "Check that TYPESAFE_API_KEY holds a valid key."
}

// resultWriter prints results as text, or as JSON lines with --json.
func resultWriter(c *cli.Context, s *skill.Skill) func(runs.Result) error {
	if c.Bool("json") {
		enc := json.NewEncoder(c.Stdout())
		return func(r runs.Result) error { return enc.Encode(r) }
	}
	return newPrinter(c.Stdout(), s, c.Bool("details")).result
}

func summarize(w io.Writer, run *runs.Run, elapsed time.Duration) {
	parts := []string{good(fmt.Sprintf("✓ %d answered", run.Complete))}
	if run.Failed > 0 {
		parts = append(parts, failed(fmt.Sprintf("✗ %d failed", run.Failed)))
	}
	if left := run.Total - run.Complete - run.Failed; left > 0 {
		parts = append(parts, fmt.Sprintf("%d left", left))
	}
	flagged := flaggedSources(run)
	if flagged != nil && run.Complete > 0 {
		if len(flagged) == 0 {
			parts = append(parts, good("nothing flagged"))
		} else {
			parts = append(parts, failed(fmt.Sprintf("! %d flagged", len(flagged))))
		}
	}
	line := strings.Join(parts, "  ")
	if elapsed > 0 {
		line += "  " + dim(humanize.DurationShort(elapsed.Round(100*time.Millisecond)))
	}
	fmt.Fprintln(w, line)
	if len(flagged) > 0 {
		const shown = 5
		list := strings.Join(flagged[:min(len(flagged), shown)], ", ")
		if len(flagged) > shown {
			list += fmt.Sprintf(", and %d more", len(flagged)-shown)
		}
		fmt.Fprintf(w, "%s %s\n", dim("Flagged:"), list)
	}
	fmt.Fprintf(w, "%s %s\n", dim("Saved as run"), run.ID)
	switch run.Status {
	case runs.Interrupted:
		fmt.Fprintf(w, "%s decide runs resume %s\n", dim("Stopped early. Continue with:"), run.ID)
	case runs.Partial:
		fmt.Fprintf(w, "%s decide runs resume %s\n", dim("Retry the failed items with:"), run.ID)
	default:
		fmt.Fprintf(w, "%s decide runs view %s\n", dim("See these results again with:"), run.ID)
	}
}

// flaggedSources lists the items with an answer that needs attention, or
// returns nil when the skill has no flags.
func flaggedSources(run *runs.Run) []string {
	flags := flagsOf(run.Skill)
	if len(flags) == 0 {
		return nil
	}
	results, err := run.Results()
	if err != nil {
		return nil
	}
	out := []string{}
	for _, res := range results {
		if res.Status == "complete" && isFlagged(flags, res) {
			out = append(out, clean(res.Source))
		}
	}
	return out
}

func (a *App) dryRun(c *cli.Context, s *skill.Skill, paths []string, opts source.Options) error {
	if c.Bool("json") {
		enc := json.NewEncoder(c.Stdout())
		return source.Walk(c.Context(), paths, c.Stdin(), opts, func(it source.Item) error { return enc.Encode(it) })
	}
	const shown = 20
	var items []source.Item
	total := 0
	err := source.Walk(c.Context(), paths, c.Stdin(), opts, func(it source.Item) error {
		if total < shown {
			items = append(items, it)
		}
		total++
		return nil
	})
	if err != nil {
		return err
	}
	if total == 0 {
		return nothingFound(s, paths, opts)
	}
	w := c.Stdout()
	fmt.Fprintf(w, "%s would look at %s:\n\n", bold(s.Name), count(total, s.Input))
	for _, it := range items {
		line := "  " + it.Label
		if preview := previewOf(it.Value, 60); preview != "" {
			line += "  " + dim(preview)
		}
		fmt.Fprintln(w, line)
	}
	if total > shown {
		fmt.Fprintf(w, "  %s\n", dim(fmt.Sprintf("… and %d more", total-shown)))
	}
	fmt.Fprintf(w, "\nand ask each one:\n\n")
	flags := flagsOf(s)
	for _, q := range s.Questions {
		var body struct {
			Instructions any `json:"instructions"`
		}
		json.Unmarshal(q.Raw, &body)
		fmt.Fprintf(w, "  %s  %s\n", bold(q.Key), dim("("+describe(q.Raw)+")"))
		fmt.Fprint(w, wrap(fmt.Sprint(body.Instructions), 72, "    "))
		if when := flagText(flags[q.Key]); when != "" {
			fmt.Fprintf(w, "    %s %s\n", dim("flagged when"), when)
		}
	}
	fmt.Fprintf(w, "\n%s\n", dim("Nothing was sent to the model. Remove --dry-run to run it."))
	return nil
}

func nothingFound(s *skill.Skill, paths []string, opts source.Options) error {
	where := "the input"
	if len(paths) > 0 {
		where = strings.Join(paths, ", ")
	}
	err := cli.Errorf("Found nothing for %s to look at in %s", s.Name, where)
	switch {
	case len(opts.Include) > 0:
		return err.Hint("No files matched --include " + strings.Join(opts.Include, ", ") + ". Patterns like '*.go' match at any depth.")
	case s.Input == skill.Image:
		return err.Hint("Image skills read PNG, JPEG, and WebP files.")
	}
	return err
}

func loadSkill(name string) (*skill.Skill, error) {
	s, err := skill.Load(name)
	var nf *skill.NotFoundError
	if errors.As(err, &nf) {
		e := cli.Errorf("There is no skill named %q", name)
		if _, statErr := os.Stat(name); statErr == nil {
			return nil, e.Hint("Put the skill name first, then your data: decide run SKILL " + name)
		}
		return nil, e.Hint("See the skills with: decide skills")
	}
	return s, err
}

func parseParams(args []string) (map[string]string, error) {
	values := map[string]string{}
	for _, arg := range args {
		name, val, ok := strings.Cut(arg, "=")
		if !ok || name == "" {
			return nil, cli.Errorf("--param %q needs a name and a value", arg).
				Hint(`Write it as name=value, like: --param question="Is this about pricing?"`)
		}
		values[name] = val
	}
	return values, nil
}

func paramError(err error) error {
	var pe *skill.ParamError
	if !errors.As(err, &pe) {
		return err
	}
	if pe.Missing != "" {
		p := pe.Skill.Parameters[pe.Missing]
		hint := fmt.Sprintf("Set it with: --param %s=\"...\"", pe.Missing)
		if p.Description != "" {
			hint = p.Description + ".\n" + hint
		}
		return cli.Error(pe.Msg).Hint(hint)
	}
	names := pe.Skill.ParameterNames()
	if len(names) == 0 {
		return cli.Error(pe.Msg).Hint(pe.Skill.Name + " has no parameters.")
	}
	return cli.Error(pe.Msg).Hint("Its parameters are: " + strings.Join(names, ", "))
}

// count says how many items there are, in the skill's terms.
func count(n int, in skill.Input) string {
	switch in {
	case skill.File:
		return humanize.PluralWord(n, "file", "files")
	case skill.Image:
		return humanize.PluralWord(n, "image", "images")
	}
	return humanize.PluralWord(n, "record", "records")
}

func exampleData(in skill.Input) string {
	switch in {
	case skill.File:
		return "src"
	case skill.Image:
		return "photos"
	}
	return "data.jsonl"
}

// displayPaths names the data a run read, shortening the home directory
// to ~.
func displayPaths(paths []string) []string {
	if len(paths) == 0 {
		return []string{"stdin"}
	}
	home, _ := os.UserHomeDir()
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = p
		if abs, err := filepath.Abs(p); err == nil && home != "" {
			if rel, err := filepath.Rel(home, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				out[i] = filepath.Join("~", rel)
			}
		}
	}
	return out
}
