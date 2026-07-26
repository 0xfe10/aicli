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

## PingCode Restish experiment

Install Restish with Linuxbrew:

```sh
brew install restish
```

Register the PingCode API base URL as a Restish profile:

```sh
aicli pingcode restish setup --base-url https://api.pingcode.com
```

Configure PingCode auth in Restish, then try the first read-only commands:

```sh
aicli pingcode project list --page-size 10
aicli pingcode work-item search --project-id <project-id> --page-size 10
```

These commands are experimental wrappers around Restish. They are useful for API
exploration and smoke tests, not a replacement for a safer domain CLI that
understands PingCode workflow rules.
