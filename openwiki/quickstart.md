---
type: Knowledge Base Entrypoint
title: aicli OpenWiki Quickstart
description: Entry point for the aicli service CLI registry and project structure.
tags: [aicli, openwiki, cli, ai-services]
---

# aicli OpenWiki quickstart

`aicli` is a Go monorepo for AI-oriented service CLIs. It keeps command
entrypoints, shared Go packages, a machine-readable service registry, and project
knowledge for how service CLIs should behave.

## Start here

- [CLI platform](architecture/cli-platform.md) explains the repository layout and
  ownership boundaries.
- [Service CLI contract](decisions/service-cli-contract.md) records the command
  contract for AI-safe service CLIs.
- [PingCode Milestone 0](architecture/pingcode-milestone0.md) records path and
  Restish gating decisions.
- `services/` contains service registrations and command manifests.
- `cmd/` contains release command entrypoints.
- `internal/` contains shared and service Go packages.

## Local smoke test

```sh
go run ./cmd/aicli services
go run ./cmd/pingcode version
go test ./...
```

OpenWiki content is maintained in-repository. Do not add OpenWiki CI or scheduled
automation unless that is approved as a separate change.
