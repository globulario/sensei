# Initializing and Populating Sensei

This document covers how to build, load, and refresh the awareness graph under
three scenarios: local development, full cluster refresh, and Day-0 first
install. It also explains how annotations work, how to register a new service
for scanning, and how to diagnose common problems.

---

## Architecture

```
Authored YAML + @awareness annotations in Go source
    |
    +--> annotation-scanner  -->  docs/awareness/generated/<prefix>_code_symbols.yaml
    |                                                       <prefix>_code_edges.yaml
    |
    +--> yaml2nt             -->  golang/server/embeddata/awareness.nt
    |
    +--> loadnt              -->  Oxigraph graph store  (http://localhost:7878/store?default)
                                            |
                                  awareness-graph gRPC service  (:10120)
                                            |
                                  Briefing / Impact / Resolve / Query RPCs
                                            |
                                  MCP bridge / CLI / AI agents
```

**Source locations:**

| Source type | Path |
|---|---|
| Hand-authored invariants, failure modes, incident patterns | `services/docs/awareness/` |
| Intent YAML files (200+) | `services/docs/intent/` |
| Awareness-graph self-awareness | `awareness-graph/docs/awareness/` |
| @awareness annotations in Go files | 30+ packages under `services/golang/` |
| Namespace registry | `services/docs/awareness/namespaces.yaml` |
| Generated YAML (scanner output) | `services/docs/awareness/generated/` |
| Embedded seed baked into binary | `awareness-graph/golang/server/embeddata/awareness.nt` |

---

## 1. Quick start — development (laptop, no cluster)

Prerequisites: Go toolchain installed, Oxigraph fetched via `./scripts/fetch-oxigraph.sh`
or `./scripts/install.sh`, both repos cloned as siblings.

```
~/code/
  awareness-graph/   ← this repo
  services/          ← globulario/services
```

**Step 1 — Start Oxigraph:**

```bash
cd awareness-graph
make oxigraph
```

This runs `scripts/bootstrap_oxigraph.sh`, which starts the native `oxigraph`
binary in the background with data persisted to
`~/.local/share/awareness-graph/oxigraph`. The script waits up to 30 seconds
for both the query and graph-store endpoints to respond.

- Query endpoint: `http://localhost:7878/query`
- Graph store endpoint: `http://localhost:7878/store?default`

**Step 2 — Build self-awareness graph and load it:**

```bash
make smoke-local
```

This is equivalent to:
```bash
# 1. Build self-awareness .nt from docs/awareness/
go run ./cmd/yaml2nt -input ./docs/awareness -output /tmp/awareness-graph-self.nt

# 2. Load it into Oxigraph
go run ./cmd/loadnt -input /tmp/awareness-graph-self.nt \
    -oxigraph-url http://localhost:7878/store?default

# 3. Start the gRPC server with -require-store, exit after 2s (smoke check)
timeout 2s go run ./golang/server \
    -addr :19090 \
    -oxigraph-url http://localhost:7878/query \
    -require-store
```

A `timeout 124` exit code from the server is expected — it means the server
started cleanly but was killed by the 2-second limit. Any other non-zero exit
is a failure.

**Step 3 — Run the full server:**

```bash
go run ./golang/server \
    -addr :10120 \
    -oxigraph-url http://localhost:7878/query \
    -require-store
```

Or use the Makefile variable override:
```bash
make server SERVER_ADDR=:10120
```

**Optional — load full platform graph:**

`smoke-local` only loads the awareness-graph's own self-awareness docs. To load
the complete graph from all services (including platform invariants, intent
files, and code annotations), run the build script instead:

```bash
SERVICES_REPO=../services bash scripts/build-awareness-graph.sh
```

Then reload into Oxigraph:
```bash
curl -X DELETE 'http://localhost:7878/store?default'
go run ./cmd/loadnt \
    -input ./golang/server/embeddata/awareness.nt \
    -oxigraph-url http://localhost:7878/store?default
```

---

## 2. Full cluster refresh (live service)

Use this when you have added annotations, updated YAML, or added new services
to the scan list, and need to push a fresh graph to a running cluster node.

