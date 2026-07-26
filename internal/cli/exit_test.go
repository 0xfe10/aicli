package cli_test

import (
	"testing"

	"github.com/0xfe10/aicli/internal/cli"
)

func TestExitCodeFor(t *testing.T) {
	cases := map[string]int{
		"":                        cli.ExitOK,
		"INVALID_INPUT":           cli.ExitUsage,
		"CONFIG_MISSING":          cli.ExitConfig,
		"AUTH_REQUIRED":           cli.ExitAuth,
		"AUTH_EXPIRED":            cli.ExitAuth,
		"FORBIDDEN":               cli.ExitForbidden,
		"READONLY":                cli.ExitForbidden,
		"NOT_FOUND":               cli.ExitNotFound,
		"AMBIGUOUS_NAME":          cli.ExitConflict,
		"EXPECTED_STATE_MISMATCH": cli.ExitConflict,
		"RATE_LIMITED":            cli.ExitRateLimited,
		"UPSTREAM_TIMEOUT":        cli.ExitUpstreamTimeout,
		"UPSTREAM_ERROR":          cli.ExitUpstream,
		"INTERNAL_ERROR":          cli.ExitInternal,
	}
	for code, want := range cases {
		if got := cli.ExitCodeFor(code); got != want {
			t.Fatalf("%s => %d want %d", code, got, want)
		}
	}
}
