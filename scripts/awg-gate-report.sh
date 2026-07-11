#!/usr/bin/env bash
# Report-only CI entrypoint for `sensei gate`.
#
# Resolves the repo's OWN domain explicitly (env or repo-local config), runs the
# dry-run gate over the PR diff, and ALWAYS exits 0. It never blocks a merge —
# it exists for visibility and false-positive observation only. No waiver
# parsing, no real enforcement, no server-side default-domain behaviour.
set -uo pipefail   # NOT -e: this script must never fail the PR.

AWG_BIN="${AWG_BIN:-awg}"
ADDR="${AWG_ADDR:-localhost:10120}"
ROOT="${AWG_GATE_REPO_ROOT:-.}"

# Diff range: explicit env, else the PR base (GitHub sets GITHUB_BASE_REF) ...
# HEAD, else working tree vs HEAD.
if [ -n "${AWG_GATE_DIFF:-}" ]; then
  RANGE="$AWG_GATE_DIFF"
elif [ -n "${GITHUB_BASE_REF:-}" ]; then
  RANGE="origin/${GITHUB_BASE_REF}...HEAD"
else
  RANGE="HEAD"
fi

# Domain: EXPLICIT only — from env, or a tiny repo-local config. We never rely on
# unscoped multi-domain behaviour, and never set a server-side default domain.
DOMAIN="${AWG_GATE_DOMAIN:-}"
CONF="$(cd "$(dirname "$0")" && pwd)/awg-gate.conf"
if [ -z "$DOMAIN" ] && [ -f "$CONF" ]; then
  # shellcheck disable=SC1090
  . "$CONF"
  DOMAIN="${AWG_GATE_DOMAIN:-}"
fi
if [ -z "$DOMAIN" ]; then
  echo "AWG gate (report-only, non-blocking) — SKIPPED"
  echo "  reason: no AWG_GATE_DOMAIN configured (set the env var or scripts/awg-gate.conf)"
  echo "AWG gate report-only: 0 hard failures, 0 would-block findings (degraded: no domain configured)"
  exit 0
fi

"$AWG_BIN" gate --report-only --diff "$RANGE" --domain "$DOMAIN" --addr "$ADDR" --repo-root "$ROOT"
# `sensei gate --report-only` already always exits 0; belt-and-suspenders for CI:
exit 0
