#!/usr/bin/env bash
set -euo pipefail

repo="."
sensei="./bin/sensei"
addr="localhost:10120"
repository_domain=""
graph_domain=""
diff_range=""
mode="enforce"
out_dir="sensei-activation"
full_how_timeout="60"

usage() {
  cat <<'EOF'
Usage: scripts/sensei-architect-activation.sh [flags]

Run the repository-owned Sensei architect evidence path for one exact diff.
The command is read-only with respect to repository source and canonical
awareness. It emits bounded review evidence under the output directory.

Flags:
  --repo <path>               repository root (default: .)
  --sensei <path>             Sensei binary (default: ./bin/sensei)
  --addr <host:port>          running Sensei gRPC endpoint
  --repository-domain <name>  canonical host/path repository identity
  --graph-domain <name>       selectable graph scope used by gate
  --diff <range>              exact git diff range
  --mode <mode>               enforce or report-only (default: enforce)
  --out <dir>                 evidence directory (default: sensei-activation)
  --full-how-timeout <sec>    full-repository HOW budget (default: 60)
  -h, --help                  show help
EOF
}

while (($#)); do
  case "$1" in
    --repo) repo=${2:?missing value}; shift 2 ;;
    --sensei) sensei=${2:?missing value}; shift 2 ;;
    --addr) addr=${2:?missing value}; shift 2 ;;
    --repository-domain) repository_domain=${2:?missing value}; shift 2 ;;
    --graph-domain) graph_domain=${2:?missing value}; shift 2 ;;
    --diff) diff_range=${2:?missing value}; shift 2 ;;
    --mode) mode=${2:?missing value}; shift 2 ;;
    --out) out_dir=${2:?missing value}; shift 2 ;;
    --full-how-timeout) full_how_timeout=${2:?missing value}; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "sensei-architect-activation: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

case "$mode" in
  enforce|report-only) ;;
  *) echo "sensei-architect-activation: --mode must be enforce or report-only" >&2; exit 2 ;;
esac
if ! [[ "$full_how_timeout" =~ ^[1-9][0-9]*$ ]]; then
  echo "sensei-architect-activation: --full-how-timeout must be positive seconds" >&2
  exit 2
fi

repo=$(cd "$repo" && pwd)
if [[ ! -x "$sensei" ]]; then
  echo "sensei-architect-activation: Sensei binary is not executable: $sensei" >&2
  exit 2
fi
sensei=$(cd "$(dirname "$sensei")" && pwd)/$(basename "$sensei")

if [[ -z "$repository_domain" ]]; then
  repository_domain=$($sensei repo-domain --repo "$repo" 2>/dev/null | tail -n1 | tr -d '\r' || true)
fi
if [[ -z "$repository_domain" ]]; then
  echo "sensei-architect-activation: canonical repository domain is required" >&2
  exit 2
fi
if [[ -z "$graph_domain" ]]; then
  echo "sensei-architect-activation: selectable graph domain is required" >&2
  exit 2
fi
if [[ -z "$diff_range" ]]; then
  diff_range="HEAD~1...HEAD"
fi
if ! git -C "$repo" diff --name-only "$diff_range" >/dev/null 2>&1; then
  echo "sensei-architect-activation: invalid diff range: $diff_range" >&2
  exit 2
fi

mkdir -p "$out_dir/preflight" "$out_dir/phase10"
out_dir=$(cd "$out_dir" && pwd)
head_sha=$(git -C "$repo" rev-parse HEAD)
base_ref=${diff_range%%...*}
base_sha=$(git -C "$repo" rev-parse "$base_ref" 2>/dev/null || git -C "$repo" rev-parse HEAD^)
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
timeout --foreground 30s "$sensei" metadata --addr "$addr" >"$out_dir/metadata.txt" 2>"$out_dir/metadata.err"
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
  timeout --foreground 30s "$sensei" preflight \
    --task "Review exact diff $diff_range" \
    --file "$file" \
    --domain "$repository_domain" \
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
  timeout --foreground 90s "$sensei" gate --diff "$diff_range" --domain "$graph_domain" --enforce --addr "$addr" >"$out_dir/gate.txt" 2>"$out_dir/gate.err"
else
  timeout --foreground 90s "$sensei" gate --diff "$diff_range" --domain "$graph_domain" --report-only --addr "$addr" >"$out_dir/gate.txt" 2>"$out_dir/gate.err"
fi
gate_rc=$?
set -e
if ((gate_rc == 0)); then
  record_status gate pass "$mode"
else
  record_status gate fail "mode=$mode exit=$gate_rc"
fi

set +e
timeout --foreground 90s bash "$repo/scripts/sensei-task-audit.sh" --repo "$repo" --sensei "$sensei" --out "$out_dir/task-audit.json" >"$out_dir/task-audit.stdout" 2>"$out_dir/task-audit.err"
task_audit_rc=$?
set -e
if ((task_audit_rc == 0)); then
  invalid_tasks=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["invalid_count"])' "$out_dir/task-audit.json")
  if ((invalid_tasks == 0)); then
    record_status task_audit clean
  else
    record_status task_audit debt "invalid_or_unreadable=$invalid_tasks"
  fi
else
  record_status task_audit unavailable "exit=$task_audit_rc"
fi

phase10_fixture_rc=0
fixture="$repo/golang/architecture/howextract/testdata/deterministic_repo"
fixture_how="$out_dir/phase10/fixture-how.json"
fixture_why="$out_dir/phase10/fixture-why.json"

