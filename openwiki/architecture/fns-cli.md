# FNS CLI

`fns` embeds Restish v2.3.0 and generates Note/File/Folder commands and a
read-only Vault list command from the
Fast Note Sync API description.

## Runtime path

1. Fetch `FNS_SPEC_URL` (default pinned Swagger 2.0 at
   `https://raw.githubusercontent.com/haierkeys/fast-note-sync-service/b6b4566352f39e0404530ed1b58248a815a6d763/docs/swagger.yaml`).
2. `fnsrt.SpecLoader` detects Swagger 2 or OpenAPI 3 (JSON/YAML).
3. Swagger 2 is converted with `swagger2rt`; OpenAPI 3 is normalized to JSON.
4. Both formats always run through `FixSpec` before Restish sees the document.
5. External OpenAPI `$ref` targets are rejected (remote http(s), `file://`, and
   relative files for remote specs). Same-document `#/components/...` refs remain.
6. Restish generates tag-grouped commands using Method + Path fallback names.

Until FNS publishes a stable OpenAPI endpoint (M9), keep the default Spec URL on
the pinned commit `b6b4566352f39e0404530ed1b58248a815a6d763`. Do not switch the default to a branch tip.

## Auth and safety

- Config: `$XDG_CONFIG_HOME/aicli/fns/config.toml` (0700/0600).
- Env overrides: `FNS_BASE_URL`, `FNS_ACCESS_TOKEN`, `FNS_CLIENT`, `FNS_SPEC_URL`, `FNS_WRITE_MODE`.
- Requests send `Authorization: Bearer`, `X-Client`, `X-Client-Name`, and `X-Client-Version`.
- Default write mode is `readonly`; DELETE requires `destructive`.
- 401 responses do not automatically replay write requests.

## First-release scope

Open: `/api/note*`, `/api/notes*`, `/api/file*`, `/api/files*`, `/api/folder*`, `/api/folders*`
(excluding paths that contain `/share`) and only `GET /api/vault` for vault discovery.

Deferred: OAuth, Share admin, Vault mutation, Backup, Storage, and other WebGUI-only APIs.
The deployed FNS server must permit REST tokens to call `GET /api/vault`; older
servers return `Auth token Client restricted` until that server-side route is updated.
Server-side OpenAPI 3 publication remains tracked as M9.

## Binary size note

Linux amd64 stripped release build (`VERSION=0.2.0`, `-trimpath -ldflags="-s -w"`):

| binary   | size   |
|----------|--------|
| pingcode | ~33M   |
| fns      | ~34.2M (`35827838` bytes) |

The increase for `fns` versus `pingcode` is mainly from `kin-openapi` / `openapi2conv`
and YAML decode in the linked closure.
