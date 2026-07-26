# aicli

`aicli` is a Go monorepo for AI-oriented service CLIs.

The project is intended to provide CLI tools for working with different AI
service providers and workflows from a single place.

## Structure

- `cmd/` contains release command entrypoints.
- `internal/` contains shared Go packages for service CLIs.
- `services/` contains service CLI registrations and command manifests.
- `openwiki/` contains repository knowledge, architecture notes, and decisions.

Service implementations may live in this repository or in external projects when
they already own mature API clients, authentication, and business safety logic.

## Local commands

List registered service CLIs:

```sh
go run ./cmd/aicli services
```

Inspect the PingCode placeholder command:

```sh
go run ./cmd/pingcode version
```

Future service CLIs are expected to use embedded Restish-backed transport while
keeping service-specific authentication, token refresh, redaction, dry-run, and
workflow safety logic in Go.
