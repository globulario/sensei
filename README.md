<p align="center">
  <img src="image/sensei-logo.png" alt="Sensei" width="200">
</p>

<h1 align="center">Sensei</h1>

<p align="center"><strong>Architectural memory for AI coding agents.</strong><br>
<em>Give your agent the rules it must know before editing your code.</em></p>

<p align="center">
  <a href="https://github.com/globulario/sensei/actions/workflows/ci.yml"><img src="https://github.com/globulario/sensei/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache_2.0-blue.svg" alt="License: Apache 2.0"></a>
  <a href="go.mod"><img src="https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.25+"></a>
  <img src="https://img.shields.io/badge/runtime-standalone-success" alt="Standalone">
</p>

Before your agent edits a file, Sensei tells it:

- the **invariants** that govern it
- the **failures** that happened there before
- the **fixes it must not attempt**
- the **tests** that prove the architecture still holds

Works with **Claude Code, Cursor, Codex, CI, and any MCP-compatible agent.**
Local. Open source. Repository-owned.

---

## See it in one command

```bash
git clone https://github.com/globulario/sensei.git
cd sensei && ./scripts/install.sh && export PATH="$PWD/bin:$PATH"

sensei demo
```

```
Sensei demo — examples/payment-cold-start

  ✓ store started
  ✓ awareness loaded (15 triples)
  ✓ briefing server ready
  ✓ briefing generated

$ sensei briefing --file src/payment_processor.py

Decision focus:
- Respect: [critical] payments.paid_state_requires_processor_confirmation —
  An order records as paid only after processor confirmation, never from a local cache write
```

`sensei demo` stands up the whole machine — the local store, the graph, the
server — on throwaway ports, returns one real briefing, and cleans up. No config,
no ports of yours touched. Run it on your own repo with `sensei demo --repo .`.

<p>
✓&nbsp;Local &amp; repository-owned &nbsp;·&nbsp; ✓&nbsp;Apache&nbsp;2.0 &nbsp;·&nbsp; ✓&nbsp;No cloud account &nbsp;·&nbsp; ✓&nbsp;Runs standalone &nbsp;·&nbsp; ✓&nbsp;Tool-agnostic (CLI&nbsp;·&nbsp;gRPC&nbsp;·&nbsp;MCP) &nbsp;·&nbsp; ✓&nbsp;Cold-start smoke test in the box
</p>

## What changes when Sensei exists

**Without Sensei**

> **Agent:** "I'll set `paid = true` when the callback arrives."

A locally correct change. Also the exact shape of last quarter's incident.

**With Sensei** — the agent asks for a briefing first and gets back:

```
CRITICAL  payments.paid_state_requires_processor_confirmation
  "paid" is money truth — it must come from the processor's confirmation,
  never from a local cache or callback payload.

Forbidden fix:
  - trusting the local callback payload

Required test:
  - TestPaidStateRequiresVerifiedConfirmation
```

> **Agent:** "I need to verify the processor's confirmation before changing state."

That is the whole product: the rule that lived in a senior engineer's head now
reaches the agent **before** the edit, in about two milliseconds.

## Why this exists

Every codebase accumulates invisible rules — things that broke, got fixed, and
now live only in someone's head or in commit archaeology: *this state comes from
that authority, never the cache; this fix looks right but reintroduces last
quarter's outage; this file is load-bearing.* AI agents write code well but
can't see those rules, so they make reasonable-looking changes that quietly
violate them, and the codebase drifts from its own design.

Sensei makes that knowledge **explicit and queryable**. You write your intent,
invariants, failure modes, forbidden fixes, and architecture rules as small YAML
files. Sensei compiles them into a graph and answers one question, before the
edit: *"What do I need to know before editing this file?"*

## Quick start (5 minutes)

Linux or macOS. **Go 1.25+** for the source build; **Oxigraph** (the local store)
is fetched for you — no Docker.

