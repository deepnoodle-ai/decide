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
	"slices"
	"strings"
	"time"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/internal/runs"
	"github.com/deepnoodle-ai/decide/internal/source"
	"github.com/deepnoodle-ai/decide/internal/template"
	"github.com/deepnoodle-ai/wonton/cli"
	"github.com/deepnoodle-ai/wonton/humanize"
)

const runHelp = `Run a template: ask its questions about each item in your data, show the
answers, and save them as a run.

What counts as an item depends on your data:
  JSONL, JSON, CSV     each record
  .txt files, stdin    each line
  other files          the whole file
  images               each image, for image templates
  a diff               each hunk: a block of changed lines

Choose another unit with --each file, line, paragraph, section, or
function. A section is the text under a Markdown heading. A function is a
function or method in Go, Python, JavaScript, TypeScript, or Java code. In
a diff, such as git diff output or a .patch file, --each file judges each
changed file and --each line each added line. An
item too long to judge whole is judged in parts, and the parts' answers
are combined.

Folders are read recursively and skip files listed in .gitignore. With no
files named, decide reads from stdin.

Examples:
  decide run code-risk src --include '*.go' --limit 5
  decide run code-risk src --each function
  decide run relevance docs -p question="pricing"
  decide run relevance CHANGELOG.md --each section -p question="tool calling"
  decide run ticket-routing tickets.jsonl --field body
  echo "This is great" | decide run sentiment
  git diff main | decide run code-risk --each hunk
  decide run code-risk . --dry-run

With --fail-on flagged, decide exits with code 2 when any item is flagged,
so a script or CI job can stop on the result. --fail-on matched does the
same for matches.`

