# Third-party notices

This file records licenses for third-party code linked into `pingcode`,
`fns`, and `aicli` release binaries.

Evidence is maintained under `openwiki/compliance/`:

- [`modules-linked.txt`](openwiki/compliance/modules-linked.txt) — `go list -deps` closure for `./cmd/pingcode`
- [`license-inventory.md`](openwiki/compliance/license-inventory.md) — per-module license scan from module-cache LICENSE files
- [`notices/`](openwiki/compliance/notices/) — copied NOTICE files for Apache-2.0 modules when present
- [`govulncheck.txt`](openwiki/compliance/govulncheck.txt) — `govulncheck ./...` output
- [`go-mod-verify.txt`](openwiki/compliance/go-mod-verify.txt) — `go mod verify` output

## Summary

- Direct dependency: Restish `v2.3.0` (MIT)
- Direct dependency for FNS Swagger conversion: `github.com/getkin/kin-openapi` `v0.145.0` (MIT)
- Direct dependency for YAML decode: `gopkg.in/yaml.v3` `v3.0.1` (MIT)
- Linked module union for `./cmd/aicli`, `./cmd/pingcode`, and `./cmd/fns`: see inventory
- No GPLv3 detected in LICENSE scan of the linked closure
- Apache-2.0 modules are present (for example `amazon-ion/ion-go`); include their NOTICE files from `openwiki/compliance/notices/` with release artifacts when shipping binaries
- Vulnerability status has an upstream residual: `github.com/shamaton/msgpack/v3`
  GO-2026-4740 and GO-2026-4513 have no fixed version and are reachable through
  the embedded Restish runtime. This residual predates the FNS CLI work and is
  not a new risk introduced by `kin-openapi`.

## Restish

- Project: https://github.com/rest-sh/restish
- Version: v2.3.0
- License: MIT

```
MIT License

Copyright 2020 Daniel G. Taylor

Permission is hereby granted, free of charge, to any person obtaining a copy of
this software and associated documentation files (the "Software"), to deal in
the Software without restriction, including without limitation the rights to
use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
the Software, and to permit persons to whom the Software is furnished to do so,
subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
```

Do not treat this file as a claim that all dependency risk is cleared. Recheck
`govulncheck` and the license inventory before each release.
