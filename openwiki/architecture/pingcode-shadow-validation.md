# PingCode shadow validation gate

Status: **blocked — waiting for dedicated test credentials**

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
