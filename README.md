# aicli

`aicli` is a Go monorepo for AI-oriented service CLIs.

The first delivered service CLI is `pingcode`: a small, agent-safe command surface
over PingCode project workflows. Domain commands use a typed Go client. Restish
v2.3.0 is embedded only for `pingcode raw` debugging.

## Structure

- `cmd/` contains release command entrypoints.
- `internal/pingcode/` contains PingCode config, auth, typed client, domain service, and command routing.
- `internal/restishrt/` embeds Restish for raw API debugging.
- `internal/cli/` contains shared JSON output and exit-code helpers.
- `services/` contains service CLI registrations and command manifests.
- `openwiki/` contains repository knowledge, architecture notes, and decisions.

## Local commands

List registered service CLIs:

```sh
go run ./cmd/aicli services
```

PingCode CLI:

```sh
go run ./cmd/pingcode version
go run ./cmd/pingcode config check
go run ./cmd/pingcode auth status
```

Write commands default to dry-run. Pass `--apply` to execute:

```sh
printf '%s\n' '{"kind":"bug","title":"demo"}' | go run ./cmd/pingcode work-item create --input -
```

## Configuration

Environment variables follow the PingCode MCP naming:

- `PINGCODE_BASE_URL`
- `PINGCODE_API_BASE_URL`
- `PINGCODE_ACCESS_TOKEN`
- `PINGCODE_CLIENT_ID` / `PINGCODE_CLIENT_SECRET`
- `PINGCODE_AUTH_TOKEN_PATH` (default `$XDG_CONFIG_HOME/aicli/pingcode/auth.json`)
- `PINGCODE_PROJECT_IDENTIFIER` / `PINGCODE_PROJECT_ID`
- `PINGCODE_DEFAULT_ASSIGNEE_NAME`
- `PINGCODE_READONLY`
- `PINGCODE_TIMEOUT_MS`

API host cannot be overridden by CLI flags. Tokens are never accepted as argv.

## Build

```sh
chmod +x scripts/build-pingcode.sh
./scripts/build-pingcode.sh
```

Produces static Linux `amd64`/`arm64` binaries and SHA256 sidecars under `dist/`.

## Tests

```sh
go test ./...
go test -race ./...
go vet ./...
go mod verify
```

## License notices

See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for embedded Restish MIT
attribution.