func (a *App) addRun(app *cli.App) {
	app.Command("run").
		Description("Run a template on your data").
		Long(runHelp).
		AddArg(&cli.Arg{Name: "template", Description: "The template to run; see them with: decide templates"}).
		AddArg(&cli.Arg{Name: "data", Description: "Files and folders to read (default: stdin)", Variadic: true}).
		Flags(
			cli.Strings("include", "i").Help("Only read files that match this pattern, like '*.go' (repeatable)"),
			cli.Strings("exclude", "x").Help("Skip files that match this pattern (repeatable)"),
			cli.Strings("param", "p").Help("Set a template parameter, as name=value (repeatable)"),
			cli.String("each").Enum(template.Units...).Help("What one item is: file, line, paragraph, section, function, or hunk (default: depends on the data)"),
			cli.String("field").Help("Ask about one field of each JSON record, like body or ticket.body"),
			cli.String("items").Help("Where the records are inside a JSON file, like data.tickets"),
			cli.Int("limit", "n").Help("Stop after this many items"),
			cli.Int("sample").Help("Pick this many items at random"),
			cli.Bool("dry-run").Help("Show what would be asked, without calling the model"),
			cli.Bool("details", "d").Help("Show the probability of every option"),
			cli.Bool("json").Help("Print results as JSON lines"),
			cli.String("fail-on").Enum("flagged", "matched").Help("Exit with code 2 if any item is flagged or matched, as you choose"),
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
		return cli.Error("Which template should decide run?").
			Hint("See the templates with: decide templates\nThen run one, like: decide run sentiment reviews.txt")
	}
	s, err := loadTemplate(c.Arg(0))
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
				s.Name, exampleData(s), s.Name))
	}
	failOn := c.String("fail-on")
	if err := checkFailOn(s, failOn); err != nil {
		return err
	}
	each := c.String("each")
	if each != "" && s.Input == template.Image {
		return cli.Errorf("%s reads one image at a time, so --each does not apply", s.Name)
	}
	if each == "" {
		each = s.Each
	}
	opts := source.Options{
		Input:   s.Input,
		Each:    each,
		Include: c.Strings("include"),
		Exclude: c.Strings("exclude"),
		Field:   c.String("field"),
		Items:   c.String("items"),
		Limit:   limit,
		Sample:  sample,
		Warn:    func(msg string) { fmt.Fprintln(c.Stderr(), dim(clean(msg))) },
	}
	if c.Bool("dry-run") {
		return a.dryRun(c, resolved, paths, opts)
	}

	provider := c.String("provider")
	if provider == "" {
		provider = "typesafe"
		if s.Input == template.Image {
			provider = "cloudflare"
		}
	}
	if s.Input == template.Image && provider != "cloudflare" {
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

	run, err := runs.Create(&runs.Run{Template: resolved, Params: values, Provider: provider, Model: model, Sources: displayPaths(paths)})
	if err != nil {
		return err
	}
	found := newTally()
	err = source.Walk(c.Context(), paths, c.Stdin(), opts, func(it source.Item) error {
		found.add(it)
		return run.Add(it)
	})
	if err == nil && run.Total == 0 {
		err = nothingFound(s, paths, opts)
	}
	if err == nil && run.Total > confirmAbove && !c.Bool("yes") && a.interactiveStdin() {
		if !a.confirm(c, fmt.Sprintf("Run %s on %s? That is %d model requests. [y/N] ", s.Name, found, run.Total)) {
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
	fmt.Fprintf(c.Stderr(), "%s\n", dim(fmt.Sprintf("Running %s on %s · %s %s", s.Name, found, provider, model)))
	if hint := eachHint(found, c.String("each") != "", s.Each, paths); hint != "" {
		fmt.Fprintln(c.Stderr(), dim(hint))
	}
	fmt.Fprintln(c.Stderr())
	return a.execute(c, run, client, workers, failOn)
}

// execute runs or resumes a run, printing each result and a summary. With
// failOn set to "flagged" or "matched", a complete run with such items
// exits with code 2.
func (a *App) execute(c *cli.Context, run *runs.Run, client *decide.Client, workers int, failOn string) error {
	saved, err := run.Results()
	if err != nil {
		return err
	}
	out := newCollector(marksOf(run.Template), saved, itemWriter(c, run.Template))
	start := time.Now()
	err = run.Execute(c.Context(), client, workers, out.add)
	elapsed := time.Since(start)
	if ferr := out.finish(); ferr != nil && err == nil {
		err = ferr
	}
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
	marked := summarize(c.Stderr(), run, elapsed)
	switch {
	case c.Context().Err() != nil:
		return cli.Exit(130)
	case run.Status != runs.Complete:
		return cli.Exit(1)
	}
	return failExit(c, failOn, marked)
}

// checkFailOn refuses --fail-on when the template never marks an item that
// way, since the run could never fail.
func checkFailOn(s *template.Template, failOn string) error {
	if (failOn == "flagged" && len(s.Flags) == 0) || (failOn == "matched" && len(s.Matches) == 0) {
		return cli.Errorf("%s never marks an item %s, so --fail-on %s would never fail", s.Name, failOn, failOn).
			Hint(fmt.Sprintf("See what it marks with: decide templates show %s", s.Name))
	}
	return nil
}

// failExit exits with code 2 when failOn names a mark that count items
// carry.
func failExit(c *cli.Context, failOn string, marked map[string]int) error {
	if failOn == "" || marked[failOn] == 0 {
		return nil
	}
	fmt.Fprintln(c.Stderr(), dim(fmt.Sprintf("Exiting with code 2 because %s %s (--fail-on %s)",
		humanize.PluralWord(marked[failOn], "item was", "items were"), failOn, failOn)))
	return cli.Exit(2)
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

// itemWriter prints items as text, or as JSON lines with --json.
func itemWriter(c *cli.Context, s *template.Template) func(item) error {
	if c.Bool("json") {
		enc := json.NewEncoder(c.Stdout())
		return func(it item) error { return enc.Encode(it.json()) }
	}
	return newPrinter(c.Stdout(), s, c.Bool("details")).item
}

// progress counts a run's items, rather than its requests: how many were
// answered, how many failed, and how many there are. It calls each, when
// not nil, with every answered item.
func progress(run *runs.Run, each func(item)) (answered, failures, total int) {
	results, _ := run.Results()
	for _, it := range group(results, marksOf(run.Template)) {
		if it.Status != "complete" {
			failures++
			continue
		}
		answered++
		if each != nil {
			each(it)
		}
	}
	total = run.Items
	if total == 0 {
		total = run.Total // a run saved before items were counted
	}
	return answered, failures, total
}

// marked counts a run's "flagged" and "matched" items.
func marked(run *runs.Run) map[string]int {
	m := marksOf(run.Template)
	counts := map[string]int{}
	progress(run, func(it item) {
		if m.has(it.Result, flagged) {
			counts["flagged"]++
		}
		if m.has(it.Result, matched) {
			counts["matched"]++
		}
	})
	return counts
}

// summarize prints a run's summary and returns how many items were
// "flagged" and "matched".
func summarize(w io.Writer, run *runs.Run, elapsed time.Duration) map[string]int {
	m := marksOf(run.Template)
	var flaggedItems, matchedItems []string
	answered, failures, total := progress(run, func(it item) {
		if m.has(it.Result, flagged) {
			flaggedItems = append(flaggedItems, clean(it.Source))
		}
		if m.has(it.Result, matched) {
			matchedItems = append(matchedItems, clean(it.Source))
		}
	})
	parts := []string{good(fmt.Sprintf("✓ %d answered", answered))}
	if failures > 0 {
		parts = append(parts, failed(fmt.Sprintf("✗ %d failed", failures)))
	}
	if left := total - answered - failures; left > 0 {
		parts = append(parts, fmt.Sprintf("%d left", left))
	}
	if len(m.flags) > 0 && answered > 0 {
		if len(flaggedItems) == 0 {
			parts = append(parts, good("nothing flagged"))
		} else {
			parts = append(parts, failed(fmt.Sprintf("! %d flagged", len(flaggedItems))))
		}
	}
	if len(m.matches) > 0 && answered > 0 {
		if len(matchedItems) == 0 {
			parts = append(parts, "no matches")
		} else {
			parts = append(parts, bold(good(fmt.Sprintf("● %d matched", len(matchedItems)))))
		}
	}
	line := strings.Join(parts, "  ")
	if elapsed > 0 {
		line += "  " + dim(humanize.DurationShort(elapsed.Round(100*time.Millisecond)))
	}
	fmt.Fprintln(w, line)
	list(w, "Flagged:", flaggedItems)
	list(w, "Matched:", matchedItems)
	fmt.Fprintf(w, "%s %s\n", dim("Saved as run"), run.ID)
	switch run.Status {
	case runs.Interrupted:
		fmt.Fprintf(w, "%s decide runs resume %s\n", dim("Stopped early. Continue with:"), run.ID)
	case runs.Partial:
		fmt.Fprintf(w, "%s decide runs resume %s\n", dim("Retry the failed items with:"), run.ID)
	default:
		fmt.Fprintf(w, "%s decide runs view %s\n", dim("See these results again with:"), run.ID)
	}
	return map[string]int{"flagged": len(flaggedItems), "matched": len(matchedItems)}
}

// list prints the first few sources after a label, such as "Flagged:".
func list(w io.Writer, label string, sources []string) {
	if len(sources) == 0 {
		return
	}
	const shown = 5
	text := strings.Join(sources[:min(len(sources), shown)], ", ")
	if len(sources) > shown {
		text += fmt.Sprintf(", and %d more", len(sources)-shown)
	}
	fmt.Fprintf(w, "%s %s\n", dim(label), text)
}

func (a *App) dryRun(c *cli.Context, s *template.Template, paths []string, opts source.Options) error {
	if c.Bool("json") {
		enc := json.NewEncoder(c.Stdout())
		return source.Walk(c.Context(), paths, c.Stdin(), opts, func(it source.Item) error { return enc.Encode(it) })
	}
	const shown = 20
	var items []source.Item
	found := newTally()
	err := source.Walk(c.Context(), paths, c.Stdin(), opts, func(it source.Item) error {
		if found.items() < shown {
			items = append(items, it)
		}
		found.add(it)
		return nil
	})
	if err != nil {
		return err
	}
	total := found.items()
	if total == 0 {
		return nothingFound(s, paths, opts)
	}
	w := c.Stdout()
	fmt.Fprintf(w, "%s would look at %s:\n\n", bold(s.Name), found)
	for _, it := range items {
		line := "  " + clean(it.Label)
		if preview := previewOf(it.Value, 60); preview != "" {
			line += "  " + dim(preview)
		}
		if len(it.Parts) > 0 {
			line += "  " + dim(fmt.Sprintf("in %d parts", len(it.Parts)))
		}
		fmt.Fprintln(w, line)
	}

	if total > shown {
		fmt.Fprintf(w, "  %s\n", dim(fmt.Sprintf("… and %d more", total-shown)))
	}
	if hint := eachHint(found, c.String("each") != "", s.Each, paths); hint != "" {
		fmt.Fprintf(w, "\n%s\n", dim(hint))
	}
	fmt.Fprintf(w, "\nand ask each one:\n\n")
	m := marksOf(s)
	for _, q := range s.Questions {
		var body struct {
			Instructions any `json:"instructions"`
		}
		json.Unmarshal(q.Raw, &body)
		fmt.Fprintf(w, "  %s  %s\n", bold(clean(q.Key)), dim("("+describe(q.Raw)+")"))
		fmt.Fprint(w, wrap(clean(fmt.Sprint(body.Instructions)), 72, "    "))
		if when := flagText(m.flags[q.Key]); when != "" {
			fmt.Fprintf(w, "    %s %s\n", dim("flagged when"), when)
		}
		if when := flagText(m.matches[q.Key]); when != "" {
			fmt.Fprintf(w, "    %s %s\n", dim("matches when"), when)
		}
	}
	fmt.Fprintf(w, "\n%s\n", dim("Nothing was sent to the model. Remove --dry-run to run it."))
	return nil
}

func nothingFound(s *template.Template, paths []string, opts source.Options) error {
	where := "the input"
	if len(paths) > 0 {
		where = strings.Join(paths, ", ")
	}
	err := cli.Errorf("Found nothing for %s to look at in %s", s.Name, where)
	switch {
	case len(opts.Include) > 0:
		return err.Hint("No files matched --include " + strings.Join(opts.Include, ", ") + ". Patterns like '*.go' match at any depth.")
	case s.Input == template.Image:
		return err.Hint("Image templates read PNG, JPEG, and WebP files.")
	}
	return err
}

func loadTemplate(name string) (*template.Template, error) {
	s, err := template.Load(name)
	var nf *template.NotFoundError
	if errors.As(err, &nf) {
		e := cli.Errorf("There is no template named %q", name)
		if _, statErr := os.Stat(name); statErr == nil {
			return nil, e.Hint("Put the template name first, then your data: decide run TEMPLATE " + name)
		}
		return nil, e.Hint("See the templates with: decide templates")
	}
	return s, err
}

func parseParams(args []string) (map[string]string, error) {
	values := map[string]string{}
	for _, arg := range args {
		name, val, ok := strings.Cut(arg, "=")
		if !ok || name == "" {
			return nil, cli.Errorf("--param %q needs a name and a value", arg).
				Hint(`Write it as name=value, like: --param question="pricing"`)
		}
		values[name] = val
	}
	return values, nil
}

func paramError(err error) error {
	var pe *template.ParamError
	if !errors.As(err, &pe) {
		return err
	}
	if pe.Missing != "" {
		p := pe.Template.Parameters[pe.Missing]
		hint := fmt.Sprintf("Set it with: --param %s=\"...\"", pe.Missing)
		if p.Description != "" {
			hint = p.Description + ".\n" + hint
		}
		return cli.Error(pe.Msg).Hint(hint)
	}
	names := pe.Template.ParameterNames()
	if len(names) == 0 {
		return cli.Error(pe.Msg).Hint(pe.Template.Name + " has no parameters.")
	}
	return cli.Error(pe.Msg).Hint("Its parameters are: " + strings.Join(names, ", "))
}

// count says how many items there are, in the template's terms.
// tally counts the items a walk finds, by unit, and the requests they
// take.
type tally struct {
	units    map[string]int
	inParts  int // items too large to judge whole
	requests int
	code     int // whole files in a language whose functions can be found
}

func newTally() *tally { return &tally{units: map[string]int{}} }

func (t *tally) add(it source.Item) {
	t.units[it.Unit]++
	t.requests += max(len(it.Parts), 1)
	if it.Unit == template.EachFile && !it.Diff && source.Language(it.Label) != "" {
		t.code++
	}
	if len(it.Parts) > 0 {
		t.inParts++
	}
}

func (t *tally) items() int {
	n := 0
	for _, c := range t.units {
		n += c
	}
	return n
}

// unitNames lists units in the order a tally names them.
var unitNames = [][3]string{
	{template.EachFile, "file", "files"},
	{template.EachFunction, "function", "functions"},
	{template.EachHunk, "hunk", "hunks"},
	{template.EachSection, "section", "sections"},
	{template.EachParagraph, "paragraph", "paragraphs"},
	{template.EachLine, "line", "lines"},
	{source.UnitRecord, "record", "records"},
	{source.UnitImage, "image", "images"},
}

// String says what the items are, such as "3 files and 120 records" or
// "41 paragraphs (1 judged in parts, 43 requests)".
func (t *tally) String() string {
	var parts []string
	for _, u := range unitNames {
		if n := t.units[u[0]]; n > 0 {
			parts = append(parts, humanize.PluralWord(n, u[1], u[2]))
		}
	}
	s := strings.Join(parts, ", ")
	if len(parts) > 1 {
		s = strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
	if t.inParts > 0 {
		s += fmt.Sprintf(" (%d judged in parts, %d requests)", t.inParts, t.requests)
	}
	return s
}

// eachHint points out --each when the data chose the unit, so splitting
// a file or not is never a surprise. For code, it points out --each
// function even when the template chose whole files.
func eachHint(t *tally, chosen bool, templateEach string, paths []string) string {
	if chosen || len(paths) == 0 || slices.Equal(paths, []string{"-"}) {
		return ""
	}
	switch {
	case t.code > 0 && t.code*2 >= t.units[template.EachFile]:
		return "Each file is one item. To judge each function instead, add --each function."
	case templateEach != "":
		return ""
	case t.units[template.EachFile] > 0:
		return "Each file is one item. To judge each section or paragraph instead, add --each section or --each paragraph."
	case t.units[template.EachLine] > 0:
		return "Each line is one item. To judge each file as a whole instead, add --each file."
	}
	return ""
}

func exampleData(s *template.Template) string {
	switch {
	case s == nil:
		return "data.jsonl"
	case s.Input == template.Image:
		return "photos"
	case s.Each == template.EachFile:
		return "src"
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
