package cli

// Stable process exit codes for service CLIs.
const (
	ExitOK              = 0
	ExitUsage           = 64
	ExitConfig          = 65
	ExitAuth            = 66
	ExitForbidden       = 67
	ExitNotFound        = 68
	ExitConflict        = 69
	ExitRateLimited     = 70
	ExitUpstreamTimeout = 71
	ExitUpstream        = 72
	ExitInternal        = 73
	ExitPartialSuccess  = 75
)

// ExitCodeFor maps a stable error code to a process exit status.
func ExitCodeFor(code string) int {
	switch code {
	case "":
		return ExitOK
	case "UNKNOWN_COMMAND", "INVALID_INPUT", "INVALID_ARGUMENT":
		return ExitUsage
	case "CONFIG_MISSING":
		return ExitConfig
	case "AUTH_REQUIRED", "AUTH_EXPIRED":
		return ExitAuth
	case "FORBIDDEN", "READONLY":
		return ExitForbidden
	case "NOT_FOUND":
		return ExitNotFound
	case "AMBIGUOUS_NAME", "EXPECTED_STATE_MISMATCH":
		return ExitConflict
	case "RATE_LIMITED":
		return ExitRateLimited
	case "UPSTREAM_TIMEOUT":
		return ExitUpstreamTimeout
	case "UPSTREAM_ERROR":
		return ExitUpstream
	case "PARTIAL_SUCCESS":
		return ExitPartialSuccess
	default:
		return ExitInternal
	}
}
