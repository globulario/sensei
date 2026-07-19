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

set -euo pipefail

INPUT=$(cat)

# Extract the file parameter from the briefing call.
FILE_PATH=$(echo "$INPUT" | python3 -c "
import json, sys
data = json.load(sys.stdin)
inp = data.get('tool_input', {})
print(inp.get('file', ''))
" 2>/dev/null || echo "")

if [ -z "$FILE_PATH" ]; then
    exit 0  # Task-only briefing — no file marker needed.
fi

# Resolve to absolute path.
if [[ "$FILE_PATH" != /* ]]; then
    FILE_PATH="$(pwd)/$FILE_PATH"
fi
FILE_PATH=$(realpath -m "$FILE_PATH" 2>/dev/null || echo "$FILE_PATH")

# Create marker file.
SESSION_ID="${CLAUDE_SESSION_ID:-default}"
MARKER_DIR="/tmp/sensei-briefings/$SESSION_ID"
mkdir -p "$MARKER_DIR"
PATH_HASH=$(echo -n "$FILE_PATH" | sha256sum | cut -d' ' -f1)
touch "$MARKER_DIR/$PATH_HASH"
