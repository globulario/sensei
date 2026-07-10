#!/usr/bin/env bash
# examples/orders-money-scar/run.sh — reproducible external-proof of the AWG
# cold-start loop on a fresh, non-Globular Go repo carrying one money scar.
#
# Proves, end to end, with no network and no /home path dependency:
#   [1] awg init installs the portable principle pack + scaffolds awareness files
#   [2] a human-authored scar protects money arithmetic (integer cents, never float)
#   [3] awg build -strict validates the project graph
#   [4] a Globular-only invariant does NOT resolve (no seed leak)
#   [5] a task briefing for "add a 10% discount" surfaces the critical scar
#   [6-8] the required Go test shows float (899c) diverging from integer (900c)
#
# AWG does not make the decision — it surfaces human-approved project truth to
# the agent before the edit. The agent still writes the code.
#
# Prereqs: go, python3, and an oxigraph binary (repo bin/oxigraph or on PATH;
# CI provides it via scripts/fetch-oxigraph.sh). Override ports with
# AWG_DEMO_GRPC_PORT / AWG_DEMO_OXI_PORT.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${HERE}/../.." && pwd)"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/awg-money-scar.XXXXXX")"
GRPC_PORT="${AWG_DEMO_GRPC_PORT:-10288}"
OXI_PORT="${AWG_DEMO_OXI_PORT:-7888}"
SERVE_PID=""

port_busy() { ss -ltn 2>/dev/null | grep -q ":$1 " ; }
fail() { echo "FAIL: $*" >&2; exit 1; }
cleanup() {
  local rc=$?
  if [[ -n "${SERVE_PID}" ]] && kill -0 "${SERVE_PID}" 2>/dev/null; then
    kill "${SERVE_PID}" 2>/dev/null || true
    for _ in $(seq 1 10); do kill -0 "${SERVE_PID}" 2>/dev/null || break; sleep 0.5; done
    kill -9 "${SERVE_PID}" 2>/dev/null || true
  fi
  for prt in "${GRPC_PORT}" "${OXI_PORT}"; do
    pid=$(ss -ltnp 2>/dev/null | grep ":${prt} " | grep -oP "pid=\K[0-9]+" | head -1) || true
    [[ -n "${pid:-}" ]] && kill "${pid}" 2>/dev/null || true
  done
  rm -rf "${WORK}" 2>/dev/null || true
  return "${rc}"
}
trap cleanup EXIT

echo "==> building awg + server (from ${REPO_ROOT})"
( cd "${REPO_ROOT}" && go build -o "${WORK}/awg" ./cmd/awg \
                    && go build -o "${WORK}/awareness-graph" ./golang/server )
if   [[ -x "${REPO_ROOT}/bin/oxigraph" ]]; then cp "${REPO_ROOT}/bin/oxigraph" "${WORK}/oxigraph"
elif command -v oxigraph >/dev/null 2>&1;  then cp "$(command -v oxigraph)"     "${WORK}/oxigraph"
else fail "oxigraph not found (need ${REPO_ROOT}/bin/oxigraph or oxigraph on PATH; CI: scripts/fetch-oxigraph.sh)"; fi
AWG="${WORK}/awg"

# Work on a COPY so the committed example dir is never mutated.
PROJ="${WORK}/project"
mkdir -p "${PROJ}"
cp -r "${HERE}/." "${PROJ}/"
rm -f "${PROJ}/run.sh" "${PROJ}/README.md"
( cd "${PROJ}" && git init -q . )

echo "==> [1] awg init (portable pack + scaffold)"
( cd "${PROJ}" && "${AWG}" init -dir . >/dev/null )
[[ -f "${PROJ}/docs/awareness/meta_principles.yaml" ]] || fail "init did not install the principle pack"
PACK=$(python3 -c "import yaml;print(len(yaml.safe_load(open('${PROJ}/docs/awareness/meta_principles.yaml'))['invariants']))")
[[ "${PACK}" -ge 80 ]] || fail "pack has ${PACK} principles (want >= 80)"
grep -q "money.amounts_are_integer_minor_units" "${PROJ}/docs/awareness/invariants.yaml" \
  || fail "[2] the human-authored money scar is missing from invariants.yaml"
echo "    portable pack: ${PACK} principles; scar present"

for prt in "${GRPC_PORT}" "${OXI_PORT}"; do
  port_busy "${prt}" && fail "port ${prt} in use — set AWG_DEMO_GRPC_PORT / AWG_DEMO_OXI_PORT"
done
echo "==> serve -no-seed (grpc :${GRPC_PORT}, oxigraph :${OXI_PORT})"
( cd "${PROJ}" && exec "${AWG}" serve -addr ":${GRPC_PORT}" \
    -oxigraph-bind "127.0.0.1:${OXI_PORT}" -data "${PROJ}/.awg/oxigraph" \
    -no-seed > "${WORK}/serve.log" 2>&1 ) &
SERVE_PID=$!
for i in $(seq 1 40); do
  curl -fsS -o /dev/null -X POST -H "Content-Type: application/sparql-query" \
    --data 'ASK {}' "http://127.0.0.1:${OXI_PORT}/query" 2>/dev/null && break
  sleep 1; [[ $i -eq 40 ]] && { cat "${WORK}/serve.log" >&2; fail "oxigraph did not start"; }
done
for i in $(seq 1 40); do grep -q "listening on" "${WORK}/serve.log" 2>/dev/null && break; sleep 1
  [[ $i -eq 40 ]] && { cat "${WORK}/serve.log" >&2; fail "gRPC server did not start"; }; done

echo "==> [3] awg build -strict"
( cd "${PROJ}" && "${AWG}" build -strict -input docs/awareness \
    -store-url "http://127.0.0.1:${OXI_PORT}/store?default" ) || fail "build -strict failed"

echo "==> [4] no Globular seed leak"
LEAK=$( "${AWG}" resolve -addr "localhost:${GRPC_PORT}" \
    invariant repository.fallback_requires_manifest_and_checksum 2>&1 || true )
echo "${LEAK}" | grep -qi "not found" || fail "a Globular-only invariant resolved in a cold-start graph: ${LEAK}"
echo "    Globular-only invariant: not found"

echo "==> [5] briefing surfaces the critical scar (task: add a 10% discount)"
BR=$( cd "${PROJ}" && "${AWG}" briefing -addr "localhost:${GRPC_PORT}" \
    -file pkg/orders/total.go -task "add a 10% discount to order totals" )
echo "${BR}" | grep -q "money.amounts_are_integer_minor_units" || { echo "${BR}" >&2; fail "briefing missing the scar"; }
echo "${BR}" | grep -qi "never float"                          || { echo "${BR}" >&2; fail "briefing missing the never-float rule"; }
echo "    briefing surfaced: [critical] money.amounts_are_integer_minor_units (never float)"

echo "==> [6-8] required test: float 899c vs integer 900c"
( cd "${PROJ}" && go test ./pkg/orders/ -run TestOrderTotal_DiscountStaysExactInteger -v ) \
  || fail "proof test failed"

echo
echo "PASS — cold-start money-scar proof green:"
echo "  init(pack=${PACK}) -> scar -> build -strict -> no-leak -> briefing -> float!=integer"
