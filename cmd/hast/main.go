package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/0xfe10/aicli/internal/contextflow"
	"github.com/0xfe10/aicli/internal/hastrt"
)

// Set by -ldflags at release build time.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	manager := hastrt.ContextManager()
	selection, args, err := manager.ResolveArgs(os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, hastrt.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	if handled, err := contextflow.MaybeRun(args, manager, selection, os.Stdout, os.Stderr); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, hastrt.RedactSecrets(err.Error()))
			os.Exit(1)
		}
		return
	}
	if err := manager.Prepare(selection); err != nil {
		fmt.Fprintln(os.Stderr, hastrt.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	if handled, err := hastrt.MaybeRunAuthWithContext(args, selection); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, hastrt.RedactSecrets(err.Error()))
			os.Exit(1)
		}
		return
	}

	session, cfg, err := hastrt.LoadSessionWithContext(selection)
	if err != nil {
		fmt.Fprintln(os.Stderr, hastrt.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	cli := hastrt.NewCLIWithContext(cfg, session, version, commit, selection)
	if err := hastrt.RunCLIWithContext(cli, args, selection); err != nil {
		message := err.Error()
		if session.Credentials.APIKey != "" {
			message = strings.ReplaceAll(message, session.Credentials.APIKey, "***")
		}
		fmt.Fprintln(os.Stderr, hastrt.RedactSecrets(message))
		os.Exit(1)
	}
}
