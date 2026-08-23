# syntax=docker/dockerfile:1.7

FROM golang:1.25-bookworm AS build

# No default: TARGETARCH is a predefined build arg BuildKit fills in per target
# platform, but a Dockerfile-supplied default SHADOWS that injected value. With
# `=amd64` here, the linux/arm64 image resolved TARGETARCH to amd64, fetched the
# x86_64 Oxigraph, and died running it in an arm64 rootfs ("not found" -- no
# x86_64 ELF interpreter). Declare it bare so buildx supplies the real arch.
ARG TARGETARCH
ARG OXIGRAPH_VERSION=0.5.9
# Digests of the linux release assets for OXIGRAPH_VERSION, pinned here so the
# integrity check is independent of the host serving the bytes. Regenerate with
# deploy/oxigraph-digests.txt when the version moves.
ARG OXIGRAPH_SHA256_AMD64=4be355715ba3945e8fb8c94a06662a29808683bf2ec355894dba9e82762e7cc7
ARG OXIGRAPH_SHA256_ARM64=dc17e58cf65d74f89853bb8b49180099399a53b9df7f1671a4a9ca188208d794
ARG SENSEI_VERSION=0.0.0-dev
ARG VCS_REF=unknown

WORKDIR /src

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Sensei's shared architecture packages intentionally include Tree-sitter CGO
# bindings. All three shipped binaries traverse that dependency graph, so build
# them consistently with CGO and inspect their actual runtime link surface.
RUN set -eux; \
    CGO_ENABLED=1 go build -trimpath \
      -ldflags "-s -w -X main.Version=${SENSEI_VERSION}" \
      -o /out/sensei ./cmd/awg; \
    CGO_ENABLED=1 go build -trimpath \
      -ldflags "-s -w -X main.Version=${SENSEI_VERSION} -X main.BuildCommit=${VCS_REF}" \
      -o /out/awareness-graph ./golang/server; \
    CGO_ENABLED=1 go build -trimpath \
      -ldflags "-s -w" \
      -o /out/awareness-mcp ./cmd/awareness-mcp; \
    ldd /out/sensei; \
    ldd /out/awareness-graph; \
    ldd /out/awareness-mcp

# Fetch Oxigraph from the release download URL and check it against a digest
# pinned in this file. TARGETARCH is supplied automatically by BuildKit/buildx.
#
# Two things changed here, and the first is why the second was worth doing.
#
# 1. No Releases JSON API call. api.github.com is rate-limited PER IP for
#    unauthenticated callers, and shared CI runners exhaust that quota: the
#    appliance build failed with `curl: (22) ... error: 403` while a sibling job
#    on the same commit passed. A build that fails on someone else's traffic is
#    not reproducible. scripts/fetch-oxigraph.sh already avoided the API for
#    exactly this reason -- "It deliberately avoids the rate-limited Releases
#    JSON API" -- and this is the same repository learning the same lesson
#    twice. The download host serves the asset from a stable, documented URL
#    that needs no API to construct.
#
# 2. The digest is pinned HERE rather than read from the release metadata.
#    The old code fetched the expected digest from the same API response that
#    gave it the URL, then checked the download against it. That detects a
#    corrupted transfer and nothing else: an attacker or mistake that changes
#    the asset changes the metadata describing it, and the check still passes.
#    The comment above it claimed to "verify the exact GitHub release asset
#    digest", which was a stronger claim than a self-referential comparison can
#    support. A value committed to this repository is independent of the host
#    serving the bytes, so it is evidence about the artifact rather than about
#    the transfer.
#
# The cost is that these digests must be updated with OXIGRAPH_VERSION. That
# failure is loud (sha256sum -c fails the build and prints the mismatch) and
# deploy/oxigraph-digests.txt records how to regenerate them.
RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) oxi_arch="x86_64_linux_gnu";  oxi_sha256="${OXIGRAPH_SHA256_AMD64}" ;; \
      arm64) oxi_arch="aarch64_linux_gnu"; oxi_sha256="${OXIGRAPH_SHA256_ARM64}" ;; \
      *) echo "unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    test -n "${oxi_sha256}"; \
    asset="oxigraph_v${OXIGRAPH_VERSION}_${oxi_arch}"; \
    url="https://github.com/oxigraph/oxigraph/releases/download/v${OXIGRAPH_VERSION}/${asset}"; \
    curl -fsSL --retry 3 --retry-delay 2 --retry-all-errors \
      --connect-timeout 15 --max-time 300 "${url}" -o /out/oxigraph; \
    printf '%s  %s\n' "${oxi_sha256}" /out/oxigraph | sha256sum -c -; \
    chmod 0755 /out/oxigraph; \
    /out/oxigraph --version; \
    mkdir -p /out/licenses/oxigraph; \
    curl -fsSL --retry 3 --retry-delay 2 --retry-all-errors \
      "https://raw.githubusercontent.com/oxigraph/oxigraph/v${OXIGRAPH_VERSION}/LICENSE-APACHE" -o /out/licenses/oxigraph/LICENSE-APACHE; \
    curl -fsSL --retry 3 --retry-delay 2 --retry-all-errors \
      "https://raw.githubusercontent.com/oxigraph/oxigraph/v${OXIGRAPH_VERSION}/LICENSE-MIT" -o /out/licenses/oxigraph/LICENSE-MIT

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
    && apt-get install -y --no-install-recommends ca-certificates libgcc-s1 \
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
