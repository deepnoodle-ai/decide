// Command decide asks typed questions about your data and saves every answer.
//
// Run "decide" for an overview, or see docs/cli.md.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"

	"github.com/deepnoodle-ai/decide/internal/cli"
)

// version is set when a release is built, with
// -ldflags "-X main.version=v1.2.3".
var version string

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	go func() {
		// The first Ctrl-C stops the run and saves progress. Restoring the
		// default handler lets a second Ctrl-C exit immediately, even while
		// waiting on stdin.
		<-ctx.Done()
		stop()
	}()
	os.Exit((&cli.App{Version: versionOf()}).Run(ctx, os.Args[1:]))
}

// versionOf returns the release version, the module version of a binary
// built with go install, or "dev".
func versionOf() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
