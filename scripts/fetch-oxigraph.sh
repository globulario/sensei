#!/usr/bin/env bash
# fetch-oxigraph.sh — download the upstream Oxigraph binary for this platform
# into bin/oxigraph. No Docker. This is what makes `sensei serve` work standalone
# (it runs Oxigraph as a child process and looks for the binary next to awg,
# in ./bin/, or on PATH).
#
# Usage: scripts/fetch-oxigraph.sh [version]
#        OXIGRAPH_VERSION=0.5.9 scripts/fetch-oxigraph.sh
#
# With no explicit version, the script follows GitHub's public "latest release"
# redirect. It deliberately avoids the rate-limited Releases JSON API.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${REPO_ROOT}/bin"
VERSION="${1:-${OXIGRAPH_VERSION:-}}"

if [[ -x "${BIN_DIR}/oxigraph" ]]; then
  echo "oxigraph already present: ${BIN_DIR}/oxigraph"
  exit 0
fi

# Resolve latest without consuming the unauthenticated GitHub REST API quota.
if [[ -z "${VERSION}" ]]; then
  latest_endpoint="https://github.com/oxigraph/oxigraph/releases/latest"
  if ! effective_url="$(curl --fail --silent --show-error --location \
    --retry 3 --retry-delay 1 --retry-all-errors \
    --connect-timeout 15 --max-time 60 \
    --output /dev/null --write-out '%{url_effective}' \
    "${latest_endpoint}")"; then
    echo "ERROR: unable to resolve the latest Oxigraph release from ${latest_endpoint}" >&2
    exit 1
  fi

  tag="${effective_url##*/}"
  if [[ "${tag}" != v* || "${tag}" == "v" ]]; then
    echo "ERROR: latest Oxigraph release redirected to an unexpected URL: ${effective_url}" >&2
    exit 1
  fi
  VERSION="${tag#v}"
fi

# Accept either 0.5.9 or v0.5.9 from callers, but reject unsafe URL fragments.
VERSION="${VERSION#v}"
if [[ ! "${VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.]+)?$ ]]; then
  echo "ERROR: invalid Oxigraph version: ${VERSION}" >&2
  exit 1
fi

os="$(uname -s)"
arch="$(uname -m)"
case "${os}/${arch}" in
  Linux/x86_64)              asset="oxigraph_v${VERSION}_x86_64_linux_gnu" ;;
  Linux/aarch64|Linux/arm64) asset="oxigraph_v${VERSION}_aarch64_linux_gnu" ;;
  Darwin/x86_64)             asset="oxigraph_v${VERSION}_x86_64_apple" ;;
  Darwin/arm64)              asset="oxigraph_v${VERSION}_aarch64_apple" ;;
  *)
    echo "ERROR: no prebuilt Oxigraph for ${os}/${arch}." >&2
    echo "  See https://github.com/oxigraph/oxigraph/releases — download manually into ${BIN_DIR}/oxigraph" >&2
    exit 1 ;;
esac

mkdir -p "${BIN_DIR}"
url="https://github.com/oxigraph/oxigraph/releases/download/v${VERSION}/${asset}"
tmp="$(mktemp "${BIN_DIR}/.oxigraph.XXXXXX")"
trap 'rm -f "${tmp}"' EXIT

echo "Fetching Oxigraph ${VERSION} (${os}/${arch})..."
echo "  ${url}"
curl --fail --silent --show-error --location \
  --retry 3 --retry-delay 1 --retry-all-errors \
  --connect-timeout 15 --max-time 180 \
  "${url}" -o "${tmp}"

if [[ ! -s "${tmp}" ]]; then
  echo "ERROR: downloaded Oxigraph asset is empty: ${url}" >&2
  exit 1
fi
chmod +x "${tmp}"
if ! version_output="$("${tmp}" --version 2>&1)"; then
  echo "ERROR: downloaded Oxigraph asset is not executable: ${url}" >&2
  echo "${version_output}" >&2
  exit 1
fi

mv "${tmp}" "${BIN_DIR}/oxigraph"
trap - EXIT

echo "  ✓ ${BIN_DIR}/oxigraph"
echo "${version_output}"
