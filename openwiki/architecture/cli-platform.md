---
type: Architecture
title: CLI Platform
description: Repository structure and ownership boundaries for aicli.
tags: [aicli, architecture, cli, services]
---

# CLI platform

`aicli` is a Go monorepo and distribution workspace for command-line tools that
let AI agents use external services through narrow, stable command surfaces.

## Repository layout

- `services/` is the service registry. Each service owns a `service.yaml` file and
  a `commands.yaml` file.
- `cmd/` contains release command entrypoints such as `aicli` and `pingcode`.
- `internal/pingcode/` owns PingCode authentication, typed client, domain safety
  logic, and command routing.
- `internal/restishrt/` embeds Restish v2.3.0 for `pingcode raw` only.
- `internal/cli/` contains shared JSON output and exit codes.
- `openwiki/` is the durable project knowledge layer.

## Implementation boundary

Service CLIs are Go commands. They expose service-specific business commands while
sharing common output, redaction, registry, and optional Restish raw transport
support through `internal/`.

PingCode domain commands use a typed Go HTTP client. Restish is not used for
create/update/transition/comment because its FetchResponse API does not support
request bodies and its Run path owns its own output tree.

## Restish direction

Restish v2.3.0 is compiled into the `pingcode` binary and exposed only as
`pingcode raw`. Users do not need a separate Restish install.
