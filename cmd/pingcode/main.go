package main

import (
	"fmt"
	"os"

	"github.com/0xfe10/aicli/internal/cli"
	"github.com/0xfe10/aicli/internal/restish"
)

const help = `pingcode

PingCode CLI for AI-oriented project workflows.

Usage:
  pingcode --help
  pingcode version

Commands:
  version    Print planned runtime metadata.

This command is a Go placeholder for the future PingCode CLI. Restish-backed
transport will be added after the embedding and release plan is approved.
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 || hasHelp(args) {
		fmt.Print(help)
		return
	}

	if len(args) == 1 && args[0] == "version" {
		runtime := restish.PlannedRuntime()
		_ = cli.WriteJSON(os.Stdout, cli.Response{
			OK: true,
			Data: map[string]any{
				"command":         "pingcode",
				"restish_version": runtime.Version,
				"transport":       "planned-embedded-restish",
			},
		})
		return
	}

	_ = cli.UnknownCommand(os.Stdout, args)
	os.Exit(64)
}

func hasHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}
