#!/bin/bash
# AWG feedback-reminder hook for Claude Code.
#
# Stop hook: when a session ends, warn (advisory — never blocks) if the work
# likely produced graph-worthy knowledge (a "scar") but no awareness graph
# feedback was written. It nudges you to record the lesson with one typed call:
#
#     awg propose --kind failure_mode --title "..." --related-invariant ... --evidence "..."
#
# Install: place in .claude/hooks/ and configure in .claude/settings.json:
#   "Stop": [{
#     "hooks": [{"type": "command", "command": ".claude/hooks/feedback-reminder.sh", "timeout": 15}]
#   }]
#
# The detection logic lives in `awg feedback-check` (testable Go), so this hook
# stays a thin, dependency-light wrapper.

set -euo pipefail

# Drain stdin (Stop hooks receive a JSON payload we do not need here).
cat >/dev/null 2>&1 || true

# Find the project root (walk up for docs/awareness/ or .awg/config.yaml).
PROJECT_ROOT="$(pwd)"
check="$PROJECT_ROOT"
while [ "$check" != "/" ]; do
    if [ -f "$check/.awg/config.yaml" ] || [ -d "$check/docs/awareness" ]; then
        PROJECT_ROOT="$check"
        break
    fi
    check=$(dirname "$check")
done

# Need the awg binary on PATH. If it is absent, stay silent — never block.
command -v awg >/dev/null 2>&1 || exit 0

# Run the advisory check. It exits 0 and prints a reminder only when a gap is
# detected. --quiet suppresses the "all clear" line so a clean session is silent.
OUT=$(awg feedback-check --repo-root "$PROJECT_ROOT" --quiet 2>/dev/null || true)

if [ -n "$OUT" ]; then
    # Surface on stderr so it shows in the transcript without blocking Stop.
    printf '%s\n' "$OUT" >&2
fi

exit 0
