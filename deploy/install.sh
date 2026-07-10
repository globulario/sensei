#!/usr/bin/env sh
# Install the AWG client tools (awg + awareness-mcp) with Go.
#
#   curl -fsSL https://raw.githubusercontent.com/globulario/awareness-graph/master/deploy/install.sh | sh
#
# or from a checkout:  ./deploy/install.sh
#
# Requires Go (1.25+). Binaries land in $(go env GOBIN) or $GOPATH/bin — add
# that to your PATH. The service itself is self-hosted separately; see
# deploy/docker-compose.yml and docs/deploy.md.
set -eu

MODULE="github.com/globulario/awareness-graph"
VERSION="${AWG_VERSION:-latest}"

if ! command -v go >/dev/null 2>&1; then
	echo "install: Go is required (https://go.dev/dl/) — 1.25+." >&2
	exit 1
fi

bindir="$(go env GOBIN)"
[ -n "$bindir" ] || bindir="$(go env GOPATH)/bin"

# Prefer a local checkout when run from inside the repo (exact source); else
# install the published module at $AWG_VERSION.
if [ -f "go.mod" ] && grep -q "^module ${MODULE}\$" go.mod 2>/dev/null; then
	echo "install: building awg + awareness-mcp from local checkout -> ${bindir}"
	go install ./cmd/awg ./cmd/awareness-mcp
else
	echo "install: go install ${MODULE}/cmd/{awg,awareness-mcp}@${VERSION} -> ${bindir}"
	go install "${MODULE}/cmd/awg@${VERSION}"
	go install "${MODULE}/cmd/awareness-mcp@${VERSION}"
fi

echo
echo "installed:"
for b in awg awareness-mcp; do
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
echo "  recommended:  cd deploy && docker compose up --build   # bundles Oxigraph"
echo "  then:         export AWG_ADDR=localhost:10120 && awg metadata"
echo
echo "note: 'awg serve' (local, no Docker) ALSO needs an 'oxigraph' binary on PATH"
echo "      (this installer does not provide it — https://github.com/oxigraph/oxigraph/releases,"
echo "      or run it externally and use 'awg serve --no-oxigraph'). The compose path avoids this."
echo "if the service requires auth, export AWG_TOKEN=<token> for the client too."
