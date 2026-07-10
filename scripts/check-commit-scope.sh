#!/usr/bin/env bash
# check-commit-scope.sh — guard against scope drift in narrow commits.
#
# If a commit's message CLAIMS narrow scope ("docs only", "adoption package
# only", "documentation only", ...) but its diff touches ENGINE paths
# (golang/, cmd/, internal/), that is the 765037e class of drift (see
# docs/commit-integrity-notes.md) — a message that says one thing while the
# diff does another. This fails the check.
#
# It only fires when the message makes the claim, so ordinary engine commits
# pass trivially — low noise by design.
#
# Escape hatch: start a line of the commit message with "[engine-ack] <reason>"
# to acknowledge an intentional engine change in an otherwise-docs commit. A
# mere prose mention of [engine-ack] does not count — the marker must lead a line.
#
# Usage: scripts/check-commit-scope.sh [<commit-ish>]   (default: HEAD)
set -euo pipefail

REF="${1:-HEAD}"
MSG="$(git log -1 --format=%B "${REF}")"
FILES="$(git diff-tree --no-commit-id --name-only -r "${REF}")"

# Does the message claim narrow/docs-only scope?
if ! printf '%s' "${MSG}" | grep -qiE "docs only|docs-only|documentation only|adoption package only|adoption-only|docs/launch only|no engine change|no new feature"; then
  echo "check-commit-scope: ${REF} makes no docs-only / no-engine-change claim — nothing to enforce."
  exit 0
fi

# Any engine files in the diff? Check this FIRST: a clean docs commit passes
# for the right reason ("no engine paths"), and we only consult the escape
# hatch when there is actually something to acknowledge.
ENGINE="$(printf '%s\n' "${FILES}" | grep -E '^(golang/|cmd/|internal/)' || true)"
if [[ -z "${ENGINE}" ]]; then
  echo "check-commit-scope: ${REF} claims docs-only and touches no engine paths — OK."
  exit 0
fi

# Engine files ARE present. Acknowledged? The marker must be the first
# non-space token on its own line (e.g. a trailer line "[engine-ack] reason"),
# so a prose MENTION of [engine-ack] — like this script's own docs — does not
# count as an acknowledgment. That distinction is the whole point.
if printf '%s' "${MSG}" | grep -qE '^[[:space:]]*\[engine-ack\]'; then
  echo "check-commit-scope: ${REF} claims docs-only but carries an [engine-ack] line — allowed."
  exit 0
fi

echo "check-commit-scope: SCOPE DRIFT in ${REF}" >&2
echo "  the message claims docs-only scope, but these engine files changed:" >&2
printf '    %s\n' ${ENGINE} >&2
echo "  Fix: split the engine change into its own commit, OR if intentional," >&2
echo "  start a line of the commit message with [engine-ack] <reason>. See" >&2
echo "  docs/commit-integrity-notes.md." >&2
exit 1
