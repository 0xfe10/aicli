# PingCode shadow validation gate

Status: **API validation passed; local fixed binary passes MCP dry-run parity. `v0.1.1` remains rejected.**

This gate is required before release. Do not publish until both route families
and controlled writes succeed against a real tenant.

## Required local env (do not commit)

Inject only into the local shell or a local untracked env file ignored by git:

```sh
export PINGCODE_BASE_URL='https://<tenant>.pingcode.com'
export PINGCODE_API_BASE_URL='https://open.pingcode.com'
export PINGCODE_CLIENT_ID='...'
export PINGCODE_CLIENT_SECRET='...'
export PINGCODE_PROJECT_IDENTIFIER='...'
# optional user mode after auth login
# export PINGCODE_AUTH_TOKEN_PATH="$XDG_CONFIG_HOME/aicli/pingcode/auth.json"
```

Never paste secrets into chat, commits, OpenWiki, or CI logs.

## Phase A — route parity (read-only)

For the same auth:

1. `GET /v1/project/projects` vs `GET /v1/pjm/projects`
2. `GET /v1/project/work_item/types?project_id=...` vs `/v1/pjm/work_item/types?...`
3. `GET /v1/project/work_items?...` vs `/v1/pjm/work_items?...`

Compare ids, totals, state names, and assignee fields. Record whether CLI should
keep `/v1/project` or switch.

Suggested CLI checks:

```sh
go run ./cmd/pingcode config check
go run ./cmd/pingcode auth status
go run ./cmd/pingcode project list
go run ./cmd/pingcode project schema --kind bug
go run ./cmd/pingcode work-item search --page-size 5
go run ./cmd/pingcode work-item mine --page-size 5
```

Also compare the same queries through the existing MCP deployment.

## Phase B — dry-run writes

```sh
printf '%s\n' '{"kind":"bug","title":"aicli-shadow-dryrun"}' \
  | go run ./cmd/pingcode work-item create --input -
```

Confirm zero remote create. Repeat for update/transition/comment plans against
an existing disposable work item.

## Phase C — controlled apply in a dedicated test project only

1. create one test work item with `--apply`
2. update title/description, including an explicit description clear
3. transition with `expectedCurrentState`
4. comment once
5. manually confirm in the PingCode UI
6. leave the item labeled as disposable test data

## Pass criteria

- No credential leakage in stdout/stderr
- Exact project match behavior confirmed on real identifiers
- `/project` vs `/pjm` decision recorded
- dry-run produces zero writes
- apply path matches MCP semantics for the exercised commands

## 2026-07-26 validation record

Tenant and credential values are intentionally omitted.

- Auth: application/client-credentials mode passed; local token file was absent.
- Exact project resolution: identifier `CS` resolved once to project
  `666eeb06c5726f152285aba4`.
- Route parity: `/v1/project` and `/v1/pjm` returned identical ids and totals for
  projects (1), work-item types (5), and work items (initially 0).
- Decision: keep `/v1/project` for the current CLI. The exercised `/pjm` aliases
  were equivalent, so there is no migration benefit in this release.
- Domain reads: project schema and work-item search passed. Schema contained
  seven bug states, five priorities, and seven transition groups.
- Dry-run create passed and produced no remote work item.
- Controlled item: `CS-1` / `6a662c88a2f1bc8bb00dc235`.
  - create passed
  - guarded title/description update passed
  - rich-text description clear passed using `<p></p>`
  - guarded transition `新提交` -> `处理中` passed
  - one comment was created (`6a662d8ca2f1bc8bb00dc241`)
  - repeated clear was correctly reported as `noChange`
  - stale expected state was rejected with `EXPECTED_STATE_MISMATCH`
- The item remains in the test project as disposable validation data.

Real-tenant defects found and fixed during validation:

1. Project member requests used `page_size=200`, but the API maximum is 100.
2. A failed `raw` request emitted two JSON documents because both transport and
   command layers encoded the error. The command layer now owns all encoding.
3. Real work items return `type` as a string id, not always an expanded object.
4. PingCode ignores both `description:""` and `description:null`; its canonical
   empty rich-text document is `<p></p>`.

Remaining release-gate checks:

- Publish `v0.1.2`, download the release artifact, and re-run this matrix on the
  artifact (not a local `go build`).
- Controlled `--apply` vs MCP write comparison.
- Manually confirm `CS-1` in the PingCode UI.

## 2026-07-27 MCP parity (kahub PingCode tools)

Source: `~/.codex/config.toml` → `mcp_servers.kahub` → PingCode tools
(`PingCode-pingcode_*`, 39 tools registered).

Compared against local fixed CLI (`go build ./cmd/pingcode`), not `bin/pingcode` v0.1.1.

| Capability | MCP | Fixed CLI | v0.1.1 |
|---|---|---|---|
| list projects (`CS`) | pass, id `666eeb06c5726f152285aba4` | pass, same id | pass |
| project schema bug | pass: 7 states / 5 priorities | pass: same names | fail `page_size=200` |
| search `CS-1` | pass | pass | fail |
| get `CS-1` | MCP output schema rejects numeric timestamps | pass | n/a via schema path |
| create dry-run | pass, no write | pass, no write | fail |
| update dry-run | pass | pass | fail |
| transition dry-run / plan | pass | pass | fail |
| comment dry-run | pass | pass | fail |
| failed `raw` JSON contract | n/a | one JSON doc, exit 72 | two JSON docs |

Notes:

- Keep `/v1/project`; `/v1/pjm` remains equivalent for exercised reads.
- Do not chase all 39 MCP discovery tools in v0.1.x. First release stays on
  `services/pingcode/commands.yaml`.
- MCP `get_work_item` currently fails LiteLLM/output schema validation because
  PingCode returns numeric epoch fields; CLI correctly accepts them.
- Next gate: commit these fixes, publish `v0.1.2`, download the release artifact,
  re-run this matrix, then do controlled `--apply` vs MCP write comparison.
