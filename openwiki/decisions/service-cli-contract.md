---
type: Decision
title: Service CLI Contract
description: Contract for Restish-backed service CLIs.
tags: [aicli, decisions, cli-contract, restish]
---

# Service CLI contract

Service binaries embed Restish and expose commands generated from an upstream API
description. The binary name, defaults, authentication, and safety policy remain
service-specific; generic API mechanics remain owned by Restish.

## Required local contract

Each service integration defines:

- one independently installable command under `cmd/`;
- an authoritative specification URL or discovery mechanism;
- the smallest adapter needed to make that specification usable by Restish;
- a credential source that does not require secrets on argv;
- a default policy that blocks unintended writes;
- tests for specification loading, authentication, and the safety gate.

Credentials may come from environment variables or Restish's credential/token
stores. Errors must not include access tokens, client secrets, authorization codes,
or token endpoint response bodies.

## Generated surface

Restish owns operation flags, request bodies, output formats, filtering,
pagination, caching, retries, profiles, TLS, and HTTP execution. Service manifests
describe the generation policy and examples; they do not duplicate every generated
operation.

Command names track the upstream specification and are not promised as a manually
versioned semantic API. Do not add aliases solely to shorten generated names.

## Writes

Every service must default to read-only operation. A service may expose explicit
write levels appropriate to its API. A permitted write is a real request, so help
text and service metadata must not describe the gate as planning or dry-run.

Handwritten workflow guards or semantic commands require a demonstrated limitation
in the generated surface and a separate decision accepting their maintenance cost.
