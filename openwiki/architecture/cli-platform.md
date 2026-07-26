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
- `internal/` contains shared Go packages for JSON output, service registry, and
  future Restish runtime integration.
- `openwiki/` is the durable project knowledge layer.

## Implementation boundary

Service CLIs are Go commands. They should expose service-specific business
commands while sharing common output, redaction, registry, and Restish runtime
support through `internal/`.

When a service already has mature client, authentication, retry, safety, and
business logic, that behavior should be ported intentionally instead of replaced
with raw HTTP calls.

## Restish direction

Future service CLIs are expected to use Restish as an embedded or bundled
transport layer rather than requiring users to install Restish separately.

The first Go cut only records the runtime boundary. The release design still
needs to decide how Restish binaries are fetched, verified, embedded, extracted,
and executed for each supported OS and architecture.
