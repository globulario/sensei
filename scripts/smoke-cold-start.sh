#!/usr/bin/env bash
# smoke-cold-start.sh — prove the stranger path works end to end.
#
# Simulates a brand-new, non-Globular project:
#   init → first invariant (linked to a pack meta-principle) →
#   serve -no-seed → build → briefing returns the PROJECT invariant
#   and does NOT contain Globular-only seeded knowledge.
#
# CI-safe: builds its own binaries, uses its own ports, kills by PID
# (never pkill -f), cleans up on every exit path. Independent of the
# caller's cwd — everything resolves from the repo root.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d /tmp/awg-coldstart-smoke.XXXXXX)"
GRPC_PORT="${AWG_SMOKE_GRPC_PORT:-10199}"
OXI_PORT="${AWG_SMOKE_OXI_PORT:-7899}"
SERVE_PID=""

port_busy() { ss -ltn 2>/dev/null | grep -q ":$1 " ; }

cleanup() {
  local rc=$?
  # `sensei serve` traps SIGTERM and stops its oxigraph + server children
  # cleanly, so a plain kill of the PID we own tears down the whole tree.
  if [[ -n "${SERVE_PID}" ]] && kill -0 "${SERVE_PID}" 2>/dev/null; then
    kill "${SERVE_PID}" 2>/dev/null || true
    for _ in $(seq 1 10); do kill -0 "${SERVE_PID}" 2>/dev/null || break; sleep 0.5; done
    kill -9 "${SERVE_PID}" 2>/dev/null || true
  fi
  # Backstop: free our ports if a child outlived the parent, by PID only.
  # The pipeline returns non-zero when the port is already free (grep finds
  # nothing) — guard the assignment so `set -e -o pipefail` cannot turn a
  # clean teardown into a false failure.
  for prt in "${GRPC_PORT}" "${OXI_PORT}"; do
    pid=$(ss -ltnp 2>/dev/null | grep ":${prt} " | grep -oP "pid=\\K[0-9]+" | head -1) || true
    [[ -n "${pid:-}" ]] && kill "${pid}" 2>/dev/null || true
  done
  rm -rf "${WORK}" 2>/dev/null || true
  return "${rc}"
}
trap cleanup EXIT

fail() { echo "SMOKE FAIL: $*" >&2; exit 1; }

echo "==> building sensei + server"
( cd "${REPO_ROOT}" && go build -o "${WORK}/sensei" ./cmd/awg \
                    && go build -o "${WORK}/awareness-graph" ./golang/server )

# serve discovers the server + oxigraph next to the sensei binary.
if [[ -x "${REPO_ROOT}/bin/oxigraph" ]]; then
  cp "${REPO_ROOT}/bin/oxigraph" "${WORK}/oxigraph"
elif command -v oxigraph >/dev/null 2>&1; then
  cp "$(command -v oxigraph)" "${WORK}/oxigraph"
else
  fail "oxigraph binary not found (need ${REPO_ROOT}/bin/oxigraph or PATH)"
fi

echo "==> scaffolding stranger project"
PROJ="${WORK}/project"
mkdir -p "${PROJ}/src"
( cd "${PROJ}" && git init -q . && "${WORK}/sensei" init -dir . >/dev/null )

[[ -f "${PROJ}/docs/awareness/meta_principles.yaml" ]] || fail "init did not install the principle pack"
PACK_COUNT=$(python3 -c "import yaml;print(len(yaml.safe_load(open('${PROJ}/docs/awareness/meta_principles.yaml'))['invariants']))")
[[ "${PACK_COUNT}" -ge 80 ]] || fail "pack has ${PACK_COUNT} principles, want >= 80"

cat > "${PROJ}/src/payment_processor.py" <<'PY'
def mark_paid(order, cache):
    cache[order.id] = "paid"
    return True
PY

cat >> "${PROJ}/docs/awareness/invariants.yaml" <<'YAML'

  - id: payments.paid_state_requires_processor_confirmation
    title: An order records as paid only after processor confirmation — never from local cache writes
    severity: critical
    status: active
    protects:
      files:
        - src/payment_processor.py
    related_invariants:
      - meta.storage_is_not_semantic_authority
YAML

for prt in "${GRPC_PORT}" "${OXI_PORT}"; do
  port_busy "${prt}" && fail "port ${prt} already in use — set AWG_SMOKE_GRPC_PORT / AWG_SMOKE_OXI_PORT"
done

echo "==> serve -no-seed (grpc :${GRPC_PORT}, oxigraph :${OXI_PORT})"
( cd "${PROJ}" && exec "${WORK}/sensei" serve \
    -addr ":${GRPC_PORT}" \
    -oxigraph-bind "127.0.0.1:${OXI_PORT}" \
    -data "${PROJ}/.sensei/oxigraph" \
    -no-seed > "${WORK}/serve.log" 2>&1 ) &
SERVE_PID=$!

for i in $(seq 1 30); do
  curl -fsS -o /dev/null -X POST -H "Content-Type: application/sparql-query" \
    --data 'ASK {}' "http://127.0.0.1:${OXI_PORT}/query" 2>/dev/null && break
  sleep 1
  [[ $i -eq 30 ]] && { cat "${WORK}/serve.log" >&2; fail "oxigraph did not come up"; }
done
for i in $(seq 1 30); do
  grep -q "listening on" "${WORK}/serve.log" 2>/dev/null && break
  sleep 1
  [[ $i -eq 30 ]] && { cat "${WORK}/serve.log" >&2; fail "gRPC server did not come up"; }
done
grep -q -- "-no-seed" "${WORK}/serve.log" || { cat "${WORK}/serve.log" >&2; fail "server did not acknowledge -no-seed"; }

echo "==> build"
( cd "${PROJ}" && "${WORK}/sensei" build -strict -all \
    -input docs/awareness \
    -store-url "http://127.0.0.1:${OXI_PORT}/store?default" ) \
  || fail "sensei build -strict failed"

echo "==> briefing"
BRIEFING=$( cd "${PROJ}" && "${WORK}/sensei" briefing -addr "localhost:${GRPC_PORT}" \
    -file src/payment_processor.py -task "refactor mark_paid" )
echo "${BRIEFING}" | grep -q "payments.paid_state_requires_processor_confirmation" \
  || { echo "${BRIEFING}" >&2; fail "briefing missing the project invariant"; }

echo "==> negative checks (no Globular-only seed leakage)"
echo "${BRIEFING}" | grep -qiE "scylla|minio|globular-only" \
  && fail "briefing leaked Globular content"
# A Globular-seed-only invariant must NOT resolve in this project's graph.
LEAK=$( "${WORK}/sensei" resolve -addr "localhost:${GRPC_PORT}" \
    invariant repository.fallback_requires_manifest_and_checksum 2>&1 || true )
echo "${LEAK}" | grep -qi "not found" \
  || fail "Globular seed invariant resolved in a cold-start graph: ${LEAK}"
# The portable pack MUST resolve.
"${WORK}/sensei" resolve -addr "localhost:${GRPC_PORT}" \
    invariant meta.ui.screen_claim_must_bind_to_authority >/dev/null \
  || fail "pack principle did not resolve"

echo "SMOKE PASS: init → serve -no-seed → build -strict → briefing all green"
