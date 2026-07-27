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

	cfg, err := fnsrt.LoadConfig(version)
	if err != nil {
		fmt.Fprintln(os.Stderr, fnsrt.RedactSecrets(err.Error()))
		os.Exit(1)
	}
	cli := fnsrt.NewCLI(cfg, version, commit)
	if err := cli.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, fnsrt.RedactSecrets(err.Error()))
		os.Exit(1)
	}
}
