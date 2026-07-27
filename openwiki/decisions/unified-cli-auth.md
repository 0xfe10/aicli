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
- trailing `/` is removed; subpaths are allowed

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

## Status and logout

`auth status` prints the shared JSON fields `configured`, `mode`, `baseUrl`,
`baseUrlSource`, `credentialSource`, and `configPath`. It never prints tokens,
client secrets, or full client IDs.

`auth logout` clears credentials only and preserves Base URL and non-secret
settings. If credential environment variables remain set, logout prints a warning
that the current process will still use environment authorization.

## FNS placeholder Base URL

`fns` keeps `DefaultBaseURL = "https://fns.example.com"` so `--help` works without
tenant configuration. Real API requests fail closed before network I/O when the
effective Base URL is still that placeholder:

```text
FNS Base URL is not configured; run "fns auth login --mode token" or set FNS_BASE_URL
```
