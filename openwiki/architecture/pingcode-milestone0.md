---
type: Architecture
title: PingCode Restish Adapter
description: PingCode specification, authentication, and safety integration.
tags: [aicli, pingcode, restish, openapi]
---

# PingCode Restish adapter

## Decision

The `pingcode` binary embeds Restish v2.3.0 as a Go library. It does not ship a
second executable and does not maintain a fork of Restish.

PingCode publishes `https://open.pingcode.com/api_data.json`, a vendor API
description rather than OpenAPI. `internal/pingcodert.APIDocLoader` converts that
document to OpenAPI 3.1 in memory, then Restish generates the CLI commands and
executes requests.

## Local responsibilities

The PingCode adapter contains only:

1. API description detection and conversion.
2. PingCode's GET-based client-credentials token exchange.
3. token-cache and forced-refresh integration with Restish.
4. branded command name, defaults, and local state paths.
5. an HTTP-method write gate controlled by `PINGCODE_WRITE_MODE`.
6. local authorization control-plane commands (`auth login|status|logout`) that
   persist credentials under `$XDG_CONFIG_HOME/aicli/pingcode/config.toml`.

It does not contain a handwritten work-item client, domain service, OAuth browser
flow, dry-run planner, MCP oracle, or manually curated aliases.

## Authentication

Callers provide credentials through either:

1. `pingcode auth login --mode client` or `--mode token` (interactive; stored locally), or
2. environment variables for CI / temporary override:
   - `PINGCODE_API_BASE_URL` (optional Base URL override),
   - `PINGCODE_ACCESS_TOKEN`, or
   - `PINGCODE_CLIENT_ID` and `PINGCODE_CLIENT_SECRET` (both required together).

Login always prompts for Base URL and writes it with `[auth]` in one atomic update.
Environment variables override individual `config.toml` fields for the current
process and never rewrite the file. Incomplete client environment pairs are
rejected. Secrets are not accepted on argv.

Shared prompt/validation helpers live in `internal/authflow`; see
[unified CLI auth](../decisions/unified-cli-auth.md).

For client credentials, the handler requests
`GET /v1/auth/token?grant_type=client_credentials&...`, stores the resulting token
through Restish's token store, and forces one refresh after an unauthorized
response for read requests. Writes are never automatically replayed after an
unauthorized response because their remote outcome may be uncertain. Token
endpoint response bodies and transport URLs are not included in errors.
Login and logout clear cached client-credentials tokens so a rotated secret cannot
reuse a stale access token.

The generated API keeps user-token-only operations visible because they are part
of the official surface. Client credentials cannot authorize those operations;
OAuth support is deferred until there is a concrete need to execute them.

## Safety

`PINGCODE_WRITE_MODE` defaults to `readonly`:

- `readonly`: `GET`, `HEAD`, `OPTIONS`
- `write`: also `POST`, `PUT`, `PATCH`
- `destructive`: also `DELETE`

This is an execution gate, not a dry-run implementation. Once a write mode permits
a request, Restish sends it to PingCode.

## Compatibility gate

Unit tests cover conversion, generated read and write requests, authentication,
token caching, forced refresh, and the default write block. Run
`just pingcode-spec-check` to download the current official corpus and prove that
all supported records still convert successfully.

Generated operation names are based on method and path. Some names are long, and
Restish may append the HTTP method when a generated alias collides. This is an
accepted consequence of keeping the surface automatic and low-maintenance.