Both `globular-oxigraph.service` and `globular-awareness-graph.service` are
already running on the target node.

### Step 1 — Regenerate the graph from source

Run the build script from within the awareness-graph checkout. It needs to
locate the services repo; set `SERVICES_REPO` explicitly if it is not a sibling
directory.

```bash
cd /path/to/awareness-graph
SERVICES_REPO=/path/to/services bash scripts/build-awareness-graph.sh
```

The script:
- Builds `annotation-scanner` and `yaml2nt` from source into `/tmp/`
- Runs annotation-scanner against all 34 registered Go packages
- Writes generated YAML to `services/docs/awareness/generated/`
- Runs yaml2nt over all awareness dirs and the intent dir
- Writes the result to `golang/server/embeddata/awareness.nt`
- Prints triple count and byte size on completion

Current seed size: approximately 17,200 triples / 3.4 MB.

### Step 2 — Wipe and reload Oxigraph

`loadnt` appends to the graph store. To do a full refresh, wipe first:

```bash
curl -X DELETE 'http://localhost:7878/store?default'
```

Then load the freshly built .nt:

```bash
go build -o /tmp/loadnt ./cmd/loadnt
/tmp/loadnt \
    -input ./golang/server/embeddata/awareness.nt \
    -oxigraph-url http://localhost:7878/store?default
```

### Step 3 — Rebuild the binary with the updated embedded seed

The `awareness-graph` binary embeds `awareness.nt` at build time via
`//go:embed embeddata/awareness.nt`. After updating the seed, rebuild and
redeploy so the embedded seed is also current (used on fresh-install auto-seed).

```bash
go build -o ./bin/awareness-graph ./golang/server
```

To deploy via the Globular package pipeline:
```bash
make service-dist
make service-package
globular pkg publish ...
```

### Step 4 — Restart the service (optional)

The running awareness-graph process queries Oxigraph directly. The graph is
live as soon as `loadnt` completes — no service restart is needed unless you
also want to update the binary.

If you do restart:
```bash
systemctl restart globular-awareness-graph.service
```

The service will detect that Oxigraph already has triples and skip the embedded
seed load. The live data loaded in Step 2 is preserved.

### Verify the reload

```bash
curl -s 'http://localhost:7878/query' \
  --data 'query=SELECT (COUNT(*) as ?n) WHERE { ?s ?p ?o }' \
  -H 'Accept: application/sparql-results+json' | python3 -m json.tool
```

Or via MCP:
```
awareness.query(mode=by_class, class=invariant, limit=5)
```

---

## 3. Day-0 first install (automatic)

No manual data-load step is required during Day-0.

The `awareness-graph` binary embeds `golang/server/embeddata/awareness.nt` at
build time. On startup, the server checks whether the Oxigraph store is empty
(`CountTriples == 0`). If it is, it loads the embedded seed automatically before
accepting gRPC connections.

**Sequence on a fresh node:**

1. Day-0 script starts `globular-oxigraph.service` (empty store).
2. Day-0 script starts `globular-awareness-graph.service`.
3. The server checks the store → zero triples → loads `awareness.nt` from
   embedded bytes (up to 90-second timeout).
4. The service logs `seed loaded successfully` and begins accepting RPCs.

If the embedded seed is stale relative to the source YAML, the correct fix is
to rebuild and republish the package, not to push data manually during Day-0.

---

## 4. How @awareness annotations work

Annotations are Go line comments placed at the top of a file or immediately
above a symbol (function, type, method). The annotation-scanner reads Go source
using `go/ast` and emits two YAML files per scanned package:

- `<prefix>_code_symbols.yaml` — each annotated file or symbol as a node
- `<prefix>_code_edges.yaml` — relationships declared with `implements=`,
  `enforces=`, `protects=`, `tested_by=`

### File-level annotation (most common)

Place annotations as the first lines of the file, before the `package`
declaration:

```go
// @awareness namespace=globular.platform
// @awareness component=platform_node_agent.install
// @awareness file_role=package_install_and_convergence_evidence_emission
// @awareness implements=globular.platform:intent.installed_state.owned_by_node_agent
// @awareness implements=globular.platform:intent.node_agent.is_executor_not_cluster_brain
// @awareness risk=critical
package node_agent_server
```

