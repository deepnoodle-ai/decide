package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/deepnoodle-ai/decide"
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
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(a.Out, helpText)
		return 0
	}
	if ctx.Err() != nil {
		return 130
	}
	switch args[0] {
	case "plan", "run", "explore":
		return a.runDataset(ctx, args[0], args[1:])
	case "sources":
		return a.runSources(ctx, args[1:])
	case "skills", "patterns":
		return a.runLibrary(ctx, args[0], args[1:])
	case "runs":
		return a.runRuns(ctx, args[1:])
	case "inspect":
		return a.runInspect(ctx, args[1:])
	case "judge", "grep", "label", "score", "check":
		return a.runBasic(ctx, args[0], args[1:])
	case "pick":
		return a.runPick(ctx, args[1:])
	case "join":
		return a.runJoin(ctx, args[1:])
	case "rank":
		return a.runRank(ctx, args[1:])
	case "pack":
		return a.runPack(ctx, args[1:])
	case "gate":
		return a.runGate(ctx, args[1:])
	case "eval":
		return a.runEval(ctx, args[1:])
	default:
		return a.fail(fmt.Errorf("unknown command %q", args[0]))
	}
}
