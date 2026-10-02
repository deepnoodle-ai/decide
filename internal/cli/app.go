// Package cli implements the decide command.
package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/backend"
	"github.com/deepnoodle-ai/wonton/cli"
	"github.com/deepnoodle-ai/wonton/tty"
)

// App runs the decide command with the given input and output.
type App struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// NewClient connects to a provider. It defaults to reading credentials
	// from the environment; tests replace it with a fake server.
	NewClient func(provider, model string) (*decide.Client, error)

	// Version is printed by "decide --version".
	Version string
}

const overview = `Decide asks typed questions about your data and saves every answer.

A template is a set of questions, like "Is this file risky?" or "Which
queue should this ticket go to?". Run a template on files, folders, JSON, or
text, and Decide shows each answer with its probability.

Get started:
  decide templates                    see the built-in templates
  decide run sentiment reviews.txt    run a template on each line of a file
  decide runs view                    look at your latest results

Set TYPESAFE_API_KEY before your first run.`

// Run executes the command line and returns the exit code.
func (a *App) Run(ctx context.Context, args []string) int {
	if a.Stdin == nil {
		a.Stdin = os.Stdin
	}
	if a.Stdout == nil {
		a.Stdout = os.Stdout
	}
	if a.Stderr == nil {
		a.Stderr = os.Stderr
	}
	if a.NewClient == nil {
		a.NewClient = connect
	}
	app := cli.New("decide").
		Description("Ask typed questions about your data").
		Long(overview).
		SetStdin(a.Stdin).SetStdout(a.Stdout).SetStderr(a.Stderr).
		ForceInteractive(false)
	if a.Version != "" {
		app.Version(a.Version)
	}
	a.addRun(app)
	a.addTemplates(app)
	a.addRuns(app)

	err := app.ExecuteContext(ctx, args)
	var exit *cli.ExitError
	switch {
	case err == nil || cli.IsHelpRequested(err):
		return 0
	case errors.As(err, &exit):
		return exit.Code // the command already explained what happened
	case ctx.Err() != nil:
		return 130
	}
	app.PrintError(safeError{err})
	return cli.GetExitCode(err)
}

// safeError removes control characters from an error's message, which can
// carry file names and other text from outside the program, while keeping
// its lines.
type safeError struct{ error }

func (e safeError) Error() string {
	lines := strings.Split(e.error.Error(), "\n")
	for i, line := range lines {
		lines[i] = printable(line)
	}
	return strings.Join(lines, "\n")
}

// interactiveStdin reports whether stdin is a terminal, where nobody is
// piping data in.
func (a *App) interactiveStdin() bool {
	f, ok := a.Stdin.(*os.File)
	return ok && tty.IsTerminal(f)
}

// Default models, shown in output when no model is chosen.
var defaultModels = map[string]string{"typesafe": "jev-latest", "cloudflare": "clef"}

// connect builds a client from environment credentials.
func connect(provider, model string) (*decide.Client, error) {
	cfg := backend.Config{Provider: backend.Provider(provider), Model: model}
	switch provider {
	case "cloudflare":
		cfg.APIKey = os.Getenv("CLOUDFLARE_AUTH_TOKEN")
		cfg.AccountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
		cfg.BaseURL = os.Getenv("CLOUDFLARE_BASE_URL")
		if cfg.APIKey == "" || cfg.AccountID == "" {
			return nil, cli.Error("Cloudflare credentials are not set").
				Hint("Set your Workers AI API token and account ID:\n  export CLOUDFLARE_AUTH_TOKEN=...\n  export CLOUDFLARE_ACCOUNT_ID=...")
		}
	default:
		cfg.APIKey = os.Getenv("TYPESAFE_API_KEY")
		cfg.BaseURL = os.Getenv("TYPESAFE_BASE_URL")
		if cfg.APIKey == "" {
			return nil, cli.Error("TYPESAFE_API_KEY is not set").
				Hint("Set your TypeSafe API key:\n  export TYPESAFE_API_KEY=...\nTo check your data first without a key, add --dry-run.")
		}
	}
	return backend.NewClient(cfg)
}
