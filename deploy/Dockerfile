# syntax=docker/dockerfile:1.7

FROM golang:1.25-bookworm AS build

ARG TARGETARCH=amd64
ARG OXIGRAPH_VERSION=0.5.9
ARG SENSEI_VERSION=0.0.0-dev
ARG VCS_REF=unknown

WORKDIR /src

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl jq \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN set -eux; \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.Version=${SENSEI_VERSION}" \
      -o /out/sensei ./cmd/awg; \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.Version=${SENSEI_VERSION} -X main.BuildCommit=${VCS_REF}" \
      -o /out/awareness-graph ./golang/server; \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w" \
      -o /out/awareness-mcp ./cmd/awareness-mcp

# Pin Oxigraph and verify the exact GitHub release asset digest before it enters
# the runtime image. TARGETARCH is supplied automatically by BuildKit/buildx.
RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) oxi_arch="x86_64_linux_gnu" ;; \
      arm64) oxi_arch="aarch64_linux_gnu" ;; \
      *) echo "unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    asset="oxigraph_v${OXIGRAPH_VERSION}_${oxi_arch}"; \
    release="$(curl -fsSL "https://api.github.com/repos/oxigraph/oxigraph/releases/tags/v${OXIGRAPH_VERSION}")"; \
    url="$(printf '%s' "${release}" | jq -er --arg asset "${asset}" '.assets[] | select(.name == $asset) | .browser_download_url')"; \
    digest="$(printf '%s' "${release}" | jq -er --arg asset "${asset}" '.assets[] | select(.name == $asset) | .digest' | sed 's/^sha256://')"; \
    test -n "${digest}"; \
    curl -fsSL "${url}" -o /out/oxigraph; \
    printf '%s  %s\n' "${digest}" /out/oxigraph | sha256sum -c -; \
    chmod 0755 /out/oxigraph; \
    /out/oxigraph --version; \
    mkdir -p /out/licenses/oxigraph; \
    curl -fsSL "https://raw.githubusercontent.com/oxigraph/oxigraph/v${OXIGRAPH_VERSION}/LICENSE-APACHE" -o /out/licenses/oxigraph/LICENSE-APACHE; \
    curl -fsSL "https://raw.githubusercontent.com/oxigraph/oxigraph/v${OXIGRAPH_VERSION}/LICENSE-MIT" -o /out/licenses/oxigraph/LICENSE-MIT

FROM debian:bookworm-slim AS runtime

ARG SENSEI_VERSION=0.0.0-dev
ARG VCS_REF=unknown
ARG OXIGRAPH_VERSION=0.5.9

LABEL org.opencontainers.image.title="Sensei appliance" \
      org.opencontainers.image.description="Sensei CLI, awareness-graph, awareness-mcp, and a private Oxigraph runtime in one lightweight image" \
      org.opencontainers.image.source="https://github.com/globulario/sensei" \
      org.opencontainers.image.version="${SENSEI_VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      io.globulario.sensei.oxigraph.version="${OXIGRAPH_VERSION}"

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 65532 sensei \
    && useradd --system --uid 65532 --gid 65532 --home-dir /var/lib/sensei --shell /usr/sbin/nologin sensei \
    && mkdir -p /workspace /var/lib/sensei /usr/share/licenses/sensei /usr/share/licenses/oxigraph \
    && chown -R 65532:65532 /var/lib/sensei

COPY --from=build /out/sensei /usr/local/bin/sensei
COPY --from=build /out/awareness-graph /usr/local/bin/awareness-graph
COPY --from=build /out/awareness-mcp /usr/local/bin/awareness-mcp
COPY --from=build /out/oxigraph /usr/local/bin/oxigraph
COPY --from=build /out/licenses/oxigraph/ /usr/share/licenses/oxigraph/
COPY LICENSE NOTICE /usr/share/licenses/sensei/
COPY deploy/appliance-entrypoint.sh /usr/local/bin/sensei-appliance

RUN chmod 0755 /usr/local/bin/sensei-appliance \
    && /usr/local/bin/sensei version \
    && /usr/local/bin/awareness-graph --version \
    && /usr/local/bin/oxigraph --version

USER 65532:65532
WORKDIR /workspace

ENV SENSEI_WORKSPACE=/workspace \
    SENSEI_DATA_DIR=/var/lib/sensei \
    SENSEI_LISTEN_ADDR=0.0.0.0:10120 \
    SENSEI_CLIENT_ADDR=127.0.0.1:10120 \
    SENSEI_OXIGRAPH_BIND=127.0.0.1:7878 \
    SENSEI_HOME_DOMAIN=project \
    SENSEI_NO_SEED=true \
    SENSEI_AUTO_BUILD=true \
    SENSEI_BUILD_STRICT=true

VOLUME ["/var/lib/sensei"]
EXPOSE 10120

HEALTHCHECK --interval=5s --timeout=3s --start-period=20s --retries=12 \
  CMD ["/usr/local/bin/sensei-appliance", "health"]

ENTRYPOINT ["/usr/local/bin/sensei-appliance"]
CMD ["serve"]
