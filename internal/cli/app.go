// Package cli implements the experimental dataset command-line interface.
package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/deepnoodle-ai/decide"
	wonton "github.com/deepnoodle-ai/wonton/cli"
)

type App struct {
	In        io.Reader
	Out, Err  io.Writer
	NewClient func() (*decide.Client, error)
}

func (a *App) Run(ctx context.Context, args []string) int {
	if a.In == nil {
		a.In = os.Stdin
	}
	if a.Out == nil {
		a.Out = os.Stdout
	}
	if a.Err == nil {
		a.Err = os.Stderr
	}
	if ctx.Err() != nil {
		return 130
	}
	framework := a.commands()
	if err := framework.ExecuteContext(ctx, normalizeColorArgs(args)); err != nil {
		if wonton.IsHelpRequested(err) {
			return 0
		}
		if ctx.Err() != nil {
			return 130
		}
		var commandError *wonton.ExitError
		if errors.As(err, &commandError) {
			return wonton.GetExitCode(err)
		}
		return a.fail(err)
	}
	return 0
}

func (a *App) commands() *wonton.App {
	app := wonton.New("decide").Description("Apply decision models to datasets and save results.").Long(helpText).
		SetStdin(a.In).SetStdout(a.Out).SetStderr(a.Err).ForceInteractive(false).
		SetColorEnabled(newResultPrinter(a.Out, "auto", false).color)
	for _, entry := range []struct{ name, description string }{
		{"run", "Show each item's decisions and save the run"},
		{"plan", "Preview inputs and questions without model calls"},
	} {
		binding := a.datasetCommand(entry.name)
		binding.attach(app.Command(entry.name).Description(entry.description).
			Args("skill-or-source?...").Long("Usage: decide " + entry.name + " SKILL SOURCES... [flags]\nUse --skill NAME or --pattern NAME to choose the judgment with a flag.\nFiles, directories, URLs, and '-' for stdin can be mixed."))
	}
	sources := app.Group("sources").Description("List and preview selected input data")
	for _, op := range []string{"list", "preview"} {
		binding := a.sourcesCommand(op)
		binding.attach(sources.Command(op).Description(map[string]string{"list": "List selected input items", "preview": "Show input data (default: five items)"}[op]).Args("sources?..."))
	}
	for _, kind := range []string{"skills", "patterns"} {
		group := app.Group(kind).Description(map[string]string{"skills": "List and manage reusable question definitions", "patterns": "List and show configurations for run --pattern"}[kind])
		ops := []string{"list", "show"}
		if kind == "skills" {
			ops = append(ops, "new", "edit", "validate", "test")
		}
		for _, op := range ops {
			binding := a.libraryCommand(kind, op)
			descriptions := map[string]string{
				"list": "List available skills", "show": "Show a skill's description, parameters, and documentation",
				"new": "Create a skill by copying an existing one", "edit": "Edit a skill using VISUAL or EDITOR",
				"validate": "Validate a skill's configuration", "test": "Validate a skill and its example inputs",
			}
			if kind == "patterns" {
				descriptions = map[string]string{"list": "List available patterns", "show": "Print a pattern's configuration as JSON"}
			}
			command := group.Command(op).Description(descriptions[op])
			if op == "list" {
				command.Args()
			} else {
				command.Args("name")
			}
			binding.attach(command)
		}
		a.libraryCommand(kind, "list").attachGroup(group)
	}
	runs := app.Group("runs").Description("Read and manage saved runs")
	for _, op := range []string{"list", "view", "show", "watch", "resume", "export"} {
		binding := a.runsCommand(op)
		command := runs.Command(op).Description(map[string]string{"list": "List saved runs", "view": "Read saved decisions without model calls", "show": "Show status and counts", "watch": "Watch run status", "resume": "Continue an interrupted run", "export": "Export full JSONL evidence"}[op])
		if op == "list" {
			command.Args()
		} else {
			command.Args("run-id")
		}
		binding.attach(command)
	}
	a.runsCommand("list").attachGroup(runs)
	return app
}

// Wonton reserves bare --color as a toggle. Keep Decide's three-way option
// by passing it to Wonton in the unambiguous --color=MODE form.
func normalizeColorArgs(args []string) []string {
	normalized := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return append(normalized, args[i:]...)
		}
		if arg == "--color" {
			value := ""
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				value = args[i]
			}
			arg += "=" + value
		}
		normalized = append(normalized, arg)
	}
	return normalized
}
