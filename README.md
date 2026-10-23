# aicli

`aicli` is a Go monorepo for AI-oriented service CLIs.

OpenAPI service CLIs embed [Restish](https://rest.sh/) v2.3.0. Users install one
command per service.

## Structure

- `cmd/` contains release command entrypoints (`aicli`, `pingcode`, `fns`, `ozon`, `lanhu`, `itsaplan`, `paseo-bridge`).
- `internal/pingcodert/` adapts PingCode's API description and authentication to Restish.
- `internal/swagger2rt/` converts Swagger / OpenAPI 2 documents to OpenAPI 3 for Restish.
- `internal/fnsrt/` adapts Fast Note Sync (FNS) specs, auth, and write safety to Restish.
- `internal/ozonrt/` repairs the Ozon Seller OpenAPI and applies header auth and operation-level write safety.
- `internal/lanhurt/` provides the observed Lanhu API, Cookie auth, design workflows, and Axure rendering.
- `internal/paseobridgert/` binds Paseo Bridge OpenAPI 3.1, scoped Bearer auth, and send safety to Restish.
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

### Description images

The generated attachment commands return attachment metadata. Inline description
images are separate resources and may be absent from that list. Download one with:

```sh
pingcode attachment download \
  'https://atlas.pingcode.com/files/public/FILE_ID' \
  --workitem WORKITEM_ID --output screenshot.jpg
```

The command uses the selected context, reads the work item with
`include_public_image_token=description`, and passes its scoped `public_image_token`
to Atlas as the `token` query parameter. It never sends the API bearer to Atlas or
the storage server, or prints the image token. No manual token copying is needed.
The image URL must belong to that work item's description. Standard Atlas URLs
and their `/origin-url` variants are supported.

HTTPS redirects to storage preserve signed URLs and remove authorization and
referrer headers. HTTP failures, JSON business errors (including HTTP 200), HTML
responses and files exceeding 100 MiB fail without publishing a partial file.
The command returns JSON containing an absolute local `path`; files have mode
`0600`. Omit `--output` for a unique filename with a detected extension. Existing
files are never overwritten. Only `GET` requests are used, so readonly mode works.

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

Central business requests require E2EE. Keep the existing `e2ee_file` setting
(or `PASEO_BRIDGE_E2EE_FILE`) and provision version 2 locally:

```json
{"version":2,"stateDir":"/absolute/protected/state","hosts":{"aspen":{"serverId":"PINNED_SERVER_ID","daemonPublicKeyB64":"PRIVATE_PAIRING_CAPABILITY","passwordFile":"/optional/protected/password"},"birch":{"serverId":"PINNED_SERVER_ID","daemonPublicKeyB64":"PRIVATE_PAIRING_CAPABILITY"}}}
```

The native client is a pure Go binary. Each command checks owner admission at
`/api/v1/native/hosts` (which contains server IDs only), then connects through the
center's transparent WebSocket proxy to the original Paseo Relay. The CLI pins
the daemon identity locally; the center cannot provide or replace these pins.
Only an authenticated, encrypted `server_info` opens the daemon session.
Existing commands and output shapes remain available; no new command flag,
Node runtime, local listener, or permanent endpoint process is required.

**Treat `daemonPublicKeyB64` as a secret pairing capability.** Although it is a
cryptographic public key, the native Relay handshake allows its holder to open
an owner session. Do not put it in the center, logs, source control, or release
artifacts. Provision pins through an independent trusted channel. Optional
daemon passwords appear only in the encrypted hello, never HTTP/WS headers.
Only trusted owner clients with all six Bridge operations may use native mode;
restricted tokens are rejected. Native Relay grants broad daemon access and
cannot preserve per-operation restrictions after the encrypted handshake.

Configuration and password files must be regular, owned by the current user,
mode `0600` or stricter, and not symlinks. State directories are owner-only
`0700`. Windows binaries can run auth, documentation, and version commands,
but E2EE business operations fail closed until file ACL verification exists.
The state directory preserves existing `submissions/HOST.AGENT.json` guards.
Before any send, the CLI fsyncs a durable guard and sends its UUID as the daemon's
`messageId`. Only a confirmed acceptance clears it. An unresolved send blocks
later sends, including after restart; reconcile it locally instead of retrying.
Idle and accepted status do not prove completion of a specific message.

The center sees bearer authorization, host/server routing, ephemeral client
public keys, frame sizes and timing. Agent IDs, prompts, snapshots, timelines,
and passwords stay inside native NaCl encryption. Native crypto has no bound
sequence number or operation and does not inherently reject replay within a
connection. The CLI rejects repeated incoming nonces, matches response type and
request ID, and uses fresh ephemeral keys per connection; send guards and daemon
message IDs protect dispatch uncertainty. These measures do not claim general
native protocol protection against all replay/reordering by a hostile relay.

Native operations have a 150-second deadline, 16 MiB decrypted-response budget,
and bounded frame counts. Queries of `host=all` fan out locally to pinned hosts;
failures are partial results. Native SSE refreshes snapshots/activity using
read-only RPCs every two seconds. Reconnect starts a fresh snapshot and emits
resync for an existing event cursor; there is no durable replay or automatic
reconnect/resend. OpenAPI discovery is public and unencrypted.

Version 1 protected E2EE files remain supported during migration. They contain
`{version:1,keyFile:"/absolute/client.key",hosts:{HOST:"BASE64_RAW_PUBLIC_KEY"}}`
and use the prior X25519/HKDF/AES-GCM broker protocol and its host-side endpoint.
The key file contains standard base64 of a 32-byte private key. Version 1 static
keys do not provide forward secrecy. There is no plaintext fallback in either
version; missing pins or authentication failures reject business requests.

Branded CLI configuration ignores `--rsh-config`; authenticated response caching
is disabled. External OpenAPI references and cross-origin credential attachment
are rejected. Documentation and browser assets are excluded from generated commands.

```sh
just test-paseo-bridge
just paseo-bridge-spec-check
just build-paseo-bridge
```

The native Relay release uses Go 1.27.2 and compatible Go support-library security patches. Compliance gates imported-package and symbol findings; raw module-only warnings remain recorded, while missing or unknown traces fail closed. Native NaCl code is covered by the Go cryptography BSD notice included in release archives.
