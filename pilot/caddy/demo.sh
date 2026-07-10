#!/usr/bin/env bash
# Pilot demo: prove the repo-scoped lifecycle end-to-end for caddy, in an
# ISOLATED authoritative graph (never the shared/live store), with no
# cross-domain leakage.
#
#   promote 1 reviewed Caddy candidate
#     → rebuild one combined authoritative artifact (home graph + pilot/caddy)
#     → serve through AWG
#     → brief a real Caddy file   → the Caddy rule APPEARS
#     → brief a real Globular file → the Caddy rule is ABSENT
#
# Usage: pilot/caddy/demo.sh   (run from the awareness-graph repo root)
set -euo pipefail

AG_REPO="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$AG_REPO"

AWG_BIN="${AWG_BIN:-/tmp/awg}"
AWG_PORT="${AWG_PORT:-10121}"
OXI_PORT="${OXI_PORT:-7901}"
DATA_DIR="${DATA_DIR:-/tmp/awg-pilot-demo}"
OXI_URL="http://localhost:${OXI_PORT}/store?default"
GRAPH_MARKER_FILE="${DATA_DIR}/graph-authority.json"
CANDIDATE_ID="caddy.reverseproxy.forwardauth_errf_preserves_location"
REPO="github.com/caddyserver/caddy"
CADDY_FILE="modules/caddyhttp/reverseproxy/forwardauth/caddyfile.go"
GLOBULAR_FILE="${GLOBULAR_FILE:-golang/repository/repository_server/repository_server.go}"

[ -x "$AWG_BIN" ] || { echo "build awg first: go build -o $AWG_BIN ./cmd/awg"; exit 1; }

# Fresh isolated store every run.
rm -rf "$DATA_DIR"; mkdir -p "$DATA_DIR"

echo "== starting isolated AWG (gRPC :$AWG_PORT, Oxigraph :$OXI_PORT, data $DATA_DIR) =="
"$AWG_BIN" serve --addr ":$AWG_PORT" --oxigraph-bind "127.0.0.1:$OXI_PORT" \
  --data "$DATA_DIR" --no-seed --graph-marker-file "$GRAPH_MARKER_FILE" \
  >/tmp/awg-pilot-demo.log 2>&1 &
SERVE_PID=$!
trap 'kill $SERVE_PID 2>/dev/null || true' EXIT

# Wait for Oxigraph; the pilot loads its own authoritative combined artifact.
for i in $(seq 1 40); do
  if curl -s -m2 -o /dev/null -w '%{http_code}' "http://localhost:${OXI_PORT}/query" \
       --data-urlencode 'query=ASK { ?s ?p ?o }' 2>/dev/null | grep -q 200; then break; fi
  sleep 0.5
done

