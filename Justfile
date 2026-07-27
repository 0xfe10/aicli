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

# --- PingCode shadow / release verification (E2E orchestration) ---
# Requires an absolute path to an extracted Release binary via PINGCODE_BIN.
# Results go under dist/shadow/<runId>/ (gitignored). Never logs credentials.

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
    python3 - <<'PY' "$out/version.json"
import json,sys
doc=json.load(open(sys.argv[1]))
assert doc.get("ok") is True, doc
print("preflight ok runId dir ready")
PY
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
    python3 - <<'PY' "$out/create-dryrun.json"
import json,sys
doc=json.load(open(sys.argv[1]))
assert doc.get("ok") is True and (doc.get("data") or {}).get("dryRun") is True, doc
assert "created" not in (doc.get("data") or {}), doc
print("dry-run create ok")
PY
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
    python3 - <<'PY' "$out/create-apply.json" "$out/created.json"
import json,sys
doc=json.load(open(sys.argv[1]))
assert doc.get("ok") is True, doc
created=(doc.get("data") or {}).get("created") or {}
assert created.get("id"), created
json.dump({"id":created.get("id"),"identifier":created.get("identifier"),"title":created.get("title")}, open(sys.argv[2],"w"), ensure_ascii=False, indent=2)
print("created", created.get("identifier"), created.get("id"))
PY
    echo "SHADOW_RUN_DIR=$out"
    echo "NOTE: do not blindly retry non-idempotent writes; reconcile by runId/title first"

pingcode-shadow-mcp-compare:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "MCP compare requires a pinned MCP image digest/commit and local compare artifacts under dist/shadow/<runId>/" >&2
    echo "Set MCP_ORACLE_DIGEST / MCP_ORACLE_COMMIT, then extend this recipe once the oracle is fixed." >&2
    exit 64

pingcode-release-verify: _pingcode-require-bin
    #!/usr/bin/env bash
    set -euo pipefail
    out="{{ shadow_root }}/release-verify-$(date -u +%Y%m%dT%H%M%SZ)"
    mkdir -p "$out"
    "{{ pingcode_bin }}" version | tee "$out/version.json"
    "{{ pingcode_bin }}" config check | tee "$out/config-check.json"
    "{{ pingcode_bin }}" raw --help | tee "$out/raw-help.txt" >/dev/null
    python3 - <<'PY' "$out/version.json"
import json,sys
doc=json.load(open(sys.argv[1]))
data=doc.get("data") or {}
assert doc.get("ok") is True, doc
assert data.get("cli"), data
assert data.get("commit"), data
print("release-verify ok", data.get("cli"), data.get("commit"))
PY
    echo "RELEASE_VERIFY_DIR=$out"
