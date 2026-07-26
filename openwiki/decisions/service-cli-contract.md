---
type: Decision
title: Service CLI Contract
description: Initial contract for AI-oriented service CLIs.
tags: [aicli, decisions, cli-contract, json]
---

# Service CLI contract

Service CLIs should expose a small, stable command surface for AI agents rather
than every raw API endpoint.

## Output

Successful commands write exactly one JSON document to stdout:

```json
{
  "ok": true,
  "data": {},
  "meta": {}
}
```

Failed commands write exactly one JSON document to stdout:

```json
{
  "ok": false,
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable error"
  }
}
```

stderr is reserved for diagnostics. Tokens, client secrets, authorization codes,
and other credentials must not be written to stdout or stderr.

## Writes

Write commands default to planning mode. They should only execute remote writes
when the caller passes `--apply`.

State-changing commands should support an expected-state guard when the backing
service has mutable workflow state. No-change updates should avoid sending write
requests.

## Pagination and retries

Paginated reads must return pagination metadata, including whether results were
truncated and how the caller can continue.

Read retries should be bounded. Non-idempotent writes should not be retried
blindly.

## Restish-backed CLIs

Restish-backed commands are allowed when they sit behind a service-specific Go
command surface. The Go command remains responsible for authentication lifecycle,
token refresh, redaction, dry-run behavior, workflow guards, and stable JSON
errors.

Credentials should be stored through a local secret mechanism, not committed to
the repository. Commands should avoid taking tokens or client secrets as
command-line options.
