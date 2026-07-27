set shell := ["bash", "-eo", "pipefail", "-c"]

version := env_var_or_default("VERSION", "0.1.0")
commit := env_var_or_default("COMMIT", `git rev-parse --short=12 HEAD 2>/dev/null || echo unknown`)
out_dir := env_var_or_default("OUT_DIR", "dist")

default:
    @just --list

verify:
    #!/usr/bin/env bash
    set -euo pipefail
    umask 0022
    go mod verify
    go test ./...
    go test -race ./...
    go vet ./...

build-pingcode: _validate-version
    @just _build-pingcode linux amd64
    @just _build-pingcode linux arm64

release-build: _validate-version
    @just build-archive aicli linux amd64
    @just build-archive aicli linux arm64
    @just build-archive aicli darwin amd64
    @just build-archive aicli darwin arm64
    @just build-archive aicli windows amd64
    @just build-archive aicli windows arm64
    @just build-archive pingcode linux amd64
    @just build-archive pingcode linux arm64
    @just build-archive pingcode darwin amd64
    @just build-archive pingcode darwin arm64
    @just build-archive pingcode windows amd64
    @just build-archive pingcode windows arm64
    @just release-checksums
    @echo "release artifacts in {{ out_dir }}"
    @ls -lh "{{ out_dir }}"

build-archive command goos goarch: _validate-version
    @just _build-archive "{{ command }}" "{{ goos }}" "{{ goarch }}"

