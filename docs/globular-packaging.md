# Globular Packaging for awareness-graph

This repository can generate a Globular service package for `awareness-graph`.

## What this package is

- Service ID/name: `awareness.AwarenessGraphService`
- Runtime binary: `bin/awareness-graph`
- Package spec: `packaging/specs/awareness_graph_service.yaml`
- Package metadata: `packaging/metadata/awareness-graph/package.json`
- Default service port/proxy: `10120` / `10121`

`awareness-graph` remains a gRPC service. Oxigraph remains a backend/sidecar dependency.
The service package does not expose raw Oxigraph access to agents.

## Build and stage

```bash
make service-build
make service-dist
```

Staged payload root defaults to:

- `/tmp/awareness-graph-service-root`

## Build package artifact

```bash
make service-package
```

Package output defaults to:

- `/tmp/awareness-graph-packages`

## Runtime configuration

Systemd unit in the package sets environment defaults:

- `AW_OXIGRAPH_QUERY_URL=http://localhost:7878/query`
- `AW_REQUIRE_STORE=false`

You can override these by editing the installed unit or service config in your node workflow.

## Health and metadata checks

```bash
go run ./golang/server --describe
go run ./golang/server --health
```

## Notes on install/publish workflow

The final publish/install step is executed by the external Globular package repository workflow.
This repo provides package-ready spec/metadata and build targets; publishing to shared package
registries is still performed from the `globulario/packages` and node installer pipeline.

For the end-to-end release procedure (regenerate seed → tag → publish → set
desired → activate via `loadnt`), follow [Release Runbook](./release-runbook.md).
The seed-activation step is mandatory: a deployed v0.0.X+1 binary will continue
to serve the v0.0.X graph until `make load-release-seed` runs against the live
Oxigraph, because `seedIfEmpty` does not clobber an already-populated store.


## Proxy behavior

- Metadata keeps proxy port `10121` reserved.
- In this package path, awareness-graph runs direct gRPC only on `10120`.
- No separate proxy listener is provisioned by this service package.

## Proto install path

- Installed to: `/var/lib/globular/services/awareness-graph/awareness_graph.proto`
