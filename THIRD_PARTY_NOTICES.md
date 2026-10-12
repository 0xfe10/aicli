# Third-party notices

This file records licenses for third-party code linked into `pingcode`, `fns`,
`ozon`, `lanhu`, `itsaplan`, `paseo-bridge`, and `aicli` release binaries.

Evidence is maintained under `openwiki/compliance/`:

- [`modules-linked.txt`](openwiki/compliance/modules-linked.txt) — `go list -deps` closure for `./cmd/pingcode`
- [`license-inventory.md`](openwiki/compliance/license-inventory.md) — per-module license scan from module-cache LICENSE files
- [`notices/`](openwiki/compliance/notices/) — copied NOTICE files for Apache-2.0 modules when present
- [`govulncheck.txt`](openwiki/compliance/govulncheck.txt) — `govulncheck ./...` output
- [`go-mod-verify.txt`](openwiki/compliance/go-mod-verify.txt) — `go mod verify` output

## Summary

- Direct dependency: Restish `v2.3.0` (MIT)
- Direct dependency for Lanhu full-page rendering: `github.com/chromedp/chromedp` `v0.14.2` (MIT)
- Direct dependency for FNS Swagger conversion: `github.com/getkin/kin-openapi` `v0.145.0` (MIT)
- Direct dependency for YAML decode: `gopkg.in/yaml.v3` `v3.0.1` (MIT)
- Linked module union for all release commands: see inventory
- No GPLv3 detected in LICENSE scan of the linked closure
- Apache-2.0 modules are present (for example `amazon-ion/ion-go`); include their NOTICE files from `openwiki/compliance/notices/` with release artifacts when shipping binaries
- Current scan after the Go 1.27.2 and Go support-library security updates has no
  unapproved imported-package or symbol findings. Raw module-only warnings remain
  in evidence; they describe code not imported into the release binaries. Missing
  or unknown traces still block release. The existing allowlist is unchanged.

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

## Go cryptography support

- Module: `golang.org/x/crypto`
- Version: v0.57.0
- License: BSD-3-Clause
- Native Relay uses NaCl box and Salsa20 packages.

```text
Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```
