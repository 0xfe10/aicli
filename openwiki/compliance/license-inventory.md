# Linked module license inventory

Generated for `./cmd/pingcode` dependency closure on release commands.

- Module count: 54
- Unknown/unreadable: 0
- GPL detected: 0
- Method: `go list -deps` + LICENSE file scan in module cache.

| Module | Version | License | License file | NOTICE required |
|---|---|---|---|---|
| `github.com/alecthomas/chroma/v2` | `v2.23.1` | MIT | COPYING | no |
| `github.com/amazon-ion/ion-go` | `v1.5.0` | Apache-2.0 | LICENSE | yes |
| `github.com/andybalholm/brotli` | `v1.2.1` | MIT | LICENSE | no |
| `github.com/aymanbagabas/go-osc52/v2` | `v2.0.1` | MIT | LICENSE | no |
| `github.com/aymerick/douceur` | `v0.2.0` | MIT | LICENSE | no |
| `github.com/bahlo/generic-list-go` | `v0.2.0` | BSD-3-Clause | LICENSE | no |
| `github.com/buger/jsonparser` | `v1.1.2` | MIT | LICENSE | no |
| `github.com/charmbracelet/colorprofile` | `v0.2.3-0.20250311203215-f60798e515dc` | MIT | LICENSE | no |
| `github.com/charmbracelet/glamour` | `v1.0.0` | MIT | LICENSE | no |
| `github.com/charmbracelet/lipgloss` | `v1.1.1-0.20250404203927-76690c660834` | MIT | LICENSE | no |
| `github.com/charmbracelet/x/ansi` | `v0.10.2` | MIT | LICENSE | no |
| `github.com/charmbracelet/x/cellbuf` | `v0.0.13` | MIT | LICENSE | no |
| `github.com/charmbracelet/x/exp/slice` | `v0.0.0-20250327172914-2fdc97757edf` | MIT | LICENSE | no |
| `github.com/charmbracelet/x/term` | `v0.2.1` | MIT | LICENSE | no |
| `github.com/clipperhouse/uax29/v2` | `v2.7.0` | MIT | LICENSE | no |
| `github.com/danielgtaylor/huma/v2` | `v2.37.3` | MIT | LICENSE.md | no |
| `github.com/danielgtaylor/mexpr` | `v1.10.1` | MIT | LICENSE | no |
| `github.com/danielgtaylor/shorthand/v2` | `v2.4.0` | MIT | LICENSE | no |
| `github.com/dlclark/regexp2` | `v1.11.5` | MIT | LICENSE | no |
| `github.com/fxamacker/cbor/v2` | `v2.9.1` | MIT | LICENSE | no |
| `github.com/google/shlex` | `v0.0.0-20191202100458-e7afc7fbc510` | Apache-2.0 | COPYING | yes |
| `github.com/gorilla/css` | `v1.0.1` | BSD-3-Clause | LICENSE | no |
| `github.com/hexops/gotextdiff` | `v1.0.3` | BSD-3-Clause | LICENSE | no |
| `github.com/itchyny/gojq` | `v0.12.19` | MIT | LICENSE | no |
| `github.com/itchyny/timefmt-go` | `v0.1.8` | MIT | LICENSE | no |
| `github.com/lucasb-eyer/go-colorful` | `v1.3.0` | MIT | LICENSE | no |
| `github.com/mattn/go-isatty` | `v0.0.20` | MIT | LICENSE | no |
| `github.com/mattn/go-runewidth` | `v0.0.20` | MIT | LICENSE | no |
| `github.com/microcosm-cc/bluemonday` | `v1.0.27` | BSD-3-Clause | LICENSE.md | no |
| `github.com/muesli/reflow` | `v0.3.0` | MIT | LICENSE | no |
| `github.com/muesli/termenv` | `v0.16.0` | MIT | LICENSE | no |
| `github.com/pb33f/jsonpath` | `v0.8.2` | Apache-2.0 | LICENSE | yes |
| `github.com/pb33f/libopenapi` | `v0.35.0` | MIT | LICENSE | no |
| `github.com/pb33f/ordered-map/v2` | `v2.3.1` | Apache-2.0 | LICENSE | yes |
| `github.com/rest-sh/restish/v2` | `v2.3.0` | MIT | LICENSE.md | no |
| `github.com/rivo/uniseg` | `v0.4.7` | MIT | LICENSE.txt | no |
| `github.com/sandrolain/httpcache` | `v1.4.0` | MIT | LICENSE.txt | no |
| `github.com/santhosh-tekuri/jsonschema/v6` | `v6.0.2` | Apache-2.0 | LICENSE | yes |
| `github.com/shamaton/msgpack/v3` | `v3.1.2` | MIT | LICENSE | no |
| `github.com/spf13/cobra` | `v1.10.2` | Apache-2.0 | LICENSE.txt | yes |
| `github.com/spf13/pflag` | `v1.0.10` | BSD-3-Clause | LICENSE | no |
| `github.com/tailscale/hujson` | `v0.0.0-20260302212456-ecc657c15afd` | BSD-3-Clause | LICENSE | no |
| `github.com/tidwall/jsonc` | `v0.3.3` | MIT | LICENSE | no |
| `github.com/x448/float16` | `v0.8.4` | MIT | LICENSE | no |
| `github.com/xo/terminfo` | `v0.0.0-20220910002029-abceb7e1c41e` | MIT | LICENSE | no |
| `github.com/yuin/goldmark-emoji` | `v1.0.6` | MIT | LICENSE | no |
| `github.com/yuin/goldmark` | `v1.7.13` | MIT | LICENSE | no |
| `golang.org/x/net` | `v0.50.0` | BSD-3-Clause | LICENSE | no |
| `golang.org/x/sync` | `v0.21.0` | BSD-3-Clause | LICENSE | no |
| `golang.org/x/sys` | `v0.42.0` | BSD-3-Clause | LICENSE | no |
| `golang.org/x/term` | `v0.41.0` | BSD-3-Clause | LICENSE | no |
| `golang.org/x/text` | `v0.39.0` | BSD-3-Clause | LICENSE | no |
| `go.yaml.in/yaml/v3` | `v3.0.4` | MIT | LICENSE | no |
| `go.yaml.in/yaml/v4` | `v4.0.0-rc.4` | Apache-2.0 | LICENSE | yes |

## Unknown / needs human review

- none

## GPL / copyleft

- none detected in LICENSE scan

## Residual vulnerability status

See [`govulncheck.txt`](govulncheck.txt).

- Cleared: `golang.org/x/text` GO-2026-5970 by upgrading to v0.39.0.
- Open: `github.com/shamaton/msgpack/v3` GO-2026-4740 (Fixed in: N/A; entered via Restish init).
- Release gate: **blocked** until msgpack residual risk is accepted in writing or Restish link surface is reduced.

## go mod verify

See [`go-mod-verify.txt`](go-mod-verify.txt).

