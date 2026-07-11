#!/usr/bin/env bash
# install-awg-user-services.sh — install supervised local AWG services under
# systemd --user.
#
# This creates one or two user units:
#   - awg-awareness-graph.service
#   - awg-oxigraph.service (only when not reusing an existing Oxigraph)
#
# The awareness-graph unit depends on Oxigraph when AWG owns the local store and
# always starts with -require-store=true.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BIN_DIR="$REPO_ROOT/bin"
SYSTEMD_USER_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
OXIGRAPH_DATA_DIR="${OXIGRAPH_DATA_DIR:-$HOME/.local/share/sensei/oxigraph}"
OXIGRAPH_BIND="${OXIGRAPH_BIND:-127.0.0.1:7878}"
OXIGRAPH_QUERY_URL="${OXIGRAPH_QUERY_URL:-http://127.0.0.1:7878/query}"
AWARENESS_ADDR="${AWARENESS_ADDR:-:10120}"
GOCACHE_DIR="${GOCACHE_DIR:-/tmp/awg-go-build}"
STORE_WAIT_SECONDS="${STORE_WAIT_SECONDS:-15}"
REUSE_EXISTING_OXIGRAPH="${REUSE_EXISTING_OXIGRAPH:-auto}"

usage() {
  cat <<EOF
Usage: ./scripts/install-awg-user-services.sh [--install-only] [--skip-build] [--reuse-existing-oxigraph|--no-reuse-existing-oxigraph]

Installs one or two supervised local user services:
  awg-awareness-graph.service
  awg-oxigraph.service (only when not reusing an existing Oxigraph)

Default behavior:
  1. rebuild ./bin/sensei and ./bin/awareness-graph
  2. write systemd --user unit files under:
       $SYSTEMD_USER_DIR
  3. systemctl --user daemon-reload
  4. enable --now the required unit(s)
  5. print verification commands

Environment overrides:
  OXIGRAPH_DATA_DIR
  OXIGRAPH_BIND
  OXIGRAPH_QUERY_URL
  AWARENESS_ADDR
  GOCACHE_DIR
  STORE_WAIT_SECONDS
  REUSE_EXISTING_OXIGRAPH=true|false|auto
EOF
}

INSTALL_ONLY=false
SKIP_BUILD=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --help|-h)
      usage
      exit 0
      ;;
    --install-only)
      INSTALL_ONLY=true
      shift
      ;;
    --skip-build)
      SKIP_BUILD=true
      shift
      ;;
    --reuse-existing-oxigraph)
      REUSE_EXISTING_OXIGRAPH=true
      shift
      ;;
    --no-reuse-existing-oxigraph)
      REUSE_EXISTING_OXIGRAPH=false
      shift
      ;;
    *)
      echo "install-awg-user-services: unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "install-awg-user-services: missing required command: $1" >&2
    exit 1
  }
}

require_cmd systemctl
require_cmd curl

if [[ "$SKIP_BUILD" != true ]]; then
  require_cmd go
fi

mkdir -p "$BIN_DIR" "$GOCACHE_DIR" "$OXIGRAPH_DATA_DIR" "$SYSTEMD_USER_DIR"

if [[ "$SKIP_BUILD" == true ]]; then
  echo "==> reuse prebuilt binaries"
else
  echo "==> rebuild"
  env GOCACHE="$GOCACHE_DIR" go build -o "$BIN_DIR/sensei" ./cmd/awg
  cp "$BIN_DIR/sensei" "$BIN_DIR/awg"   # deprecated alias (one release)
  env GOCACHE="$GOCACHE_DIR" go build -o "$BIN_DIR/awareness-graph" ./golang/server
fi

if [[ ! -x "$BIN_DIR/oxigraph" ]]; then
  echo "install-awg-user-services: missing $BIN_DIR/oxigraph" >&2
  echo "Run ./scripts/fetch-oxigraph.sh or ./scripts/install.sh first." >&2
  exit 1
fi

