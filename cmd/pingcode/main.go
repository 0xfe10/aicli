package main

import (
	"context"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/0xfe10/aicli/internal/pingcode"
	"github.com/0xfe10/aicli/internal/restishrt"
)

// Set by -ldflags at release build time.
var (
	version = "0.1.2"
	commit  = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	deps := pingcode.RuntimeDependencies{
		Version: pingcode.VersionInfo{
			CLI:     version,
			Commit:  commit,
			Go:      runtime.Version(),
			Restish: restishrt.Version(),
		},
	}

	if cfg, err := pingcode.LoadConfig(); err == nil {
		store := pingcode.NewAuthStore(cfg.AuthTokenPath)
		client := pingcode.NewClient(cfg, store)
		raw := &restishrt.Runtime{
			APIBaseURL: cfg.APIBaseURL,
			Auth:       client.AuthorizationHeader,
		}
		deps.Raw = func(ctx context.Context, args []string) (pingcode.RawResult, error) {
			result, err := raw.Run(ctx, args)
			return pingcode.RawResult{Data: result.Data, Meta: result.Meta}, err
		}
	}

	result := pingcode.Execute(ctx, os.Args[1:], deps)
	os.Exit(result.ExitCode)
}
