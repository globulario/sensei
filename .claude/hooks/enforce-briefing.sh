#!/bin/bash
# Sensei enforce-briefing hook for Claude Code.
#
# PreToolUse hook on Edit/Write/MultiEdit: blocks edits to protected files
# unless an awareness briefing was obtained first.
#
# Install: place in .claude/hooks/ and configure in .claude/settings.json:
#   "PreToolUse": [{
#     "matcher": "Edit|Write|MultiEdit",
#     "hooks": [{"type": "command", "command": ".claude/hooks/enforce-briefing.sh", "timeout": 10}]
#   }]
#
# Protection classification is delegated ENTIRELY to `sensei protection-check`
# (golang/architecture/protection) — the one canonical protection owner. This
# hook must never re-parse docs/awareness/high_risk_files.yaml itself or
# reimplement path matching; it is a thin transport adapter (contract §9).
#
# Unlike edit-check-guard.sh (which fails OPEN when the CLI is absent — a
# missing advisory check is not a safety regression), this hook fails CLOSED
# on classifier unavailability: an empty/missing manual registry must never
# be silently read as "nothing to protect" (contract §9.7, the exact
# bootstrap gap this repair closes).
#
# The record-briefing.sh PostToolUse hook creates the session marker file
# that this hook checks. The marker binds hash(repository root + repository
# domain + file path) — never hash(file path) alone — so a briefing obtained
# against the wrong loaded domain can never authorize an edit in this
# checkout (contract §3/§9 correction). The domain used here is the
# checkout's own resolved default (no explicit override — editing has no
# --domain flag), via the same `sensei repo-domain` resolver record-briefing.sh
# uses.

set -euo pipefail

block() {
    # $1: reason string
    printf '{\n  "decision": "block",\n  "reason": %s\n}\n' "$(printf '%s' "$1" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))')"
}

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

# Find project root (walk up to find a state-dir config — .sensei/config.yaml
# or the legacy .awg/config.yaml — or docs/awareness/).
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

# Resolve the Sensei CLI (falls back to the deprecated 'awg' alias).
BIN="$(command -v sensei || command -v awg || true)"
if [ -z "$BIN" ]; then
    block "Sensei: the sensei/awg binary is required to classify $REL_PATH for protection and is not on PATH. Install Sensei or add it to PATH before editing — an unavailable classifier is never treated as \"nothing to protect.\""
    exit 0
fi

RESOLVED_DOMAIN=$("$BIN" repo-domain --path "$PROJECT_ROOT" 2>/dev/null || echo "")

ERR_FILE=$(mktemp)
if ! CHECK_JSON=$("$BIN" protection-check --path "$PROJECT_ROOT" --file "$REL_PATH" --json 2>"$ERR_FILE"); then
    ERR_MSG=$(cat "$ERR_FILE" 2>/dev/null || echo "unknown error")
    rm -f "$ERR_FILE"
    block "Sensei: protection-check failed for $REL_PATH ($ERR_MSG) — a failed classification is never treated as safe. Run: sensei protection-check --file $REL_PATH"
    exit 0
fi
rm -f "$ERR_FILE"

read -r COVERAGE_STATUS PROTECTED PROVISIONAL <<< "$(echo "$CHECK_JSON" | python3 -c "
import json, sys
d = json.load(sys.stdin)
print(d.get('coverage_status',''), d.get('protected', False), d.get('provisional', False))
" 2>/dev/null || echo "PARSE_ERROR false false")" || true

if [ "$COVERAGE_STATUS" = "PARSE_ERROR" ] || [ -z "$COVERAGE_STATUS" ]; then
    block "Sensei: could not parse protection-check output for $REL_PATH — treating as DEGRADED. Run: sensei protection-check --file $REL_PATH"
    exit 0
fi

if [ "$COVERAGE_STATUS" = "DEGRADED" ]; then
    block "Sensei: protection coverage is DEGRADED for this repository — the effective protected set cannot be established safely. Run: sensei protection-status for details before editing $REL_PATH."
    exit 0
fi

if [ "$PROTECTED" != "True" ]; then
    exit 0  # Not classified as protected by current coverage.
fi

# Check for briefing marker (protected AND provisionally-protected files both
# require briefing — candidate-derived caution still means "consult Sensei").
# Keyed by (project root, resolved domain, file) — matching record-briefing.sh
# — never by file path alone.
SESSION_ID="${CLAUDE_SESSION_ID:-default}"
MARKER_DIR="/tmp/sensei-briefings/$SESSION_ID"
MARKER_KEY="$PROJECT_ROOT|$RESOLVED_DOMAIN|$REL_PATH"
PATH_HASH=$(printf '%s' "$MARKER_KEY" | sha256sum | cut -d' ' -f1)

if [ -f "$MARKER_DIR/$PATH_HASH" ]; then
    exit 0  # Briefing was obtained for this exact repository/domain/file.
fi

KIND="high-risk path"
if [ "$PROVISIONAL" = "True" ]; then
    KIND="provisionally protected path (candidate-derived caution)"
fi
DOMAIN_FLAG=""
if [ -n "$RESOLVED_DOMAIN" ]; then
    DOMAIN_FLAG=" --domain $RESOLVED_DOMAIN"
fi
block "Sensei: call awareness briefing for $REL_PATH before editing this $KIND. Run: sensei briefing --file $REL_PATH$DOMAIN_FLAG"