```bash
# 1. Install — builds sensei + server, fetches oxigraph into bin/
git clone https://github.com/globulario/sensei.git
cd sensei
./scripts/install.sh
export PATH="$PWD/bin:$PATH"

# 2. See a real briefing, end to end, in one command
sensei demo

# 3. Do it on your own project
cd /path/to/your-repo
sensei init                    # scaffold docs/awareness/ + agent hooks
#   ... write one invariant (see "Protect your first file" below) ...
sensei demo --repo .
```

More: **[QUICKSTART.md](QUICKSTART.md)** (the detailed walkthrough incl. Claude
Code hooks) · **[INSTALL.md](INSTALL.md)** (platforms, the Oxigraph dependency,
the supervised local runtime).

## What Sensei returns before an edit

One query, `sensei briefing --file <path>`, returns exactly what an agent needs
to change a file safely:

- **Invariants** — the rules that govern the file, with severity
- **Forbidden fixes** — the tempting changes that reintroduce known bugs
- **Failure modes** — what went wrong here before, and the real fix
- **Required tests** — the proof the architecture still holds
- **Authority** — whether the graph itself is current and trustworthy

Other surfaces for other moments: `impact` (structured nodes), `preflight` (risk
before a task), `edit-check` (does *this* proposed content violate a rule),
`gate` (block a bad diff in CI), `propose` (record a new scar).

## Works with your coding agent

Sensei is a CLI + a local gRPC server + an MCP bridge. Nothing is locked to one
assistant.

