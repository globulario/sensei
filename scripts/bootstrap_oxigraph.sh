#!/usr/bin/env bash
# bootstrap_oxigraph.sh — one-shot helper to start Oxigraph for local dev.
#
# This script starts the native Oxigraph binary in the background. It prefers
# ./bin/oxigraph from this repo and falls back to an oxigraph binary on PATH.
#
# Usage:
#   ./scripts/bootstrap_oxigraph.sh [--data-dir <path>] [--port <port>]
#
# Defaults:
#   data dir: $HOME/.local/share/awareness-graph/oxigraph
#   port:     7878 (Oxigraph default)
#   binary:   ./bin/oxigraph, then PATH

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DATA_DIR="${HOME}/.local/share/awareness-graph/oxigraph"
PORT="7878"
PID_FILE="${TMPDIR:-/tmp}/awareness-graph-oxigraph.pid"
LOG_FILE="${TMPDIR:-/tmp}/awareness-graph-oxigraph.log"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --data-dir) DATA_DIR="$2"; shift 2 ;;
    --port)     PORT="$2";     shift 2 ;;
    -h|--help)
      grep '^# ' "$0" | sed 's/^# //'
      exit 0
      ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

mkdir -p "$DATA_DIR"

if [[ -x "$REPO_ROOT/bin/oxigraph" ]]; then
  OXIGRAPH_BIN="$REPO_ROOT/bin/oxigraph"
elif command -v oxigraph >/dev/null 2>&1; then
  OXIGRAPH_BIN="$(command -v oxigraph)"
else
  echo "oxigraph binary not found." >&2
  echo "Run ./scripts/fetch-oxigraph.sh or ./scripts/install.sh first." >&2
  exit 1
fi

QUERY_URL="http://localhost:${PORT}/query"
STORE_URL="http://localhost:${PORT}/store?default"

if curl -sS -f -X POST \
    -H "Content-Type: application/sparql-query" \
    --data 'ASK {}' \
    "$QUERY_URL" >/dev/null \
  && curl -sS -f "$STORE_URL" >/dev/null; then
  echo "oxigraph already running on port $PORT"
  echo "query endpoint: $QUERY_URL"
  echo "store endpoint: $STORE_URL"
  exit 0
fi

if [[ -f "$PID_FILE" ]]; then
  stale_pid="$(cat "$PID_FILE" 2>/dev/null || true)"
  if [[ -n "$stale_pid" ]] && kill -0 "$stale_pid" >/dev/null 2>&1; then
    echo "oxigraph process $stale_pid exists but is not ready yet; waiting for readiness"
  else
    rm -f "$PID_FILE"
  fi
fi

if [[ ! -f "$PID_FILE" ]]; then
  "$OXIGRAPH_BIN" serve --location "$DATA_DIR" --bind "0.0.0.0:${PORT}" \
    >"$LOG_FILE" 2>&1 &
  echo "$!" >"$PID_FILE"
fi

for _ in $(seq 1 30); do
  if [[ -f "$PID_FILE" ]]; then
    pid="$(cat "$PID_FILE" 2>/dev/null || true)"
  else
    pid=""
  fi
  if [[ -n "$pid" ]] && ! kill -0 "$pid" >/dev/null 2>&1; then
    echo "oxigraph exited before readiness; see $LOG_FILE" >&2
    rm -f "$PID_FILE"
    exit 1
  fi
  if curl -sS -f -X POST \
      -H "Content-Type: application/sparql-query" \
      --data 'ASK {}' \
      "$QUERY_URL" >/dev/null \
    && curl -sS -f "$STORE_URL" >/dev/null; then
    echo "oxigraph ready on port $PORT (data dir: $DATA_DIR)"
    echo "pid file: $PID_FILE"
    echo "log file: $LOG_FILE"
    echo "query endpoint: $QUERY_URL"
    echo "store endpoint: $STORE_URL"
    exit 0
  fi
  sleep 1
done

echo "oxigraph did not become ready within 30s (query/store checks failed)" >&2
exit 1
