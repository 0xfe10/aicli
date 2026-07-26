# aicli

`aicli` is a command-line interface for AI-oriented services.

The project is intended to provide CLI tools for working with different AI
service providers and workflows from a single place.

## Structure

- `packages/aicli/` contains the umbrella CLI package.
- `services/` contains service CLI registrations and command manifests.
- `openwiki/` contains repository knowledge, architecture notes, and decisions.

Service implementations may live in this repository or in external projects when
they already own mature API clients, authentication, and business safety logic.
