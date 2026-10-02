// Command decide composes experimental TypeSafe judgments in shell pipelines.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/deepnoodle-ai/decide/internal/cli"
)

func main() { os.Exit(run()) }
func run() int {
	signal.Ignore(syscall.SIGPIPE)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	app := &cli.App{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
	// A terminal runtime owns raw mode and must restore it before process exit.
	// Batch mode retains the process-level escape hatch for an open stdin pipe.
	if len(os.Args) > 1 && (os.Args[1] == "explore" || os.Args[1] == "inspect") {
		return app.Run(ctx, os.Args[1:])
	}
	result := make(chan int, 1)
	// A read of an open stdin pipe cannot be canceled reliably by closing fd 0
	// on every platform. Only this executable owns the process lifetime: on
	// SIGINT it exits, releasing any blocked read with the process. App itself
	// never starts an unbounded goroutine around a caller-owned reader.
	go func() { result <- app.Run(ctx, os.Args[1:]) }()
	select {
	case code := <-result:
		return code
	case <-ctx.Done():
		return 130
	}
}