release-checksums: _validate-version
    #!/usr/bin/env bash
    set -euo pipefail
    cd "{{ out_dir }}"
    archive_count="$(find . -maxdepth 1 -type f \( -name '*_{{ version }}_*.tar.gz' -o -name '*_{{ version }}_*.zip' \) | wc -l)"
    if [[ "$archive_count" -ne 12 ]]; then
      echo "expected 12 release archives, found ${archive_count}" >&2
      exit 1
    fi
    sha256sum ./*_"{{ version }}"_*.tar.gz ./*_"{{ version }}"_*.zip > checksums.txt

_validate-version:
    @if [[ ! "{{ version }}" =~ ^[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]]; then echo "VERSION must be a semantic version without the leading v: {{ version }}" >&2; exit 64; fi

_build-pingcode goos goarch:
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p "{{ out_dir }}"
    name="pingcode-{{ version }}-{{ goos }}-{{ goarch }}"
    echo "building ${name}"
    CGO_ENABLED=0 GOOS="{{ goos }}" GOARCH="{{ goarch }}" \
      go build -trimpath -buildvcs=false \
      -ldflags "-s -w -buildid= -X main.version={{ version }} -X main.commit={{ commit }}" \
      -o "{{ out_dir }}/${name}" ./cmd/pingcode
    (
      cd "{{ out_dir }}"
      sha256sum "${name}" > "${name}.sha256"
    )

_build-archive command goos goarch:
    #!/usr/bin/env bash
    set -euo pipefail
    command="{{ command }}"
    goos="{{ goos }}"
    goarch="{{ goarch }}"
    binary="$command"
    archive_base="${command}_{{ version }}_${goos}_${goarch}"
    ldflags="-s -w -buildid="
    mkdir -p "{{ out_dir }}"
    output_dir="$(cd "{{ out_dir }}" && pwd)"

    if [[ "$goos" == "windows" ]]; then
      binary="${binary}.exe"
    fi
    if [[ "$command" == "pingcode" ]]; then
      ldflags+=" -X main.version={{ version }} -X main.commit={{ commit }}"
    fi

    stage="$(mktemp -d)"
    echo "building ${archive_base}"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      go build -trimpath -buildvcs=false -ldflags "$ldflags" \
      -o "${stage}/${binary}" "./cmd/${command}"

    cp LICENSE THIRD_PARTY_NOTICES.md "$stage/"
    chmod 0755 "${stage}/${binary}"
    chmod 0644 "${stage}/LICENSE" "${stage}/THIRD_PARTY_NOTICES.md"

    if [[ "$goos" == "windows" ]]; then
      (
        cd "$stage"
        zip -q "${output_dir}/${archive_base}.zip" "$binary" LICENSE THIRD_PARTY_NOTICES.md
      )
    else
      tar -C "$stage" -czf "${output_dir}/${archive_base}.tar.gz" \
        "$binary" LICENSE THIRD_PARTY_NOTICES.md
    fi
    rm -rf "$stage"

# --- PingCode shadow / release verification (E2E orchestration) ---
# Requires an absolute path to an extracted Release binary via PINGCODE_BIN.
# Results go under dist/shadow/<runId>/ (gitignored). Never logs credentials.
#
# Optional env for dry-run field ops / apply / release-verify:
#   PINGCODE_SHADOW_WORK_ITEM_ID or PINGCODE_SHADOW_WORK_ITEM_IDENTIFIER
#   PINGCODE_E2E_APPLY=1 (dedicated test project only)
#   PINGCODE_ARCHIVE / PINGCODE_CHECKSUMS / PINGCODE_EXPECT_VERSION / PINGCODE_EXPECT_COMMIT
#   MCP_ORACLE_DIGEST / MCP_ORACLE_COMMIT (mcp-compare still gated until oracle is pinned)

pingcode_bin := env_var_or_default("PINGCODE_BIN", "")
shadow_root := env_var_or_default("SHADOW_ROOT", "dist/shadow")
e2e_apply := env_var_or_default("PINGCODE_E2E_APPLY", "0")

_pingcode-require-bin:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ -z "{{ pingcode_bin }}" ]]; then
      echo "PINGCODE_BIN must be an absolute path to an extracted Release pingcode binary" >&2
      exit 64
    fi
    if [[ "{{ pingcode_bin }}" != /* ]]; then
      echo "PINGCODE_BIN must be absolute: {{ pingcode_bin }}" >&2
      exit 64
    fi
    if [[ ! -x "{{ pingcode_bin }}" ]]; then
      echo "PINGCODE_BIN is not executable: {{ pingcode_bin }}" >&2
      exit 64
    fi

pingcode-shadow-preflight: _pingcode-require-bin
    #!/usr/bin/env bash
    set -euo pipefail
    run_id="${PINGCODE_SHADOW_RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)-$$}"
    out="{{ shadow_root }}/${run_id}"
    mkdir -p "$out"
    echo "$run_id" > "$out/runId.txt"
    "{{ pingcode_bin }}" version | tee "$out/version.json"
    "{{ pingcode_bin }}" config check | tee "$out/config-check.json"
    "{{ pingcode_bin }}" auth status | tee "$out/auth-status.json"
    "{{ pingcode_bin }}" work-item create --help | tee "$out/help-create.txt" >/dev/null
    python3 -c 'import json,sys; doc=json.load(open(sys.argv[1])); assert doc.get("ok") is True, doc; print("preflight ok")' "$out/version.json"
    echo "SHADOW_RUN_DIR=$out"

pingcode-shadow-read: _pingcode-require-bin
    #!/usr/bin/env bash
    set -euo pipefail
    run_id="${PINGCODE_SHADOW_RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)-$$}"
    out="{{ shadow_root }}/${run_id}"
    mkdir -p "$out"
    "{{ pingcode_bin }}" project list | tee "$out/project-list.json"
    "{{ pingcode_bin }}" project schema --kind bug | tee "$out/schema-bug.json"
    "{{ pingcode_bin }}" project schema --kind requirement | tee "$out/schema-requirement.json"
    "{{ pingcode_bin }}" work-item search --kinds bug,requirement --page-size 5 | tee "$out/search.json"
    echo "SHADOW_RUN_DIR=$out"

pingcode-shadow-dry-run: _pingcode-require-bin
    #!/usr/bin/env bash
    set -euo pipefail
    run_id="${PINGCODE_SHADOW_RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)-$$}"
    out="{{ shadow_root }}/${run_id}"
    mkdir -p "$out"
    title="aicli-shadow-${run_id}"
    printf '%s\n' "{\"kind\":\"bug\",\"title\":\"${title}\"}" \
      | "{{ pingcode_bin }}" work-item create --input - | tee "$out/create-dryrun.json"
    python3 -c 'import json,sys; doc=json.load(open(sys.argv[1])); d=doc.get("data") or {}; assert doc.get("ok") is True and d.get("dryRun") is True, doc; assert "created" not in d, doc; print("dry-run create ok")' "$out/create-dryrun.json"

    # Resolve a disposable target for update/transition/comment dry-runs.
    wi_id="${PINGCODE_SHADOW_WORK_ITEM_ID:-}"
    wi_ident="${PINGCODE_SHADOW_WORK_ITEM_IDENTIFIER:-}"
    if [[ -z "$wi_id" && -z "$wi_ident" ]]; then
      "{{ pingcode_bin }}" work-item search --kinds bug --page-size 1 | tee "$out/search-for-dryrun.json"
      eval "$(python3 -c 'import json,sys; doc=json.load(open(sys.argv[1])); vals=((doc.get("data") or {}).get("values") or []); assert vals, "no work item available for dry-run field ops - set PINGCODE_SHADOW_WORK_ITEM_ID"; v=vals[0]; print("wi_id=%r" % (v.get("id") or "")); print("wi_ident=%r" % (v.get("identifier") or ""))' "$out/search-for-dryrun.json")"
    fi
    get_args=(work-item get --kind bug --include-comments)
    if [[ -n "$wi_id" ]]; then get_args+=(--id "$wi_id"); else get_args+=(--identifier "$wi_ident"); fi
    "{{ pingcode_bin }}" "${get_args[@]}" | tee "$out/before-dryrun-get.json"
    python3 -c 'import json,sys; json.dump((json.load(open(sys.argv[1])).get("data") or {}).get("target"), open(sys.argv[2],"w"), sort_keys=True)' "$out/before-dryrun-get.json" "$out/before-target.json"

    payload_base="\"kind\":\"bug\""
    if [[ -n "$wi_id" ]]; then payload_base+=",\"workItemId\":\"${wi_id}\""; else payload_base+=",\"identifier\":\"${wi_ident}\""; fi
    printf '%s\n' "{${payload_base},\"title\":\"aicli-shadow-dryrun-update-${run_id}\"}" \
      | "{{ pingcode_bin }}" work-item update --input - | tee "$out/update-dryrun.json"
    python3 -c 'import json,sys; doc=json.load(open(sys.argv[1])); d=doc.get("data") or {}; assert doc.get("ok") is True and d.get("dryRun") is True, doc; print("dry-run update ok")' "$out/update-dryrun.json"

    "{{ pingcode_bin }}" project schema --kind bug | tee "$out/schema-for-dryrun.json"
    eval "$(python3 "{{ justfile_directory() }}/scripts/pingcode_shadow_pick_transition.py" "$out/before-dryrun-get.json" "$out/schema-for-dryrun.json")"
    printf '%s\n' "{${payload_base},\"statusName\":\"${to_state}\",\"expectedCurrentState\":\"${from_state}\"}" \
      | "{{ pingcode_bin }}" work-item transition --input - | tee "$out/transition-dryrun.json"
    python3 -c 'import json,sys; doc=json.load(open(sys.argv[1])); d=doc.get("data") or {}; assert doc.get("ok") is True and d.get("dryRun") is True, doc; print("dry-run transition ok", "from=%s" % sys.argv[2], "to=%s" % sys.argv[3])' "$out/transition-dryrun.json" "$from_state" "$to_state"

    printf '%s\n' "{${payload_base},\"content\":\"aicli-shadow-dryrun-comment-${run_id}\"}" \
      | "{{ pingcode_bin }}" work-item comment --input - | tee "$out/comment-dryrun.json"
    python3 -c 'import json,sys; doc=json.load(open(sys.argv[1])); d=doc.get("data") or {}; assert doc.get("ok") is True and d.get("dryRun") is True, doc; print("dry-run comment ok")' "$out/comment-dryrun.json"

    "{{ pingcode_bin }}" "${get_args[@]}" | tee "$out/after-dryrun-get.json"
    python3 -c 'import json,sys; before=json.load(open(sys.argv[1])); after=(json.load(open(sys.argv[2])).get("data") or {}).get("target"); assert before==after, ("dry-run mutated remote work item", before, after); print("dry-run zero-write compare ok")' "$out/before-target.json" "$out/after-dryrun-get.json"
    echo "SHADOW_RUN_DIR=$out"

pingcode-shadow-apply: _pingcode-require-bin
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ "{{ e2e_apply }}" != "1" ]]; then
      echo "Refusing apply: set PINGCODE_E2E_APPLY=1 and use a dedicated test project" >&2
      exit 64
    fi
    if [[ -z "${PINGCODE_PROJECT_IDENTIFIER:-}" && -z "${PINGCODE_PROJECT_ID:-}" ]]; then
      echo "Dedicated PINGCODE_PROJECT_IDENTIFIER or PINGCODE_PROJECT_ID required for apply" >&2
      exit 64
    fi
    run_id="${PINGCODE_SHADOW_RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)-$$}"
    out="{{ shadow_root }}/${run_id}"
    mkdir -p "$out"
    title="aicli-shadow-apply-${run_id}"
    printf '%s\n' "{\"kind\":\"bug\",\"title\":\"${title}\",\"description\":\"shadow apply runId=${run_id}\"}" \
      | "{{ pingcode_bin }}" work-item create --input - --apply | tee "$out/create-apply.json"
    python3 -c 'import json,sys; doc=json.load(open(sys.argv[1])); assert doc.get("ok") is True, doc; created=(doc.get("data") or {}).get("created") or {}; assert created.get("id"), created; state=created.get("state"); state_name=state.get("name") if isinstance(state, dict) else state; json.dump({"id":created.get("id"),"identifier":created.get("identifier"),"title":created.get("title"),"state":state_name}, open(sys.argv[2],"w"), ensure_ascii=False, indent=2); print("created", created.get("identifier"), created.get("id"))' "$out/create-apply.json" "$out/created.json"
    eval "$(python3 -c 'import json,sys; c=json.load(open(sys.argv[1])); print("wi_id=%r" % c["id"]); print("wi_ident=%r" % (c.get("identifier") or "")); print("from_state=%r" % (c.get("state") or ""))' "$out/created.json")"
    "{{ pingcode_bin }}" work-item get --kind bug --id "$wi_id" --include-comments | tee "$out/get-after-create.json"

    printf '%s\n' "{\"kind\":\"bug\",\"workItemId\":\"${wi_id}\",\"title\":\"${title}-updated\",\"description\":\"shadow apply update runId=${run_id}\"}" \
      | "{{ pingcode_bin }}" work-item update --input - --apply | tee "$out/update-apply.json"
    python3 -c 'import json,sys; doc=json.load(open(sys.argv[1])); assert doc.get("ok") is True, doc; print("update apply ok")' "$out/update-apply.json"
    "{{ pingcode_bin }}" work-item get --kind bug --id "$wi_id" | tee "$out/get-after-update.json"

    "{{ pingcode_bin }}" project schema --kind bug | tee "$out/schema-for-apply.json"
    eval "$(python3 "{{ justfile_directory() }}/scripts/pingcode_shadow_pick_transition.py" "$out/get-after-update.json" "$out/schema-for-apply.json")"
    printf '%s\n' "{\"kind\":\"bug\",\"workItemId\":\"${wi_id}\",\"statusName\":\"${to_state}\",\"expectedCurrentState\":\"${from_state}\"}" \
      | "{{ pingcode_bin }}" work-item transition --input - --apply | tee "$out/transition-apply.json"
    python3 -c 'import json,sys; doc=json.load(open(sys.argv[1])); assert doc.get("ok") is True, doc; print("transition apply ok", "from=%s" % sys.argv[2], "to=%s" % sys.argv[3])' "$out/transition-apply.json" "$from_state" "$to_state"
    "{{ pingcode_bin }}" work-item get --kind bug --id "$wi_id" | tee "$out/get-after-transition.json"

    printf '%s\n' "{\"kind\":\"bug\",\"workItemId\":\"${wi_id}\",\"content\":\"shadow apply comment runId=${run_id}\"}" \
      | "{{ pingcode_bin }}" work-item comment --input - --apply | tee "$out/comment-apply.json"
    python3 -c 'import json,sys; doc=json.load(open(sys.argv[1])); assert doc.get("ok") is True, doc; print("comment apply ok")' "$out/comment-apply.json"
    "{{ pingcode_bin }}" work-item get --kind bug --id "$wi_id" --include-comments | tee "$out/get-after-comment.json"
    python3 -c 'import json,sys; doc=json.load(open(sys.argv[1])); target=(doc.get("data") or {}).get("target") or {}; assert target.get("id") or target.get("identifier"), target; comments=((doc.get("data") or {}).get("comments") or {}); vals=comments.get("values") or []; assert isinstance(vals, list), comments; print("final get ok id=", target.get("identifier") or target.get("id"), "comments=", len(vals), "truncated=", comments.get("truncated"))' "$out/get-after-comment.json"
    echo "SHADOW_RUN_DIR=$out"
    echo "NOTE: do not blindly retry non-idempotent writes; reconcile by runId/title first"
    echo "NOTE: confirm the item in PingCode UI before cleanup"

pingcode-shadow-mcp-compare:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ -z "${MCP_ORACLE_DIGEST:-}" || -z "${MCP_ORACLE_COMMIT:-}" ]]; then
      echo "MCP compare requires pinned MCP_ORACLE_DIGEST and MCP_ORACLE_COMMIT" >&2
      echo "Plus local compare artifacts under dist/shadow/<runId>/ once the oracle is fixed." >&2
      exit 64
    fi
    echo "MCP oracle digest=${MCP_ORACLE_DIGEST} commit=${MCP_ORACLE_COMMIT}" >&2
    echo "Oracle twin compare not implemented yet for this digest; refusing rather than claiming parity." >&2
    exit 64

pingcode-release-verify: _pingcode-require-bin
    #!/usr/bin/env bash
    set -euo pipefail
    out="{{ shadow_root }}/release-verify-$(date -u +%Y%m%dT%H%M%SZ)"
    mkdir -p "$out"
    archive="${PINGCODE_ARCHIVE:-}"
    checksums="${PINGCODE_CHECKSUMS:-}"

    if [[ -n "$archive" ]]; then
      if [[ ! -f "$archive" ]]; then
        echo "PINGCODE_ARCHIVE not found: $archive" >&2
        exit 64
      fi
      cp -f "$archive" "$out/"
      echo "archive=$(basename "$archive")" | tee "$out/archive.txt"
    else
      echo "WARN: PINGCODE_ARCHIVE unset; skipping archive presence check" | tee "$out/archive-skipped.txt"
    fi

    if [[ -n "$checksums" ]]; then
      if [[ ! -f "$checksums" ]]; then
        echo "PINGCODE_CHECKSUMS not found: $checksums" >&2
        exit 64
      fi
      if [[ -z "$archive" ]]; then
        echo "PINGCODE_CHECKSUMS set but PINGCODE_ARCHIVE is empty" >&2
        exit 64
      fi
      archive_base="$(basename "$archive")"
      line="$(grep -F "$archive_base" "$checksums" || true)"
      if [[ -z "$line" ]]; then
        echo "checksum line for $archive_base not found in $checksums" >&2
        exit 64
      fi
      (
        cd "$(dirname "$archive")"
        echo "$line" | sha256sum -c -
      ) | tee "$out/checksum-verify.txt"
    else
      echo "WARN: PINGCODE_CHECKSUMS unset; skipping checksum verification" | tee "$out/checksum-skipped.txt"
    fi

    "{{ pingcode_bin }}" version | tee "$out/version.json"
    "{{ pingcode_bin }}" config check | tee "$out/config-check.json"
    "{{ pingcode_bin }}" raw --help | tee "$out/raw-help.txt" >/dev/null
    PINGCODE_EXPECT_VERSION="${PINGCODE_EXPECT_VERSION:-}" PINGCODE_EXPECT_COMMIT="${PINGCODE_EXPECT_COMMIT:-}" \
      python3 -c 'import json,sys,os; doc=json.load(open(sys.argv[1])); data=doc.get("data") or {}; assert doc.get("ok") is True, doc; cli=data.get("cli") or ""; commit=data.get("commit") or ""; assert cli, data; assert commit and commit != "unknown", data; expect_v=os.environ.get("PINGCODE_EXPECT_VERSION") or ""; expect_c=os.environ.get("PINGCODE_EXPECT_COMMIT") or ""; assert not expect_v or cli == expect_v, "version mismatch got=%s want=%s" % (cli, expect_v); assert not expect_c or commit == expect_c or commit.startswith(expect_c) or expect_c.startswith(commit), "commit mismatch got=%s want=%s" % (commit, expect_c); print("release-verify ok", cli, commit)' "$out/version.json"
    echo "RELEASE_VERIFY_DIR=$out"
