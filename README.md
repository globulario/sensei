# AWG — Awareness Graph

**Give your AI coding agent the project knowledge that currently lives only in your head.**

AI agents write code well but lose the architecture. The rules that actually
keep a large codebase alive — *this state comes from that authority, never
infer it from the cache; this fix looks right but reintroduces last quarter's
outage; this file is load-bearing, tread carefully* — are implicit. They live
in senior engineers' heads and in commit archaeology. A fresh agent (or a new
hire) can't see them, so it makes a reasonable-looking change that quietly
violates one, and the codebase drifts a little further from its own design.

AWG makes that knowledge **explicit and queryable**. You write your project's
intent, invariants, failure modes, forbidden fixes, and architecture rules as
small YAML files. AWG compiles them into a graph and serves a simple question:

> *"What do I need to know before editing this file?"*

An agent asks that **before** it edits — and gets the rules that apply, in
about two milliseconds.

```
$ awg briefing -file src/payment_processor.py -task "refactor mark_paid"

Direct invariants:
- [critical] payments.paid_state_requires_processor_confirmation —
  An order records as paid only after processor confirmation, never from a local cache write
```

### Three things that make it different

- **You own the graph.** It's YAML in your repo. Not a SaaS, not a model's
  hidden context — your knowledge, versioned with your code, yours to read and edit.
- **It's tool-agnostic.** AWG is a CLI + a local gRPC server. Use it from
  Claude Code, Cursor, Codex, a CI step, or a plain shell. Nothing is locked
  to one assistant.
- **It runs standalone.** No cloud, no account, no Globular. One local store
  (Oxigraph, fetched for you) and a binary you build. `-no-seed` keeps the
  graph 100% yours.

