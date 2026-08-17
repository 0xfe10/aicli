package main

import (
	"fmt"
	"os"

	"github.com/0xfe10/aicli/internal/contextflow"
	"github.com/0xfe10/aicli/internal/pingcodert"
)

// Set by -ldflags at release build time.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	manager := pingcodert.ContextManager()
	selection, args, err := manager.ResolveArgs(os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, pingcodert.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	if handled, err := contextflow.MaybeRun(args, manager, selection, os.Stdout, os.Stderr); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, pingcodert.RedactSecrets(err.Error()))
			os.Exit(1)
		}
		return
	}
	if err := manager.Prepare(selection); err != nil {
		fmt.Fprintln(os.Stderr, pingcodert.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	if handled, err := pingcodert.MaybeRunAuthWithContext(args, selection); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, pingcodert.RedactSecrets(err.Error()))
			os.Exit(1)
		}
		return
	}

	session, err := pingcodert.LoadSessionWithContext(selection)
	if err != nil {
		fmt.Fprintln(os.Stderr, pingcodert.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	cfg, err := pingcodert.ConfigFromSession(session)
	if err != nil {
		fmt.Fprintln(os.Stderr, pingcodert.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	cli := pingcodert.NewCLIWithContext(cfg, session, version, commit, selection)
	if err := pingcodert.RunCLIWithContext(cli, args, selection); err != nil {
		fmt.Fprintln(os.Stderr, pingcodert.RedactSecrets(err.Error()))
		os.Exit(1)
	}
}
