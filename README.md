<p align="center">
  <img src="image/sensei-logo.png" alt="Sensei" width="200">
</p>

<h1 align="center">Sensei</h1>

<p align="center"><strong>Architectural memory for AI coding agents.</strong><br>
<em>Make your agent aware of the repository before it changes it.</em></p>

<p align="center">
  <a href="https://github.com/globulario/sensei/actions/workflows/ci.yml"><img src="https://github.com/globulario/sensei/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache_2.0-blue.svg" alt="License: Apache 2.0"></a>
  <a href="go.mod"><img src="https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.25+"></a>
  <img src="https://img.shields.io/badge/runtime-standalone-success" alt="Standalone">
</p>

Your AI agent writes code well but can't see your architecture — the rules that
live in a senior engineer's head and in commit archaeology. Sensei makes that
knowledge a **local, queryable graph** your agent consults through **MCP**, so
before it edits a file it already knows:

- the **invariants** that govern it
- the **failures** that happened there before
- the **fixes it must not attempt**
- the **tests** that prove the architecture still holds

Local. Open source. Repository-owned. Works with **Claude Code, Cursor, Codex,
and any MCP-compatible agent** — and gates pull requests in **CI**.

## What changes when Sensei exists

**Without Sensei** — the agent makes a locally correct change that is the exact
shape of last quarter's incident:

> **Agent:** "I'll set `paid = true` when the callback arrives."

**With Sensei** — the agent asks for a briefing first and gets:

```
CRITICAL  payments.paid_state_requires_processor_confirmation
  "paid" is money truth — it must come from the processor's confirmation,
  never from a local cache or callback payload.

Forbidden fix:  trusting the local callback payload
Required test:  TestPaidStateRequiresVerifiedConfirmation
```

> **Agent:** "I need to verify the processor's confirmation before changing state."

---

## Proof: does it work on a real codebase?

