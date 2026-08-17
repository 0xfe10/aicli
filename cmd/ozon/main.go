package main

import (
	"fmt"
	"os"

	"github.com/0xfe10/aicli/internal/contextflow"
	"github.com/0xfe10/aicli/internal/ozonrt"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	manager := ozonrt.ContextManager()
	selection, args, err := manager.ResolveArgs(os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, ozonrt.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	if handled, err := contextflow.MaybeRun(args, manager, selection, os.Stdout, os.Stderr); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, ozonrt.RedactSecrets(err.Error()))
			os.Exit(1)
		}
		return
	}
	if err := manager.Prepare(selection); err != nil {
		fmt.Fprintln(os.Stderr, ozonrt.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	if handled, err := ozonrt.MaybeRunAuthWithContext(args, selection); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, ozonrt.RedactSecrets(err.Error()))
			os.Exit(1)
		}
		return
	}
	session, cfg, err := ozonrt.LoadSessionWithContext(selection)
	if err != nil {
		fmt.Fprintln(os.Stderr, ozonrt.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	cli := ozonrt.NewCLIWithContext(cfg, session, version, commit, selection)
	if err := ozonrt.RunCLIWithContext(cli, args, selection); err != nil {
		fmt.Fprintln(os.Stderr, ozonrt.RedactSecrets(err.Error()))
		os.Exit(1)
	}
}