### Symbol-level annotation

Place annotations immediately before a function or type:

```go
// @awareness namespace=globular.awareness_graph
// @awareness component=server.resolve
// @awareness implements=globular.awareness_graph:intent.awareness.resolve_returns_precise_node_by_class_and_id
// @awareness enforces=globular.awareness_graph:invariant.awareness.store_unavailable_explicit
// @awareness tested_by=golang/server/resolve_test.go:TestResolveNotFound
// @awareness risk=low
func (s *server) Resolve(ctx context.Context, req *awarenesspb.ResolveRequest) (*awarenesspb.ResolveResponse, error) {
```

### Recognized annotation keys

| Key | Format | Purpose |
|---|---|---|
| `namespace` | `<namespace-id>` | Must match an entry in `namespaces.yaml` |
| `component` | `<prefix>.<sub>` | Logical component the file/symbol belongs to |
| `file_role` | free text | Human-readable description of what the file does |
| `implements` | `<namespace>:<intent-id>` | Declares an intent this file satisfies |
| `enforces` | `<namespace>:<invariant-id>` | Declares an invariant this file enforces |
| `protects` | `<namespace>:<failure-mode-id>` | Declares a failure mode this file guards against |
| `tested_by` | `<path>:<TestName>` | Links to a specific test |
| `risk` | `low` / `medium` / `high` / `critical` | Risk severity of the file/symbol |

### Namespace registry

Every `namespace=` value must be registered in
`services/docs/awareness/namespaces.yaml`. The `--strict` flag passed to
annotation-scanner rejects annotations that reference unknown namespaces.

To add a namespace for a new project, append to the `namespaces:` list:

```yaml
namespaces:
  - id: globular.my_service
    label: My Service
    owns:
      - golang/my_service
    description: Purpose and ownership scope.
```

---

## 5. Adding a new service to the scan list

To include a new Go package in the awareness graph:

**Step 1 — Register the namespace** (if the service is in a different repo or
owns its own namespace). Edit `services/docs/awareness/namespaces.yaml`.

**Step 2 — Add annotations** to the files you want tracked. At minimum, add a
file-level `namespace=` and `component=` to each significant file.

**Step 3 — Register the scan invocation** in
`awareness-graph/scripts/build-awareness-graph.sh`. Add a `run_scanner` call
in both the normal-mode block and the check-mode block, and a matching
`check_file` pair:

```bash
# In the normal-mode block (line ~89):
run_scanner "$SVC/golang/my_service" "$SVC" "platform_my_service" "$GENERATED"
echo "  platform_my_service: OK"

# In the check-mode block:
run_scanner "$SVC/golang/my_service" "$SVC" "platform_my_service" "$TMPDIR_WORK"

# In the check_file block:
check_file "platform_my_service_code_symbols.yaml" \
    "$GENERATED/platform_my_service_code_symbols.yaml" \
    "$TMPDIR_WORK/platform_my_service_code_symbols.yaml"
check_file "platform_my_service_code_edges.yaml" \
    "$GENERATED/platform_my_service_code_edges.yaml" \
    "$TMPDIR_WORK/platform_my_service_code_edges.yaml"
```

**Step 4 — Run the build script** to generate the YAML and rebuild the .nt:

```bash
SERVICES_REPO=../services bash scripts/build-awareness-graph.sh
```

**Step 5 — Commit** the generated YAML files under
`services/docs/awareness/generated/` and the updated `awareness.nt`.

---

## 6. CI staleness check

The `--check` flag regenerates all files into a temp directory and diffs them
against the committed versions. It exits 1 if anything is stale.

```bash
SERVICES_REPO=/path/to/services bash scripts/build-awareness-graph.sh --check
```

Use this in CI to catch committed code annotations or YAML edits that were not
followed by a graph rebuild. Stale output looks like:

```
Stale generated files detected.
Run scripts/build-awareness-graph.sh and commit the generated files.
```

