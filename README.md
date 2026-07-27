# aicli

`aicli` is a Go monorepo for AI-oriented service CLIs.

The first service CLI is `pingcode`. It embeds [Restish](https://rest.sh/) v2.3.0
and generates commands from PingCode's official API description at runtime. Users
install only the `pingcode` binary; a separate Restish installation is not needed.

## Structure

- `cmd/` contains release command entrypoints.
- `internal/pingcodert/` adapts PingCode's API description and authentication to Restish.
- `internal/cli/` contains JSON helpers used by the `aicli` registry command.
- `services/` contains service registrations and command-surface metadata.
- `openwiki/` contains repository knowledge and architecture decisions.

## PingCode

Set either a fixed access token or client credentials:

```sh
export PINGCODE_CLIENT_ID='...'
export PINGCODE_CLIENT_SECRET='...'
```

Discover and call generated commands:

```sh
go run ./cmd/pingcode --help
go run ./cmd/pingcode pjm --help
go run ./cmd/pingcode pjm get-projects -o json
```

The default mode permits only `GET`, `HEAD`, and `OPTIONS`. Enable writes
explicitly for a trusted session:

```sh
export PINGCODE_WRITE_MODE=write        # also allow POST, PUT, PATCH
export PINGCODE_WRITE_MODE=destructive  # also allow DELETE
```

Configuration:

- `PINGCODE_ACCESS_TOKEN`
- `PINGCODE_CLIENT_ID` / `PINGCODE_CLIENT_SECRET`
- `PINGCODE_WRITE_MODE`: `readonly` (default), `write`, or `destructive`
- `PINGCODE_API_BASE_URL`: defaults to `https://open.pingcode.com`
- `PINGCODE_SPEC_URL`: defaults to `https://open.pingcode.com/api_data.json`
- `RSH_CONFIG`, `RSH_CONFIG_DIR`, and `RSH_CACHE_DIR`: optional Restish state overrides

Restish provides the request body, output, filtering, pagination, cache, retry,
profile, and TLS options. Use command-level `--help` for the generated flags.

## Build and verify

```sh
just verify
just pingcode-spec-check
just build-pingcode
```

Release binaries are static (`CGO_ENABLED=0`) and built with stripped symbols.
See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for embedded dependency notices.
