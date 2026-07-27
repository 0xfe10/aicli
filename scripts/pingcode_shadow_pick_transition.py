#!/usr/bin/env python3
"""Pick a legal transition target from project schema stateTransitions.

Usage:
  pingcode_shadow_pick_transition.py <work_item_or_get.json> <schema.json>

Prints bash-assignable lines:
  from_state='...'
  to_state='...'

Fails with a non-zero exit if no legal edge exists for the current state.
"""

from __future__ import annotations

import json
import sys


def state_name(value) -> str:
    if isinstance(value, dict):
        return str(value.get("name") or "")
    return str(value or "")


def extract_current_state(doc: dict) -> str:
    data = doc.get("data") if isinstance(doc.get("data"), dict) else None
    if data:
        for key in ("target", "created", "updated"):
            item = data.get(key)
            if isinstance(item, dict) and item.get("state") is not None:
                return state_name(item.get("state"))
    if doc.get("state") is not None:
        return state_name(doc.get("state"))
    return ""


def pick_to_state(schema_doc: dict, current: str) -> str:
    data = schema_doc.get("data") if isinstance(schema_doc.get("data"), dict) else schema_doc
    transitions = (data or {}).get("stateTransitions") or []
    for edge in transitions:
        if not isinstance(edge, dict):
            continue
        from_state = state_name(edge.get("fromState"))
        if from_state != current:
            continue
        for dest in edge.get("to") or []:
            name = state_name(dest)
            if name and name != current:
                return name
    return ""


def main() -> int:
    if len(sys.argv) != 3:
        print(
            "usage: pingcode_shadow_pick_transition.py <work_item_or_get.json> <schema.json>",
            file=sys.stderr,
        )
        return 64
    work = json.load(open(sys.argv[1], encoding="utf-8"))
    schema = json.load(open(sys.argv[2], encoding="utf-8"))
    current = extract_current_state(work)
    target = pick_to_state(schema, current)
    if not current or not target:
        print(
            f"no legal transition from {current!r} via schema stateTransitions",
            file=sys.stderr,
        )
        return 1
    print(f"from_state={current!r}")
    print(f"to_state={target!r}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