The check covers both the generated YAML files and `awareness.nt` itself. Only
the `_annotation_report.yaml` files are excluded — they are informational
diagnostics, not load-bearing artifacts.

---

## 7. Troubleshooting

### Store is empty after startup

The server only auto-seeds on an empty store. If the store is non-empty (e.g.,
from a partial previous load), the seed is skipped. Do not wipe the store just
to force a reload: Oxigraph is runtime state and may contain facts beyond the
embedded seed.

To load the current embedded seed additively:

```bash
go run ./cmd/loadnt \
    -input ./golang/server/embeddata/awareness.nt \
    -oxigraph-url http://localhost:7878/store?default
```

To confirm the live store has actually loaded that seed:

```bash
go run ./cmd/awg seed-status \
    --seed ./golang/server/embeddata/awareness.nt \
    --oxigraph-url http://localhost:7878/query \
    --require-current
```

`seed-status` checks the full authority chain for the committed seed: fresh
generation vs committed seed, committed transaction stamp, and live-store
digest alignment. A non-`current` result means part of that chain is stale,
split, unknown, or degraded; the fix is to run the governed rebuild/reload
path, not to destructively reset Oxigraph.

### loadnt rejects the .nt file

`loadnt` validates N-Triples content before sending any HTTP request to
Oxigraph. A validation error means the .nt file contains malformed triples —
most likely yaml2nt received invalid YAML input.

Diagnose by running yaml2nt in strict mode directly:
```bash
go run ./cmd/yaml2nt \
    -input ./docs/awareness \
    -input ../services/docs/awareness \
    -input ../services/docs/awareness/generated \
    -intent ../services/docs/intent \
    -strict \
    -output /tmp/test.nt
```

Strict mode exits 1 if any YAML file fails to import. The per-file summary on
stderr identifies the problematic file.

### Server starts with "backend unhealthy" warning

Without `-require-store`, the server starts even if Oxigraph is down. RPCs that
need the store will return errors but the server process stays up. To make
startup fail fast when Oxigraph is absent:

```bash
go run ./golang/server -addr :10120 -oxigraph-url http://localhost:7878/query -require-store
```

In systemd, set `RequireStore = true` in the service config JSON or use
`-require-store` in `ExecStart`.

### Oxigraph not starting (dev mode)

Check that port 7878 is not already bound:
```bash
ss -tlnp | grep 7878
```

To use a different port:
```bash
./scripts/bootstrap_oxigraph.sh --port 7879
make server OXIGRAPH_URL=http://localhost:7879/query
```

To use a different data directory (e.g., to start fresh):
```bash
./scripts/bootstrap_oxigraph.sh --data-dir /tmp/fresh-oxigraph
```

### Awareness service reports degraded in MCP

The MCP bridge calls `awareness.briefing` / `awareness.query` over gRPC. A
degraded status means either:

1. The awareness-graph gRPC service is not running or not reachable on `:10120`
   (cluster or dev, default `:10120`).
2. Oxigraph is running but the store is empty (seed load failed or was skipped).

Check the service log:
```bash
journalctl -u globular-awareness-graph.service -n 50
```

Verify Oxigraph is healthy:
```bash
make oxigraph-health
```

Verify triple count:
```bash
curl -s 'http://localhost:7878/query' \
  --data 'query=SELECT (COUNT(*) as ?n) WHERE { ?s ?p ?o }' \
  -H 'Accept: application/sparql-results+json'
```

A count of zero with a running service means the seed load failed or the store
was never populated. Load the seed additively with `loadnt`, then verify with
`awg seed-status --require-current`.

### annotation-scanner: "namespace not registered"

The `--strict` flag rejects any `@awareness namespace=` value that is not in
`namespaces.yaml`. Add the namespace to
`services/docs/awareness/namespaces.yaml` and rerun the build script.

### Generated YAML is stale in CI

Run the build script and commit the output:
```bash
SERVICES_REPO=/path/to/services bash scripts/build-awareness-graph.sh
git add services/docs/awareness/generated/ awareness-graph/golang/server/embeddata/awareness.nt
git commit -m "chore: regenerate awareness graph"
```
