---
type: Knowledge Base Entrypoint
title: aicli OpenWiki Quickstart
description: Entry point for the aicli service CLI registry and project structure.
tags: [aicli, openwiki, cli, ai-services]
---

# aicli OpenWiki quickstart

`aicli` is the workspace for AI-oriented service CLIs. It keeps a small umbrella
CLI, a machine-readable service registry, and project knowledge for how service
CLIs should behave.

## Start here

- [CLI platform](architecture/cli-platform.md) explains the repository layout and
  ownership boundaries.
- [Service CLI contract](decisions/service-cli-contract.md) records the initial
  command contract for AI-safe service CLIs.
- `services/` contains service registrations and command manifests.
- `packages/aicli/` contains the minimal umbrella CLI package.

OpenWiki content is maintained in-repository. Do not add OpenWiki CI or scheduled
automation unless that is approved as a separate change.