set +e
timeout --foreground 90s "$sensei" investigate how \
  --repo "$fixture" \
  --domain "$repository_domain" \
  --revision "$head_sha" \
  --captured-at "$captured_at" \
  --resource-limit "surface=ci_fixture" \
  --out "$fixture_how" \
  --format json \
  >"$out_dir/phase10/fixture-how.stdout" 2>"$out_dir/phase10/fixture-how.err"
fixture_how_rc=$?
if ((fixture_how_rc == 0)); then
  timeout --foreground 30s "$sensei" investigate validate --artifact "$fixture_how" --json >"$out_dir/phase10/fixture-how-validation.json" 2>"$out_dir/phase10/fixture-how-validation.err"
  fixture_how_validate_rc=$?
else
  fixture_how_validate_rc=1
fi
set -e
if ((fixture_how_rc == 0 && fixture_how_validate_rc == 0)); then
  record_status phase10_fixture_how available "validated"
else
  phase10_fixture_rc=1
  record_status phase10_fixture_how degraded "how=$fixture_how_rc validate=$fixture_how_validate_rc"
fi

why_target_kind=""
why_target_id=""
if ((fixture_how_rc == 0)); then
  readarray -t why_target < <(python3 - "$fixture_how" <<'PY'
import json, sys
p = json.load(open(sys.argv[1], encoding="utf-8"))
if p.get("observations"):
    print("observation")
    print(p["observations"][0]["id"])
elif p.get("raw_evidence"):
    print("evidence")
    print(p["raw_evidence"][0]["id"])
PY
)
  if ((${#why_target[@]} == 2)); then
    why_target_kind=${why_target[0]}
    why_target_id=${why_target[1]}
  fi
fi

fixture_why_rc=1
fixture_why_validate_rc=1
if [[ -n "$why_target_id" ]]; then
  target_flag="--observation"
  [[ "$why_target_kind" == "evidence" ]] && target_flag="--evidence-id"
  set +e
  timeout --foreground 90s "$sensei" investigate why \
    --repo "$repo" \
    --how "$fixture_how" \
    --captured-at "$captured_at" \
    --history-start "$base_sha" \
    --history-end "$head_sha" \
    --provider git_history_provider \
    "$target_flag" "$why_target_id" \
    --out "$fixture_why" \
    --format json \
    >"$out_dir/phase10/fixture-why.stdout" 2>"$out_dir/phase10/fixture-why.err"
  fixture_why_rc=$?
  if ((fixture_why_rc == 0)); then
    timeout --foreground 30s "$sensei" investigate validate --artifact "$fixture_why" --json >"$out_dir/phase10/fixture-why-validation.json" 2>"$out_dir/phase10/fixture-why-validation.err"
    fixture_why_validate_rc=$?
  fi
  set -e
fi
if ((fixture_why_rc == 0 && fixture_why_validate_rc == 0)); then
  record_status phase10_fixture_why available "validated provider=git_history_provider"
else
  phase10_fixture_rc=1
  record_status phase10_fixture_why degraded "why=$fixture_why_rc validate=$fixture_why_validate_rc target=${why_target_id:-missing}"
fi

set +e
timeout --foreground "${full_how_timeout}s" "$sensei" investigate how \
  --repo "$repo" \
  --domain "$repository_domain" \
  --revision "$head_sha" \
  --captured-at "$captured_at" \
  --resource-limit "wall_clock=${full_how_timeout}s" \
  --out "$out_dir/phase10/full-how.json" \
  --format json \
  >"$out_dir/phase10/full-how.stdout" 2>"$out_dir/phase10/full-how.err"
full_how_rc=$?
set -e
if ((full_how_rc == 0)); then
  set +e
  timeout --foreground 30s "$sensei" investigate validate --artifact "$out_dir/phase10/full-how.json" --json >"$out_dir/phase10/full-how-validation.json" 2>"$out_dir/phase10/full-how-validation.err"
  full_how_validate_rc=$?
  set -e
  if ((full_how_validate_rc == 0)); then
    record_status phase10_full_how available "validated"
  else
    record_status phase10_full_how degraded "validation_exit=$full_how_validate_rc"
  fi
elif ((full_how_rc == 124)); then
  record_status phase10_full_how bounded_timeout "budget=${full_how_timeout}s"
else
  record_status phase10_full_how degraded "exit=$full_how_rc"
fi

record_status phase10_architecture not_ready "requires canonical graph, claims, closure, questions, and review-history digests"
record_status phase10_candidate_views not_ready "architecture result unavailable; blast-radius and challenge remain downstream"
record_status mcp unavailable_in_github_actions "interactive agents remain MCP-first; CI uses explicit CLI fallback"

STATUS_JSONL="$out_dir/status.jsonl" CHANGED_FILES="$changed_file_list" REPOSITORY_DOMAIN="$repository_domain" GRAPH_DOMAIN="$graph_domain" DIFF_RANGE="$diff_range" BASE_SHA="$base_sha" HEAD_SHA="$head_sha" MODE="$mode" python3 - <<'PY' > "$out_dir/activation.json"
import json, os
from pathlib import Path
statuses = [json.loads(line) for line in Path(os.environ["STATUS_JSONL"]).read_text().splitlines() if line.strip()]
files = [line for line in Path(os.environ["CHANGED_FILES"]).read_text().splitlines() if line.strip()]
print(json.dumps({
    "schema_version": "sensei.architect_activation.v1",
    "repository_domain": os.environ["REPOSITORY_DOMAIN"],
    "graph_domain": os.environ["GRAPH_DOMAIN"],
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
print(f"- Repository domain: `{p['repository_domain']}`")
print(f"- Graph domain: `{p['graph_domain']}`")
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
  if ((metadata_rc != 0 || preflight_failures != 0 || gate_rc != 0 || phase10_fixture_rc != 0)); then
    exit 1
  fi
fi
