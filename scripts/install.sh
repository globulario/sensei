#!/usr/bin/env bash
# install.sh — Build and set up Sensei for local development.
#
# Usage:
#   ./scripts/install.sh                       # build binaries + seed graph
#   ./scripts/install.sh --no-seed             # build only, skip Oxigraph seed
#   ./scripts/install.sh --user-services       # build + install supervised local user services
#   ./scripts/install.sh --no-seed --user-services
#   ./scripts/install.sh --help
#
# Prerequisites:
#   - Go1.25+ on PATH
#
# After install:
#   1. ./scripts/install-awg-user-services.sh   # supervised local runtime (recommended)
#   2. awareness-mcp                            # start the MCP bridge (stdio)
#   3. sensei briefing --file <path>               # query the graph

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BIN_DIR="$REPO_ROOT/bin"

NO_SEED=false
OXIGRAPH_URL="http://localhost:7878"
USER_SERVICES=false
OXIGRAPH_DATA_DIR="${OXIGRAPH_DATA_DIR:-$HOME/.local/share/sensei/oxigraph}"

usage() {
  cat <<'EOF'
Usage: ./scripts/install.sh [options]

Options:
  --no-seed         Skip Oxigraph seed (build binaries only)
  --user-services   Install/start supervised local systemd --user services after build
  --oxigraph-url    Oxigraph endpoint (default: http://localhost:7878)
  --help            Show this help

What it does:
  1. Builds sensei, awareness-mcp, and awareness-graph binaries → bin/
  2. Fetches the Oxigraph binary into bin/
  3. Seeds the graph from YAML sources using an existing or temporary local Oxigraph
  4. Optionally installs supervised local user services
  5. Prints connection info for Claude Code MCP config
EOF
  exit 0
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-seed) NO_SEED=true; shift ;;
    --user-services) USER_SERVICES=true; shift ;;
    --oxigraph-url) OXIGRAPH_URL="$2"; shift 2 ;;
    --help|-h) usage ;;
    *) echo "unknown option: $1"; exit 2 ;;
  esac
done

echo "Sensei install — $REPO_ROOT"
echo

# ── 1. Build binaries ────────────────────────────────────────────────────
echo "Building binaries..."
mkdir -p "$BIN_DIR"

go build -o "$BIN_DIR/sensei" "$REPO_ROOT/cmd/awg"
echo "  ✓ bin/sensei"

go build -o "$BIN_DIR/awareness-mcp" "$REPO_ROOT/cmd/awareness-mcp"
echo "  ✓ bin/awareness-mcp"

go build -o "$BIN_DIR/awareness-graph" "$REPO_ROOT/golang/server"
echo "  ✓ bin/awareness-graph"

echo

# ── 1b. Fetch the Oxigraph binary (no Docker needed) ─────────────────────
# `sensei serve` runs Oxigraph as a child process and looks for the binary in
# bin/. Fetching it here means the standalone path (`sensei serve -no-seed`)
# works out of the box. Best-effort: a failure is non-fatal — the Docker
# path below and the manual instructions remain.
if [ ! -x "$BIN_DIR/oxigraph" ]; then
  echo "Fetching Oxigraph binary (no Docker)..."
  "$REPO_ROOT/scripts/fetch-oxigraph.sh" || \
    echo "  ⚠ binary fetch failed — Docker fallback / manual install still available"
  echo
fi

# ── 1c. Helpers ──────────────────────────────────────────────────────────
check_oxigraph_health() {
  curl -sf "$OXIGRAPH_URL/query" \
    -d "query=ASK{}" \
    -H "Content-Type: application/sparql-query" >/dev/null 2>&1
}

TEMP_OXIGRAPH_PID=""
cleanup_temp_oxigraph() {
  if [[ -n "$TEMP_OXIGRAPH_PID" ]]; then
    kill "$TEMP_OXIGRAPH_PID" >/dev/null 2>&1 || true
    wait "$TEMP_OXIGRAPH_PID" >/dev/null 2>&1 || true
    TEMP_OXIGRAPH_PID=""
  fi
}
trap cleanup_temp_oxigraph EXIT

# ── 2. Ensure Oxigraph is running ────────────────────────────────────────
if [ "$NO_SEED" = "false" ]; then
  if check_oxigraph_health; then
    echo "Oxigraph: already running at $OXIGRAPH_URL"
  else
    echo "Starting temporary local Oxigraph for graph seed..."
    mkdir -p "$OXIGRAPH_DATA_DIR"
    "$BIN_DIR/oxigraph" serve --location "$OXIGRAPH_DATA_DIR" --bind 127.0.0.1:7878 >/tmp/awg-install-oxigraph.log 2>&1 &
    TEMP_OXIGRAPH_PID="$!"
    echo "  waiting for Oxigraph to start..."
    for i in $(seq 1 20); do
      if check_oxigraph_health; then
        break
      fi
      sleep 1
    done
    if ! check_oxigraph_health; then
      echo "  ✗ Oxigraph did not become ready."
      echo "    Check /tmp/awg-install-oxigraph.log for startup errors."
      exit 1
    fi
    echo "  ✓ Oxigraph running"
  fi
  echo

  # ── 3. Seed the graph ────────────────────────────────────────────────────
  if [ "$NO_SEED" = "false" ]; then
    echo "Seeding awareness graph..."

    # Find input directories
    SEED_ARGS=()
    if [ -d "$REPO_ROOT/docs/awareness" ]; then
      SEED_ARGS+=(--input "$REPO_ROOT/docs/awareness")
    fi
    # Check for sibling services repo
    SERVICES_DIR="$(dirname "$REPO_ROOT")/services"
    if [ -d "$SERVICES_DIR/docs/awareness" ]; then
      SEED_ARGS+=(--input "$SERVICES_DIR/docs/awareness")
    fi

    if [ ${#SEED_ARGS[@]} -eq 0 ]; then
      echo "  ⚠ No docs/awareness directories found — skipping seed"
    else
      "$BIN_DIR/sensei" build "${SEED_ARGS[@]}" --store-url "$OXIGRAPH_URL/store?default"
      echo "  ✓ Graph seeded"
    fi
    echo
  fi
fi

# ── 4. Install supervised user services (optional) ───────────────────────
if [ "$USER_SERVICES" = "true" ]; then
  cleanup_temp_oxigraph
  echo "Installing supervised local user services..."
  bash "$REPO_ROOT/scripts/install-awg-user-services.sh"
  echo
fi

# ── 5. Print connection info ─────────────────────────────────────────────
cat <<EOF
Done! Sensei is ready.

Recommended local runtime:
  bash $REPO_ROOT/scripts/install-awg-user-services.sh

Ad hoc local runtime:
  $BIN_DIR/awareness-graph --addr :10120 --oxigraph-url $OXIGRAPH_URL/query

Or use the CLI directly:
  $BIN_DIR/sensei briefing --file <path>
  $BIN_DIR/sensei validate
  $BIN_DIR/sensei audit

Claude Code MCP config (add to .mcp.json):
  {
    "mcpServers": {
      "sensei": {
        "command": "$BIN_DIR/awareness-mcp",
        "args": ["--awareness-addr", "localhost:10120"]
      }
    }
  }

Tool names after MCP registration:
  mcp__sensei__awareness_briefing
  mcp__sensei__awareness_impact
  mcp__sensei__awareness_resolve
EOF
