//go:build embedded_runtime

package mcp2clirt

import _ "embed"

//go:embed runtime.gz
var embeddedRuntime []byte
