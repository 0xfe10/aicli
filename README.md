# aicli

`aicli` is a Go monorepo for AI-oriented service CLIs.

OpenAPI service CLIs embed [Restish](https://rest.sh/) v2.3.0. MCP services use
the pinned [mcp2cli](https://github.com/mcp2cli/source-code) runtime shipped in
their release archive. Users install one command per service.

## Structure

- `cmd/` contains release command entrypoints (`aicli`, `pingcode`, `fns`, `ozon`, `lanhu`, `devopsh`).
- `internal/pingcodert/` adapts PingCode's API description and authentication to Restish.
- `internal/swagger2rt/` converts Swagger / OpenAPI 2 documents to OpenAPI 3 for Restish.
- `internal/fnsrt/` adapts Fast Note Sync (FNS) specs, auth, and write safety to Restish.
- `internal/ozonrt/` repairs the Ozon Seller OpenAPI and applies header auth and operation-level write safety.
- `internal/lanhurt/` provides the observed Lanhu API, Cookie auth, design workflows, and Axure rendering.
- `internal/devopshrt/` binds DevOpsH to the packaged mcp2cli runtime.
- `internal/cli/` contains JSON helpers used by the `aicli` registry command.
- `services/` contains service registrations and command-surface metadata.
- `openwiki/` contains repository knowledge and architecture decisions.

## PingCode

Save credentials once (interactive; secrets are not accepted on argv).
Login always prompts for Base URL, then credentials:

```sh
pingcode auth login --mode client   # Base URL + Client ID + Client Secret
pingcode auth login --mode token    # Base URL + access token
pingcode auth status
pingcode auth logout
```

Credentials and Base URL are stored at `$XDG_CONFIG_HOME/aicli/pingcode/config.toml`
(default `~/.config/aicli/pingcode/config.toml`) with directory mode `0700` and
file mode `0600`. Example:

```toml
base_url = "https://open.pingcode.com"

[auth]
mode = "client"
client_id = "..."
client_secret = "..."
```

Environment variables override individual config fields for CI and temporary use
(without modifying the file):

```sh
export PINGCODE_API_BASE_URL='https://open.pingcode.com'
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
- Precedence: environment > `config.toml` > compile-time defaults
- `PINGCODE_ACCESS_TOKEN` or `PINGCODE_CLIENT_ID` / `PINGCODE_CLIENT_SECRET` (override)
- `PINGCODE_WRITE_MODE`: `readonly` (default), `write`, or `destructive`
- `PINGCODE_API_BASE_URL`: defaults to `https://open.pingcode.com`
- `PINGCODE_SPEC_URL`: defaults to `https://open.pingcode.com/api_data.json`
- `RSH_CACHE_DIR`: optional Restish HTTP/spec cache override; branded CLIs ignore Restish config overrides

## Fast Note Sync (`fns`)

Save a Bearer token created in the FNS WebGUI
(for example `p:rest c:aicli f:note_rw,file_rw`).
Login always prompts for Base URL, then the access token:

```sh
fns auth login --mode token
fns auth status
fns auth logout
```

Credentials and Base URL are stored at `$XDG_CONFIG_HOME/aicli/fns/config.toml`
(default `~/.config/aicli/fns/config.toml`) with directory mode `0700` and
file mode `0600`. Example:

```toml
base_url = "https://obsidian-fns.example.org"
client = "aicli"

[auth]
mode = "token"
access_token = "..."
```

```sh
export FNS_ACCESS_TOKEN='...'
export FNS_BASE_URL='https://your-fns-host.example'
export FNS_SPEC_URL='https://raw.githubusercontent.com/haierkeys/fast-note-sync-service/b6b4566352f39e0404530ed1b58248a815a6d763/docs/swagger.yaml'
export FNS_CLIENT='aicli'
export FNS_WRITE_MODE=write        # also allow POST, PUT, PATCH
export FNS_WRITE_MODE=destructive  # also allow DELETE / recycle-clear
```

The compile-time default Base URL (`https://fns.example.com`) is a placeholder so
`fns --help` works. Real API requests fail until you run `fns auth login` or set
`FNS_BASE_URL`. See [unified CLI auth](openwiki/decisions/unified-cli-auth.md).

Branded CLIs do not use Restish `restish.json` for Base URL or credentials;
`--rsh-config` is ignored. `RSH_CACHE_DIR` may still be set for Restish caches.

The CLI exposes Note/File/Folder commands plus the read-only Vault list command.
Vault creation, update, deletion, and index rebuild remain excluded. Restish owns argument
encoding, multipart upload, binary download, and output formatting:

```sh
go run ./cmd/fns --help
go run ./cmd/fns vault get-api-vault -o table
go run ./cmd/fns note get-api-note Notes/test.md genesis -o json
go run ./cmd/fns file get-api-file assets/test.png genesis > test.png
go run ./cmd/fns file post-api-file 'vault: genesis, path: assets/test.bin, file: @./test.bin'
```

The default `FNS_SPEC_URL` is pinned to FNS commit `b6b4566352f39e0404530ed1b58248a815a6d763` Swagger until the server
publishes a stable OpenAPI endpoint. Override `FNS_SPEC_URL` only when you
intentionally need another description.

## Ozon Seller (`ozon`)

The `ozon` binary exposes the complete command surface from a pinned Ozon Seller
OpenAPI snapshot. Save `Client-Id` and `Api-Key` interactively:

```sh
ozon auth login --mode key
ozon auth status
ozon --help
ozon product-api --help
```

Environment variables override local configuration:

```sh
export OZON_CLIENT_ID='...'
export OZON_API_KEY='...'
export OZON_WRITE_MODE=write        # allow operations classified as writes
export OZON_WRITE_MODE=destructive  # also allow destructive operations
```

`OZON_BASE_URL` and `OZON_SPEC_URL` override the defaults. The default spec URL
is pinned because Ozon does not currently publish a stable raw OpenAPI URL that
this project can consume directly. The adapter removes credential parameters,
repairs known invalid schema metadata, and generates all operations at runtime.

## Lanhu (`lanhu`)

Lanhu uses a browser session Cookie because its project and design endpoints are
private web APIs. Save it interactively, or inject it from a secret manager:

```sh
lanhu auth login --mode cookie
export LANHU_COOKIE='...'
export DDS_COOKIE='...'       # optional; falls back to LANHU_COOKIE
```

Raw read-only API operations are generated from the embedded observed OpenAPI
contract:

```sh
lanhu project list-documents TEAM_ID PROJECT_ID
lanhu project get-image IMAGE_ID --project-id PROJECT_ID
lanhu design list PROJECT_ID
lanhu dds get-schema-source VERSION_ID
```

Local workflows compose those APIs and signed resources:

```sh
lanhu design overview '<design-url>'
lanhu design inspect '<design-url>' --region 0,0,800,600 --output crop.png
lanhu design export '<design-url>' --output assets.zip
lanhu axure pages '<document-url>'
lanhu axure download '<document-url>' ./prototype
lanhu axure render ./prototype index.html screenshot.png
```

`axure render` uses Chromium found on `PATH`; set `LANHU_CHROMIUM` to an explicit
binary when needed. Root-only containers may explicitly set
`LANHU_CHROMIUM_NO_SANDBOX=1`; it is never enabled by default. Cookies are
attached only to the exact Lanhu and DDS API
origins, never to signed CDN or OSS downloads.

## DevOpsH (`devopsh`)

```sh
devopsh auth login
devopsh ls
devopsh --help
devopsh context use staging
devopsh --context production auth login
```

Bearer tokens and discovery caches are isolated per context. Release archives
contain both `devopsh` and the pinned `mcp2cli` runtime.

## Build and verify

```sh
just verify
just test-fns
just test-ozon
just test-lanhu
just test-devopsh
just fns-spec-check
just ozon-spec-check
just verify-fns
just pingcode-spec-check
just compliance-check
just build
```

Release binaries are static (`CGO_ENABLED=0`) and built with stripped symbols.
See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for embedded dependency notices.