| Agent / surface | How it connects |
|---|---|
| **Claude Code** | Generated pre-edit hooks (`sensei init`) enforce *consult-then-comply* |
| **Cursor** | CLI or MCP integration |
| **Codex** | Repository instructions + CLI / MCP |
| **CI (GitHub Actions)** | Gate PR diffs before merge — [one `uses:` line](#ci-gate-github-actions) |
| **Any agent** | gRPC, MCP, or a plain shell command |

## Protect your first file

The smallest useful thing Sensei can do — encode one rule and get one briefing:

**1.** In your project, create `docs/awareness/invariants.yaml`:

```yaml
invariants:
  - id: auth.session_token_must_be_httponly
    title: Session tokens must use HttpOnly cookies — never JS-readable
    severity: critical
    status: active
    protects:
      files: [src/auth/session.go]
    forbidden_fixes:
      - expose_token_in_response_body_for_spa
    required_tests:
      - TestSessionCookieIsHttpOnly
```

**2.** Mark it high-risk in `docs/awareness/high_risk_files.yaml`:

```yaml
files:
  - src/auth/session.go
```

**3.** See it reach the agent:

```bash
sensei demo --repo .
# → CRITICAL auth.session_token_must_be_httponly, before any edit to session.go
```

That's Level 1. You never have to do more than this to get value.

## Adopt it as a staircase, not a cliff

| Level | You do | You get |
|---|---|---|
| **1 — One invariant** | protect one dangerous file | briefings on that file |
| **2 — One scar** | record an incident + its forbidden fixes | the bug can't come back the same way |
| **3 — Pre-edit consultation** | enable the Claude Code hooks | agents must look before they edit |
| **4 — CI governance** | add the [gate action](#ci-gate-github-actions) | dangerous diffs blocked before merge |
| **5 — Architectural graph** | `sensei bootstrap` | inferred contracts, dependencies, symbols, coverage |

## Who is this for?

Sensei earns its place when:

- senior engineers keep repeating the same architecture warnings
- AI agents make locally correct but globally dangerous changes
- important rules live in Slack, post-mortems, and memory
- regressions repeat after team turnover
- code review depends on one or two people who know the history
- onboarding is months of repository archaeology

## What Sensei is (and isn't)

Sensei is **governed architectural memory for a software repository.**

| Sensei is **not** | Sensei **is** |
|---|---|
| another code-generation model | repository-owned architectural knowledge |
| a generic doc-search / RAG tool | queried *before* changes, at edit time |
| a linter with hard-coded rules | connected to files, incidents, forbidden fixes, tests |
| a hosted knowledge base | local, versioned with your code |
| a replacement for tests or review | usable by any agent, enforceable in CI |

## CI gate (GitHub Actions)

Block architecture-violating pull requests before merge. The action fetches
Oxigraph and runs the server for you, so your workflow is one `uses:` line:

```yaml
# .github/workflows/sensei-gate.yml  (see docs/ci/sensei-gate.yml)
name: Sensei architecture gate
on: [pull_request]
jobs:
  gate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: globulario/sensei/.github/actions/sensei-gate@main
        with:
          enforce: 'true'      # 'false' = advisory report, never blocks
```

The gate evaluates the PR's added/changed lines with the same engine agents call.
`--enforce` exits non-zero on a blocking finding and **fails closed** if it can't
verify the diff. Per-repo policy (`.sensei/gate-policy.yaml`) can re-level or
silence any rule without a code change.

## The meta-principles: rules you haven't written yet

Your project's invariants and scars tell Sensei **what must remain true here.**
The meta-principles help Sensei discover **where architectural danger tends to
hide** in the first place. Every project ships with **133 universal principles
across 8 categories**, distilled from real production incidents and portable to
any system:

| Category | Count | Examples |
|---|---|---|
| **Authority** | 20 | wrong actor writes truth; one value, two meanings |
| **Signal** | 19 | a fallback that looks like truth; errors absorbed into timeouts |
| **Lifecycle** | 38 | a write with no cleanup; intermediate state that looks done |
| **Dependency** | 7 | critical path blocked by a non-critical service |
| **Perception** | 19 | a green badge lying about runtime state |
| **Composition** | 7 | success color on unconfirmed state |
| **Structure** | 12 | a "shared" component whose consumers import its internals |
| **Evolution** | 11 | releasable trunk; reviewable slices; deterministic builds |

Reference: **[docs/meta-principles.md](docs/meta-principles.md)**. Query any one:
`sensei resolve invariant meta.<id>`.

## A real-world origin

Sensei was born in **[Globular](https://github.com/globulario)** — a 465K-line
distributed platform with 33 services, built with AI agents doing significant
implementation work. The agents were productive, but kept reintroducing the same
class of architectural failure:

- authority confusion (the wrong component writing truth)
- stale state presented as truth
- recovery paths that depended on the systems they had to recover
- fixes that solved a symptom while violating an ownership boundary

Those 50+ incidents became queryable invariants, failure modes, forbidden fixes,
and required tests — and the recurring shapes became the 133 portable
meta-principles. What bit us is provenance; what we learned ships with every
`sensei init`. Sensei now runs entirely on its own, for any codebase.

---

## Core concepts — what you write

Three YAML files carry most of the value (`sensei init` scaffolds them):

- **`invariants.yaml`** — rules that must always hold (with `protects`,
  `forbidden_fixes`, `required_tests`).
- **`failure_modes.yaml`** — incidents that happened or could, with `root_cause`
  and the real `architecture_fix`.
- **`incident_patterns.yaml`** — the edit *shapes* that introduce bugs.

Full schema + examples: **[QUICKSTART.md](QUICKSTART.md)** and
**[docs/cli-reference.md](docs/cli-reference.md)**.

## Common commands

The **[full CLI reference](docs/cli-reference.md)** documents every command and flag.

| Command | What it does |
|---|---|
| `sensei demo` | Stand up a graph and return one real briefing — one command |
| `sensei init` | Scaffold a project (YAML templates, hooks, CLAUDE.md) |
| `sensei bootstrap` | Initialize Sensei for an existing repo (extraction + optional history) |
| `sensei build` | Compile YAML → the store (`--output file.nt` for no-store) |
| `sensei serve` | Start Oxigraph + the gRPC server |
| `sensei briefing --file <p>` / `--task "…"` | Context for a file or task |
| `sensei impact --file <p>` | Structured knowledge nodes for a file |
| `sensei preflight --file <p> --task "…"` | Risk classification before editing |
| `sensei edit-check --file <p> --content-file -` | Advisory: does this edit violate a rule? |
| `sensei gate --diff <range> --enforce` | Hard gate over a git diff (CI) |
| `sensei propose --kind …` | Record a scar (typed feedback); rebuild; stage — never commits |
| `sensei check` / `validate` / `audit` | Validate YAML / structural check / self-audit |

## Agent integration (hooks + MCP)

`sensei init` generates Claude Code hooks that enforce *consult-then-comply*:
`enforce-briefing.sh` (did you *look*?), `edit-check-guard.sh` (does what you're
about to *write* violate a rule?), `record-briefing.sh`. Wire them in
`.claude/settings.json` — see **[QUICKSTART.md](QUICKSTART.md)**.

For MCP-based agents, the bridge exposes seven stdio tools
(`awareness_briefing`, `awareness_impact`, `awareness_preflight`,
`awareness_edit_check`, `awareness_resolve`, `awareness_query`,
`awareness_metadata`):

```bash
go run ./cmd/awareness-mcp -awareness-addr localhost:10120
```

See **[docs/api-reference.md](docs/api-reference.md)**.

## Architecture

```
docs/awareness/*.yaml  ──sensei build──▶  Oxigraph (RDF store)  ──gRPC──▶  sensei serve
       (you write)        (yaml2nt)                                            │
                                                                              ▼
                            sensei briefing / impact / preflight / edit-check / gate
```

Sensei compiles YAML into RDF triples, stores them in Oxigraph (a single-binary
SPARQL store — no JVM, no cluster), and serves them over gRPC. The CLI commands
are thin clients. **No arbitrary SPARQL is exposed** — every query mode is a
closed whitelist. Seven RPCs: `Briefing`, `Impact`, `Preflight`, `EditCheck`,
`Resolve`, `Query`, `Metadata` — see **[docs/api-reference.md](docs/api-reference.md)**.

<details>
<summary><strong>Repo layout</strong></summary>

```
cmd/awg/               standalone CLI (demo, init, build, serve, briefing, gate, …)
cmd/yaml2nt/           YAML → N-Triples compiler
cmd/awareness-mcp/     MCP bridge (stdio)
cmd/annotation-scanner Go AST @awareness annotation extractor
golang/rdf/            triple emission + vocabulary
golang/extractor/      YAML → RDF importers + validators + structural scanners
golang/server/         gRPC service handlers (embeds the self seed)
golang/client/         Go client library
proto/                 gRPC service contract
ontology/awareness.ttl RDF vocabulary (source of truth)
docs/                  cli-reference · api-reference · agent-usage · meta-principles
```
</details>

## Building from source

```bash
go build ./...                 # everything
make sensei                    # CLI + server together
make sensei-smoke              # end-to-end smoke test (init → check → build)
go test ./...                  # tests
```

Prerequisites: **Go 1.25+** (protoc optional, only for proto changes). Oxigraph
is fetched by `scripts/fetch-oxigraph.sh` (called by `install.sh`) — a single
static binary. Endpoints: query `http://localhost:7878/query`, store
`http://localhost:7878/store?default`.

<details>
<summary><strong>Source annotation, principle scanners, running inside Globular (optional)</strong></summary>

**Source annotation** — Go files can link code to graph nodes:

```go
// @awareness namespace=myproject
// @awareness implements=myproject:invariant.auth.session_httponly
func SetSessionCookie(w http.ResponseWriter, token string) { /* … */ }
```

**Principle scanners** — `make principle-check-all` runs AST + regex conformance
scanners; each has a positive-control fixture, so a clean result means *attested
clean*, not *scanner dead*.

**Running inside Globular** — Sensei also runs as a native Globular gRPC service
(etcd registration, cluster mTLS, MCP tool exposure, `make service-package`).
None of it is required for standalone use.
</details>

## License

Apache License, Version 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE). Covers
the whole local runtime (CLI, server, MCP bridge, extractors, the gate, the VS
Code extension). Use, modify, and redistribute — including commercially.
