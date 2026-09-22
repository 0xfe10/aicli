package main

import (
	"fmt"
	"os"

	"github.com/0xfe10/aicli/internal/contextflow"
	"github.com/0xfe10/aicli/internal/lanhurt"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	manager := lanhurt.ContextManager()
	selection, args, err := manager.ResolveArgs(os.Args)
	if err == nil {
		if handled, localErr := contextflow.MaybeRun(args, manager, selection, os.Stdout, os.Stderr); handled {
			exit(localErr)
			return
		}
		err = manager.Prepare(selection)
	}
	if err == nil {
		if handled, localErr := lanhurt.MaybeRunAuthWithContext(args, selection); handled {
			exit(localErr)
			return
		}
	}
	var session lanhurt.Session
	if err == nil {
		session, err = lanhurt.LoadSessionWithContext(selection)
	}
	if err == nil {
		if handled, localErr := lanhurt.MaybeRunWorkflow(args[1:], session, os.Stdout); handled {
			exit(localErr)
			return
		}
	}
	if err == nil {
		var cliErr error
		cli, createErr := lanhurt.NewCLI(session, selection, version, commit)
		if createErr != nil {
			err = createErr
		} else {
			cliErr = lanhurt.RunCLI(cli, args, selection)
			err = cliErr
		}
	}
	exit(err)
}

func exit(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
