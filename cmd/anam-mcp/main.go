// Command anam-mcp serves Anam to a model over MCP on stdio. It is a client of the
// Anam daemon named by ANAM_SERVER and nothing more.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mattjoyce/anam-mcp/internal/app"
)

// Set by the Makefile through ldflags; "dev" is an honest fallback, not a version.
var (
	version   = "dev"
	gitCommit = ""
	buildDate = ""
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("anam-mcp %s %s %s\n", version, gitCommit, buildDate)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