echo
echo "== promote the reviewed Caddy candidate (writes the domain-tagged canonical rule) =="
if grep -q "id: $CANDIDATE_ID" pilot/caddy/candidates/*.yaml 2>/dev/null; then
  # --no-rebuild: write the canonical YAML only; the load happens once, below.
  "$AWG_BIN" promote --repo "$REPO" --ag-repo "$AG_REPO" --no-rebuild "$CANDIDATE_ID"
else
  echo "(candidate already promoted to pilot/caddy/invariants.yaml — replaying the load)"
fi

echo
echo "== build and load one authoritative combined graph (home + pilot/caddy) =="
BUILD_ARGS=(
  build
  --store-url "$OXI_URL"
  --graph-marker-file "$GRAPH_MARKER_FILE"
  --graph-transaction-file "${GRAPH_MARKER_FILE%.json}.transaction.tsv"
  --ag-repo "$AG_REPO"
)
for dir in \
  docs/awareness \
  docs/awareness/generated \
  eval/multi-swe-bench/contracts \
  eval/multi-swe-bench/notes/learning_events \
  pilot/caddy
do
  if [ -d "$dir" ]; then
    BUILD_ARGS+=(--input "$dir")
  fi
done
"$AWG_BIN" "${BUILD_ARGS[@]}" >/tmp/awg-pilot-build.log
echo "loaded authoritative graph into $OXI_URL with runtime marker $GRAPH_MARKER_FILE"

echo
echo "== brief the real Caddy file (scope=$REPO) — expect the Caddy rule =="
"$AWG_BIN" briefing --addr "localhost:$AWG_PORT" --file "$CADDY_FILE" --domain "$REPO" | tee /tmp/caddy_brief.txt

echo
echo "== brief a real Globular file — expect NO Caddy rule =="
"$AWG_BIN" briefing --addr "localhost:$AWG_PORT" --file "$GLOBULAR_FILE" | tee /tmp/globular_brief.txt

echo
echo "== isolation check =="
if grep -qi 'dispenser.Errf\|forwardauth\|caddy' /tmp/caddy_brief.txt; then
  echo "PASS: Caddy rule present in Caddy briefing"
else
  echo "FAIL: Caddy rule missing from Caddy briefing"; exit 1
fi
if grep -qi 'dispenser.Errf\|forwardauth_errf\|caddyserver' /tmp/globular_brief.txt; then
  echo "FAIL: Caddy rule LEAKED into Globular briefing"; exit 1
else
  echo "PASS: Caddy rule absent from Globular briefing — no cross-domain leakage"
fi

# ── Provenance visible in the Caddy briefing ────────────────────────────────
echo
echo "== provenance check (Caddy briefing must explain the rule's origin) =="
if grep -qi 'Provenance' /tmp/caddy_brief.txt && grep -qi 'load-bearing' /tmp/caddy_brief.txt; then
  echo "PASS: provenance (origin/review/bundle/citations) shown for the promoted rule"
else
  echo "FAIL: provenance block missing from Caddy briefing"; exit 1
fi

# ── Warning-level enforcement via EditCheck ─────────────────────────────────
BAD_EDIT='func (h *Handler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
    return fmt.Errorf("cannot re-declare uri: %s", uri)
}'
GOOD_EDIT='func (h *Handler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
    return d.Errf("cannot re-declare uri: %s", uri) // dispenser.Errf(
}'

echo
echo "== edit-check: BAD shape (fmt.Errorf) on the Caddy file under Caddy domain — expect WARNING =="
"$AWG_BIN" edit-check --addr "localhost:$AWG_PORT" --file "$CADDY_FILE" --domain "$REPO" --content "$BAD_EDIT" | tee /tmp/editcheck_bad.txt

echo
echo "== edit-check: COMPLIANT shape (dispenser.Errf) on the Caddy file — expect NO warning =="
"$AWG_BIN" edit-check --addr "localhost:$AWG_PORT" --file "$CADDY_FILE" --domain "$REPO" --content "$GOOD_EDIT" | tee /tmp/editcheck_good.txt

echo
echo "== edit-check: BAD shape on a Globular file — expect NO Caddy warning (no leak) =="
"$AWG_BIN" edit-check --addr "localhost:$AWG_PORT" --file "$GLOBULAR_FILE" --content "$BAD_EDIT" | tee /tmp/editcheck_globular.txt

echo
echo "== edit-check isolation =="
if grep -qi 'forwardauth_errf_preserves_location' /tmp/editcheck_bad.txt; then
  echo "PASS: bad fmt.Errorf shape warns under Caddy domain"
else
  echo "FAIL: bad shape did not warn"; exit 1
fi
if grep -q 'warnings: 0' /tmp/editcheck_good.txt; then
  echo "PASS: compliant dispenser.Errf shape does not warn"
else
  echo "FAIL: compliant shape unexpectedly warned"; exit 1
fi
if grep -qi 'caddy' /tmp/editcheck_globular.txt; then
  echo "FAIL: Caddy warning LEAKED into a Globular file's edit-check"; exit 1
else
  echo "PASS: Caddy warning absent for Globular file — no cross-domain leakage"
fi

# ── Hard-gate v1 (DRY-RUN / soft-fail) over a git diff ──────────────────────
# The Caddy rule is marked enforcement: block, so a bad edit shows as
# WOULD-BLOCK. The gate is a dry run: it always exits 0 and blocks nothing.
echo
echo "== hard-gate dry-run: a throwaway repo edits the Caddy file to use fmt.Errorf =="
GATE_REPO="$(mktemp -d)"
mkdir -p "$GATE_REPO/modules/caddyhttp/reverseproxy/forwardauth"
GF="$GATE_REPO/modules/caddyhttp/reverseproxy/forwardauth/caddyfile.go"
printf 'package forwardauth\nfunc x(d *Dispenser) error { return dispenser.Errf("ok") }\n' > "$GF"
git -C "$GATE_REPO" init -q
git -C "$GATE_REPO" add -A
git -C "$GATE_REPO" -c user.email=demo@demo -c user.name=demo commit -qm init
printf 'package forwardauth\nfunc x(d *Dispenser) error { return fmt.Errorf("bad") }\n' > "$GF"

"$AWG_BIN" gate --addr "localhost:$AWG_PORT" --repo-root "$GATE_REPO" --diff HEAD \
  --domain "$REPO" | tee /tmp/gate_out.txt
GATE_RC=${PIPESTATUS[0]}
rm -rf "$GATE_REPO"

echo
echo "== hard-gate isolation =="
if grep -q 'WOULD-BLOCK' /tmp/gate_out.txt; then
  echo "PASS: bad fmt.Errorf edit is reported as WOULD-BLOCK"
else
  echo "FAIL: gate did not flag the bad edit"; exit 1
fi
if [ "$GATE_RC" = "0" ]; then
  echo "PASS: gate exited 0 — dry-run / soft-fail, nothing blocked"
else
  echo "FAIL: gate exited $GATE_RC — v1 must never fail the build"; exit 1
fi

# ── CI report-only wrapper (scripts/awg-gate-report.sh) ─────────────────────
# Same dry-run, packaged for CI: explicit domain, canonical summary line, and
# always exit 0 — even when AWG is unavailable (degraded / fail-open).
AG_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
RR="$(mktemp -d)"
mkdir -p "$RR/modules/caddyhttp/reverseproxy/forwardauth"
RF="$RR/modules/caddyhttp/reverseproxy/forwardauth/caddyfile.go"
printf 'package forwardauth\nfunc x(d *Dispenser) error { return dispenser.Errf("ok") }\n' > "$RF"
git -C "$RR" init -q; git -C "$RR" add -A
git -C "$RR" -c user.email=demo@demo -c user.name=demo commit -qm init
printf 'package forwardauth\nfunc x(d *Dispenser) error { return fmt.Errorf("bad") }\n' > "$RF"

echo
echo "== CI report-only wrapper: explicit domain, would-block, exit 0 =="
AWG_BIN="$AWG_BIN" AWG_ADDR="localhost:$AWG_PORT" AWG_GATE_DOMAIN="$REPO" \
  AWG_GATE_REPO_ROOT="$RR" AWG_GATE_DIFF=HEAD \
  bash "$AG_ROOT/scripts/awg-gate-report.sh" | tee /tmp/report_ok.txt
RPT_RC=${PIPESTATUS[0]}

echo
echo "== CI report-only wrapper: AWG unavailable → degraded, exit 0 =="
AWG_BIN="$AWG_BIN" AWG_ADDR="localhost:59999" AWG_GATE_DOMAIN="$REPO" \
  AWG_GATE_REPO_ROOT="$RR" AWG_GATE_DIFF=HEAD \
  bash "$AG_ROOT/scripts/awg-gate-report.sh" | tee /tmp/report_degraded.txt
DEG_RC=${PIPESTATUS[0]}
rm -rf "$RR"

echo
echo "== CI report-only checks =="
grep -q 'AWG gate report-only: 0 hard failures,' /tmp/report_ok.txt \
  && echo "PASS: report has the canonical summary line" || { echo "FAIL: no summary line"; exit 1; }
grep -qE 'domain:|would-block:' /tmp/report_ok.txt \
  && echo "PASS: report includes domain + counts" || { echo "FAIL: missing domain/counts"; exit 1; }
grep -q 'WOULD-BLOCK' /tmp/report_ok.txt \
  && echo "PASS: would-block finding present in report" || { echo "FAIL: no would-block finding"; exit 1; }
[ "$RPT_RC" = "0" ] && echo "PASS: report-only exited 0 with would-block findings" \
  || { echo "FAIL: report-only exited $RPT_RC"; exit 1; }
grep -qi 'DEGRADED' /tmp/report_degraded.txt \
  && echo "PASS: AWG-unavailable run reported DEGRADED" || { echo "FAIL: not degraded"; exit 1; }
[ "$DEG_RC" = "0" ] && echo "PASS: degraded run exited 0 (fail-open)" \
  || { echo "FAIL: degraded run exited $DEG_RC"; exit 1; }
