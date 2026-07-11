#!/bin/sh
# install.sh — one-line installer for prebuilt Sensei binaries (Linux/macOS).
#
#   curl -fsSL https://raw.githubusercontent.com/globulario/sensei/main/install.sh | sh
#
# Detects your platform, downloads the matching self-contained release tarball
# (sensei + awareness-graph + awareness-mcp + oxigraph, no Go toolchain, no
# Docker), verifies its checksum, and installs the binaries onto your PATH.
#
# Options (environment variables):
#   SENSEI_VERSION=v1.1.0             pin a release (default: latest)
#   SENSEI_PREFIX=/custom/bin         install dir (default: ~/.local/bin)
#
# POSIX sh only (this runs piped into `sh`) — no bashisms.
set -eu

REPO="globulario/sensei"
VERSION="${SENSEI_VERSION:-latest}"
PREFIX="${SENSEI_PREFIX:-${HOME}/.local/bin}"

say()  { printf '%s\n' "$*"; }
die()  { printf 'install.sh: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

have curl || have wget || die "need curl or wget"
have tar || die "need tar"

# ── Detect platform ─────────────────────────────────────────────────────────
os="$(uname -s)"
arch="$(uname -m)"
case "$os" in
  Linux)  OS=linux ;;
  Darwin) OS=darwin ;;
  MINGW*|MSYS*|CYGWIN*) die "on Windows use install.ps1 (irm .../install.ps1 | iex)" ;;
  *) die "unsupported OS: $os" ;;
esac
case "$arch" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported architecture: $arch" ;;
esac
PLATFORM="${OS}-${ARCH}"

# macOS ships as Apple Silicon (arm64) only.
if [ "$OS" = darwin ] && [ "$ARCH" != arm64 ]; then
  die "Sensei ships macOS as Apple Silicon (arm64) only; Intel Macs must build from source (see README)"
fi

TARBALL="sensei-${PLATFORM}.tar.gz"
if [ "$VERSION" = latest ]; then
  BASE="https://github.com/${REPO}/releases/latest/download"
else
  BASE="https://github.com/${REPO}/releases/download/${VERSION}"
fi

# ── Download (+ verify) into a temp workspace ───────────────────────────────
TMP="$(mktemp -d 2>/dev/null || mktemp -d -t sensei)"
trap 'rm -rf "$TMP"' EXIT INT TERM

fetch() { # fetch <url> <dest>
  if have curl; then curl -fsSL -o "$2" "$1"; else wget -qO "$2" "$1"; fi
}

say "Installing Sensei (${VERSION}, ${PLATFORM})"
say "  ↓ ${BASE}/${TARBALL}"
fetch "${BASE}/${TARBALL}" "${TMP}/${TARBALL}" || die "download failed — is ${VERSION} a real release for ${PLATFORM}?"

if fetch "${BASE}/${TARBALL}.sha256" "${TMP}/${TARBALL}.sha256" 2>/dev/null; then
  if have sha256sum;  then ( cd "$TMP" && sha256sum -c "${TARBALL}.sha256" >/dev/null ) || die "checksum mismatch"
  elif have shasum;   then ( cd "$TMP" && shasum -a 256 -c "${TARBALL}.sha256" >/dev/null ) || die "checksum mismatch"
  fi
  say "  ✓ checksum verified"
fi

tar xzf "${TMP}/${TARBALL}" -C "$TMP"
SRC="${TMP}/sensei-${PLATFORM}/bin"
[ -d "$SRC" ] || die "unexpected tarball layout (no bin/)"

# ── Install (copy — the temp dir is removed on exit) ────────────────────────
mkdir -p "$PREFIX"
for f in "$SRC"/*; do
  name="$(basename "$f")"
  cp -f "$f" "${PREFIX}/${name}"
  chmod +x "${PREFIX}/${name}" 2>/dev/null || true
  say "  ✓ ${name}"
done

# ── Report ──────────────────────────────────────────────────────────────────
say ""
if "${PREFIX}/sensei" version >/dev/null 2>&1; then
  say "Installed sensei $("${PREFIX}/sensei" version 2>/dev/null) → ${PREFIX}"
else
  say "Installed → ${PREFIX}"
fi

case ":${PATH}:" in
  *":${PREFIX}:"*) say "✓ ${PREFIX} is on your PATH — run: sensei" ;;
  *)
    say ""
    say "⚠ ${PREFIX} is not on your PATH. Add it, then restart your shell:"
    say "    export PATH=\"${PREFIX}:\$PATH\"   # add to ~/.bashrc or ~/.zshrc"
    ;;
esac

say ""
say "Wire your agent up (from your repo):  sensei init --mcp"
say "  → writes CLAUDE.md / AGENTS.md / .cursor rule + the MCP server into .mcp.json."
say "Or add the MCP tools by hand (Claude Code, .mcp.json at your repo root):"
say '    { "mcpServers": { "sensei": {'
say "        \"command\": \"${PREFIX}/awareness-mcp\","
say '        "args": ["--awareness-addr", "localhost:10120"] } } }'
say "Then:  sensei serve -no-seed &   # start the local store + server"
