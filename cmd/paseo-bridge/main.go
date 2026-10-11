package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/0xfe10/aicli/internal/contextflow"
	"github.com/0xfe10/aicli/internal/paseobridgert"
)

// Set by -ldflags at release build time.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	manager := paseobridgert.ContextManager()
	selection, args, err := manager.ResolveArgs(os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, paseobridgert.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	if handled, err := contextflow.MaybeRun(args, manager, selection, os.Stdout, os.Stderr); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, paseobridgert.RedactSecrets(err.Error()))
			os.Exit(1)
		}
		return
	}
	if err := manager.Prepare(selection); err != nil {
		fmt.Fprintln(os.Stderr, paseobridgert.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	if handled, err := paseobridgert.MaybeRunAuthWithContext(args, selection); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, paseobridgert.RedactSecrets(err.Error()))
			os.Exit(1)
		}
		return
	}

	session, cfg, err := paseobridgert.LoadSessionWithContext(selection)
	if err != nil {
		fmt.Fprintln(os.Stderr, paseobridgert.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	cli := paseobridgert.NewCLIWithContext(cfg, session, version, commit, selection)
	if err := paseobridgert.RunCLIWithContext(cli, args, selection); err != nil {
		message := err.Error()
		if session.Credentials.AccessToken != "" {
			message = strings.ReplaceAll(message, session.Credentials.AccessToken, "***")
		}
		fmt.Fprintln(os.Stderr, paseobridgert.RedactSecrets(message))
		os.Exit(1)
	}
}
