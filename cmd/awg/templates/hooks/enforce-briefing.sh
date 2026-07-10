#!/bin/bash
# AWG enforce-briefing hook for Claude Code.
#
# PreToolUse hook on Edit/Write/MultiEdit: blocks edits to files in
# high-risk directories unless an awareness briefing was obtained first.
#
# Install: place in .claude/hooks/ and configure in .claude/settings.json:
#   "PreToolUse": [{
#     "matcher": "Edit|Write|MultiEdit",
#     "hooks": [{"type": "command", "command": ".claude/hooks/enforce-briefing.sh", "timeout": 10}]
#   }]
#
# The hook reads high_risk_files.yaml to determine which paths need
# briefing. The record-briefing.sh PostToolUse hook creates the marker
# file that this hook checks.

set -euo pipefail

# Read tool input from stdin.
INPUT=$(cat)

# Extract file path from the tool input.
FILE_PATH=$(echo "$INPUT" | python3 -c "
import json, sys
data = json.load(sys.stdin)
inp = data.get('tool_input', {})
print(inp.get('file_path', inp.get('file', '')))
" 2>/dev/null || echo "")

if [ -z "$FILE_PATH" ]; then
    exit 0  # No file path — not our concern.
fi

# Resolve to absolute path.
if [[ "$FILE_PATH" != /* ]]; then
    FILE_PATH="$(pwd)/$FILE_PATH"
fi
FILE_PATH=$(realpath -m "$FILE_PATH" 2>/dev/null || echo "$FILE_PATH")

# Find project root (walk up to find .awg/config.yaml or docs/awareness/).
PROJECT_ROOT="$(pwd)"
check="$PROJECT_ROOT"
while [ "$check" != "/" ]; do
    if [ -f "$check/.awg/config.yaml" ] || [ -d "$check/docs/awareness" ]; then
        PROJECT_ROOT="$check"
        break
    fi
    check=$(dirname "$check")
done

# Read high-risk paths from high_risk_files.yaml.
HR_FILE="$PROJECT_ROOT/docs/awareness/high_risk_files.yaml"
if [ ! -f "$HR_FILE" ]; then
    exit 0  # No high-risk file list — nothing to enforce.
fi

# Extract paths from YAML (simple grep — no heavy deps).
HIGH_RISK_PATHS=$(grep -E '^\s*-\s+' "$HR_FILE" | sed 's/^\s*-\s*//' | sed 's/#.*//' | tr -d ' ' | grep -v '^$' || true)

if [ -z "$HIGH_RISK_PATHS" ]; then
    exit 0  # Empty list.
fi

# Check if the file matches any high-risk path.
REL_PATH="${FILE_PATH#$PROJECT_ROOT/}"
MATCHES=false
while IFS= read -r prefix; do
    if [[ "$REL_PATH" == "$prefix"* ]]; then
        MATCHES=true
        break
    fi
done <<< "$HIGH_RISK_PATHS"

if ! $MATCHES; then
    exit 0  # Not a high-risk file.
fi

# Check for briefing marker.
SESSION_ID="${CLAUDE_SESSION_ID:-default}"
MARKER_DIR="/tmp/awg-briefings/$SESSION_ID"
PATH_HASH=$(echo -n "$FILE_PATH" | sha256sum | cut -d' ' -f1)

if [ -f "$MARKER_DIR/$PATH_HASH" ]; then
    exit 0  # Briefing was obtained.
fi

# Block the edit.
cat <<EOF
{
  "decision": "block",
  "reason": "AWG: call awareness briefing for $REL_PATH before editing this high-risk path. Run: awg briefing --file $REL_PATH"
}
EOF