echo "==> write user units"
if [[ "$REUSE_EXISTING_OXIGRAPH" == auto ]]; then
  if curl -sS -f -X POST \
    -H "Content-Type: application/sparql-query" \
    --data 'ASK {}' \
    "$OXIGRAPH_QUERY_URL" >/dev/null 2>&1; then
    REUSE_EXISTING_OXIGRAPH=true
  else
    REUSE_EXISTING_OXIGRAPH=false
  fi
fi

if [[ "$REUSE_EXISTING_OXIGRAPH" == false ]]; then
  cat >"$SYSTEMD_USER_DIR/awg-oxigraph.service" <<EOF
[Unit]
Description=Local AWG Oxigraph backend
After=default.target

[Service]
Type=simple
ExecStart=$BIN_DIR/oxigraph serve --location $OXIGRAPH_DATA_DIR --bind $OXIGRAPH_BIND
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
EOF
else
  rm -f "$SYSTEMD_USER_DIR/awg-oxigraph.service"
fi

cat >"$SYSTEMD_USER_DIR/awg-awareness-graph.service" <<EOF
[Unit]
Description=Local AWG awareness-graph
$( [[ "$REUSE_EXISTING_OXIGRAPH" == true ]] && printf 'After=default.target' || printf 'Requires=awg-oxigraph.service\nAfter=awg-oxigraph.service' )

[Service]
Type=simple
WorkingDirectory=$REPO_ROOT
Environment=AWG_REPO_ROOT=$REPO_ROOT
ExecStartPre=/bin/bash -lc 'i=0; while [ \$i -lt $STORE_WAIT_SECONDS ]; do /usr/bin/curl -sS -f -X POST -H "Content-Type: application/sparql-query" --data "ASK {}" "$OXIGRAPH_QUERY_URL" >/dev/null && exit 0; i=\$((i+1)); sleep 1; done; exit 1'
ExecStart=$BIN_DIR/awareness-graph -addr $AWARENESS_ADDR -oxigraph-url $OXIGRAPH_QUERY_URL -require-store=true
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
EOF

echo "==> reload user manager"
systemctl --user daemon-reload

if [[ "$INSTALL_ONLY" == true ]]; then
  cat <<EOF
Installed unit files:
  $SYSTEMD_USER_DIR/awg-awareness-graph.service
$( [[ "$REUSE_EXISTING_OXIGRAPH" == false ]] && printf '  %s/awg-oxigraph.service\n' "$SYSTEMD_USER_DIR" )

Start them with:
  $( [[ "$REUSE_EXISTING_OXIGRAPH" == true ]] && printf 'systemctl --user enable --now awg-awareness-graph.service' || printf 'systemctl --user enable --now awg-oxigraph.service awg-awareness-graph.service' )
EOF
  exit 0
fi

echo "==> enable and start"
if [[ "$REUSE_EXISTING_OXIGRAPH" == true ]]; then
  systemctl --user enable --now awg-awareness-graph.service
else
  systemctl --user enable --now awg-oxigraph.service awg-awareness-graph.service
fi

echo
echo "==> status"
if [[ "$REUSE_EXISTING_OXIGRAPH" == true ]]; then
  systemctl --user --no-pager --full status awg-awareness-graph.service || true
else
  systemctl --user --no-pager --full status awg-oxigraph.service awg-awareness-graph.service || true
fi

echo
echo "==> verify"
cat <<EOF
Run:
  $BIN_DIR/sensei metadata

Expected:
  Freshness state:     current
  Seed state:          current

Useful commands:
  $( [[ "$REUSE_EXISTING_OXIGRAPH" == true ]] && printf 'systemctl --user restart awg-awareness-graph.service' || printf 'systemctl --user restart awg-oxigraph.service awg-awareness-graph.service' )
  $( [[ "$REUSE_EXISTING_OXIGRAPH" == true ]] && printf 'systemctl --user stop awg-awareness-graph.service' || printf 'systemctl --user stop awg-awareness-graph.service awg-oxigraph.service' )
  $( [[ "$REUSE_EXISTING_OXIGRAPH" == true ]] && printf 'journalctl --user -u awg-awareness-graph.service -n 100 --no-pager' || printf 'journalctl --user -u awg-oxigraph.service -u awg-awareness-graph.service -n 100 --no-pager' )
EOF
