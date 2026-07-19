#!/bin/bash
# Sensei edit-check-guard hook for Claude Code.
#
# PreToolUse hook on Edit|Write|MultiEdit: runs the *proposed edit content*
# through edit_check and BLOCKS the edit when it would introduce a forbidden-fix
# shape or trip a high-severity rule; lower-severity advisories are surfaced but
# allowed.
#
# This is the compliance gate that complements enforce-briefing.sh. That hook
# asks "did you *look* (call briefing) before editing a high-risk file?"; this
# one asks "does what you're about to *write* actually violate a rule?". Only
# together do you get consult-then-comply.
#
# All logic lives in `sensei edit-guard` (tested Go), so this hook stays a thin,
# dependency-light wrapper — the same discipline as feedback-reminder.sh.
#
# Advisory contract: AWG's edit_check RPC is warning-only and never blocks —
# that contract is intact. This hook is an opt-in local enforcement layer.
# Knobs (all optional, read by `sensei edit-guard`):
#   AWG_EDIT_CHECK_ADVISORY=1       warn-only: surface on stderr, never block.
#   AWG_EDIT_CHECK_BLOCK_SEVERITY   comma list of blocking severities
#                                   (default: "critical,high"; a forbidden-fix
#                                   class always blocks unless ADVISORY=1).
#   AWG_ADDR                        gRPC address (default localhost:10120).
#   AWG_DOMAIN                      domain scope, for a multi-domain graph.
#
# Install: place in .claude/hooks/ and configure in .claude/settings.json:
#   "PreToolUse": [{
#     "matcher": "Edit|Write|MultiEdit",
#     "hooks": [{"type": "command", "command": ".claude/hooks/edit-check-guard.sh", "timeout": 10}]
#   }]
#
# Fails OPEN: if the CLI is absent it allows the edit. `sensei edit-guard` itself also
# fails open on unparseable input, an out-of-project file, or an unreachable
# server — it only ever blocks on a real rule match.

set -euo pipefail

# Resolve the Sensei CLI (falls back to the deprecated 'awg' alias). If neither
# is on PATH, stay out of the way.
BIN="$(command -v sensei || command -v awg || true)"
[ -n "$BIN" ] || exit 0

exec "$BIN" edit-guard
