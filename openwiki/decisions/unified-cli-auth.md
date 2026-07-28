---
type: Decision
title: Unified CLI Authentication Contract
description: Shared auth login/status/logout behavior for service CLIs.
tags: [aicli, decisions, auth, pingcode, fns]
---

# Unified CLI authentication contract

`pingcode` and `fns` share the same authorization control-plane commands:

```text
<cli> auth login --mode <mode>
<cli> auth status
<cli> auth logout
```

Shared interactive helpers live in `internal/authflow` (Base URL prompt, hidden
secret prompt, URL normalization, and the status JSON shape). Each service keeps
its own config schema and credential modes.

## Login

Every `auth login` prompts for Base URL first, then credentials for the selected
mode, then writes both atomically. Secrets are never accepted on argv.

Base URL rules:

- must be an absolute HTTP(S) URL
- production hosts require HTTPS; only `localhost` / `127.0.0.1` / `::1` may use HTTP
- username, password, query, and fragment are rejected
- trailing `/` is removed; subpaths parse correctly but carry the redirect limitation below
- encoded path separators (`%2F`) are rejected so normalization cannot change routing semantics

Config paths follow XDG:

```text
~/.config/aicli/pingcode/config.toml
~/.config/aicli/fns/config.toml
```

Directory mode is `0700`, file mode is `0600`, symlinks are rejected, and writes
are atomic (failed writes leave the previous file intact).

## Precedence

API commands resolve configuration as:

```text
environment variables > config.toml > compile-time defaults
```

Environment variables may override individual fields. For example a file may
provide Base URL while an environment variable supplies only a token.

| Service  | Base URL                 | Credentials                                      |
|----------|--------------------------|--------------------------------------------------|
| PingCode | `PINGCODE_API_BASE_URL`  | `PINGCODE_ACCESS_TOKEN` or client id/secret pair |
| FNS      | `FNS_BASE_URL`           | `FNS_ACCESS_TOKEN`                               |

Branded CLIs do **not** treat Restish `restish.json` as configuration authority.
`pingcode` and `fns` isolate Restish to an empty engine config under
`~/.config/aicli/<service>/engine/restish.json`, ignore `--rsh-config`, and pin
`RSH_CONFIG` to that engine file. `RSH_CACHE_DIR` remains available for Restish
HTTP/spec caches only.

When no writable user config directory exists, API commands using environment
credentials use a private transient engine directory. `auth login` and `auth logout`
still require a persistent config path.

Base URL and credentials are loaded once per process into a session snapshot.
Auth handlers attach credentials only when `req.URL` is under that snapshot Base
URL; otherwise they fail closed.

Restish v2.3.0 strips credentials on cross-origin redirects, but does not expose a
redirect hook for enforcing a Base URL subpath on same-origin redirects. Current
PingCode and FNS deployments use origin-only Base URLs. Treat subpath Base URLs as
unsupported for strict credential isolation until Restish exposes that hook.

Authenticated API commands force Restish response caching off so switching
credentials at the same Base URL cannot reuse another identity's cached GET.

## Status and logout

`auth status` prints the shared JSON fields `configured`, `mode`, `baseUrl`,
`baseUrlSource`, `credentialSource`, and `configPath`. It never prints tokens,
client secrets, or full client IDs. For FNS, `configured` is true only when
credentials exist **and** the effective Base URL is a real tenant URL (not the
placeholder default).

`auth logout` clears credentials only and preserves Base URL and non-secret
settings. If credential environment variables remain set, logout prints a warning
that the current process will still use environment authorization.

`auth --help` and `auth login --help` are supported. Top-level `--help` also
mentions the local `auth` commands.

## FNS placeholder Base URL

`fns` keeps `DefaultBaseURL = "https://fns.example.com"` so `--help` works without
tenant configuration. Placeholder detection matches the hostname case-insensitively
(including trailing dots, ports, and paths). Login/`SaveLogin` reject that host.
Real API requests fail closed before network I/O when the effective Base URL still
targets it:

```text
FNS Base URL is not configured; run "fns auth login --mode token" or set FNS_BASE_URL
```

RC configs that stored a top-level `access_token` are read and migrated into
`[auth]` on the next safe write.
