---
type: Knowledge Base Entrypoint
title: aicli OpenWiki Quickstart
description: Entry point for the aicli service CLI registry and project structure.
tags: [aicli, openwiki, cli, ai-services, restish]
---

# aicli OpenWiki quickstart

`aicli` is a Go monorepo for independently released, Restish-backed service CLIs.

## Start here

- [CLI platform](architecture/cli-platform.md) explains the shared runtime model.
- [Service CLI contract](decisions/service-cli-contract.md) records integration rules.
- [PingCode Restish adapter](architecture/pingcode-milestone0.md) records the first service implementation.
- `services/` contains service registrations and generation metadata.
- `cmd/` contains release command entrypoints.
- `internal/` contains the small shared and service-specific Go packages.

## Local smoke test

```sh
go run ./cmd/aicli services
go run ./cmd/pingcode --help
go run ./cmd/pingcode auth status
just verify
just pingcode-spec-check
```

Authorize once with interactive login (no secrets on argv), then call generated
commands without exporting credentials:

```sh
go run ./cmd/pingcode auth login --mode client
go run ./cmd/pingcode pjm get-projects -o json
```

OpenWiki content is maintained in-repository. Do not add OpenWiki CI or scheduled
automation unless that is approved as a separate change.
