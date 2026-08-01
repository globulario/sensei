#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# One small process boundary for the Sensei appliance. Normal service mode keeps
# the repository read-only: Oxigraph state, graph identity, and readiness live
# under SENSEI_DATA_DIR.
set -eu

bin_dir="${SENSEI_BIN_DIR:-/usr/local/bin}"
workspace="${SENSEI_WORKSPACE:-/workspace}"
data_dir="${SENSEI_DATA_DIR:-/var/lib/sensei}"
listen_addr="${SENSEI_LISTEN_ADDR:-0.0.0.0:10120}"
client_addr="${SENSEI_CLIENT_ADDR:-127.0.0.1:10120}"
oxigraph_bind="${SENSEI_OXIGRAPH_BIND:-127.0.0.1:7878}"
home_domain="${SENSEI_HOME_DOMAIN:-project}"
repo_domain="${SENSEI_REPO_DOMAIN:-}"
awareness_input="${SENSEI_AWARENESS_INPUT:-docs/awareness}"
marker_file="${SENSEI_GRAPH_MARKER_FILE:-${data_dir}/graph-authority.json}"
transaction_file="${SENSEI_GRAPH_TRANSACTION_FILE:-${data_dir}/graph-transaction.tsv}"
ready_file="${data_dir}/appliance.ready"
store_url="http://${oxigraph_bind}/store?default"

is_true() {
  case "${1:-}" in
    1|true|TRUE|yes|YES|on|ON) return 0 ;;
    *) return 1 ;;
  esac
}

require_workspace() {
  if [ ! -d "${workspace}" ]; then
    echo "sensei appliance: workspace does not exist: ${workspace}" >&2
    exit 2
  fi
}

run_health() {
  [ -f "${ready_file}" ] || exit 1
  "${bin_dir}/sensei" metadata -addr "${client_addr}" >/dev/null 2>&1
}

run_bootstrap() {
  require_workspace
  cd "${workspace}"
  exec "${bin_dir}/sensei" bootstrap -path "${workspace}" "$@"
}

run_cli() {
  require_workspace
  cd "${workspace}"
  exec "${bin_dir}/sensei" "$@"
}

run_mcp() {
  exec "${bin_dir}/awareness-mcp" -awareness-addr "${client_addr}" "$@"
}

run_serve() {
  require_workspace
  mkdir -p "${data_dir}"
  rm -f "${ready_file}"

  set -- "${bin_dir}/sensei" serve \
    -addr "${listen_addr}" \
    -oxigraph-bind "${oxigraph_bind}" \
    -data "${data_dir}/oxigraph" \
    -graph-marker-file "${marker_file}" \
    -home-domain "${home_domain}"

  if is_true "${SENSEI_NO_SEED:-true}"; then
    set -- "$@" -no-seed
  fi
  if is_true "${SENSEI_ALLOW_STALE_SEED:-false}"; then
    set -- "$@" -allow-stale-seed
  fi
  if is_true "${SENSEI_ENABLE_PROPOSE:-false}"; then
    set -- "$@" -enable-propose
  fi
  if [ -n "${repo_domain}" ]; then
    set -- "$@" -repo-root "${workspace}" -repo-domain "${repo_domain}"
  fi

  "$@" &
  serve_pid=$!

  stop_children() {
    rm -f "${ready_file}"
    if kill -0 "${serve_pid}" 2>/dev/null; then
      kill -TERM "${serve_pid}" 2>/dev/null || true
      wait "${serve_pid}" 2>/dev/null || true
    fi
  }
  trap 'stop_children; exit 143' HUP INT TERM
  trap 'stop_children' EXIT

  attempt=0
  until "${bin_dir}/sensei" metadata -addr "${client_addr}" >/dev/null 2>&1; do
    if ! kill -0 "${serve_pid}" 2>/dev/null; then
      wait "${serve_pid}"
      exit $?
    fi
    attempt=$((attempt + 1))
    if [ "${attempt}" -ge "${SENSEI_STARTUP_ATTEMPTS:-60}" ]; then
      echo "sensei appliance: service did not become ready at ${client_addr}" >&2
      exit 1
    fi
    sleep 1
  done

  if is_true "${SENSEI_AUTO_BUILD:-true}"; then
    if [ ! -d "${workspace}/${awareness_input}" ]; then
      echo "sensei appliance: ${awareness_input} is missing in ${workspace}" >&2
      echo "sensei appliance: initialize once with the appliance 'bootstrap' command, or set SENSEI_AUTO_BUILD=false" >&2
      exit 1
    fi

    set -- "${bin_dir}/sensei" build \
      -all \
      -input "${awareness_input}" \
      -store-url "${store_url}" \
      -graph-marker-file "${marker_file}" \
      -graph-transaction-file "${transaction_file}"
    if is_true "${SENSEI_BUILD_STRICT:-true}"; then
      set -- "$@" -strict
    fi

    echo "sensei appliance: publishing ${awareness_input} to private Oxigraph" >&2
    (cd "${workspace}" && "$@")
  fi

  "${bin_dir}/sensei" metadata -addr "${client_addr}" >/dev/null
  : > "${ready_file}"
  echo "sensei appliance: ready on ${listen_addr} (Oxigraph is private at ${oxigraph_bind})" >&2

  wait "${serve_pid}"
  rc=$?
  rm -f "${ready_file}"
  trap - EXIT HUP INT TERM
  exit "${rc}"
}

command="${1:-serve}"
if [ "$#" -gt 0 ]; then
  shift
fi

case "${command}" in
  serve) run_serve "$@" ;;
  health) run_health ;;
  bootstrap) run_bootstrap "$@" ;;
  mcp) run_mcp "$@" ;;
  sensei) run_cli "$@" ;;
  awareness-graph) exec "${bin_dir}/awareness-graph" "$@" ;;
  oxigraph) exec "${bin_dir}/oxigraph" "$@" ;;
  shell|sh) exec /bin/sh "$@" ;;
  *) run_cli "${command}" "$@" ;;
esac
