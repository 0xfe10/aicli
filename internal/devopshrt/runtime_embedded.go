//go:build embedded_runtime

package devopshrt

import _ "embed"

//go:embed runtime.gz
var embeddedRuntime []byte
