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

	// envClient is set when NewClient is the default that reads the
	// environment, not a replacement.
	envClient bool
}

const overview = `Decide asks typed questions about your data and saves every answer.

A template is a set of questions, like "Is this file risky?" or "Which
queue should this ticket go to?". Run a template on files, folders, JSON, or
text, and Decide shows each answer with its probability.

Get started:
  decide templates                    see the built-in templates
  decide run sentiment reviews.txt    run a template on each line of a file
  decide runs view                    look at your latest results
  decide models                       check your key and list models

Set TYPESAFE_API_KEY before your first run. Templates that read images,
PDFs, audio, or video need a Cloudflare, OpenAI, or Gemini key instead.`

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
		a.envClient = true
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
	a.addModels(app)
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
	if cli.IsUsageError(err) {
		// Wonton exits 2 for usage errors, but 2 means "flagged" here, so a
		// mistyped flag in CI must not look like a flagged item.
		return 1
	}
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
var defaultModels = map[string]string{"typesafe": "jev-latest", "cloudflare": "clef", "openai": "gpt-6-luna", "gemini": "gemini-decision-1"}

// Model names decide knows for providers that cannot list them, the default
// first.
var knownModels = map[string][]string{"cloudflare": {"clef", "clef-flash"}, "openai": {"gpt-6-luna"}, "gemini": {"gemini-decision-1"}}

// connect builds a client from environment credentials.
func connect(provider, model string) (*decide.Client, error) {
	return connectWith(provider, model, true)
}

// connectWithoutDryRun is connect for commands that have no --dry-run.
func connectWithoutDryRun(provider, model string) (*decide.Client, error) {
	return connectWith(provider, model, false)
}

// newClientWithoutDryRun is NewClient for commands that have no --dry-run,
// so a missing key does not suggest a flag they reject.
func (a *App) newClientWithoutDryRun(provider, model string) (*decide.Client, error) {
	if a.envClient {
		return connectWithoutDryRun(provider, model)
	}
	return a.NewClient(provider, model)
}

func connectWith(provider, model string, dryRunHint bool) (*decide.Client, error) {
	cfg, err := credentials(provider, dryRunHint)
	if err != nil {
		return nil, err
	}
	cfg.Model = model
	return backend.NewClient(cfg)
}

// mediaWorkers is the most media requests sent at once unless --workers
// says otherwise, since each one holds its file in memory several times.
const mediaWorkers = 2

// workersFor returns --workers, or at most mediaWorkers for a run that
// holds media files when --workers was not given.
func workersFor(c *cli.Context, media bool) int {
	n := c.Int("workers")
	if media && !c.IsSet("workers") {
		n = min(n, mediaWorkers)
	}
	return n
}

// credentials reads a provider's credentials and base URL from the
// environment, and says which to set when they are missing. The hint
// suggests --dry-run only when dryRunHint is set, for commands that have it.
func credentials(provider string, dryRunHint bool) (backend.Config, error) {
	dryRun := ""
	if dryRunHint {
		dryRun = "\nTo check your data first without a key, add --dry-run."
	}
	cfg := backend.Config{Provider: backend.Provider(provider)}
	switch provider {
	case "cloudflare":
		cfg.APIKey = os.Getenv("CLOUDFLARE_AUTH_TOKEN")
		cfg.AccountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
		cfg.BaseURL = os.Getenv("CLOUDFLARE_BASE_URL")
		if cfg.APIKey == "" || cfg.AccountID == "" {
			return backend.Config{}, cli.Error("Cloudflare credentials are not set").
				Hint("Set your Workers AI API token and account ID:\n  export CLOUDFLARE_AUTH_TOKEN=...\n  export CLOUDFLARE_ACCOUNT_ID=...")
		}
	case "openai":
		cfg.APIKey = os.Getenv("OPENAI_API_KEY")
		cfg.BaseURL = os.Getenv("OPENAI_BASE_URL")
		if cfg.APIKey == "" {
			return backend.Config{}, cli.Error("OPENAI_API_KEY is not set").
				Hint("Set your OpenAI API key:\n  export OPENAI_API_KEY=..." + dryRun)
		}
	case "gemini":
		cfg.APIKey = os.Getenv("GEMINI_API_KEY")
		cfg.BaseURL = os.Getenv("GEMINI_BASE_URL")
		if cfg.APIKey == "" {
			return backend.Config{}, cli.Error("GEMINI_API_KEY is not set").
				Hint("Set your Gemini API key:\n  export GEMINI_API_KEY=..." + dryRun)
		}
	default:
		cfg.APIKey = os.Getenv("TYPESAFE_API_KEY")
		cfg.BaseURL = os.Getenv("TYPESAFE_BASE_URL")
		if cfg.APIKey == "" {
			return backend.Config{}, cli.Error("TYPESAFE_API_KEY is not set").
				Hint("Set your TypeSafe API key:\n  export TYPESAFE_API_KEY=..." + dryRun)
		}
	}
	return cfg, nil
}
