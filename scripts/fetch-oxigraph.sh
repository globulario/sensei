#!/usr/bin/env bash
# fetch-oxigraph.sh — download the upstream Oxigraph binary for this platform
# into bin/oxigraph. No Docker. This is what makes `sensei serve` work standalone
# (it runs Oxigraph as a child process and looks for the binary next to awg,
# in ./bin/, or on PATH).
#
# Usage: scripts/fetch-oxigraph.sh [version]   (default: latest)
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${REPO_ROOT}/bin"
VERSION="${1:-}"

if [[ -x "${BIN_DIR}/oxigraph" ]]; then
  echo "oxigraph already present: ${BIN_DIR}/oxigraph"
  exit 0
fi

# Resolve latest version if not pinned.
if [[ -z "${VERSION}" ]]; then
  VERSION="$(curl -fsSL https://api.github.com/repos/oxigraph/oxigraph/releases/latest \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["tag_name"].lstrip("v"))')"
fi

os="$(uname -s)"; arch="$(uname -m)"
case "${os}/${arch}" in
  Linux/x86_64)        asset="oxigraph_v${VERSION}_x86_64_linux_gnu" ;;
  Linux/aarch64|Linux/arm64) asset="oxigraph_v${VERSION}_aarch64_linux_gnu" ;;
  Darwin/x86_64)       asset="oxigraph_v${VERSION}_x86_64_apple" ;;
  Darwin/arm64)        asset="oxigraph_v${VERSION}_aarch64_apple" ;;
  *)
    echo "ERROR: no prebuilt Oxigraph for ${os}/${arch}." >&2
    echo "  See https://github.com/oxigraph/oxigraph/releases — download manually into ${BIN_DIR}/oxigraph" >&2
    exit 1 ;;
esac

mkdir -p "${BIN_DIR}"
url="https://github.com/oxigraph/oxigraph/releases/download/v${VERSION}/${asset}"
echo "Fetching Oxigraph ${VERSION} (${os}/${arch})..."
echo "  ${url}"
curl -fsSL "${url}" -o "${BIN_DIR}/oxigraph"
chmod +x "${BIN_DIR}/oxigraph"
echo "  ✓ ${BIN_DIR}/oxigraph"
"${BIN_DIR}/oxigraph" --version 2>/dev/null || true
