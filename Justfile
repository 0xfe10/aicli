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
    @just _build-archive aicli linux amd64
    @just _build-archive aicli linux arm64
    @just _build-archive aicli darwin amd64
    @just _build-archive aicli darwin arm64
    @just _build-archive aicli windows amd64
    @just _build-archive aicli windows arm64
    @just _build-archive pingcode linux amd64
    @just _build-archive pingcode linux arm64
    @just _build-archive pingcode darwin amd64
    @just _build-archive pingcode darwin arm64
    @just _build-archive pingcode windows amd64
    @just _build-archive pingcode windows arm64
    @cd "{{ out_dir }}" && sha256sum ./*_"{{ version }}"_*.tar.gz ./*_"{{ version }}"_*.zip > checksums.txt
    @echo "release artifacts in {{ out_dir }}"
    @ls -lh "{{ out_dir }}"

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
