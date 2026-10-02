package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/deepnoodle-ai/decide/internal/runs"
	"github.com/deepnoodle-ai/wonton/cli"
	"github.com/deepnoodle-ai/wonton/humanize"
)

func (a *App) addRuns(app *cli.App) {
	g := app.Group("runs").
		Description("Look at saved runs and continue unfinished ones").
		Run(a.runsList)
	g.Command("list").
		Description("List saved runs, newest first").
		Run(a.runsList)
	g.Command("view").
		Description("Show a run's results (default: the latest run)").
		AddArg(runArg).
		Flags(
			cli.Bool("details", "d").Help("Show the probability of every option"),
			cli.Bool("json").Help("Print results as JSON lines; short for --format json"),
			cli.String("format", "f").Enum(formats...).Help(formatHelp),
		).
		Run(a.runsView)
	g.Command("resume").
		Description("Finish a run that stopped early or had failures (default: the latest run)").
		AddArg(runArg).
		Flags(
			cli.Bool("details", "d").Help("Show the probability of every option"),
			cli.Bool("json").Help("Print results as JSON lines; short for --format json"),
			cli.String("format", "f").Enum(formats...).Help(formatHelp),
			cli.Int("workers").Default(4).Help("How many requests to send at once"),
			cli.String("fail-on").Enum("flagged", "matched").Help("Exit with code 2 if any item is flagged or matched, as you choose"),
		).
		Run(a.runsResume)
}

var runArg = &cli.Arg{Name: "run", Description: "A run ID, or the start of one (default: the latest run)"}

func (a *App) runsList(c *cli.Context) error {
	all, err := runs.List()
	if err != nil {
		return err
	}
	w := c.Stdout()
	if len(all) == 0 {
		fmt.Fprintln(w, "No runs yet. Start one with:")
		fmt.Fprintln(w, "  echo \"I love it\" | decide run sentiment")
		return nil
	}
	templateWidth := len("TEMPLATE")
	for _, r := range all {
		templateWidth = max(templateWidth, len(r.Template.Name))
	}
	idWidth := len(all[0].ID)
	fmt.Fprintln(w, dim(fmt.Sprintf("%-*s  %-*s  %-22s  %s", idWidth, "RUN", templateWidth, "TEMPLATE", "RESULT", "STARTED")))
	for _, r := range all {
		fmt.Fprintf(w, "%-*s  %-*s  %s  %s\n", idWidth, r.ID, templateWidth, r.Template.Name,
			pad(outcome(r), 22), dim(humanize.Time(r.Created)))
	}
	fmt.Fprintf(w, "\n%s decide runs view RUN\n", dim("See results:"))
	return nil
}

// outcome describes how far a run got, in a few words.
func outcome(r *runs.Run) string {
	answered, failures, total := r.Complete, r.Failed, r.Total
	if r.Items > 0 && r.Items != r.Total { // some items were judged in parts
		answered, failures, total = progress(r, nil)
	}
	switch r.Status {
	case runs.Complete:
		return good(fmt.Sprintf("%d answered", answered))
	case runs.Partial:
		return failed(fmt.Sprintf("%d of %d failed", failures, total))
	case runs.Running:
		return value(fmt.Sprintf("running %d/%d", answered+failures, total))
	}
	return fmt.Sprintf("stopped at %d/%d", answered+failures, total)
}

// pad pads styled text to a visible width.
func pad(s string, width int) string {
	visible := len([]rune(stripANSI(s)))
	return s + strings.Repeat(" ", max(width-visible, 0))
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func openRun(c *cli.Context) (*runs.Run, error) {
	var (
		r   *runs.Run
		err error
	)
	if c.NArg() == 0 {
		r, err = runs.Latest()
		if errors.Is(err, runs.ErrNotFound) {
			return nil, cli.Error("There are no saved runs yet").
				Hint("Start one with: echo \"I love it\" | decide run sentiment")
		}
		return r, err
	}
	r, err = runs.Open(c.Arg(0))
	if errors.Is(err, runs.ErrNotFound) {
		return nil, cli.Errorf("There is no run %q", c.Arg(0)).Hint("See your runs with: decide runs")
	}
	return r, err
}

func (a *App) runsView(c *cli.Context) error {
	format, err := formatOf(c)
	if err != nil {
		return err
	}
	r, err := openRun(c)
	if err != nil {
		return err
	}
	if format != "json" {
		fmt.Fprintf(c.Stderr(), "%s\n\n", dim(fmt.Sprintf("Run %s · %s on %s · %s",
			r.ID, r.Template.Name, clean(strings.Join(r.Sources, ", ")), humanize.Time(r.Created))))
	}
	if err := a.printRun(c, r, format, ""); err != nil {
		return err
	}
	if format != "json" {
		summarize(c.Stderr(), r, 0)
	}
	return nil
}

// printRun prints every item of a saved run in a format.
func (a *App) printRun(c *cli.Context, r *runs.Run, format, failOn string) error {
	results, err := r.Results()
	if err != nil {
		return err
	}
	w := newOutput(c, format, r.Template, failOn)
	for _, it := range group(results, marksOf(r.Template)) {
		if err := w.item(it); err != nil {
			return err
		}
	}
	return w.finish(r)
}

func (a *App) runsResume(c *cli.Context) error {
	format, err := formatOf(c)
	if err != nil {
		return err
	}
	if c.Int("workers") < 1 {
		return cli.Error("--workers must be at least 1")
	}
	r, err := openRun(c)
	if err != nil {
		return err
	}
	if r.Active() {
		return cli.Errorf("Run %s is still running in another terminal", r.ID)
	}
	failOn := c.String("fail-on")
	if err := checkFailOn(r.Template, failOn); err != nil {
		return err
	}
	if r.Pending() == 0 {
		fmt.Fprintf(c.Stderr(), "Every item in run %s already has an answer.\n", r.ID)
		if format == "text" {
			fmt.Fprintf(c.Stderr(), "%s decide runs view %s\n", dim("See them with:"), r.ID)
		} else if err := a.printRun(c, r, format, failOn); err != nil {
			return err
		}
		return failExit(c, failOn, marked(r))
	}
	client, err := a.NewClient(r.Provider, r.Model)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.Stderr(), "%s\n\n", dim(fmt.Sprintf("Resuming run %s: %s left · %s %s",
		r.ID, humanize.PluralWord(r.Pending(), "request", "requests"), r.Provider, r.Model)))
	return a.execute(c, r, client, c.Int("workers"), format, failOn)
}
