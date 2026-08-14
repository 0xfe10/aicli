package main

import (
	"fmt"
	"os"

	"github.com/0xfe10/aicli/internal/ozonrt"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	if handled, err := ozonrt.MaybeRunAuth(os.Args); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, ozonrt.RedactSecrets(err.Error()))
			os.Exit(1)
		}
		return
	}
	session, cfg, err := ozonrt.LoadSession()
	if err != nil {
		fmt.Fprintln(os.Stderr, ozonrt.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	cli := ozonrt.NewCLIWithSession(cfg, session, version, commit)
	if err := ozonrt.RunCLI(cli, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, ozonrt.RedactSecrets(err.Error()))
		os.Exit(1)
	}
}
