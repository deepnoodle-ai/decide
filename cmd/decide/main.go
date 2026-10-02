// Command decide asks typed questions about your data and saves every answer.
//
// Run "decide" for an overview, or see docs/cli.md.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/deepnoodle-ai/decide/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	go func() {
		// The first Ctrl-C stops the run and saves progress. Restoring the
		// default handler lets a second Ctrl-C exit immediately, even while
		// waiting on stdin.
		<-ctx.Done()
		stop()
	}()
	os.Exit((&cli.App{}).Run(ctx, os.Args[1:]))
}
