package restish

// Runtime records the project boundary for Restish-backed service CLIs.
//
// The first Go monorepo cut does not download or embed Restish. A later release
// task should decide how Restish binaries are fetched, verified, embedded, and
// executed for each target platform.
type Runtime struct {
	Version string
}

func PlannedRuntime() Runtime {
	return Runtime{Version: "2.3.0"}
}
