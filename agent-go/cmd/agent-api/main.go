// Command agent-api runs the agent API.
//
// By default it runs as an interactive terminal agent (stdin/stdout).
// Pass --server to start the HTTP API server instead.
//
// Configuration is entirely via environment variables (see internal/config).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/obot-platform/discobot/agent-go/cmd/agent-api/cli"
	"github.com/obot-platform/discobot/agent-go/cmd/agent-api/server"
	"github.com/obot-platform/discobot/agent-go/internal/config"

	// Side-effect imports register provider factories so the registry can
	// build them on demand when credentials arrive via X-Discobot-Credentials.
	_ "github.com/obot-platform/discobot/agent-go/providers/anthropic"
	_ "github.com/obot-platform/discobot/agent-go/providers/openai"
	_ "github.com/obot-platform/discobot/agent-go/providers/openaicompatible"
)

func main() {
	// Handle subcommands before flag parsing so they get clean args.
	if len(os.Args) >= 2 && os.Args[1] == "login" {
		if err := cli.RunLogin(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "login: %v\n", err)
			os.Exit(1)
		}
		return
	}

	serverMode := flag.Bool("server", false, "Run as HTTP API server (default: interactive terminal mode)")
	flags := cli.AddFlags()
	flag.Parse()

	cfg := config.Load()

	// When invoked as "discobot-agent-api" (drop-in replacement), default to server mode.
	if filepath.Base(os.Args[0]) == "discobot-agent-api" || *serverMode {
		server.Run(cfg)
	} else {
		cli.Run(cfg, flags)
	}
}
