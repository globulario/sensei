#!/usr/bin/env bash
# setup.sh — install the Sensei binaries from this bundle onto your PATH.
#
# This script is platform-independent: it only places the binaries that ship
# next to it in ./bin (sensei, awareness-graph, awareness-mcp, oxigraph, and the
# deprecated awg alias). The binaries themselves are built for the platform named
# in the tarball (sensei-<os>-<arch>.tar.gz).
#
# Usage:
#   ./setup.sh                 # symlink into ~/.local/bin (no sudo)
#   ./setup.sh --prefix DIR    # symlink into DIR/ (e.g. /usr/local/bin)
#   ./setup.sh --copy          # copy instead of symlink
#   ./setup.sh --help
#
# Sensei locates awareness-graph and oxigraph next to the sensei executable, so
# keeping all five binaries in one directory is all that is required — this just
# makes `sensei` reachable on your PATH.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_SRC="${HERE}/bin"
PREFIX="${HOME}/.local/bin"
MODE="symlink"

# On Windows (Git Bash / MSYS) symlinks are unreliable — copy by default.
case "$(uname -s 2>/dev/null)" in MINGW*|MSYS*|CYGWIN*) MODE="copy" ;; esac

usage() { sed -n '2,20p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --prefix) PREFIX="${2:?--prefix needs a directory}"; shift 2 ;;
    --prefix=*) PREFIX="${1#*=}"; shift ;;
    --copy) MODE="copy"; shift ;;
    -h|--help) usage ;;
    *) echo "setup.sh: unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [[ ! -d "${BIN_SRC}" ]]; then
  echo "setup.sh: no bin/ directory next to this script (${BIN_SRC})" >&2
  exit 1
fi

mkdir -p "${PREFIX}"

echo "Installing Sensei binaries → ${PREFIX} (${MODE})"
for f in "${BIN_SRC}"/*; do
  name="$(basename "$f")"
  chmod +x "$f" 2>/dev/null || true
  dest="${PREFIX}/${name}"
  if [[ "${MODE}" == "copy" ]]; then
    cp -f "$f" "$dest"
  else
    ln -sf "$f" "$dest"
  fi
  echo "  ✓ ${name}"
done

# Sanity check: run the two binaries that matter most.
echo
if "${PREFIX}/sensei" version >/dev/null 2>&1; then
  echo "  sensei $("${PREFIX}/sensei" version 2>/dev/null)"
fi
if "${PREFIX}/oxigraph" --version >/dev/null 2>&1; then
  echo "  $("${PREFIX}/oxigraph" --version 2>/dev/null)"
fi

# PATH guidance.
case ":${PATH}:" in
  *":${PREFIX}:"*) echo; echo "✓ ${PREFIX} is already on your PATH — run: sensei" ;;
  *)
    echo
    echo "⚠ ${PREFIX} is not on your PATH. Add it:"
    echo "    export PATH=\"${PREFIX}:\$PATH\""
    echo "  (add that line to your ~/.bashrc or ~/.zshrc to make it permanent)"
    ;;
esac

echo
echo "Next: point your agent's MCP config at the bridge, e.g. (Claude Code, .mcp.json):"
echo '    { "mcpServers": { "sensei": {'
echo "        \"command\": \"${PREFIX}/awareness-mcp\","
echo '        "args": ["--awareness-addr", "localhost:10120"] } } }'
echo "Then: sensei serve -no-seed &   # starts the local store + server"
