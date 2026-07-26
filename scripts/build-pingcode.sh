# scripts/build-pingcode.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-0.1.0}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
OUT_DIR="${OUT_DIR:-$ROOT/dist}"
mkdir -p "$OUT_DIR"

LDFLAGS="-X main.version=${VERSION} -X main.commit=${COMMIT}"

build_one() {
  local goos="$1"
  local goarch="$2"
  local name="pingcode-${VERSION}-${goos}-${goarch}"
  echo "building ${name}"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "$LDFLAGS" -o "${OUT_DIR}/${name}" ./cmd/pingcode
  (
    cd "$OUT_DIR"
    sha256sum "${name}" > "${name}.sha256"
  )
}

build_one linux amd64
build_one linux arm64

echo "artifacts in ${OUT_DIR}"
ls -lh "$OUT_DIR"
