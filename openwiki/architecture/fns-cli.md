# FNS CLI

`fns` embeds Restish v2.3.0 and generates Note/File/Folder commands from the
Fast Note Sync Swagger description.

## Runtime path

1. Fetch `FNS_SPEC_URL` (default `https://obsidian-fns.kahub.in/openapi.yaml`).
2. `swagger2rt` detects Swagger 2.0 (JSON or YAML) and converts with `openapi2conv`.
3. `fnsrt` removes localhost servers, converts `UserAuthToken` to HTTP Bearer,
   strips duplicate `token` header parameters, and keeps only Note/File/Folder paths.
4. Restish generates tag-grouped commands using Method + Path fallback names.

## Auth and safety

- Config: `$XDG_CONFIG_HOME/aicli/fns/config.toml` (0700/0600).
- Env overrides: `FNS_BASE_URL`, `FNS_ACCESS_TOKEN`, `FNS_CLIENT`, `FNS_SPEC_URL`, `FNS_WRITE_MODE`.
- Requests send `Authorization: Bearer`, `X-Client`, `X-Client-Name`, and `X-Client-Version`.
- Default write mode is `readonly`; DELETE requires `destructive`.
- 401 responses do not automatically replay write requests.

## First-release scope

Open: `/api/note*`, `/api/notes*`, `/api/file*`, `/api/files*`, `/api/folder*`, `/api/folders*`
(excluding paths that contain `/share`).

Deferred: OAuth, Share admin, Vault management, Backup, Storage, WebGUI-only APIs.
Server-side OpenAPI 3 publication and Vault REST consistency are tracked as M9.

## Binary size note

Linux amd64 stripped builds measured during implementation:

| binary   | size |
|----------|------|
| pingcode | 33M  |
| fns      | 35M  |

The ~2M increase for `fns` is mainly from promoting `kin-openapi` / `openapi2conv`
into the linked closure for Swagger 2 conversion.
