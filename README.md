# aicli

`aicli` is a Go monorepo for AI-oriented service CLIs.

Service CLIs embed [Restish](https://rest.sh/) v2.3.0 and generate commands from
each service's API description at runtime. Users install one binary per service;
a separate Restish installation is not needed.

## Structure

- `cmd/` contains release command entrypoints (`aicli`, `pingcode`, `fns`).
- `internal/pingcodert/` adapts PingCode's API description and authentication to Restish.
- `internal/swagger2rt/` converts Swagger / OpenAPI 2 documents to OpenAPI 3 for Restish.
- `internal/fnsrt/` adapts Fast Note Sync (FNS) specs, auth, and write safety to Restish.
- `internal/cli/` contains JSON helpers used by the `aicli` registry command.
- `services/` contains service registrations and command-surface metadata.
- `openwiki/` contains repository knowledge and architecture decisions.

## PingCode

Save credentials once (interactive; secrets are not accepted on argv):

```sh
pingcode auth login --mode client   # Client ID + Client Secret
pingcode auth login --mode token    # existing access token
pingcode auth status
pingcode auth logout
```

Credentials are stored at `$XDG_CONFIG_HOME/aicli/pingcode/config.toml`
(default `~/.config/aicli/pingcode/config.toml`) with directory mode `0700` and
file mode `0600`.

Environment variables still override local config for CI and temporary use
(without modifying the file):

```sh
export PINGCODE_ACCESS_TOKEN='...'                    # token mode
export PINGCODE_CLIENT_ID='...'                       # client mode (both required)
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

- Local auth: `pingcode auth login|status|logout`
- `PINGCODE_ACCESS_TOKEN` or `PINGCODE_CLIENT_ID` / `PINGCODE_CLIENT_SECRET` (override)
- `PINGCODE_WRITE_MODE`: `readonly` (default), `write`, or `destructive`
- `PINGCODE_API_BASE_URL`: defaults to `https://open.pingcode.com`
- `PINGCODE_SPEC_URL`: defaults to `https://open.pingcode.com/api_data.json`
- `RSH_CONFIG`, `RSH_CONFIG_DIR`, and `RSH_CACHE_DIR`: optional Restish state overrides

## Fast Note Sync (`fns`)

Save a Bearer token created in the FNS WebGUI
(for example `p:rest c:aicli f:note_rw,file_rw`):

```sh
fns auth login --mode token
fns auth status
fns auth logout
```

Credentials are stored at `$XDG_CONFIG_HOME/aicli/fns/config.toml`
(default `~/.config/aicli/fns/config.toml`) with directory mode `0700` and
file mode `0600`.

```sh
export FNS_ACCESS_TOKEN='...'
export FNS_BASE_URL='https://fns.example.com'
export FNS_SPEC_URL='https://raw.githubusercontent.com/haierkeys/fast-note-sync-service/b6b4566352f39e0404530ed1b58248a815a6d763/docs/swagger.yaml'
export FNS_CLIENT='aicli'
export FNS_WRITE_MODE=write        # also allow POST, PUT, PATCH
export FNS_WRITE_MODE=destructive  # also allow DELETE / recycle-clear
```

First release exposes Note/File/Folder commands only. Restish owns argument
encoding, multipart upload, binary download, and output formatting:

```sh
go run ./cmd/fns --help
go run ./cmd/fns note get-api-note Notes/test.md genesis -o json
go run ./cmd/fns file get-api-file assets/test.png genesis > test.png
go run ./cmd/fns file post-api-file 'vault: genesis, path: assets/test.bin, file: @./test.bin'
```

The default `FNS_SPEC_URL` is pinned to FNS commit `b6b4566352f39e0404530ed1b58248a815a6d763` Swagger until the server
publishes a stable OpenAPI endpoint. Override `FNS_SPEC_URL` only when you
intentionally need another description.

## Build and verify

```sh
just verify
just test-fns
just fns-spec-check
just verify-fns
just pingcode-spec-check
just compliance-check
just build
```

Release binaries are static (`CGO_ENABLED=0`) and built with stripped symbols.
See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for embedded dependency notices.
