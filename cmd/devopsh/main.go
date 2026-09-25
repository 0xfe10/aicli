package main

import (
	"fmt"
	"os"

	"github.com/0xfe10/aicli/internal/contextflow"
	"github.com/0xfe10/aicli/internal/devopshrt"
)

func main() {
	manager := devopshrt.ContextManager()
	selection, args, err := manager.ResolveArgs(os.Args)
	if err == nil {
		if handled, localErr := contextflow.MaybeRun(args, manager, selection, os.Stdout, os.Stderr); handled {
			exit(localErr)
			return
		}
		err = manager.Prepare(selection)
	}
	if err == nil {
		err = devopshrt.Run(args, selection)
	}
	exit(err)
}

func exit(err error) {
	if err != nil {
		if exit, ok := err.(interface{ ExitCode() int }); ok {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
