---
type: Architecture
title: CLI Platform
description: Repository structure and ownership boundaries for aicli.
tags: [aicli, architecture, cli, services]
---

# CLI platform

`aicli` is a registry and distribution workspace for command-line tools that let
AI agents use external services through narrow, stable command surfaces.

## Repository layout

- `services/` is the service registry. Each service owns a `service.yaml` file and
  a `commands.yaml` file.
- `packages/aicli/` is the umbrella CLI package. It can discover or route to
  registered service CLIs, but it should not absorb every service implementation.
- `openwiki/` is the durable project knowledge layer.

## Implementation boundary

Service CLIs can be implemented inside this repository or maintained externally.
When a service already has mature client, authentication, retry, safety, and
business logic, the CLI should reuse that implementation instead of reimplementing
raw REST calls here.

PingCode is registered as an external implementation because the existing
`0xfe10/pingcode-mcp` project already owns the API client, OAuth, token storage,
dry-run behavior, status guards, and error redaction.