> AWG started inside [Globular](https://github.com/globulario), a distributed
> platform, where these principles were validated against real production
> incidents. It now runs on its own, for any codebase.

## Try it in 15 minutes

Honest assumptions, Linux or macOS:
- **Go 1.23+ is required** today for source builds. Linux `amd64` also has a prebuilt local runtime release bundle.
- **Oxigraph** (the local store) is **fetched for you** by `./scripts/install.sh`. No Docker.
- **Windows is not yet a validated path** (the enforcement hooks are bash). Coming next.

```bash
# 1. Install — builds awg + server, fetches the oxigraph binary into bin/
git clone https://github.com/globulario/awareness-graph.git
cd awareness-graph && ./scripts/install.sh
export PATH="$PWD/bin:$PATH"

# Or on Linux amd64, download the prebuilt local runtime bundle:
#   awg-local_<version>_linux_amd64.tgz

# Recommended local runtime for actual use
#   source-build checkout: bash ./scripts/install-awg-user-services.sh
#   prebuilt bundle:       bash ./scripts/install-awg-user-services.sh --skip-build
bash ./scripts/install-awg-user-services.sh

# 2. Run the bundled demo (no setup — one rule, one file)
cd examples/payment-cold-start
awg init                 # adds the 83-principle pack + hooks alongside the rule already here
awg serve -no-seed &     # local store + server; -no-seed = your rules only
awg build                # compile docs/awareness into the graph
awg briefing -file src/payment_processor.py -task "refactor mark_paid"
```

You should see the `payments.paid_state_requires_processor_confirmation`
invariant come back. That's the whole idea: the rule you wrote now reaches the
agent before the edit. Full walkthrough of the demo: **[examples/payment-cold-start/](examples/payment-cold-start/)**.

To do the same on **your** project: `cd` into it, `awg init`, write your first
rule in `docs/awareness/invariants.yaml`, then `awg serve -no-seed & && awg build`.

- **[QUICKSTART.md](QUICKSTART.md)** — the 15-minute guide, including the Claude Code hook wiring.
- **[INSTALL.md](INSTALL.md)** — platforms, the Oxigraph dependency, and the supervised local runtime path.
- **[docs/case-study-cold-start.md](docs/case-study-cold-start.md)** — the why, and the proof it runs on a clean machine.
- **[docs/the-discipline.md](docs/the-discipline.md)** — the seven moves the tooling can't teach you. The practice, not the mechanics. Read this if you want AWG to become *how you work*, not just a tool you run.

## The problem

Every codebase accumulates invisible rules — things that broke, got fixed, and now live only in someone's head. When an AI agent or a new developer makes a "simple fix" in that area, the rule gets violated. A release ships broken. A patch release follows.

AWG encodes these rules as a queryable graph and enforces consultation before edits.

## What you write

Three YAML files encode your project's architectural knowledge:

**invariants.yaml** — rules that must always hold:
```yaml
invariants:
  - id: auth.session_token_must_be_httponly
    title: Session tokens must use HttpOnly cookies
    severity: critical
    status: active
    protects:
      files: [src/auth/session.go]
    forbidden_fixes:
      - expose_token_in_response_body_for_spa
    required_tests:
      - TestSessionCookieIsHttpOnly
```

**failure_modes.yaml** — incidents that happened or could happen:
```yaml
failure_modes:
  - id: auth.xss_steals_session_token
    title: XSS attack steals session token from non-HttpOnly cookie
    severity: critical
    root_cause: |
      A developer made the cookie JavaScript-readable for the SPA.
      Any XSS vector on the page could now steal session tokens.
    architecture_fix: |
      Cookies MUST have HttpOnly. Use a separate CSRF token for SPA.
    forbidden_fixes:
      - add_csp_headers_instead_of_httponly
```

**incident_patterns.yaml** — edit shapes that introduce bugs:
```yaml
incident_patterns:
  - id: pat.cookie_made_js_readable
    edit_shapes:
      - "Removing HttpOnly from session cookie"
      - "Adding JavaScript-readable token cookie for SPA"
    failure_mode: auth.xss_steals_session_token
    lesson: |
      HttpOnly exists to prevent exactly this. The SPA should use
      same-origin requests with the cookie, not read it via JS.
```

## CLI commands

The most common commands — the **[full CLI reference](docs/cli-reference.md)**
documents all ~30 with every flag.

| Command | What it does |
|---------|-------------|
| `awg init` | Scaffold a new project (YAML templates, hooks, CLAUDE.md) |
| `awg bootstrap` | Initialize AWG for an existing repo (extraction + optional history) |
| `awg build` | Compile YAML → N-Triples, load into Oxigraph (`--output file.nt` for no-Oxigraph) |
| `awg serve` | Start Oxigraph + the gRPC awareness server |
| `awg briefing --file <path>` / `--task "desc"` | Prose context for a file or task |
| `awg impact --file <path>` | Structured knowledge nodes for a file |
| `awg preflight --task ... --file ...` | Risk classification before editing |
| `awg edit-check --file <path> --content-file -` | Advisory: does this edit violate a rule? |
| `awg resolve <class> <id>` | Fetch one node |
| `awg query --mode <mode>` | Typed graph browse (`by_file`/`by_id`/`by_class`/`related`) |
| `awg metadata` | Graph coverage, freshness, build provenance |
| `awg propose --kind ...` | Record a scar (typed feedback), rebuild, stage — never commits |
| `awg feedback-check` | Stop-hook: warn if a fix added no graph feedback |
| `awg check` / `validate` / `audit` | Validate YAML / deep structural check / self-audit drift |
| `awg gate --diff <range>` | Dry-run hard gate over a git diff |
| `awg version` | Print version |

## The meta-principles

Every AWG project ships with **133 universal principles across 8 categories** — seven that predict where bugs hide, plus *evolution*: how a project may change safely over time. They apply to any software system.

| Category | Count | Examples |
|----------|-------|---------|
| **Authority** | 20 | Wrong actor writes truth; same value means different things |
| **Signal** | 19 | Fallback looks like truth; errors absorbed into timeouts |
| **Lifecycle** | 38 | Write with no cleanup; intermediate state looks done |
| **Dependency** | 7 | Critical path blocked by non-critical service |
| **Perception** | 19 | A green badge lying about runtime state; warning visible only in color |
| **Composition** | 7 | Decorative card louder than a drift warning; success color on unconfirmed state |
| **Structure** | 12 | GenericMegaTable merged on resemblance; pass-through wrapper hiding nothing |
| **Evolution** | 11 | Releasable trunk; reviewable slices; deterministic builds; intent before drift |

See **[docs/meta-principles.md](docs/meta-principles.md)** for the framework reference. The authoritative list is the generated pack (`docs/awareness/meta_principles.yaml`) — query any principle with `awg resolve invariant meta.<id>`.

## Claude Code integration

`awg init` generates Claude Code hooks that enforce *consult-then-comply* before edits:

- **enforce-briefing.sh** — blocks edits to high-risk files unless `awg briefing` was called first (did you *look*?)
- **edit-check-guard.sh** — runs the proposed edit content through `awg edit-check` and blocks a forbidden-fix / high-severity shape (does what you're about to *write* violate a rule?). Advisory by default for low-severity; `AWG_EDIT_CHECK_ADVISORY=1` makes it warn-only.
- **record-briefing.sh** — records that a briefing was obtained

Add to `.claude/settings.json`:
```json
{
  "hooks": {
    "PreToolUse": [{
      "matcher": "Edit|Write|MultiEdit",
      "hooks": [
        {"command": ".claude/hooks/enforce-briefing.sh", "timeout": 10},
        {"command": ".claude/hooks/edit-check-guard.sh", "timeout": 10}
      ]
    }],
    "PostToolUse": [{
      "matcher": "awareness_briefing",
      "hooks": [{"command": ".claude/hooks/record-briefing.sh", "timeout": 10}]
    }]
  }
}
```

## Architecture

```
Your YAML files         awg build           Oxigraph          awg serve
(docs/awareness/)  -->  (yaml2nt)    -->   (RDF store)   -->  (gRPC)
                                                                 |
                        awg briefing  <--- gRPC client  <--------+
                        awg impact
                        awg preflight
```

AWG compiles YAML into RDF triples (N-Triples format), stores them in Oxigraph (a single-binary SPARQL store backed by RocksDB), and serves them via a gRPC API. The CLI commands are thin gRPC clients.

### gRPC RPCs

Seven RPCs — see the **[API reference](docs/api-reference.md)** for full message
shapes, enums, and status semantics.

| RPC | Purpose |
|-----|---------|
| `Briefing(file, task)` | Prose context (~500 tokens) with invariants, forbidden fixes, required tests |
| `Impact(file)` | Structured knowledge nodes grouped by type (direct + inferred) |
| `Preflight(files, task)` | Risk classification with confidence and required actions |
| `EditCheck(file, content)` | Advisory warnings if proposed content violates an in-scope rule (never blocks) |
| `Resolve(class, id)` | Single node lookup with full metadata |
| `Query(mode, ...)` | Constrained graph lookup (BY_FILE, BY_ID, BY_CLASS, RELATED) |
| `Metadata()` | Graph coverage and freshness stats |

No arbitrary SPARQL is exposed. All query modes are closed whitelists.

## Repo layout

```
cmd/
  awg/                 standalone CLI (init, build, serve, briefing, ...)
  yaml2nt/             YAML → N-Triples compiler
  loadnt/              N-Triples → Oxigraph loader
  awareness-mcp/       MCP bridge (optional, for Globular integration)
  annotation-scanner/  Go AST @awareness annotation extractor
  principle-check/     Meta-principle conformance scanners

golang/
  rdf/                 Triple emission + vocabulary constants
  extractor/           YAML → RDF importers + validators
  store/               Store interface + Oxigraph HTTP client
  server/              gRPC service handlers (Briefing, Impact, Resolve, ...)
  pb/                  Generated protobuf bindings
  client/              Go client library

proto/
  awareness_graph.proto    gRPC service contract

ontology/
  awareness.ttl            RDF vocabulary (Turtle) — source of truth

docs/
  cli-reference.md         every awg command + flag
  api-reference.md         gRPC service + MCP bridge
  agent-usage.md           how an agent should call AWG before edits
  meta-principles.md       the meta-principle framework reference
```

## Building from source

```bash
# Prerequisites: Go 1.23+, protoc (optional, for proto changes)

# Build everything
go build ./...

# Build the standalone CLI and server together
make awg

# Or build them separately if you need one artifact only
make awg-cli
make service-build

# Direct Go builds are still available
go build -o bin/awg ./cmd/awg
go build -o bin/awareness-graph ./golang/server

# Run tests
go test ./...

# Run AWG smoke test
make awg-smoke
```

## Oxigraph

Oxigraph is the RDF store that holds the compiled knowledge graph. It's a single static binary — no JVM, no cluster, no configuration.

```bash
# Docker
docker run -d --name oxigraph -p 7878:7878 ghcr.io/oxigraph/oxigraph

# Or binary (download from https://github.com/oxigraph/oxigraph/releases)
oxigraph serve --location .awg/data --bind 0.0.0.0:7878

# Verify
curl -s -X POST -H "Content-Type: application/sparql-query" \
  --data 'ASK {}' http://localhost:7878/query
# → true
```

Endpoints:
- Query: `http://localhost:7878/query` (SPARQL, used by the server)
- Store: `http://localhost:7878/store?default` (Graph Store, used by `awg build`)

## Server flags

| Flag | Default | Purpose |
|------|---------|---------|
| `-addr` | `:10120` | gRPC listen address |
| `-oxigraph-url` | `http://localhost:7878/query` | SPARQL query endpoint |
| `-require-store` | `false` | Exit if backend is unhealthy at startup |
| `-config` | (none) | Path to service config JSON |
| `-preflight` | (none) | Run offline preflight and exit (no Oxigraph needed) |
| `-version` | (none) | Print version and exit |
| `-describe` | (none) | Print service metadata JSON and exit |
| `-health` | (none) | Print health status JSON and exit |

## Principle checking

AWG includes meta-principle conformance scanners that check your code against the architectural principles:

```bash
# Run all scanners
make principle-check-all

# Run a specific scanner
make principle-check

# Positive-control attestation (proves scanners are alive)
make principle-check-positive
```

Scanners use ruleguard (AST-based pattern matching) and regex. Each scanner has a positive-control fixture that proves it fires on known-bad code, so a clean result means "attested clean" not "scanner is dead."

## Source annotation

Go source files can reference graph nodes with `@awareness` comments:

```go
// @awareness namespace=myproject
// @awareness implements=myproject:invariant.auth.session_httponly
// @awareness risk=high
func SetSessionCookie(w http.ResponseWriter, token string) {
    // ...
}
```

The annotation scanner (`cmd/annotation-scanner`) walks the Go AST and emits triples linking code symbols to knowledge nodes.

---

## Globular integration

AWG was extracted from the [Globular](https://github.com/globulario/services) platform where it runs as a native gRPC service with etcd registration, cluster TLS, and MCP tool exposure. The Globular-specific integration includes:

- **etcd service registration** — automatic discovery via the Globular service mesh
- **Cluster mTLS** — uses Globular PKI paths (`/var/lib/globular/pki/`)
- **MCP bridge** — exposes briefing/impact/resolve as MCP tools
- **Globular packaging** — `make service-package` builds a Globular-native package

For standalone use, none of this is needed — `awg serve` works with just Oxigraph.

### Globular packaging

```bash
make service-build     # Build server binary with ldflags
make service-dist      # Stage payload for package build
make service-package   # Build Globular package artifact
```

### MCP bridge

```bash
# Start the MCP bridge (connects to the gRPC server)
go run ./cmd/awareness-mcp -awareness-addr localhost:10120
```

Exposes seven stdio tools — `awareness_briefing`, `awareness_impact`,
`awareness_preflight`, `awareness_edit_check`, `awareness_resolve`,
`awareness_query`, `awareness_metadata`. See
[docs/api-reference.md](docs/api-reference.md#mcp-bridge-awareness-mcp).

---

## Origin

AWG was born from a real problem: the [Globular](https://github.com/globulario/services) codebase (465K lines, 33 services) kept having the same class of bugs ship because architectural rules lived in people's heads, not in a queryable system. After encoding 50+ incidents as invariants and failure modes, the awareness graph prevented regressions that post-mortems alone could not.

The meta-principles emerged from classifying those incidents. They turned out to be universal — they apply to any software system, not just distributed infrastructure. The set has grown to 133 across 8 categories as the GUI, code-structure, and safe-evolution categories were added.

## License

AWG is licensed under the **GNU Affero General Public License, Version 3 (AGPLv3)** — see [LICENSE](LICENSE)
and [NOTICE](NOTICE). This covers the local runtime: the `awg` CLI, the gRPC
server, the MCP bridge, the extraction/scanner pipeline, the gate, and the VS Code
extension. You may use, modify, and redistribute it, including commercially, under
the terms of that license.
