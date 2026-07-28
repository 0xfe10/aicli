package main

import (
	"fmt"
	"os"

	"github.com/0xfe10/aicli/internal/fnsrt"
)

// Set by -ldflags at release build time.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	if handled, err := fnsrt.MaybeRunAuth(os.Args); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, fnsrt.RedactSecrets(err.Error()))
			os.Exit(1)
		}
		return
	}

	session, cfg, err := fnsrt.LoadSession(version)
	if err != nil {
		fmt.Fprintln(os.Stderr, fnsrt.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	cli := fnsrt.NewCLIWithSession(cfg, session, version, commit)
	if err := fnsrt.RunCLI(cli, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, fnsrt.RedactSecrets(err.Error()))
		os.Exit(1)
	}
}
