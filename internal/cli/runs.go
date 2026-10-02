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
			cli.Bool("json").Help("Print results as JSON lines"),
		).
		Run(a.runsView)
	g.Command("resume").
		Description("Finish a run that stopped early or had failures (default: the latest run)").
		AddArg(runArg).
		Flags(
			cli.Bool("details", "d").Help("Show the probability of every option"),
			cli.Bool("json").Help("Print results as JSON lines"),
			cli.Int("workers").Default(4).Help("How many requests to send at once"),
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
	skillWidth := len("SKILL")
	for _, r := range all {
		skillWidth = max(skillWidth, len(r.Skill.Name))
	}
	idWidth := len(all[0].ID)
	fmt.Fprintln(w, dim(fmt.Sprintf("%-*s  %-*s  %-22s  %s", idWidth, "RUN", skillWidth, "SKILL", "RESULT", "STARTED")))
	for _, r := range all {
		fmt.Fprintf(w, "%-*s  %-*s  %s  %s\n", idWidth, r.ID, skillWidth, r.Skill.Name,
			pad(outcome(r), 22), dim(humanize.Time(r.Created)))
	}
	fmt.Fprintf(w, "\n%s decide runs view RUN\n", dim("See results:"))
	return nil
}

// outcome describes how far a run got, in a few words.
func outcome(r *runs.Run) string {
	switch r.Status {
	case runs.Complete:
		return good(fmt.Sprintf("%d answered", r.Complete))
	case runs.Partial:
		return failed(fmt.Sprintf("%d of %d failed", r.Failed, r.Total))
	case runs.Running:
		return value(fmt.Sprintf("running %d/%d", r.Complete+r.Failed, r.Total))
	}
	return fmt.Sprintf("stopped at %d/%d", r.Complete+r.Failed, r.Total)
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
	if c.Bool("json") && c.Bool("details") {
		return cli.Error("Use --json or --details, not both")
	}
	r, err := openRun(c)
	if err != nil {
		return err
	}
	results, err := r.Results()
	if err != nil {
		return err
	}
	if !c.Bool("json") {
		fmt.Fprintf(c.Stderr(), "%s\n\n", dim(fmt.Sprintf("Run %s · %s on %s · %s",
			r.ID, r.Skill.Name, strings.Join(r.Sources, ", "), humanize.Time(r.Created))))
	}
	if c.Bool("json") {
		write := resultWriter(c, r.Skill, nil)
		for _, res := range results {
			if err := write(res); err != nil {
				return err
			}
		}
		return nil
	}
	p := newPrinter(c.Stdout(), r.Skill, c.Bool("details"), nil)
	for _, it := range group(results, p.marks) {
		if err := p.item(it); err != nil {
			return err
		}
	}
	if !c.Bool("json") {
		summarize(c.Stderr(), r, 0)
	}
	return nil
}

func (a *App) runsResume(c *cli.Context) error {
	if c.Bool("json") && c.Bool("details") {
		return cli.Error("Use --json or --details, not both")
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
	if r.Pending() == 0 {
		fmt.Fprintf(c.Stdout(), "Every item in run %s already has an answer.\n", r.ID)
		fmt.Fprintf(c.Stdout(), "%s decide runs view %s\n", dim("See them with:"), r.ID)
		return nil
	}
	client, err := a.NewClient(r.Provider, r.Model)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.Stderr(), "%s\n\n", dim(fmt.Sprintf("Resuming run %s: %s left · %s %s",
		r.ID, humanize.PluralWord(r.Pending(), "request", "requests"), r.Provider, r.Model)))
	return a.execute(c, r, client, c.Int("workers"))
}
