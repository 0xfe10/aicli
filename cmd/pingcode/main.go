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
	cfg, err := pingcodert.LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cli := pingcodert.NewCLI(cfg, version, commit)
	if err := cli.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
