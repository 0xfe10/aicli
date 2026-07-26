# Milestone 0 notes

Date: 2026-07-26

## /project vs /pjm

Anonymous probes against `https://open.pingcode.com`:

- `GET /v1/project/projects` → HTTP 400 `access_token` required
- `GET /v1/pjm/projects` → HTTP 400 `access_token` required

Both route families exist and enter the same auth layer. Authenticated response
field parity was not verified in this environment. The typed client therefore
keeps MCP-validated `/v1/project/...` paths, centralized in
`internal/pingcode/client.go`.

## Restish

- Pinned: `github.com/rest-sh/restish/v2@v2.3.0`
- License: MIT
- Restish `go.mod` requires Go `>= 1.25.3`; aicli uses Go `1.25.3`
- `FetchResponse` does not support request bodies; domain writes use the typed
  Go client. Restish is embedded only for `pingcode raw`.

## Static build gate

Release builds use `CGO_ENABLED=0` via `scripts/build-pingcode.sh`.
