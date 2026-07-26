package restish

import "github.com/0xfe10/aicli/internal/restishrt"

// Runtime records Restish embedding metadata for service CLIs.
type Runtime struct {
	Version string
}

// PlannedRuntime returns the pinned Restish version used by aicli.
func PlannedRuntime() Runtime {
	return Runtime{Version: restishrt.Version()}
}
