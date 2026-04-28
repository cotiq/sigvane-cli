// Command sigvane is the Sigvane CLI entry point.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/cotiq/sigvane-cli/internal/commands"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := commands.NewRootCommand().ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
