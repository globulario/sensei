#!/usr/bin/env sh
# Install the Sensei client tools (sensei + awareness-mcp) with Go.
#
#   curl -fsSL https://raw.githubusercontent.com/globulario/sensei/master/deploy/install.sh | sh
#
# or from a checkout:  ./deploy/install.sh
#
# Requires Go (1.25+). Binaries land in $(go env GOBIN) or $GOPATH/bin — add
# that to your PATH. The service itself is self-hosted separately; see
# deploy/docker-compose.yml and docs/deploy.md.
set -eu

MODULE="github.com/globulario/sensei"
VERSION="${SENSEI_VERSION:-${AWG_VERSION:-latest}}"

if ! command -v go >/dev/null 2>&1; then
	echo "install: Go is required (https://go.dev/dl/) — 1.25+." >&2
	exit 1
fi

bindir="$(go env GOBIN)"
[ -n "$bindir" ] || bindir="$(go env GOPATH)/bin"
mkdir -p "$bindir"

# Prefer a local checkout when run from inside the repo (exact source); else
# install the published module at $SENSEI_VERSION (or legacy $AWG_VERSION).
if [ -f "go.mod" ] && grep -q "^module ${MODULE}\$" go.mod 2>/dev/null; then
	echo "install: building sensei + awareness-mcp from local checkout -> ${bindir}"
	go build -o "${bindir}/sensei" ./cmd/awg
	go install ./cmd/awareness-mcp
else
	echo "install: building sensei + awareness-mcp from ${MODULE}@${VERSION} -> ${bindir}"
	tmpdir="$(mktemp -d)"
	trap 'rm -rf "$tmpdir"' EXIT HUP INT TERM
	GOBIN="$tmpdir" go install "${MODULE}/cmd/awg@${VERSION}"
	go install "${MODULE}/cmd/awareness-mcp@${VERSION}"
	cp "${tmpdir}/awg" "${bindir}/sensei"
fi

echo
echo "installed:"
for b in sensei awareness-mcp; do
	if [ -x "${bindir}/${b}" ]; then
		echo "  ${bindir}/${b}"
	fi
done
echo
case ":${PATH}:" in
	*":${bindir}:"*) ;;
	*) echo "note: ${bindir} is not on your PATH — add it: export PATH=\"${bindir}:\$PATH\"" ;;
esac
echo "next — run the SERVICE, then point the client at it:"
echo "  recommended:  SENSEI_REPO=$PWD docker compose -f deploy/docker-compose.yml up -d --build"
echo "  then:         export SENSEI_ADDR=localhost:10120 && sensei metadata"
echo
echo "note: the Docker appliance contains Sensei, awareness-graph, awareness-mcp, and a pinned Oxigraph binary."
echo "      Local 'sensei serve' still needs an 'oxigraph' binary on PATH."
echo "if the service requires auth, export SENSEI_TOKEN=<token> for the client too."
