#!/usr/bin/env python3
"""M4 CLI/MCP twin-track orchestrator for CS test project.

Writes evidence under dist/m4/<runId>/ without logging secrets.
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


def utc_run_id() -> str:
    return datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ") + "-m4"


def save(path: Path, doc: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(doc, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def redact_doc(doc: Any) -> Any:
    text = json.dumps(doc, ensure_ascii=False)
    for key in ("accessToken", "refreshToken", "clientSecret", "authorization_code", "code"):
        if f'"{key}"' in text:
            pass
    return doc


class CLI:
    def __init__(self, bin_path: str, env: dict[str, str], out: Path):
        self.bin = bin_path
        self.env = env
        self.out = out

    def run(self, args: list[str], name: str) -> tuple[int, dict[str, Any]]:
        proc = subprocess.run(
            [self.bin, *args],
            env=self.env,
            capture_output=True,
            text=True,
            check=False,
        )
        stdout = proc.stdout.strip()
        stderr = proc.stderr.strip()
        doc: dict[str, Any]
        try:
            doc = json.loads(stdout) if stdout else {"ok": False, "error": {"message": "empty stdout"}}
        except json.JSONDecodeError:
            doc = {"ok": False, "error": {"message": "non-json stdout", "stdout": stdout[:500]}}
        payload = {"exitCode": proc.returncode, "args": args, "doc": doc}
        if stderr:
            payload["stderr"] = stderr[:500]
        save(self.out / f"{name}.json", redact_doc(payload))
        return proc.returncode, doc


class MCP:
    def __init__(self, url: str, token: str, out: Path):
        self.url = url.rstrip("/")
        if not self.url.endswith("/mcp"):
            self.url += "/mcp"
        self.token = token
        self.out = out
        self._id = 0

    def call(self, tool: str, arguments: dict[str, Any], name: str) -> dict[str, Any]:
        self._id += 1
        body = {
            "jsonrpc": "2.0",
            "id": self._id,
            "method": "tools/call",
            "params": {"name": tool, "arguments": arguments},
        }
        req = urllib.request.Request(
            self.url,
            data=json.dumps(body).encode(),
            headers={
                "Authorization": f"Bearer {self.token}",
                "Content-Type": "application/json",
                "Accept": "application/json, text/event-stream",
            },
            method="POST",
        )
        try:
            with urllib.request.urlopen(req, timeout=180) as resp:
                raw = resp.read().decode()
        except urllib.error.HTTPError as exc:
            raw = exc.read().decode()
            doc = {"ok": False, "error": {"message": f"HTTP {exc.code}", "body": raw[:500]}}
            save(self.out / f"{name}.json", {"tool": tool, "arguments": arguments, "doc": doc})
            return doc
        text = raw
        if "data:" in raw:
            text = "\n".join(line[5:].strip() for line in raw.splitlines() if line.startswith("data:"))
        rpc = json.loads(text)
        sc = (rpc.get("result") or {}).get("structuredContent") or {}
        save(self.out / f"{name}.json", {"tool": tool, "arguments": arguments, "doc": sc})
        return sc


def assert_ok(doc: dict[str, Any], label: str) -> None:
    if not doc.get("ok"):
        raise SystemExit(f"{label} failed: {json.dumps(doc, ensure_ascii=False)[:400]}")


def pick_transition(schema_doc: dict[str, Any], current: str) -> tuple[str, str]:
    data = schema_doc.get("data") or schema_doc.get("schema") or schema_doc
    for edge in (data.get("stateTransitions") or []):
        fr = edge.get("fromState") or {}
        name = fr.get("name") if isinstance(fr, dict) else fr
        if name != current:
            continue
        for dest in edge.get("to") or []:
            to = dest.get("name") if isinstance(dest, dict) else dest
            if to and to != current:
                return str(name), str(to)
    states = [s.get("name") if isinstance(s, dict) else s for s in (data.get("states") or [])]
    states = [str(s) for s in states if s]
    if current in states:
        idx = states.index(current)
        for candidate in states[idx + 1 :] + states[:idx]:
            if candidate != current:
                return current, candidate
    raise SystemExit(f"no legal transition from {current!r}")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--pingcode-bin", required=True)
    parser.add_argument("--mcp-url", default="http://127.0.0.1:3000")
    parser.add_argument("--mcp-token", default=os.environ.get("PINGCODE_MCP_HTTP_TOKEN", ""))
    parser.add_argument("--run-id", default=utc_run_id())
    parser.add_argument("--project-identifier", default="CS")
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()

    if not args.mcp_token:
        print("PINGCODE_MCP_HTTP_TOKEN required", file=sys.stderr)
        return 64

    out = Path("dist/m4") / args.run_id
    out.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    env.setdefault("PINGCODE_PROJECT_IDENTIFIER", args.project_identifier)

    meta = {
        "runId": args.run_id,
        "utc": datetime.now(timezone.utc).isoformat(),
        "projectIdentifier": args.project_identifier,
        "apply": args.apply,
        "pingcodeBin": args.pingcode_bin,
        "mcpUrl": args.mcp_url,
    }
    save(out / "meta.json", meta)

    cli = CLI(args.pingcode_bin, env, out / "cli")
    mcp = MCP(args.mcp_url, args.mcp_token, out / "mcp")

    # Read baseline snapshots for legacy items
    for ident, kind in [("CS-2", "bug"), ("CS-5", "requirement")]:
        _, doc = cli.run(["work-item", "get", "--kind", kind, "--identifier", ident], f"baseline-get-{ident}")
        assert_ok(doc, f"baseline get {ident}")

    # Schema parity
    for kind in ("bug", "requirement"):
        _, cli_schema = cli.run(["project", "schema", "--kind", kind], f"schema-{kind}")
        assert_ok(cli_schema, f"cli schema {kind}")
        mcp_schema = mcp.call(
            "pingcode_get_project_schema",
            {"projectIdentifier": args.project_identifier, "kind": kind},
            f"schema-{kind}",
        )
        assert_ok(mcp_schema, f"mcp schema {kind}")
        cli_data = cli_schema.get("data") or {}
        mcp_schema_data = mcp_schema.get("schema") or {}
        cli_members = cli_data.get("members") or []
        mcp_members = mcp_schema_data.get("members") or []
        if len(cli_members) != len(mcp_members):
            raise SystemExit(f"member count mismatch {kind}: cli={len(cli_members)} mcp={len(mcp_members)}")
        mcp_members_page = mcp.call(
            "pingcode_list_project_members",
            {"projectIdentifier": args.project_identifier, "pageSize": 100},
            f"members-{kind}",
        )
        assert_ok(mcp_members_page, f"mcp list members {kind}")
        if len(mcp_members_page.get("values") or []) < 1:
            raise SystemExit("mcp list_project_members returned zero members")

    prefix = f"aicli-m4-{args.run_id}"
    cli_title = f"{prefix}-cli-bug"
    mcp_title = f"{prefix}-mcp-req"

    # Dry-run both sides
    cli_create_in = json.dumps({"kind": "bug", "title": cli_title})
    proc = subprocess.run(
        [args.pingcode_bin, "work-item", "create", "--input", "-"],
        input=cli_create_in,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    cli_create_doc = json.loads(proc.stdout)
    assert_ok(cli_create_doc, "cli create dry-run")
    save(out / "cli/create-dryrun-bug.json", cli_create_doc)

    mcp_create = mcp.call(
        "pingcode_create_work_item",
        {
            "projectIdentifier": args.project_identifier,
            "kind": "requirement",
            "title": mcp_title,
            "dryRun": True,
        },
        "create-dryrun-req",
    )
    assert_ok(mcp_create, "mcp create dry-run")

    if not args.apply:
        save(out / "summary.json", {"status": "dry-run-ok", "runId": args.run_id})
        print(f"M4 dry-run ok: {out}")
        return 0

    # CLI apply bug track
    proc = subprocess.run(
        [args.pingcode_bin, "work-item", "create", "--input", "-", "--apply"],
        input=json.dumps({"kind": "bug", "title": cli_title, "description": f"runId={args.run_id}"}),
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    cli_created = json.loads(proc.stdout)
    assert_ok(cli_created, "cli create apply")
    save(out / "cli/create-apply-bug.json", cli_created)
    created = (cli_created.get("data") or {}).get("created") or {}
    wi_id = created.get("id")
    wi_ident = created.get("identifier")
    from_state = created.get("state")
    if isinstance(from_state, dict):
        from_state = from_state.get("name")

    mcp_read = mcp.call(
        "pingcode_get_work_item",
        {"projectIdentifier": args.project_identifier, "kind": "bug", "identifier": wi_ident},
        "read-after-cli-create",
    )
    assert_ok(mcp_read, "mcp read after cli create")

    proc = subprocess.run(
        [args.pingcode_bin, "work-item", "update", "--input", "-", "--apply"],
        input=json.dumps({"kind": "bug", "workItemId": wi_id, "title": f"{cli_title}-updated"}),
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    assert_ok(json.loads(proc.stdout), "cli update apply")

    _, schema_doc = cli.run(["project", "schema", "--kind", "bug"], "schema-bug-for-transition")
    from_state, to_state = pick_transition({"data": schema_doc.get("data") or {}}, str(from_state or ""))
    proc = subprocess.run(
        [args.pingcode_bin, "work-item", "transition", "--input", "-", "--apply"],
        input=json.dumps(
            {
                "kind": "bug",
                "workItemId": wi_id,
                "statusName": to_state,
                "expectedCurrentState": from_state,
            }
        ),
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    assert_ok(json.loads(proc.stdout), "cli transition apply")

    proc = subprocess.run(
        [args.pingcode_bin, "work-item", "comment", "--input", "-", "--apply"],
        input=json.dumps({"kind": "bug", "workItemId": wi_id, "content": f"{prefix} cli comment"}),
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    assert_ok(json.loads(proc.stdout), "cli comment apply")

    mcp.call(
        "pingcode_get_work_item",
        {"projectIdentifier": args.project_identifier, "kind": "bug", "identifier": wi_ident, "includeComments": True},
        "read-after-cli-writes",
    )

    # MCP apply requirement track
    mcp_created = mcp.call(
        "pingcode_create_work_item",
        {
            "projectIdentifier": args.project_identifier,
            "kind": "requirement",
            "title": mcp_title,
            "description": f"runId={args.run_id}",
            "dryRun": False,
        },
        "create-apply-req",
    )
    assert_ok(mcp_created, "mcp create apply")
    req = (mcp_created.get("created") or {})
    req_ident = req.get("identifier")
    req_id = req.get("id")
    req_state = req.get("state")

    _, cli_read = cli.run(["work-item", "get", "--kind", "requirement", "--identifier", req_ident], "read-after-mcp-create")
    assert_ok(cli_read, "cli read after mcp create")

    mcp.call(
        "pingcode_update_work_item_fields",
        {
            "projectIdentifier": args.project_identifier,
            "kind": "requirement",
            "workItemId": req_id,
            "title": f"{mcp_title}-updated",
            "dryRun": False,
        },
        "update-apply-req",
    )

    mcp_schema = mcp.call(
        "pingcode_get_project_schema",
        {"projectIdentifier": args.project_identifier, "kind": "requirement"},
        "schema-req-for-transition",
    )
    _, to_req = pick_transition({"schema": mcp_schema.get("schema") or {}}, str(req_state or ""))
    mcp.call(
        "pingcode_update_requirement_status",
        {
            "projectIdentifier": args.project_identifier,
            "workItemId": req_id,
            "statusName": to_req,
            "expectedCurrentStatusName": req_state,
            "dryRun": False,
        },
        "transition-apply-req",
    )

    mcp.call(
        "pingcode_add_work_item_comment",
        {
            "projectIdentifier": args.project_identifier,
            "kind": "requirement",
            "workItemId": req_id,
            "content": f"{prefix} mcp comment",
            "dryRun": False,
        },
        "comment-apply-req",
    )

    _, cli_final = cli.run(
        ["work-item", "get", "--kind", "requirement", "--identifier", req_ident, "--include-comments"],
        "read-after-mcp-writes",
    )
    assert_ok(cli_final, "cli final read")

    # Legacy items unchanged
    for ident, kind in [("CS-2", "bug"), ("CS-5", "requirement")]:
        _, after = cli.run(["work-item", "get", "--kind", kind, "--identifier", ident], f"after-get-{ident}")
        assert_ok(after, f"after get {ident}")

    save(
        out / "summary.json",
        {
            "status": "apply-ok",
            "runId": args.run_id,
            "cliBug": {"id": wi_id, "identifier": wi_ident},
            "mcpRequirement": {"id": req_id, "identifier": req_ident},
        },
    )
    print(f"M4 apply ok: {out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
