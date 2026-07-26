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

## Restish-backed wrappers

Restish-backed commands are allowed as experiments for read-only API discovery
and smoke tests. They should be marked as experimental in service manifests.

Credentials should be configured through Restish profiles or another local secret
mechanism, not committed to the repository. Wrapper commands should avoid taking
tokens or client secrets as command-line options.
