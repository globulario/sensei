#!/usr/bin/env bash
set -euo pipefail

repo="."
sensei="./bin/sensei"
out=""
fail_on_invalid=0

usage() {
  cat <<'EOF'
Usage: scripts/sensei-task-audit.sh [flags]

Audit every local .sensei/tasks task directory through the canonical
`sensei task-status --verify` reader. The audit is read-only and never clears,
supersedes, repairs, or rewrites task state.

Flags:
  --repo <path>          repository root (default: .)
  --sensei <path>        Sensei binary (default: ./bin/sensei)
  --out <path>           JSON report path (default: stdout)
  --fail-on-invalid      exit 1 when any task cannot be verified
  -h, --help             show help
EOF
}

while (($#)); do
  case "$1" in
    --repo) repo=${2:?missing value}; shift 2 ;;
    --sensei) sensei=${2:?missing value}; shift 2 ;;
    --out) out=${2:?missing value}; shift 2 ;;
    --fail-on-invalid) fail_on_invalid=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "sensei-task-audit: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

repo=$(cd "$repo" && pwd)
if [[ ! -x "$sensei" ]]; then
  echo "sensei-task-audit: Sensei binary is not executable: $sensei" >&2
  exit 2
fi
sensei=$(cd "$(dirname "$sensei")" && pwd)/$(basename "$sensei")

tasks_root="$repo/.sensei/tasks"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
jsonl="$tmp/tasks.jsonl"
: > "$jsonl"

active_session_path=""
if [[ -f "$tasks_root/active.yaml" ]]; then
  active_session_path=$(python3 - "$tasks_root/active.yaml" <<'PY'
import re, sys
from pathlib import Path

for line in Path(sys.argv[1]).read_text(errors="replace").splitlines():
    match = re.match(r"^\s*session_path\s*:\s*['\"]?([^'\"#]+?)['\"]?\s*$", line)
    if match:
        print(match.group(1).strip())
        break
PY
)
fi

if [[ -d "$tasks_root" ]]; then
  while IFS= read -r -d '' task_dir; do
    task_name=$(basename "$task_dir")
    stdout_file="$tmp/${task_name}.stdout"
    stderr_file="$tmp/${task_name}.stderr"
    set +e
    "$sensei" task-status --repo "$repo" --task "$task_dir" --verify --format json >"$stdout_file" 2>"$stderr_file"
    rc=$?
    set -e

    combined=$(cat "$stdout_file" "$stderr_file")
    active=false
    if [[ -n "$active_session_path" ]]; then
      active_task_dir=$(dirname "$repo/${active_session_path#./}")
      if [[ "$task_dir" == "$active_task_dir" ]]; then
        active=true
      fi
    fi

    classification="inactive_valid"
    if [[ "$active" == true ]]; then
      classification="active"
    elif grep -qi 'supersed' <<<"$combined"; then
      classification="superseded"
    elif ((rc != 0)); then
      classification="invalid_or_unreadable"
    fi

    TASK_NAME="$task_name" TASK_DIR="$task_dir" ACTIVE="$active" CLASSIFICATION="$classification" RC="$rc" \
      STDOUT_FILE="$stdout_file" STDERR_FILE="$stderr_file" python3 - <<'PY' >> "$jsonl"
import json, os
from pathlib import Path

def bounded(path: str, limit: int = 8192) -> str:
    data = Path(path).read_text(errors="replace")
    return data if len(data) <= limit else data[:limit] + "\n...truncated..."

print(json.dumps({
    "task": os.environ["TASK_NAME"],
    "directory": os.environ["TASK_DIR"],
    "active": os.environ["ACTIVE"] == "true",
    "classification": os.environ["CLASSIFICATION"],
    "verified": int(os.environ["RC"]) == 0,
    "exit_code": int(os.environ["RC"]),
    "stdout": bounded(os.environ["STDOUT_FILE"]),
    "stderr": bounded(os.environ["STDERR_FILE"]),
}, sort_keys=True))
PY
  done < <(find "$tasks_root" -mindepth 1 -maxdepth 1 -type d -print0 | sort -z)
fi

report="$tmp/report.json"
REPO="$repo" TASKS_ROOT="$tasks_root" ACTIVE_SESSION_PATH="$active_session_path" JSONL="$jsonl" python3 - <<'PY' > "$report"
import json, os
from collections import Counter
from pathlib import Path

rows = []
for line in Path(os.environ["JSONL"]).read_text().splitlines():
    if line.strip():
        rows.append(json.loads(line))
counts = Counter(row["classification"] for row in rows)
invalid = sum(1 for row in rows if not row["verified"])
print(json.dumps({
    "schema_version": "sensei.task_audit.v1",
    "repository": os.environ["REPO"],
    "tasks_root": os.environ["TASKS_ROOT"],
    "active_session_path": os.environ["ACTIVE_SESSION_PATH"],
    "task_count": len(rows),
    "verified_count": len(rows) - invalid,
    "invalid_count": invalid,
    "classification_counts": dict(sorted(counts.items())),
    "tasks": rows,
}, indent=2, sort_keys=True))
PY

if [[ -n "$out" ]]; then
  mkdir -p "$(dirname "$out")"
  cp "$report" "$out"
else
  cat "$report"
fi

invalid_count=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["invalid_count"])' "$report")
if ((fail_on_invalid && invalid_count > 0)); then
  exit 1
fi
