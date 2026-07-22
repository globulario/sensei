# Sensei Docker appliance

The appliance is the shortest path from a Sensei-aware repository to a live
agent service. One image contains:

- the `sensei` CLI;
- the `awareness-graph` gRPC service;
- the `awareness-mcp` stdio bridge;
- a pinned and release-digest-verified Oxigraph binary.

Sensei already owns the local process lifecycle, so the image does not run a
second container or duplicate service orchestration. Oxigraph binds only to
`127.0.0.1` inside the container. The only published port is governed gRPC on
`10120`.

## Fast path

A repository must contain `docs/awareness`. Initialize it once using a local
Sensei installation or the image itself:

```bash
docker run --rm \
  --user "$(id -u):$(id -g)" \
  -v "$PWD:/workspace" \
  ghcr.io/globulario/sensei-appliance:latest \
  bootstrap --skip-history
```

Bootstrap intentionally writes governed project files. Normal service mode does
not. Start the appliance with the repository mounted read-only:

```bash
docker run -d \
  --name sensei \
  -p 127.0.0.1:10120:10120 \
  -v "$PWD:/workspace:ro" \
  -v sensei-data:/var/lib/sensei \
  ghcr.io/globulario/sensei-appliance:latest
```

The appliance starts its private Oxigraph child, starts awareness-graph, builds
`docs/awareness` into the live store, verifies the service, and becomes healthy.
Runtime graph identity and database state stay in the `sensei-data` volume.
Nothing writes `.sensei/graph-authority.json` into the mounted repository.

Verify it from a locally installed client:

```bash
SENSEI_ADDR=localhost:10120 sensei metadata
```

Or use the bundled client:

```bash
docker exec sensei sensei metadata -addr 127.0.0.1:10120
```

## Docker Compose

From the Sensei checkout:

```bash
SENSEI_REPO=/absolute/path/to/project \
  docker compose -f deploy/docker-compose.yml up -d --build
```

The Compose file keeps the root filesystem read-only, drops Linux capabilities,
binds the host port to loopback, and stores only runtime data in a named volume.

## Agent access

Run the MCP bridge over stdio inside the appliance:

```bash
docker exec -i sensei sensei-appliance mcp
```

Generic agents can use the bundled CLI without MCP:

```bash
docker exec sensei sensei briefing \
  -addr 127.0.0.1:10120 \
  -file path/to/file.go \
  -task "describe the intended change"
```

For cloud agents that cannot reach local Docker, run the same image in GitHub
Actions and surface its result as a check. A persistent remote gateway can later
reuse the same image and contracts without changing Sensei's graph owners.

## Authentication

Bearer authentication is opt-in. Set the same token for the service and clients:

```bash
docker run -d \
  --name sensei \
  -e SENSEI_TOKEN="$(openssl rand -hex 24)" \
  -p 127.0.0.1:10120:10120 \
  -v "$PWD:/workspace:ro" \
  -v sensei-data:/var/lib/sensei \
  ghcr.io/globulario/sensei-appliance:latest
```

The port remains plaintext gRPC. Keep it on loopback or place a TLS/mTLS gateway
in front before exposing it to an untrusted network.

## Commands

The image entrypoint keeps common operations compact:

```text
serve                       default appliance service
health                      readiness plus live metadata check
bootstrap [flags]           initialize/extract a writable mounted repository
mcp [flags]                 run awareness-mcp against the local service
sensei <command> [flags]    run the bundled CLI
<command> [flags]           shorthand for a Sensei CLI command
shell                       open /bin/sh
```

## Configuration

| Environment variable | Default | Purpose |
|---|---|---|
| `SENSEI_WORKSPACE` | `/workspace` | Mounted repository root |
| `SENSEI_DATA_DIR` | `/var/lib/sensei` | Oxigraph, marker, transaction, readiness state |
| `SENSEI_LISTEN_ADDR` | `0.0.0.0:10120` | gRPC listener |
| `SENSEI_OXIGRAPH_BIND` | `127.0.0.1:7878` | Private Oxigraph listener |
| `SENSEI_HOME_DOMAIN` | `project` | Home domain for untagged graph nodes |
| `SENSEI_REPO_DOMAIN` | empty | Optional canonical repository domain for feedback verification |
| `SENSEI_NO_SEED` | `true` | Use the mounted project's graph instead of the embedded Globular graph |
| `SENSEI_AUTO_BUILD` | `true` | Publish `docs/awareness` at startup |
| `SENSEI_BUILD_STRICT` | `true` | Refuse unrecognized awareness schemas during startup build |
| `SENSEI_ENABLE_PROPOSE` | `false` | Enable the candidate-writing RPC |
| `SENSEI_TOKEN` | empty | Require bearer authentication when non-empty |

## Image discipline

The final image contains no Go compiler, Git checkout, Homebrew installation, or
package-manager cache. Homebrew remains a useful client-install and packaging
validation route, but the appliance copies pinned release binaries into a small
runtime image. CI enforces a 250 MiB uncompressed image ceiling and performs a
cold bootstrap, graph publication, health, binary, and read-only-workspace smoke.
