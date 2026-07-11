#!/usr/bin/env bash
# smoke-local-bundle.sh — prove the extracted awg-local runtime bundle works.
#
# This simulates the prebuilt Linux bundle path:
#   build a local-runtime tarball → extract it → provide oxigraph →
#   run sensei serve -no-seed from the extracted bundle →
#   build a tiny project graph → confirm briefing returns the project invariant.
#
# CI-safe: builds into /tmp, uses dedicated ports, kills by PID only, and never
# touches the caller's checkout state beyond reading source files.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
mkdir -p "${REPO_ROOT}/.tmp"
WORK="$(mktemp -d "${REPO_ROOT}/.tmp/awg-bundle-smoke.XXXXXX")"
GRPC_PORT="${AWG_BUNDLE_SMOKE_GRPC_PORT:-10219}"
OXI_PORT="${AWG_BUNDLE_SMOKE_OXI_PORT:-7919}"
SERVE_PID=""
OXI_PID=""
export GOCACHE="${WORK}/gocache"

port_busy() { ss -ltn 2>/dev/null | grep -q ":$1 " ; }

cleanup() {
  local rc=$?
  if [[ -n "${SERVE_PID}" ]] && kill -0 "${SERVE_PID}" 2>/dev/null; then
    kill "${SERVE_PID}" 2>/dev/null || true
    for _ in $(seq 1 10); do kill -0 "${SERVE_PID}" 2>/dev/null || break; sleep 0.5; done
    kill -9 "${SERVE_PID}" 2>/dev/null || true
  fi
  if [[ -n "${OXI_PID}" ]] && kill -0 "${OXI_PID}" 2>/dev/null; then
    kill "${OXI_PID}" 2>/dev/null || true
    for _ in $(seq 1 10); do kill -0 "${OXI_PID}" 2>/dev/null || break; sleep 0.5; done
    kill -9 "${OXI_PID}" 2>/dev/null || true
  fi
  for prt in "${GRPC_PORT}" "${OXI_PORT}"; do
    pid=$(ss -ltnp 2>/dev/null | grep ":${prt} " | grep -oP "pid=\\K[0-9]+" | head -1) || true
    [[ -n "${pid:-}" ]] && kill "${pid}" 2>/dev/null || true
  done
  rm -rf "${WORK}" 2>/dev/null || true
  rmdir "${REPO_ROOT}/.tmp" 2>/dev/null || true
  return "${rc}"
}
trap cleanup EXIT

fail() { echo "BUNDLE SMOKE FAIL: $*" >&2; exit 1; }

for prt in "${GRPC_PORT}" "${OXI_PORT}"; do
  port_busy "${prt}" && fail "port ${prt} already in use — set AWG_BUNDLE_SMOKE_GRPC_PORT / AWG_BUNDLE_SMOKE_OXI_PORT"
done

echo "==> building release bundle binaries"
( cd "${REPO_ROOT}" && go build -o "${WORK}/build/bin/sensei" ./cmd/awg \
                    && go build -o "${WORK}/build/bin/awareness-mcp" ./cmd/awareness-mcp \
                    && go build -o "${WORK}/build/bin/awareness-graph" ./golang/server )

mkdir -p "${WORK}/bundle/bin" "${WORK}/bundle/scripts"
cp "${WORK}/build/bin/sensei" "${WORK}/bundle/bin/sensei"
cp "${WORK}/build/bin/awareness-mcp" "${WORK}/bundle/bin/awareness-mcp"
cp "${WORK}/build/bin/awareness-graph" "${WORK}/bundle/bin/awareness-graph"
cp "${REPO_ROOT}/scripts/fetch-oxigraph.sh" "${WORK}/bundle/scripts/fetch-oxigraph.sh"
cp "${REPO_ROOT}/scripts/install-awg-user-services.sh" "${WORK}/bundle/scripts/install-awg-user-services.sh"
chmod +x "${WORK}/bundle/scripts/fetch-oxigraph.sh" "${WORK}/bundle/scripts/install-awg-user-services.sh"

