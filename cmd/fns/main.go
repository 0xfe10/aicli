package main

import (
	"fmt"
	"os"

	"github.com/0xfe10/aicli/internal/contextflow"
	"github.com/0xfe10/aicli/internal/fnsrt"
)

// Set by -ldflags at release build time.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	manager := fnsrt.ContextManager()
	selection, args, err := manager.ResolveArgs(os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, fnsrt.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	if handled, err := contextflow.MaybeRun(args, manager, selection, os.Stdout, os.Stderr); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, fnsrt.RedactSecrets(err.Error()))
			os.Exit(1)
		}
		return
	}
	if err := manager.Prepare(selection); err != nil {
		fmt.Fprintln(os.Stderr, fnsrt.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	if handled, err := fnsrt.MaybeRunAuthWithContext(args, selection); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, fnsrt.RedactSecrets(err.Error()))
			os.Exit(1)
		}
		return
	}

	session, cfg, err := fnsrt.LoadSessionWithContext(version, selection)
	if err != nil {
		fmt.Fprintln(os.Stderr, fnsrt.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	cli := fnsrt.NewCLIWithContext(cfg, session, version, commit, selection)
	if err := fnsrt.RunCLIWithContext(cli, args, selection); err != nil {
		fmt.Fprintln(os.Stderr, fnsrt.RedactSecrets(err.Error()))
		os.Exit(1)
	}
}
