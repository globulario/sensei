#!/usr/bin/env bash
set -euo pipefail

repo="."
sensei="./bin/sensei"
addr="localhost:10120"
domain=""
diff_range=""
mode="enforce"
out_dir="sensei-activation"

usage() {
  cat <<'EOF'
Usage: scripts/sensei-architect-activation.sh [flags]

Run the repository-owned Sensei architect evidence path for one exact diff.
This command is read-only with respect to repository source and canonical
awareness. It emits review evidence under the output directory.

Flags:
  --repo <path>          repository root (default: .)
  --sensei <path>        Sensei binary (default: ./bin/sensei)
  --addr <host:port>     running Sensei gRPC endpoint
  --domain <domain>      exact repository domain
  --diff <range>         exact git diff range
  --mode <mode>          enforce or report-only (default: enforce)
  --out <dir>            evidence directory (default: sensei-activation)
  -h, --help             show help
EOF
}

while (($#)); do
  case "$1" in
    --repo) repo=${2:?missing value}; shift 2 ;;
    --sensei) sensei=${2:?missing value}; shift 2 ;;
    --addr) addr=${2:?missing value}; shift 2 ;;
    --domain) domain=${2:?missing value}; shift 2 ;;
    --diff) diff_range=${2:?missing value}; shift 2 ;;
    --mode) mode=${2:?missing value}; shift 2 ;;
    --out) out_dir=${2:?missing value}; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "sensei-architect-activation: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

case "$mode" in
  enforce|report-only) ;;
  *) echo "sensei-architect-activation: --mode must be enforce or report-only" >&2; exit 2 ;;
esac

repo=$(cd "$repo" && pwd)
if [[ ! -x "$sensei" ]]; then
  echo "sensei-architect-activation: Sensei binary is not executable: $sensei" >&2
  exit 2
fi
sensei=$(cd "$(dirname "$sensei")" && pwd)/$(basename "$sensei")

if [[ -z "$domain" ]]; then
  domain=$($sensei repo-domain --repo "$repo" 2>/dev/null | tail -n1 | tr -d '\r' || true)
fi
if [[ -z "$domain" ]]; then
  echo "sensei-architect-activation: repository domain is required" >&2
  exit 2
fi
if [[ -z "$diff_range" ]]; then
  diff_range="HEAD~1...HEAD"
fi
if ! git -C "$repo" diff --quiet "$diff_range" -- 2>/dev/null && ! git -C "$repo" diff --name-only "$diff_range" >/dev/null 2>&1; then
  echo "sensei-architect-activation: invalid diff range: $diff_range" >&2
  exit 2
fi

mkdir -p "$out_dir/preflight" "$out_dir/phase10"
out_dir=$(cd "$out_dir" && pwd)

head_sha=$(git -C "$repo" rev-parse HEAD)
base_sha=$(git -C "$repo" rev-parse "${diff_range%%...*}" 2>/dev/null || git -C "$repo" rev-parse HEAD^)
captured_at=$(git -C "$repo" show -s --format=%cI "$head_sha")
changed_file_list="$out_dir/changed-files.txt"
git -C "$repo" diff --name-only --diff-filter=ACDMRTUXB "$diff_range" | sed '/^[[:space:]]*$/d' > "$changed_file_list"

record_status() {
  local name=$1
  local status=$2
  local detail=${3:-}
  python3 - "$out_dir/status.jsonl" "$name" "$status" "$detail" <<'PY'
import json, sys
path, name, status, detail = sys.argv[1:]
with open(path, "a", encoding="utf-8") as fh:
    fh.write(json.dumps({"name": name, "status": status, "detail": detail}, sort_keys=True) + "\n")
PY
}
: > "$out_dir/status.jsonl"

set +e
"$sensei" metadata --domain "$domain" --addr "$addr" >"$out_dir/metadata.txt" 2>"$out_dir/metadata.err"
metadata_rc=$?
set -e
if ((metadata_rc == 0)); then
  record_status metadata available
else
  record_status metadata unavailable "exit=$metadata_rc"
fi

preflight_failures=0
while IFS= read -r file; do
  [[ -n "$file" ]] || continue
  safe_name=$(printf '%s' "$file" | sha256sum | cut -d' ' -f1)
  set +e
  "$sensei" preflight \
    --task "Review exact diff $diff_range" \
    --file "$file" \
    --domain "$domain" \
    --mode standard \
    --addr "$addr" \
    >"$out_dir/preflight/${safe_name}.txt" 2>"$out_dir/preflight/${safe_name}.err"
  rc=$?
  set -e
  if ((rc != 0)); then
    preflight_failures=$((preflight_failures + 1))
  fi
done < "$changed_file_list"
if ((preflight_failures == 0)); then
  record_status preflight available
else
  record_status preflight degraded "failed_files=$preflight_failures"
fi

set +e
if [[ "$mode" == "enforce" ]]; then
  "$sensei" gate --diff "$diff_range" --domain "$domain" --enforce --addr "$addr" >"$out_dir/gate.txt" 2>"$out_dir/gate.err"
