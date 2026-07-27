---
type: Architecture
title: CLI Platform
description: Repository structure and ownership boundaries for aicli.
tags: [aicli, architecture, cli, services, restish]
---

# CLI platform

`aicli` is a Go monorepo and distribution workspace for service-specific CLIs
used by people and AI agents.

## Repository layout

- `services/` registers each service and records how its command surface is built.
- `cmd/` contains release entrypoints such as `aicli` and `pingcode`.
- `internal/<service>rt/` contains only the service adapters needed by Restish.
- `internal/cli/` contains helpers used by the umbrella registry command.
- `openwiki/` is the durable project knowledge layer.

## Runtime model

The preferred service CLI model is:

```text
service binary
  -> embedded Restish
  -> OpenAPI or a small service-specific specification loader
  -> generated operation commands
  -> service-specific authentication handler
  -> service API
```

Restish owns command generation, request construction, output formats, filtering,
pagination, caching, retries, profiles, and transport options. The repository owns
only branding, service discovery, specification adaptation, authentication that
Restish does not provide, and narrow safety policy.

This keeps each service binary independently installable while avoiding a second
Restish executable or a large handwritten API client. A service with a usable
OpenAPI document may need almost no loader code. PingCode needs a loader because
its official description uses a vendor JSON format rather than OpenAPI.

## Command stability

Generated command names follow the upstream specification. They are deterministic,
but they are not a separately curated compatibility layer. When an upstream API
record is added or removed, the generated surface changes after Restish refreshes
the specification cache.

Handwritten aliases and semantic wrappers are not added by default. They are only
justified when a concrete workflow cannot be expressed safely through the generated
operation and the maintenance cost is explicitly accepted.