echo "==> creating and extracting awg-local bundle"
tar czf "${WORK}/awg-local_test_linux_amd64.tgz" -C "${WORK}/bundle" .
mkdir -p "${WORK}/extract"
tar -xzf "${WORK}/awg-local_test_linux_amd64.tgz" -C "${WORK}/extract"

[[ -x "${WORK}/extract/bin/sensei" ]] || fail "extracted bundle missing bin/sensei"
[[ -x "${WORK}/extract/bin/awareness-graph" ]] || fail "extracted bundle missing bin/awareness-graph"
[[ -f "${WORK}/extract/scripts/install-awg-user-services.sh" ]] || fail "extracted bundle missing install-awg-user-services.sh"

echo "==> preparing external oxigraph for extracted bundle"
if [[ -x "${REPO_ROOT}/bin/oxigraph" ]]; then
  cp "${REPO_ROOT}/bin/oxigraph" "${WORK}/extract/bin/oxigraph"
elif command -v oxigraph >/dev/null 2>&1; then
  cp "$(command -v oxigraph)" "${WORK}/extract/bin/oxigraph"
else
  fail "oxigraph binary not found (need ${REPO_ROOT}/bin/oxigraph or PATH)"
fi
chmod +x "${WORK}/extract/bin/oxigraph"

mkdir -p "${PROJ:-${WORK}/project}/.sensei/oxigraph"
"${WORK}/extract/bin/oxigraph" serve \
  --location "${WORK}/external-oxigraph" \
  --bind "127.0.0.1:${OXI_PORT}" > "${WORK}/oxigraph.log" 2>&1 &
OXI_PID="$!"
for i in $(seq 1 30); do
  curl -fsS -o /dev/null -X POST -H "Content-Type: application/sparql-query" \
    --data 'ASK {}' "http://127.0.0.1:${OXI_PORT}/query" 2>/dev/null && break
  sleep 1
  [[ $i -eq 30 ]] && { cat "${WORK}/oxigraph.log" >&2; fail "external oxigraph did not come up"; }
done

echo "==> scaffolding project with extracted awg"
PROJ="${WORK}/project"
mkdir -p "${PROJ}/src"
( cd "${PROJ}" && git init -q . && "${WORK}/extract/bin/sensei" init -dir . >/dev/null )

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

echo "==> serve from extracted bundle"
( cd "${PROJ}" && exec "${WORK}/extract/bin/sensei" serve \
    -addr ":${GRPC_PORT}" \
    -oxigraph-bind "127.0.0.1:${OXI_PORT}" \
    -no-oxigraph \
    -no-seed > "${WORK}/serve.log" 2>&1 ) &
SERVE_PID=$!
for i in $(seq 1 30); do
  grep -q "listening on" "${WORK}/serve.log" 2>/dev/null && break
  sleep 1
  [[ $i -eq 30 ]] && { cat "${WORK}/serve.log" >&2; fail "gRPC server did not come up"; }
done

echo "==> build with extracted awg"
( cd "${PROJ}" && "${WORK}/extract/bin/sensei" build -strict \
    -input docs/awareness \
    -store-url "http://127.0.0.1:${OXI_PORT}/store?default" ) \
  || fail "bundle sensei build -strict failed"

echo "==> briefing with extracted awg"
BRIEFING=$( cd "${PROJ}" && "${WORK}/extract/bin/sensei" briefing -addr "localhost:${GRPC_PORT}" \
    -file src/payment_processor.py -task "refactor mark_paid" )
echo "${BRIEFING}" | grep -q "payments.paid_state_requires_processor_confirmation" \
  || { echo "${BRIEFING}" >&2; fail "briefing missing the project invariant"; }

echo "BUNDLE SMOKE PASS: extracted awg-local bundle can serve, build, and brief"
