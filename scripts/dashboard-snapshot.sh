#!/usr/bin/env bash
# dashboard-snapshot.sh — fetch the read-only architecture projections that
# back the static GitHub Pages dashboard (dashboard/) and write them as JSON
# into dashboard/snapshot/.
#
# Precondition: a `sensei serve -no-seed` instance is already running and its
# graph already built (the .github/workflows/dashboard.yml workflow does this
# the same way scripts/smoke-cold-start.sh does — build from source, health
# poll, `sensei build -strict -all`). This script owns only the RPC fetch +
# JSON assembly, not the server lifecycle.
#
# Uses grpcurl against the server's gRPC reflection (no compiled client or
# vendored .proto needed — golang/server/service.go registers reflection and
# golang/server/auth.go exempts it from auth; with no token configured the
# whole server is unauthenticated, matching an ephemeral CI-local instance).
#
# Domain scoping: this repo's docs/awareness/*.yaml carries no domain tags, so
# the build is untagged (`sensei build -all`, no --repo) and every request
# below passes domain="" — resolveControlScope auto-resolves the graph's sole
# domain. See the "Domain scoping" decision in the dashboard design plan.
#
# Field naming: grpcurl's dynamic-message JSON marshaling emits lowerCamelCase
# (protojson's default, confirmed by a real local run — NOT snake_case, which
# was the working assumption during design). The forked dashboard JS
# (dashboard/controlPanel.js) expects the same snake_case field names the VS
# Code extension's grpcClient.ts uses (it configures @grpc/proto-loader with
# keepCase:true). Every response below is therefore piped through a recursive
# camelCase->snake_case jq conversion before anything else touches it.
#
# Fails closed: every JSON file is written to a `.tmp`-suffixed name first and
# renamed to its final name only after every fetch step below succeeds, so a
# mid-script failure can never leave behind a snapshot/ directory that looks
# complete but isn't.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GRPC_PORT="${GRPC_PORT:?GRPC_PORT must be set to the running sensei serve gRPC port}"
GRPC="127.0.0.1:${GRPC_PORT}"
SVC="globular.awareness_graph.AwarenessGraph"
REPO_IDENTITY="github.com/globulario/sensei"
OUT="${1:-${REPO_ROOT}/dashboard/snapshot}"
SERVE_LOG="${SERVE_LOG:-}"
MAX_PAGES=500

fail() {
  echo "DASHBOARD-SNAPSHOT FAIL: $*" >&2
  [[ -n "${SERVE_LOG}" && -f "${SERVE_LOG}" ]] && { echo "---- serve.log ----" >&2; cat "${SERVE_LOG}" >&2; }
  exit 1
}

# Recursive lowerCamelCase -> snake_case key conversion, applied to every RPC
# response (see "Field naming" above). "schemaVersion" -> "schema_version",
# "digestSha256" -> "digest_sha256", etc.
CAMEL_TO_SNAKE_JQ='
def to_snake: gsub("(?<a>[a-z0-9])(?<b>[A-Z])"; "\(.a)_\(.b)") | ascii_downcase;
def camel_to_snake:
  if type == "object" then with_entries(.key |= to_snake | .value |= camel_to_snake)
  elif type == "array" then map(camel_to_snake)
  else . end;
camel_to_snake
'

call() {
  # call <method> <json-request-body>
  grpcurl -plaintext -max-time 15 -d "$2" "${GRPC}" "${SVC}/$1" | jq "${CAMEL_TO_SNAKE_JQ}" \
    || fail "RPC $1 failed"
}

command -v grpcurl >/dev/null 2>&1 || fail "grpcurl not found on PATH"
command -v jq >/dev/null 2>&1 || fail "jq not found on PATH"

rm -rf "${OUT}"
mkdir -p "${OUT}"

echo "==> reflection readiness probe"
grpcurl -plaintext -max-time 10 "${GRPC}" list >/dev/null || fail "reflection not queryable"

echo "==> GetOntologyNavigationDescriptor"
call GetOntologyNavigationDescriptor '{}' | jq '.descriptor // {}' > "${OUT}/.navigation.json.tmp"

echo "==> GetArchitectureControlSnapshot"
control_req=$(jq -n --arg repo "${REPO_IDENTITY}" '{repository_identity: $repo, domain: ""}')
call GetArchitectureControlSnapshot "${control_req}" | jq '.snapshot // {}' > "${OUT}/.control_snapshot.json.tmp"

echo "==> ListArchitectureArtifacts (paginating to exhaustion)"
cursor=""
page=0
pages_file="${OUT}/.pages.jsonl.tmp"
: > "${pages_file}"
truncated=false
while :; do
  page=$((page + 1))
  [[ "${page}" -gt "${MAX_PAGES}" ]] && fail "ListArchitectureArtifacts pagination did not terminate after ${MAX_PAGES} pages"
  req=$(jq -n --arg repo "${REPO_IDENTITY}" --arg c "${cursor}" \
    '{repository_identity: $repo, domain: "", page_size: 250, cursor: $c}')
  resp=$(call ListArchitectureArtifacts "${req}")
  echo "${resp}" | jq -c '.index.page // [] | .[]' >> "${pages_file}"
  if [[ "$(echo "${resp}" | jq -r '.index.truncated // false')" == "true" ]]; then
    truncated=true
  fi
  cursor=$(echo "${resp}" | jq -r '.index.next_cursor // ""')
  [[ -z "${cursor}" ]] && break
done
jq -s --argjson truncated "${truncated}" \
  '{page: ., next_cursor: "", truncated: $truncated}' "${pages_file}" > "${OUT}/.artifacts.json.tmp"
rm -f "${pages_file}"

echo "==> GetArchitectureArtifactState (bounded to top_attention's affected_artifacts)"
iris=$(jq -r '[.top_attention[]?.affected_artifacts[]?] | unique | .[]' "${OUT}/.control_snapshot.json.tmp")
states_file="${OUT}/.attention_states.jsonl.tmp"
: > "${states_file}"
if [[ -n "${iris}" ]]; then
  while IFS= read -r iri; do
    [[ -z "${iri}" ]] && continue
    req=$(jq -n --arg repo "${REPO_IDENTITY}" --arg iri "${iri}" \
      '{repository_identity: $repo, domain: "", node_iri: $iri}')
    resp=$(call GetArchitectureArtifactState "${req}")
    echo "${resp}" | jq -c --arg iri "${iri}" '{key: $iri, value: (.state // {})}' >> "${states_file}"
  done <<< "${iris}"
fi
jq -s 'map({(.key): .value}) | add // {}' "${states_file}" > "${OUT}/.attention_states.json.tmp"
rm -f "${states_file}"

echo "==> stamping meta.json"
COMMIT="${GITHUB_SHA:-$(cd "${REPO_ROOT}" && git rev-parse HEAD)}"
GENERATED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
jq -n --arg commit "${COMMIT}" --arg ts "${GENERATED_AT}" \
  '{commit: $commit, generated_at: $ts}' > "${OUT}/.meta.json.tmp"

echo "==> every fetch succeeded — publishing final filenames"
for f in "${OUT}"/.*.json.tmp; do
  mv "${f}" "${OUT}/$(basename "${f}" .tmp | sed 's/^\.//')"
done

echo "DASHBOARD-SNAPSHOT PASS: wrote $(find "${OUT}" -maxdepth 1 -type f | wc -l) file(s) to ${OUT}"
