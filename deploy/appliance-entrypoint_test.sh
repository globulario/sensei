#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
set -eu

root="$(mktemp -d)"
serve_pid=""
cleanup() {
  if [ -n "${serve_pid}" ] && kill -0 "${serve_pid}" 2>/dev/null; then
    kill -TERM "${serve_pid}" 2>/dev/null || true
    wait "${serve_pid}" 2>/dev/null || true
  fi
  rm -rf "${root}"
}
trap cleanup EXIT HUP INT TERM

bin_dir="${root}/bin"
workspace="${root}/workspace"
data_dir="${root}/data"
log_file="${root}/calls.log"
mkdir -p "${bin_dir}" "${workspace}/docs/awareness" "${data_dir}"
: > "${workspace}/docs/awareness/invariants.yaml"

cat > "${bin_dir}/sensei" <<'STUB'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "${SENSEI_TEST_LOG}"
case "${1:-}" in
  serve)
    trap 'exit 0' HUP INT TERM
    while :; do sleep 1; done
    ;;
  metadata)
    exit 0
    ;;
  build)
    marker=""
    transaction=""
    while [ "$#" -gt 0 ]; do
      case "$1" in
        -graph-marker-file) marker="$2"; shift 2 ;;
        -graph-transaction-file) transaction="$2"; shift 2 ;;
        *) shift ;;
      esac
    done
    [ -n "${marker}" ] && : > "${marker}"
    [ -n "${transaction}" ] && : > "${transaction}"
    ;;
  bootstrap)
    exit 0
    ;;
  version)
    echo test
    ;;
esac
STUB
chmod +x "${bin_dir}/sensei"

cat > "${bin_dir}/awareness-mcp" <<'STUB'
#!/bin/sh
printf '%s\n' "$*" >> "${SENSEI_TEST_LOG}"
STUB
chmod +x "${bin_dir}/awareness-mcp"

SENSEI_BIN_DIR="${bin_dir}" \
SENSEI_TEST_LOG="${log_file}" \
SENSEI_WORKSPACE="${workspace}" \
SENSEI_DATA_DIR="${data_dir}" \
SENSEI_STARTUP_ATTEMPTS=5 \
./deploy/appliance-entrypoint.sh serve >"${root}/serve.out" 2>"${root}/serve.err" &
serve_pid=$!

for _ in 1 2 3 4 5; do
  [ -f "${data_dir}/appliance.ready" ] && break
  sleep 1
done
[ -f "${data_dir}/appliance.ready" ] || {
  cat "${root}/serve.err" >&2
  echo "entrypoint test: readiness file missing" >&2
  exit 1
}

SENSEI_BIN_DIR="${bin_dir}" \
SENSEI_TEST_LOG="${log_file}" \
SENSEI_WORKSPACE="${workspace}" \
SENSEI_DATA_DIR="${data_dir}" \
./deploy/appliance-entrypoint.sh health

grep -q 'serve .* -oxigraph-bind 127.0.0.1:7878 .* -no-seed' "${log_file}"
grep -q 'build -all -input docs/awareness .* -graph-marker-file .*data/graph-authority.json .* -strict' "${log_file}"
[ -f "${data_dir}/graph-authority.json" ]
[ -f "${data_dir}/graph-transaction.tsv" ]
[ ! -e "${workspace}/.sensei/graph-authority.json" ]

kill -TERM "${serve_pid}"
wait "${serve_pid}" || true
serve_pid=""
[ ! -e "${data_dir}/appliance.ready" ]

SENSEI_BIN_DIR="${bin_dir}" \
SENSEI_TEST_LOG="${log_file}" \
SENSEI_WORKSPACE="${workspace}" \
SENSEI_DATA_DIR="${data_dir}" \
./deploy/appliance-entrypoint.sh bootstrap --skip-history

grep -q "bootstrap -path ${workspace} --skip-history" "${log_file}"

echo "appliance-entrypoint test: PASS"
