#!/usr/bin/env bash
# smoke-governance-publication.sh — prove the managed-governance publication
# path works end to end.
#
# Flow:
#   build sensei -> mint publisher key -> derive trust root -> build/sign/release
#   a governance pack -> bootstrap a fresh client from the published trust root
#   -> fetch + activate the pack against a local Oxigraph -> verify current
#   governance status.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
mkdir -p "${REPO_ROOT}/.tmp"
WORK="$(mktemp -d "${REPO_ROOT}/.tmp/awg-governance-smoke.XXXXXX")"
OXI_PORT="${AWG_GOVERNANCE_SMOKE_OXI_PORT:-7929}"
OXI_PID=""
export GOCACHE="${WORK}/gocache"
export GOMODCACHE="${WORK}/gomodcache"

port_busy() { ss -ltn 2>/dev/null | grep -q ":$1 " ; }

cleanup() {
  local rc=$?
  if [[ -n "${OXI_PID}" ]] && kill -0 "${OXI_PID}" 2>/dev/null; then
    kill "${OXI_PID}" 2>/dev/null || true
    for _ in $(seq 1 10); do kill -0 "${OXI_PID}" 2>/dev/null || break; sleep 0.5; done
    kill -9 "${OXI_PID}" 2>/dev/null || true
  fi
  pid=$(ss -ltnp 2>/dev/null | grep ":${OXI_PORT} " | grep -oP "pid=\\K[0-9]+" | head -1) || true
  [[ -n "${pid:-}" ]] && kill "${pid}" 2>/dev/null || true
  chmod -R u+w "${WORK}" 2>/dev/null || true
  rm -rf "${WORK}" 2>/dev/null || true
  rmdir "${REPO_ROOT}/.tmp" 2>/dev/null || true
  return "${rc}"
}
trap cleanup EXIT

fail() { echo "GOVERNANCE SMOKE FAIL: $*" >&2; exit 1; }

port_busy "${OXI_PORT}" && fail "port ${OXI_PORT} already in use — set AWG_GOVERNANCE_SMOKE_OXI_PORT"

echo "==> building awg"
( cd "${REPO_ROOT}" && go build -o "${WORK}/sensei" ./cmd/awg )

if [[ -x "${REPO_ROOT}/bin/oxigraph" ]]; then
  cp "${REPO_ROOT}/bin/oxigraph" "${WORK}/oxigraph"
elif command -v oxigraph >/dev/null 2>&1; then
  cp "$(command -v oxigraph)" "${WORK}/oxigraph"
else
  fail "oxigraph binary not found (need ${REPO_ROOT}/bin/oxigraph or PATH)"
fi
chmod +x "${WORK}/oxigraph"

echo "==> starting local oxigraph"
"${WORK}/oxigraph" serve \
  --location "${WORK}/oxigraph-data" \
  --bind "127.0.0.1:${OXI_PORT}" > "${WORK}/oxigraph.log" 2>&1 &
OXI_PID="$!"
for i in $(seq 1 30); do
  curl -fsS -o /dev/null -X POST -H "Content-Type: application/sparql-query" \
    --data 'ASK {}' "http://127.0.0.1:${OXI_PORT}/query" 2>/dev/null && break
  sleep 1
  [[ $i -eq 30 ]] && { cat "${WORK}/oxigraph.log" >&2; fail "oxigraph did not come up"; }
done

echo "==> creating managed-governance publication root"
printf '<https://example.test/principle/a> <https://example.test/p> "x" .\n' > "${WORK}/canonical.nt"
PACK_DIR="${WORK}/vendor/build/core.meta-principles/2026.06.25"
PUB_ROOT="${WORK}/vendor/published"
SIGNING_KEY="${WORK}/vendor/signing-key.json"
TRUST_ROOT="${WORK}/vendor/trusted-publishers.json"

"${WORK}/sensei" governance publish gen-key \
  --out "${SIGNING_KEY}" \
  --publisher-id "core@globular.io" \
  --key-id "core-2026-q3" >/dev/null

"${WORK}/sensei" governance publish trust-root \
  --signing-key "${SIGNING_KEY}" \
  --out "${TRUST_ROOT}" \
  --display-name "Globular Core" >/dev/null

"${WORK}/sensei" governance publish build \
  --input-nt "${WORK}/canonical.nt" \
  --out-dir "${PACK_DIR}" \
  --pack-id "core.meta-principles" \
  --pack-version "2026.06.25" \
  --publisher-id "core@globular.io" \
  --publisher-name "Globular Core" \
  --issued-at "2026-06-25T12:00:00Z" \
  --min-awg-version "0.0.0" \
  --key-id "core-2026-q3" >/dev/null

"${WORK}/sensei" governance publish sign \
  --signing-key "${SIGNING_KEY}" \
  "${PACK_DIR}" >/dev/null

"${WORK}/sensei" governance publish release \
  --trusted-keys "${TRUST_ROOT}" \
  --signing-key "${SIGNING_KEY}" \
  --publication-root "${PUB_ROOT}" \
  --channel stable \
  "${PACK_DIR}" >/dev/null

[[ -f "${PUB_ROOT}/trusted-publishers.json" ]] || fail "published root missing trusted-publishers.json"
[[ -f "${PUB_ROOT}/governance/index.json" ]] || fail "published root missing governance/index.json"

echo "==> bootstrapping fresh client from published root"
CLIENT="${WORK}/client"
mkdir -p "${CLIENT}"
( cd "${CLIENT}" && git init -q . && "${WORK}/sensei" init -dir . >/dev/null )
"${WORK}/sensei" governance init -project-root "${CLIENT}" >/dev/null
"${WORK}/sensei" governance trust fetch -project-root "${CLIENT}" --source "${PUB_ROOT}" >/dev/null
"${WORK}/sensei" governance trust add \
  -project-root "${CLIENT}" \
  --file "${CLIENT}/.sensei/governance/incoming/trusted-publishers.json" >/dev/null

echo "==> fetching and activating signed governance pack"
"${WORK}/sensei" governance fetch \
  -project-root "${CLIENT}" \
  -source "${PUB_ROOT}" \
  -pack-id "core.meta-principles" \
  -channel stable \
  -activate \
  -store-url "http://127.0.0.1:${OXI_PORT}/store?default" >/dev/null

STATUS="$("${WORK}/sensei" governance status -project-root "${CLIENT}")"
echo "${STATUS}" | grep -q "Managed mode:        true" || fail "managed mode not enabled"
echo "${STATUS}" | grep -q "Governance state:    current" || { echo "${STATUS}" >&2; fail "governance status not current"; }
echo "${STATUS}" | grep -q "Fetched state:       current" || { echo "${STATUS}" >&2; fail "fetched status not current"; }

echo "GOVERNANCE SMOKE PASS: publication root bootstraps trust, fetches, and activates a signed pack"