else
  "$sensei" gate --diff "$diff_range" --domain "$domain" --report-only --addr "$addr" >"$out_dir/gate.txt" 2>"$out_dir/gate.err"
fi
gate_rc=$?
set -e
if ((gate_rc == 0)); then
  record_status gate pass "$mode"
else
  record_status gate fail "mode=$mode exit=$gate_rc"
fi

set +e
bash "$repo/scripts/sensei-task-audit.sh" --repo "$repo" --sensei "$sensei" --out "$out_dir/task-audit.json" >"$out_dir/task-audit.stdout" 2>"$out_dir/task-audit.err"
task_audit_rc=$?
set -e
if ((task_audit_rc == 0)); then
  record_status task_audit available
else
  record_status task_audit unavailable "exit=$task_audit_rc"
fi

phase10_rc=0
if "$sensei" investigate --help >"$out_dir/phase10/help.txt" 2>"$out_dir/phase10/help.err"; then
  set +e
  "$sensei" investigate how \
    --repo "$repo" \
    --domain "$domain" \
    --revision "$head_sha" \
    --captured-at "$captured_at" \
    --out "$out_dir/phase10/how.json" \
    --format json \
    >"$out_dir/phase10/how.stdout" 2>"$out_dir/phase10/how.err"
  how_rc=$?
  if ((how_rc == 0)); then
    "$sensei" investigate validate --artifact "$out_dir/phase10/how.json" --json >"$out_dir/phase10/how-validation.json" 2>"$out_dir/phase10/how-validation.err"
    how_validate_rc=$?
  else
    how_validate_rc=1
  fi

  if ((how_rc == 0 && how_validate_rc == 0)); then
    "$sensei" investigate why \
      --repo "$repo" \
      --how "$out_dir/phase10/how.json" \
      --captured-at "$captured_at" \
      --history-start "$base_sha" \
      --history-end "$head_sha" \
      --out "$out_dir/phase10/why.json" \
      --format json \
      >"$out_dir/phase10/why.stdout" 2>"$out_dir/phase10/why.err"
    why_rc=$?
    if ((why_rc == 0)); then
      "$sensei" investigate validate --artifact "$out_dir/phase10/why.json" --json >"$out_dir/phase10/why-validation.json" 2>"$out_dir/phase10/why-validation.err"
      why_validate_rc=$?
    else
      why_validate_rc=1
    fi
  else
    why_rc=1
    why_validate_rc=1
  fi
  set -e

  if ((how_rc == 0 && how_validate_rc == 0 && why_rc == 0 && why_validate_rc == 0)); then
    record_status phase10 available "how+why validated"
  else
    phase10_rc=1
    record_status phase10 degraded "how=$how_rc how_validate=$how_validate_rc why=$why_rc why_validate=$why_validate_rc"
  fi
else
  phase10_rc=1
  record_status phase10 unreachable "investigate is compiled but not dispatched"
fi

STATUS_JSONL="$out_dir/status.jsonl" CHANGED_FILES="$changed_file_list" DOMAIN="$domain" DIFF_RANGE="$diff_range" BASE_SHA="$base_sha" HEAD_SHA="$head_sha" MODE="$mode" python3 - <<'PY' > "$out_dir/activation.json"
import json, os
from pathlib import Path

statuses = [json.loads(line) for line in Path(os.environ["STATUS_JSONL"]).read_text().splitlines() if line.strip()]
files = [line for line in Path(os.environ["CHANGED_FILES"]).read_text().splitlines() if line.strip()]
print(json.dumps({
    "schema_version": "sensei.architect_activation.v1",
    "repository_domain": os.environ["DOMAIN"],
    "diff_range": os.environ["DIFF_RANGE"],
    "base_revision": os.environ["BASE_SHA"],
    "head_revision": os.environ["HEAD_SHA"],
    "gate_mode": os.environ["MODE"],
    "changed_files": files,
    "surfaces": statuses,
}, indent=2, sort_keys=True))
PY

python3 - "$out_dir/activation.json" > "$out_dir/summary.md" <<'PY'
import json, sys
p = json.load(open(sys.argv[1], encoding="utf-8"))
print("## Sensei architect activation")
print()
print(f"- Domain: `{p['repository_domain']}`")
print(f"- Diff: `{p['diff_range']}`")
print(f"- Gate mode: `{p['gate_mode']}`")
print(f"- Changed files: {len(p['changed_files'])}")
print()
print("| Surface | Status | Detail |")
print("|---|---|---|")
for row in p["surfaces"]:
    print(f"| {row['name']} | {row['status']} | {row.get('detail','')} |")
PY

cat "$out_dir/summary.md"

if [[ "$mode" == "enforce" ]]; then
  if ((gate_rc != 0 || metadata_rc != 0 || phase10_rc != 0)); then
    exit 1
  fi
fi
