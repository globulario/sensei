#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  echo "usage: scripts/awg-benchmark-score.sh <task-file.json> [repo-root]" >&2
  exit 2
fi

TASK_FILE="$1"
TARGET_REPO_ROOT="${2:-.}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
AG_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
AWG_BIN_PATH="${AWG_BIN:-}"
AWG_ADDR_VALUE="${AWG_ADDR:-127.0.0.1:10120}"
SERVICES_REPO_VALUE="${SERVICES_REPO:-$AG_ROOT/../services}"

supports_benchmark_score() {
  local bin="$1"
  [ -x "$bin" ] || return 1
  "$bin" help 2>&1 | grep -q 'benchmark-score'
}

if [ -n "$AWG_BIN_PATH" ] && supports_benchmark_score "$AWG_BIN_PATH"; then
  :
elif supports_benchmark_score "$AG_ROOT/bin/sensei"; then
  AWG_BIN_PATH="$AG_ROOT/bin/sensei"
else
  mkdir -p /tmp/go-build-cache
  GOCACHE=/tmp/go-build-cache go build -o "$AG_ROOT/bin/sensei" "$AG_ROOT/cmd/awg"
  AWG_BIN_PATH="$AG_ROOT/bin/sensei"
fi

exec "$AWG_BIN_PATH" benchmark-score \
  --task-file "$TASK_FILE" \
  --repo-root "$TARGET_REPO_ROOT" \
  --addr "$AWG_ADDR_VALUE" \
  --ag-repo "$AG_ROOT" \
  --services-repo "$SERVICES_REPO_VALUE" \
  --json
