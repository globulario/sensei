#!/bin/bash
# Sensei push-briefing hook for Claude Code.
#
# PreToolUse hook on Edit|Write|MultiEdit: fetches a compact briefing for the
# file about to be edited and *pushes* it to the agent as additionalContext —
# the invariants, forbidden fixes, and failure modes that govern the file — so
# the agent sees them before it writes, without having to call briefing itself.
#
# This is the "push" complement to the two "pull/block" hooks:
#   - enforce-briefing.sh  BLOCKS a high-risk edit until you *asked* for a briefing
#   - edit-check-guard.sh   BLOCKS a write that actually *violates* a rule
#   - push-briefing.sh      HANDS you the briefing up front (never blocks)
# Run push-briefing alongside edit-check-guard for consult-and-comply where the
# consult is delivered, not demanded.
#
# All logic lives in `sensei edit-brief` (tested Go), so this hook stays a thin,
# dependency-light wrapper — the same discipline as edit-check-guard.sh.
#
# Never blocks: it always emits an "allow" decision (with context) or nothing.
# Knobs (all optional, read by `sensei edit-brief`):
#   AWG_EDIT_BRIEF_DEPTH   briefing depth (default agent_compact; also compact|standard|deep)
#   AWG_ADDR               gRPC address (default localhost:10120)
#   AWG_DOMAIN             domain scope, for a multi-domain graph
#
# Install: place in .claude/hooks/ and configure in .claude/settings.json:
#   "PreToolUse": [{
#     "matcher": "Edit|Write|MultiEdit",
#     "hooks": [{"type": "command", "command": ".claude/hooks/push-briefing.sh", "timeout": 10}]
#   }]
#
# Fails OPEN and SILENT: if the CLI is absent, the file is outside the project,
# nothing anchors to it, or the server is unreachable, it emits nothing and the
# edit proceeds unannotated.

set -euo pipefail

# Resolve the Sensei CLI (falls back to the deprecated 'awg' alias). If neither
# is on PATH, stay out of the way.
BIN="$(command -v sensei || command -v awg || true)"
[ -n "$BIN" ] || exit 0

exec "$BIN" edit-brief
