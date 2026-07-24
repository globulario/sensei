#!/bin/bash
# Sensei record-briefing hook for Claude Code.
#
# PostToolUse hook on awareness_briefing: records that a briefing was
# obtained so the enforce-briefing hook permits subsequent edits.
#
# Install: place in .claude/hooks/ and configure in .claude/settings.json:
#   "PostToolUse": [{
#     "matcher": "awareness_briefing",
#     "hooks": [{"type": "command", "command": ".claude/hooks/record-briefing.sh", "timeout": 10}]
#   }]
#
# The marker binds hash(repository root + repository domain + file path), not
# hash(file path) alone (contract §3/§9 correction): a briefing obtained
# against one domain must never authorize an edit evaluated under a
# different loaded domain. The domain is resolved via `sensei repo-domain`
# using the SAME explicit>configured>env precedence the briefing call itself
# used, so the marker reflects exactly what data the briefing consulted.

set -euo pipefail

INPUT=$(cat)

# Extract the file and domain parameters from the briefing call.
IFS=$'\t' read -r FILE_PATH CALL_DOMAIN <<< "$(echo "$INPUT" | python3 -c "
import json, sys
data = json.load(sys.stdin)
inp = data.get('tool_input', {})
print(inp.get('file', ''), inp.get('domain', ''), sep='\t')
" 2>/dev/null || printf '\t')" || true

if [ -z "$FILE_PATH" ]; then
    exit 0  # Task-only briefing — no file marker needed.
fi

# Resolve to absolute path.
if [[ "$FILE_PATH" != /* ]]; then
    FILE_PATH="$(pwd)/$FILE_PATH"
fi
FILE_PATH=$(realpath -m "$FILE_PATH" 2>/dev/null || echo "$FILE_PATH")

# Find project root (walk up to find a state-dir config — .sensei/config.yaml
# or the legacy .awg/config.yaml — or docs/awareness/) — identical logic to
# enforce-briefing.sh, so both hooks agree on the same root.
PROJECT_ROOT="$(pwd)"
check="$PROJECT_ROOT"
while [ "$check" != "/" ]; do
    if [ -f "$check/.sensei/config.yaml" ] || [ -f "$check/.awg/config.yaml" ] || [ -d "$check/docs/awareness" ]; then
        PROJECT_ROOT="$check"
        break
    fi
    check=$(dirname "$check")
done

case "$FILE_PATH" in
    "$PROJECT_ROOT"/*) : ;;
    *)
        exit 0  # Outside the project root entirely — not this hook's concern.
        ;;
esac
REL_PATH="${FILE_PATH#$PROJECT_ROOT/}"

# Resolve the domain the briefing call actually used (explicit call domain
# wins, else the checkout's configured/env domain) — the Sensei CLI is the
# one canonical resolver; never re-derive precedence here.
BIN="$(command -v sensei || command -v awg || true)"
RESOLVED_DOMAIN=""
if [ -n "$BIN" ]; then
    RESOLVED_DOMAIN=$("$BIN" repo-domain --path "$PROJECT_ROOT" --domain "$CALL_DOMAIN" 2>/dev/null || echo "")
fi

# Create marker file, keyed by (project root, resolved domain, file) — never
# by file path alone, so a briefing scoped to one domain cannot authorize an
# edit evaluated under a different one.
SESSION_ID="${CLAUDE_SESSION_ID:-default}"
MARKER_DIR="/tmp/sensei-briefings/$SESSION_ID"
mkdir -p "$MARKER_DIR"
MARKER_KEY="$PROJECT_ROOT|$RESOLVED_DOMAIN|$REL_PATH"
PATH_HASH=$(printf '%s' "$MARKER_KEY" | sha256sum | cut -d' ' -f1)
touch "$MARKER_DIR/$PATH_HASH"
