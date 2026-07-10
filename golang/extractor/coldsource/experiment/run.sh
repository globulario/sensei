#!/usr/bin/env bash
#
# run.sh — convenience runner for the EXPERIMENTAL cold-source hit-rate test.
# Offline fixture is the DEFAULT and recommended path. This script is optional;
# you can always call `awg cold-bootstrap` directly (see README.md). It is NOT
# used by CI and has no required GitHub/network dependency.
#
# Usage:
#   ANTHROPIC_API_KEY=... ./run.sh --repo <path> [--since <range>] [--fixture <json>]
#                                   [--model <id>] [--drafter echo|llm] [--write]
#                                   [--gh OWNER/REPO]
#
# Defaults: --drafter llm, --dry-run (no files written), --max 10, services fixture.
# --drafter echo runs the full pipeline with NO API key (parse/validate/citation
# checks only; no model call) — handy to smoke-test the plumbing.
# --gh OWNER/REPO fetches real PR review comments via `gh`+`jq` into a temp
# fixture (opt-in; requires those tools). --write drops --dry-run and emits files.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ag_root="$(cd "$here/../../../.." && pwd)"   # experiment/ -> coldsource -> extractor -> golang -> repo root

repo="" ; since="" ; fixture="$here/fixtures/services_pr_comments.json"
model="" ; drafter="llm" ; gh_slug="" ; dry="--dry-run"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)     repo="$2"; shift 2 ;;
    --since)    since="$2"; shift 2 ;;
    --fixture)  fixture="$2"; shift 2 ;;
    --model)    model="$2"; shift 2 ;;
    --drafter)  drafter="$2"; shift 2 ;;
    --gh)       gh_slug="$2"; shift 2 ;;
    --write)    dry=""; shift ;;
    -h|--help)  grep '^#' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

[[ -n "$repo" ]] || { echo "error: --repo is required" >&2; exit 2; }
if [[ "$drafter" == "llm" && -z "${ANTHROPIC_API_KEY:-}" ]]; then
  echo "error: --drafter llm needs ANTHROPIC_API_KEY (offline echo drafter: --drafter echo)" >&2
  exit 2
fi

# Locate or build the awg binary.
awg="${AWG:-$(command -v awg || true)}"
if [[ -z "$awg" ]]; then
  awg="$(mktemp -d)/awg"
  echo "building awg from $ag_root ..." >&2
  ( cd "$ag_root" && GOWORK=off go build -o "$awg" ./cmd/awg )
fi

# Optional: pull real review comments via gh (opt-in, needs gh + jq).
if [[ -n "$gh_slug" ]]; then
  command -v gh >/dev/null || { echo "error: --gh needs the gh CLI" >&2; exit 2; }
  command -v jq >/dev/null || { echo "error: --gh needs jq" >&2; exit 2; }
  fixture="$(mktemp).json"
  echo "fetching PR review comments from $gh_slug via gh ..." >&2
  gh api --paginate "repos/$gh_slug/pulls/comments?per_page=100" \
    | jq 'map({PRID:(.pull_request_url|split("/")|last), CommentID:(.id|tostring),
               Path:.path, Line:(.line // .original_line // 0), Body:.body})' > "$fixture"
fi

set -x
exec "$awg" cold-bootstrap \
  --repo "$repo" \
  ${since:+--since "$since"} \
  --drafter "$drafter" \
  ${model:+--model "$model"} \
  --pr-comments "$fixture" \
  --max 10 \
  $dry
