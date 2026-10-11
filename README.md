# aicli

`aicli` is a Go monorepo for AI-oriented service CLIs.

OpenAPI service CLIs embed [Restish](https://rest.sh/) v2.3.0. MCP services use
the pinned [mcp2cli](https://github.com/mcp2cli/source-code) runtime shipped in
their release archive. Users install one command per service.

## Structure

- `cmd/` contains release command entrypoints (`aicli`, `pingcode`, `fns`, `ozon`, `lanhu`, `itsaplan`, `paseo-bridge`, `hast`).
- `internal/pingcodert/` adapts PingCode's API description and authentication to Restish.
- `internal/swagger2rt/` converts Swagger / OpenAPI 2 documents to OpenAPI 3 for Restish.
- `internal/fnsrt/` adapts Fast Note Sync (FNS) specs, auth, and write safety to Restish.
- `internal/ozonrt/` repairs the Ozon Seller OpenAPI and applies header auth and operation-level write safety.
- `internal/lanhurt/` provides the observed Lanhu API, Cookie auth, design workflows, and Axure rendering.
- `internal/paseobridgert/` binds Paseo Bridge OpenAPI 3.1, scoped Bearer auth, and send safety to Restish.
- `internal/hastrt/` exposes Hast MCP tools, resources and prompts with Bearer auth and write guards.
- `internal/mcp2clirt/` extracts the pinned MCP runtime used by Hast.
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

## Build and verify

```sh
just verify
just test-fns
just test-ozon
just test-lanhu
just fns-spec-check
just ozon-spec-check
just verify-fns
just pingcode-spec-check
just compliance-check
just build
```

Release binaries are static (`CGO_ENABLED=0`) and built with stripped symbols.
See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for embedded dependency notices.

## It's a Plan (`itsaplan`)

The CLI generates commands from the selected instance's OpenAPI JSON at
`<base-url>/docs/json`. The default API origin is `https://its-api.kahub.in`.
Personal API keys inherit their owner's permissions; create one in Account → API Keys.

```sh
itsaplan auth login --mode key  # prompts for API origin, then hidden API key
itsaplan auth status
itsaplan --help
itsaplan projects --help
itsaplan projects get-projects -o json
itsaplan --context staging auth login --mode key
```

Config is stored at `$XDG_CONFIG_HOME/aicli/itsaplan/config.toml` with mode `0600`
(directory `0700`). Named contexts isolate credentials and caches. Environment
variables override file settings without changing the file:

- `ITSAPLAN_BASE_URL`: API origin (not the frontend URL; no path prefix).
- `ITSAPLAN_API_KEY`: personal API key, sent only to the configured API origin in `x-api-key`.
- `ITSAPLAN_SPEC_URL`: optional alternative OpenAPI JSON URL; discovery sends no API key.
- `ITSAPLAN_CONTEXT`: account context; overridden by `--context`.
- `ITSAPLAN_WRITE_MODE`: `readonly` (default), `write`, or `destructive`.

`write` permits POST/PUT/PATCH; DELETE and paths containing the exact segments
`delete`, `purge`, `clear`, or `revoke` require `destructive` even when using POST.
These are real requests, not dry runs. The guard covers generated and raw API
requests. Git webhook receivers and SCIM operations require separate credentials
and are excluded. Public API operations use the same auth/safety handler.
Authenticated response caching is disabled. External OpenAPI references are rejected.

```sh
just test-itsaplan
just itsaplan-spec-check
just build-itsaplan
```

## Paseo Bridge (`paseo-bridge`)

Commands are generated from `<base-url>/api/v1/openapi.json`; the default origin
is `https://paseo-bridge.kahub.in`. Use a scoped Bridge bearer token provisioned by
the server administrator. Token administration is local-only on the server.

```sh
paseo-bridge auth login --mode token  # API origin, then hidden Bearer token
paseo-bridge auth status
paseo-bridge status list-host-status -o json
paseo-bridge status get-host-status HOST -o json
paseo-bridge workspaces list-workspaces --host all -o json
paseo-bridge agents list-agents --host HOST -o json
paseo-bridge wait get-agent-snapshot HOST AGENT -o json
paseo-bridge activity get-agent-activity HOST AGENT --tail 20 -o json
paseo-bridge observation observe-agent HOST AGENT --include activity -o ndjson

# A real send; prompt is required, interrupt defaults to false.
PASEO_BRIDGE_WRITE_MODE=write paseo-bridge send send-message HOST AGENT 'prompt: Check the test results'
```

SSE observation uses one connection (the server closes it within 60 seconds).
Use `--rsh-max-items 100` to limit the number of events; reconnect manually
with backoff when needed. SSE `error` events remain visible in output and must be
inspected. `include=activity` requires both wait and activity scopes. Idle is an
agent snapshot, not proof that a particular message completed. Inspect `partial`,
`unknown`, `retrySafe`, and activity gap/epoch indicators in API responses.
A 202 send is accepted, not completed; never resend an uncertain message to
recover observation. No automatic send retry occurs, including after 401,
rate limits, network failure, or body-preserving redirects; Restish unsafe-retry
flags and environment overrides cannot enable it.

Config is stored at `$XDG_CONFIG_HOME/aicli/paseo-bridge/config.toml` with mode
`0600` (directory `0700`). `--context NAME` isolates credentials and caches.
Environment overrides do not rewrite configuration:

- `PASEO_BRIDGE_BASE_URL`: API origin without a path prefix.
- `PASEO_BRIDGE_ACCESS_TOKEN`: scoped bearer token.
- `PASEO_BRIDGE_SPEC_URL`: optional alternate spec URL; discovery sends no token.
- `PASEO_BRIDGE_E2EE_FILE`: absolute path to the protected E2EE configuration.
- `PASEO_BRIDGE_CONTEXT`: selected account context.
- `PASEO_BRIDGE_WRITE_MODE`: `readonly` (default), `write`, or `destructive`.

Central business requests require E2EE. Existing token-only configurations must
add `e2ee_file = "/absolute/path/e2ee.json"` at the top level of `config.toml`
(or use `PASEO_BRIDGE_E2EE_FILE`). The JSON file is:

```json
{"version":1,"keyFile":"/absolute/path/client.key","hosts":{"aspen":"BASE64_RAW_X25519_PUBLIC_KEY","birch":"BASE64_RAW_X25519_PUBLIC_KEY"}}
```

The key file contains the standard base64 encoding of a 32-byte X25519 private
key. Both files must be regular, owned by the current user, and mode `0600` or
stricter. Windows binaries can run auth, documentation, and version commands,
but E2EE business operations fail closed until Windows file ACL verification
is implemented. Provision the client's SHA-256 public-key fingerprint in each host's
allowlist, and verify host public keys through an independent trusted channel;
never obtain pins from the central broker. Private keys stay on their owner.
Requests fail closed when configuration, pins, or response authentication fail.
There is no plaintext fallback. `host=all` queries fan out locally to pinned
hosts and report per-host errors as partial results.

The center sees routing metadata (host, client fingerprint, operation, request
ID, expiry, frame sizes/timing) and bearer-token authorization, but cannot read
paths, agent IDs, prompts, snapshots, or activity. Encrypted SSE is verified
frame by frame; truncation is an error. Nonstreaming responses are exposed only
after the authenticated end frame. Limits are 256 KiB per request body, 16 MiB
per host response, and 150 seconds per encrypted operation. A failed send can
still have been delivered: do not resend automatically. Static host/client keys
do not provide forward secrecy; compromise requires key rotation and pin updates.
OpenAPI discovery is public and is not encrypted.

Branded CLI configuration ignores `--rsh-config`; authenticated response caching
is disabled. External OpenAPI references and cross-origin credential attachment
are rejected. Documentation and browser assets are excluded from generated commands.

```sh
just test-paseo-bridge
just paseo-bridge-spec-check
just build-paseo-bridge
```

## Hast AIP MCP (`hast`)

`hast` discovers the tools, resources and prompts available to the selected key at
`https://backend.hast.so/api/public/v1/mcp`, following the
[MCP guide](https://hast.so/zh-CN/docs/mcp). Saved AIP Agents are represented by
Webhooks; `post_webhooks` creates one. Tool availability depends on the key's
Profile and scopes. Agent configuration requires `webhooks:read`; creating,
updating and invoking Agents requires `webhooks:write`.

Existing `hast auth` credentials and contexts remain valid. Login accepts an API
origin such as `https://backend.hast.so`, then prompts for a hidden API key. Hast
MCP uses Bearer keys and does not provide OAuth login.

```sh
hast auth login --mode key
hast auth status
hast context list
hast --context work auth login --mode key
hast ls
hast --help
hast get hast://guides/overview
hast get hast://guides/enterprise-agent
hast get_models --json
hast get_webhooks --json
hast get_sessions --args-json '{"query":{"limit":5}}' --json
```

Commands follow MCP tool names, including underscores. Pass the complete nested
`path`, `query`, `body` or `upload` object with `--args-json` or `--args-file`;
`--input-json` is for client elicitation, not the tool request body. For example,
`agent.json` contains `{ "body": { "title": "degov", ... } }`, with model and
policy values selected from `get_models` and `get_webhooks`:

```sh
HAST_WRITE_MODE=write hast post_webhooks --args-file agent.json --json
hast get_webhooks_hook_id --args-json '{"path":{"hook_id":"HOOK_ID"}}' --json
```

`HAST_WRITE_MODE` defaults to `readonly`; `write` permits non-destructive writes,
and `destructive` also permits deletions. Gates apply to typed tools and generic
MCP calls. Failed MCP tools produce a nonzero exit code. Writes are not
transparently replayed or redirected. HTTP 202 means accepted; follow paginated
session events until the matching run reaches a terminal state.

Environment variables:

- `HAST_API_KEY`: overrides the stored Bearer key.
- `HAST_BASE_URL`: overrides the API origin.
- `HAST_API_BASE`: accepts the documented `/api/public/v1` suffix; `HAST_BASE_URL` takes precedence.
- `HAST_CONTEXT`: selected account context; `--context NAME` takes precedence.
- `HAST_WRITE_MODE`: `readonly`, `write` or `destructive`.

Credentials remain at `$XDG_CONFIG_HOME/aicli/hast/config.toml`, mode `0600`.
The runtime receives a local authenticated bridge address; the real key stays in
the Go process. Discovery caches are isolated by context, origin and credential.
Telemetry is disabled. `HAST_SPEC_URL` and Restish options no longer apply.
Use `--json` for structured output. MCP file transfers use Base64; resources and
prompts are discovered alongside tools.

This is a command-surface change from v0.9.x: commands such as
`hast public-task-agents get-api-public-v1-sessions` become `hast get_sessions`.
Hast MCP release archives include the pinned mcp2cli 0.1.10 runtime and its
license, for Linux/macOS amd64 and arm64. Windows releases are unavailable
because this runtime version has no upstream Windows artifacts.

```sh
just test-hast
just build-hast
just hast-mcp-check
```
