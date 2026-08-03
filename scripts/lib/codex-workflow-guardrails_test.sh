#!/usr/bin/env bash
# codex-workflow-guardrails_test.sh — static regressions against the real
# sensei-codex-architect-review.yml, not a mock: the two properties this
# proves were both real defects found by review, not hypothetical ones, so
# the test reads the actual file the workflow runs.
#
# Usage: bash scripts/lib/codex-workflow-guardrails_test.sh
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workflow="$repo_root/.github/workflows/sensei-codex-architect-review.yml"
prompt="$repo_root/.github/codex/sensei-architect-review.md"

failures=0
fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}
ok() {
  echo "ok $1"
}

if [[ ! -f "$workflow" ]]; then
  fail "workflow file not found: $workflow"
  echo "1 codex workflow guardrail(s) failed" >&2
  exit 1
fi
if [[ ! -f "$prompt" ]]; then
  fail "prompt file not found: $prompt"
  echo "1 codex workflow guardrail(s) failed" >&2
  exit 1
fi

# Property 1: the raw secret must never reach ambient job/step scope. It may
# only ever appear as `secrets.OPENAI_API_KEY` inside an `if:` condition
# (evaluated by the runner, never exposed to a process environment) or as the
# codex-action's own `openai-api-key:` input (the action's documented,
# supported channel for receiving it -- its own steps then isolate it via
# their own env -u pattern). It must never be the value of an `env:` key,
# which every step in the job -- including whatever codex exec spawns --
# would inherit.
env_check_output=$(python3 - "$workflow" <<'PY'
import sys
import yaml

path = sys.argv[1]
with open(path, encoding="utf-8") as fh:
    doc = yaml.safe_load(fh)

violations = []

def check_env_block(env, where):
    if not isinstance(env, dict):
        return
    for key in env:
        if key == "OPENAI_API_KEY":
            violations.append(f"{where}: env key {key!r}")

for job_name, job in (doc.get("jobs") or {}).items():
    check_env_block(job.get("env"), f"jobs.{job_name}.env")
    for i, step in enumerate(job.get("steps") or []):
        step_name = step.get("name", f"#{i}")
        check_env_block(step.get("env"), f"jobs.{job_name}.steps[{step_name}].env")

if violations:
    print("\n".join(violations))
    sys.exit(1)
PY
)
env_check_rc=$?
if ((env_check_rc == 0)); then
  ok "OPENAI_API_KEY never appears as an env: key anywhere in the workflow"
else
  fail "OPENAI_API_KEY appears in an env: block -- every step in that scope, including whatever codex exec spawns, would inherit it:"
  echo "$env_check_output" >&2
fi

if grep -Fq 'openai-api-key: ${{ secrets.OPENAI_API_KEY }}' "$workflow"; then
  ok "codex-action receives the secret directly from secrets.*, not via an intermediate env var"
else
  fail "codex-action's openai-api-key input does not read directly from secrets.OPENAI_API_KEY"
fi

if grep -q "env\.OPENAI_API_KEY" "$workflow"; then
  fail "workflow still references env.OPENAI_API_KEY somewhere -- that value should not exist"
else
  ok "no remaining env.OPENAI_API_KEY reference"
fi

# Property 2: every file the prompt tells Codex to read as a "repository-
# owned instruction" must be covered by the trusted-default-branch overlay,
# or a PR could rewrite its own governing review instructions through
# whichever one was missed.
overlay_check_output=$(python3 - "$prompt" "$workflow" <<'PY'
import re
import sys
import yaml

prompt_path, workflow_path = sys.argv[1], sys.argv[2]
prompt_text = open(prompt_path, encoding="utf-8").read()
referenced = set(re.findall(r"`([.\w/-]+\.(?:md|json))`", prompt_text))
if not referenced:
    print("no backtick-quoted instruction paths found in the prompt -- nothing to check")
    sys.exit(1)

with open(workflow_path, encoding="utf-8") as fh:
    doc = yaml.safe_load(fh)

# Two independent lists, checked independently: sparse-checkout only FETCHES
# a path into trusted-config/; the overlay step's cp commands are what
# actually put a trusted copy at the real, live path Codex reads. A path
# present in one but not the other is not actually protected at runtime.
sparse_checkout_lines = []
cp_destinations = []
for job in (doc.get("jobs") or {}).values():
    for step in job.get("steps") or []:
        if step.get("name") == "Fetch trusted review instructions from the default branch":
            raw = (step.get("with") or {}).get("sparse-checkout") or ""
            sparse_checkout_lines = [line.strip() for line in raw.splitlines() if line.strip()]
        if step.get("name") == "Overlay trusted review instructions":
            for line in (step.get("run") or "").splitlines():
                line = line.strip()
                if line.startswith("cp "):
                    cp_destinations.append(line.split()[-1])

def covered_by(path, candidates):
    parts = path.split("/")
    prefixes = {"/".join(parts[:n]) for n in range(1, len(parts) + 1)}
    return any(c in prefixes or c == path for c in candidates)

missing = []
for path in sorted(referenced):
    if not covered_by(path, sparse_checkout_lines):
        missing.append(f"{path} (not in sparse-checkout)")
    elif not covered_by(path, cp_destinations):
        missing.append(f"{path} (fetched but never copied to its live path by the overlay step)")

if missing:
    print("prompt-referenced instruction path(s) not fully covered by the trusted overlay: " + ", ".join(missing))
    sys.exit(1)
PY
)
overlay_check_rc=$?
if ((overlay_check_rc == 0)); then
  ok "every instruction path the prompt names is covered by the trusted default-branch overlay"
else
  fail "$overlay_check_output"
fi

if ((failures > 0)); then
  echo "$failures codex workflow guardrail(s) failed" >&2
  exit 1
fi
echo "all codex workflow guardrails passed"