We pointed Sensei at [**Caddy**](https://github.com/caddyserver/caddy) from a
pristine, pre-Sensei checkout. Cold and deterministic, it mapped the structure
(17 components, 490 tests, 176 source anchors). Fed the project's PR history, it
recovered real architecture laws — **each citing a specific Caddy PR** — like:

> A streaming reverse-proxy copy must honor context cancellation — the shortcut
> leaks the connection. *Grounded in PR #4952, tried in `f5dce84a` and reverted
> by `238f1108`.*

That's mined from Caddy's own history, not guessed. **[Read the full Caddy case
study →](docs/case-studies/caddy.md)** (every command reproducible).

---

## Install (one line)

Prebuilt, self-contained (`sensei` + server + MCP bridge + `oxigraph`) — no Go
toolchain, no Docker. **Linux / macOS:**

```bash
curl -fsSL https://raw.githubusercontent.com/globulario/sensei/main/install.sh | sh
```

**Windows** (PowerShell):

```powershell
irm https://raw.githubusercontent.com/globulario/sensei/main/install.ps1 | iex
```

It detects your platform (`linux-amd64/arm64`, `darwin-arm64`, `windows-amd64`),
downloads and checksum-verifies the matching release, and installs the binaries
onto your PATH (then run `sensei init --mcp` in your repo to wire up your agent).
Pin a version with `SENSEI_VERSION=v1.2.1`, or change the target dir with
`SENSEI_PREFIX=…`.

Or via a **package manager** (you get `upgrade` for free):

```bash
brew install globulario/tap/sensei     # Homebrew — macOS (Apple Silicon), Linux
```
```powershell
winget install Globulario.Sensei       # winget — Windows
```

<details>
<summary>Other ways: download a tarball, or build from source</summary>

Each [release](https://github.com/globulario/sensei/releases) also ships the
raw `sensei-<platform>.tar.gz` (the four binaries + a `setup.sh`) if you prefer
to grab it yourself:

```bash
curl -fsSL -O https://github.com/globulario/sensei/releases/latest/download/sensei-linux-amd64.tar.gz
tar xzf sensei-linux-amd64.tar.gz && cd sensei-linux-amd64 && ./setup.sh
```

Or build from source (needs **Go 1.25+**):

```bash
git clone https://github.com/globulario/sensei.git
cd sensei && ./scripts/install.sh && export PATH="$PWD/bin:$PATH"
```

On **Windows**, `sensei.exe` (serve/build/gate/queries) and the CI Action run
natively; the pre-edit enforcement hooks are bash, so *local* enforcement needs
Git Bash or WSL.
</details>

> Kick the tires first (optional): `sensei demo` stands up the whole stack on
> throwaway ports and returns one real briefing, then cleans up.

## Make your agent repo-aware (the core workflow)

Everything below is something you say to your coding agent. Sensei does the work;
the agent drives it and then benefits from it.

**1. Wire your agent up — one command.** From your repo:

```bash
sensei init --mcp
```

This configures whatever agent tool you use — additively, and it never
overwrites your existing rules (re-running is idempotent):

- **`.sensei/skills/sensei-architect/`** — the canonical bundled Sensei
  Architect skill, installed by default. It teaches agents to turn Sensei
  memory into a proportional architectural reflex.
- **`.agents/skills/sensei-architect/`** and
  **`.claude/skills/sensei-architect/`** — native project skill copies for
  Codex / Agent Skills and Claude Code.
- **CLAUDE.md** and **AGENTS.md** — appends a Sensei section (the cross-tool
  `AGENTS.md` convention is read by Cursor, Amp, and others).
- **`.cursor/rules/sensei.mdc`** — a Cursor rule pointing to the canonical
  skill package; Cursor does not rely on project `SKILL.md` discovery here.
- **`.claude/hooks/`** — the PreToolUse push/guard hooks.
- **`.mcp.json`** (with `--mcp`) — writes/merges the `sensei` MCP server,
  resolving the `awareness-mcp` path for you; it never clobbers other servers
  and removes stale legacy `awg` / `awareness-graph` MCP entries.

Toggle any surface with `--skills` / `--claude-md` / `--agents-md` / `--cursor`
/ `--hooks` (default on) and `--mcp` (opt-in). Re-running the same Sensei
version does not rewrite unchanged managed skills. A newer Sensei can update an
untouched managed skill copy; a locally edited copy is preserved with a notice
unless you explicitly pass `--skills-force`. Prefer to add the MCP server by
hand? Put this in `.mcp.json` at the repo root (Claude Code):

```json
{
  "mcpServers": {
    "sensei": {
      "command": "/absolute/path/to/bin/awareness-mcp",
      "args": ["--awareness-addr", "localhost:10120"]
    }
  }
}
```

Then, in your agent:

> **You:** "Is the Sensei MCP available? List the `mcp__sensei__*` tools."

The agent should see `awareness_metadata`, `awareness_preflight`,
`awareness_briefing`, `awareness_impact`, `awareness_resolve`,
`awareness_query`, `awareness_edit_check`, and `awareness_propose`.

**2. Bootstrap the repository's awareness graph.**

> **You:** "Bootstrap this repo with Sensei, then start the server."

```bash
sensei bootstrap --repo .      # extract architecture (contracts, components, symbols, tests) → docs/awareness/
sensei serve -no-seed &        # start the local store + server on :10120 (your graph only)
sensei build                   # compile docs/awareness/ into the running graph
```

**3. Ask the agent to evaluate the repository.** Now the agent can reason about
the repo *from the graph*, not just the files it happened to read:

> **You:** "Run a Sensei repo evaluation and summarize the risks."

```bash
sensei repo-eval               # architecture + awareness quality: is this repo ready for controlled agent work?
```

**4. Ask it to audit the awareness graph itself** — drift, gaps, dangling rules:

```bash
sensei audit                   # self-audit: what's stale, uncovered, or inconsistent
```

**5. From now on, the agent consults before it edits.** With the MCP tools wired,
your agent calls `awareness_briefing` before touching a file and gets the rules,
forbidden fixes, and required tests that apply — in about two milliseconds. Add
the generated hooks (`sensei init` writes them) to *enforce* consult-then-comply
rather than rely on the agent's good behavior.

## CI: gate every pull request

Make the same knowledge block architecture-violating PRs before merge. The action
installs Sensei, fetches Oxigraph and runs the server for you, evaluates the diff,
and writes a summary into the job — so the consuming workflow is one `uses:` line:

```yaml
# .github/workflows/sensei.yml  (full example: docs/ci/sensei-gate.yml)
name: Sensei architectural review
on: [pull_request]
permissions:
  contents: read
  security-events: write     # so findings appear in Security → Code scanning
jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: globulario/sensei-action@v1
        with:
          mode: advisory          # advisory (report, never blocks) | enforce (fail on a violation)
```

`enforce` mode exits non-zero on a blocking finding and **fails closed** if it
can't verify the diff. Per-repo policy (`.sensei/gate-policy.yaml`) can re-level
or silence any rule without a code change. The action posts a Markdown summary to
the GitHub **job summary** each run.

## What the agent gets before an edit

`awareness_briefing` (or `sensei briefing --file <path>`) returns exactly what an
agent needs to change a file safely:

- **Invariants** — the rules that govern the file, with severity
- **Forbidden fixes** — tempting changes that reintroduce known bugs
- **Failure modes** — what went wrong here before, and the real fix
- **Required tests** — the proof the architecture still holds
- **Authority** — whether the graph itself is current and trustworthy

Other surfaces for other moments: `impact` (structured nodes), `preflight` (risk
before a task), `edit-check` (does *this* proposed content violate a rule),
`propose` (record a new scar the agent just learned).

## See it in your editor (VS Code)

Sensei has a companion VS Code extension — **[Sensei on the
Marketplace](https://marketplace.visualstudio.com/items?itemName=globulario.sensei-awareness)**
(`globulario.sensei-awareness`):

```
code --install-extension globulario.sensei-awareness
```

It's a **client of the Sensei CLI** — install `sensei` (see [Install](#install-one-line))
and run `sensei serve`; the extension reads the graph that server hosts over gRPC.
It gives you two surfaces:

- **This File** (activity bar) — the invariants, forbidden fixes, failure modes,
  risk class, and required tests that govern the file you're editing, with
  explicit "visible absence" when nothing anchors to it.
- **Project dashboard** (`Sensei: Open Project Dashboard`) — an architect's
  cockpit: a control banner (per-class totals + a trust signal), aspect
  navigation across invariants / failure modes / intents / patterns / files, and
  a clickable focus-graph of any node's neighbourhood.

<!-- TODO: add a dashboard screenshot captured on a public graph (Sensei's own
     repo or `sensei demo`) — not a private project, to avoid publishing internal
     architecture. -->

## Adopt it as a staircase, not a cliff

| Level | You do | You get |
|---|---|---|
| **1 — One invariant** | protect one dangerous file | briefings on that file |
| **2 — One scar** | record an incident + its forbidden fixes | that bug can't return the same way |
| **3 — Agent consults** | wire the MCP tools + hooks | agents look before they edit |
| **4 — CI governance** | add the [gate action](#ci-gate-every-pull-request) | dangerous diffs blocked before merge |
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

## The meta-principles: rules you haven't written yet

Your project's invariants and scars tell Sensei **what must remain true here.**
The meta-principles help Sensei discover **where architectural danger tends to
hide.** Every project ships with **134 universal principles across 8 categories**,
distilled from real production incidents and portable to any system:

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

Reference: **[docs/meta-principles.md](docs/meta-principles.md)**. Query one:
`sensei resolve invariant meta.<id>`.

## A real-world origin

Sensei was born in **[Globular](https://github.com/globulario)** — a 465K-line
distributed platform with 33 services, built with AI agents doing significant
implementation work. They were productive but kept reintroducing the same class
of architectural failure (authority confusion, stale state presented as truth,
recovery paths that depend on what they must recover, fixes that violate ownership
boundaries). Those 50+ incidents became queryable invariants, failure modes,
forbidden fixes, and required tests — and the recurring shapes became the 134
portable meta-principles that ship with every `sensei init`. Sensei now runs
entirely on its own, for any codebase.

---

## Command reference

The **[full CLI reference](docs/cli-reference.md)** documents every command and flag.

| Command | What it does |
|---|---|
| `sensei bootstrap --repo .` | Extract a repo's architecture into `docs/awareness/` |
| `sensei init` | Scaffold a project (YAML templates) + install the Sensei Architect skill + wire agent tools (CLAUDE.md, AGENTS.md, Cursor, hooks; `--mcp` for `.mcp.json`) |
| `sensei serve` | Start Oxigraph + the gRPC server |
| `sensei build` | Compile `docs/awareness/` into the store |
| `sensei repo-eval` | Evaluate architecture + awareness quality |
| `sensei audit` | Self-audit the graph for drift, gaps, inconsistencies |
| `sensei briefing --file <p>` / `--task "…"` | Context for a file or task |
| `sensei impact` / `preflight` / `edit-check` | Nodes / risk / advisory edit check |
| `sensei gate --diff <range> --mode enforce` | Gate a git diff in CI |
| `sensei propose --kind …` | Record a scar (typed feedback); stage — never commits |
| `sensei check` / `validate` | Validate YAML sources / deep structural check |
| `sensei demo` | Stand up a graph and return one briefing — one command |

## Agent integration (MCP + hooks)

The MCP bridge exposes eight stdio tools (`awareness_metadata`,
`awareness_preflight`, `awareness_briefing`, `awareness_impact`,
`awareness_resolve`, `awareness_query`, `awareness_edit_check`,
`awareness_propose`):

```bash
go run ./cmd/awareness-mcp -awareness-addr localhost:10120
```

`sensei init` also generates Claude Code hooks that *enforce* consult-then-comply:
`enforce-briefing.sh` (did you *look*?), `edit-check-guard.sh` (does what you're
about to *write* violate a rule?), `record-briefing.sh`. Wire them in
`.claude/settings.json` — see **[QUICKSTART.md](QUICKSTART.md)** and
**[docs/api-reference.md](docs/api-reference.md)**.

## Architecture

```
docs/awareness/*.yaml  ──sensei build──▶  Oxigraph (RDF store)  ──gRPC──▶  sensei serve ──▶  MCP bridge ──▶  your agent
       (extracted)        (yaml2nt)                                            │
                            sensei briefing / impact / preflight / edit-check / gate
```

Sensei compiles YAML into RDF triples, stores them in Oxigraph (a single-binary
SPARQL store — no JVM, no cluster), and serves them over gRPC. The CLI and the MCP
bridge are thin clients. **No arbitrary SPARQL is exposed** — every query mode is
a closed whitelist.

<details>
<summary><strong>Repo layout · building from source · running inside Globular</strong></summary>

```
cmd/awg/               standalone CLI (bootstrap, init, build, serve, briefing, gate, demo, …)
cmd/awareness-mcp/     MCP bridge (stdio)
cmd/yaml2nt/           YAML → N-Triples compiler
golang/server/         gRPC service handlers (embeds the self seed)
golang/extractor/      extraction + validators + structural scanners
proto/  ontology/      gRPC contract · RDF vocabulary (source of truth)
docs/                  cli-reference · api-reference · agent-usage · meta-principles
```

```bash
go build ./...                 # everything
make sensei                    # CLI + server together
make sensei-smoke              # end-to-end smoke test
go test ./...                  # tests
```

Prerequisites: **Go 1.25+** (protoc optional). Sensei also runs as a native
Globular gRPC service (etcd registration, cluster mTLS, `make service-package`);
none of it is required for standalone use.
</details>

## License

GNU Affero General Public License, Version 3 (AGPLv3) — see [LICENSE](LICENSE) and [NOTICE](NOTICE). Covers
the whole local runtime (CLI, server, MCP bridge, extractors, the gate, the VS
Code extension). Use, modify, and redistribute — including commercially.
