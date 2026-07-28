package main

import (
	"fmt"
	"os"

	"github.com/0xfe10/aicli/internal/pingcodert"
)

// Set by -ldflags at release build time.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	if handled, err := pingcodert.MaybeRunAuth(os.Args); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, pingcodert.RedactSecrets(err.Error()))
			os.Exit(1)
		}
		return
	}

	session, err := pingcodert.LoadSession()
	if err != nil {
		fmt.Fprintln(os.Stderr, pingcodert.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	cfg, err := pingcodert.ConfigFromSession(session)
	if err != nil {
		fmt.Fprintln(os.Stderr, pingcodert.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	cli := pingcodert.NewCLIWithSession(cfg, session, version, commit)
	if err := pingcodert.RunCLI(cli, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, pingcodert.RedactSecrets(err.Error()))
		os.Exit(1)
	}
}
