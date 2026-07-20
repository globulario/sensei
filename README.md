<p align="center">
  <img src="image/sensei-logo.png" alt="Sensei" width="200">
</p>

<h1 align="center">Sensei</h1>

<p align="center">
  <strong>Executable architecture for AI-driven software change.</strong><br>
  <em>Make architecture queryable, authority explicit, changes bounded, results reproducible, and proof accountable.</em>
</p>

<p align="center">
  <a href="https://github.com/globulario/sensei/actions/workflows/ci.yml"><img src="https://github.com/globulario/sensei/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-AGPL_v3-blue.svg" alt="License: AGPLv3"></a>
  <a href="go.mod"><img src="https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.25+"></a>
  <img src="https://img.shields.io/badge/runtime-local%20%2B%20standalone-success" alt="Local and standalone">
</p>

Sensei is an open-source architecture governance system for AI coding agents.

It turns the knowledge scattered across code, tests, documentation, incidents,
pull-request history, and senior engineers' memory into a repository-owned
architectural model that agents can query before they change the system.

That is the starting point.

Sensei is also building a deterministic **architectural closure protocol** around
probabilistic coding agents. The goal is not merely to help an agent write a
plausible patch. The goal is to determine whether an exact software change was
authorized, stayed inside scope, preserved the applicable architecture, produced
a reproducible result, satisfied its proof obligations, and may legally become
part of the project's history.

```text
agent proposes
     ↓
Sensei binds identity, scope, direction, and authority
     ↓
the mutation is admitted and observed
     ↓
the exact result architecture is rebuilt deterministically
     ↓
proof requirements are derived and discharged
     ↓
the transition is recorded
     ↓
the task may earn architectural closure
```

> More context can make an agent smarter. It does not make the agent authoritative,
> deterministic, complete, or accountable.

---

## Why Sensei exists

AI coding agents are excellent at producing locally reasonable code.

They do not automatically know:

- which source owns a piece of truth
- which rule is current, historical, intended, inferred, or contested
- who is allowed to change a state
- which mutation path is legal
- whether a patch matches the operation that was approved
- whether generated architecture still describes the post-change repository
- whether all required evidence was collected
- whether a clean diff or passing test suite is sufficient
- whether a result may become an authoritative project fact

A larger context window helps the model read more of the case. It does not create
the court, the constitution, the evidence rules, or the public record.

Sensei supplies those missing structures.

---

## The core model

Sensei keeps three truth surfaces separate.

### 1. Governed knowledge sources

Repository-owned sources express architectural meaning:

- invariants
- contracts
- intents and direction
- authority domains and mutation paths
- failure modes
- forbidden fixes
- required tests and proof obligations
- evidence and certification policies
- code annotations
- approved decisions

These sources are reviewed and versioned with the repository.

### 2. Compiled architecture graph

Sensei deterministically compiles governed sources and extracted repository
structure into a queryable RDF graph.

The graph supports:

- file and task briefings
- impact analysis
- preflight risk
- claims and epistemic status
- architecture planes
- closure assessment
- architect questions
- required proof extraction

The graph is a **compiled projection**, not authority merely because it exists.

### 3. Task closure ledger

Operational decisions belong in an append-only, content-addressed ledger:

- task and result bindings
- actor and authority resolution
- admission and capability consumption
- observed mutations
- scope verification
- evidence receipts
- proof discharge
- certification
- result transitions
- completion, revocation, and migration receipts

The ledger records what happened. It does not become a second architectural
knowledge corpus.

```text
governed sources ──compile──▶ architecture graph
       │                           │
       │                           └── query, reason, assess
       │
       └── govern ──▶ task protocol ──append──▶ closure ledger
```

Each surface has one job. Sensei refuses to blur them.

---

## What architectural closure means

A task is architecturally closed only when the same declared task and exact
result snapshot satisfy every required dimension.

| Dimension | Required truth |
|---|---|
| **Identity** | Repository, base revision, result tree, graph, task, session, policies, and artifacts resolve to one consistent world. |
| **Scope** | Every read and mutation target is represented, bounded, and associated with the intended architecture. |
| **Direction** | The change has an authoritative preserve, evolve, migrate, or not-applicable direction. |
| **Authority** | The actor, delegation, owner, action, target, and legal mutation mechanism are valid. |
| **Mutation** | The observed result matches the admitted operations and no capability was replayed. |
| **Protection** | Applicable invariants, contracts, failure modes, forbidden moves, exceptions, tests, and proof obligations are accounted for. |
| **Epistemic state** | Every load-bearing proposition is supported, contradicted, explicitly unknown, or covered by a valid bounded exception. |
| **Proof** | Every required proof slot is discharged by compatible, binding-valid, fresh evidence. |
| **Freshness** | Generated artifacts, graph state, tests, runtime observations, and evidence still bind the exact result. |
| **Completion** | A terminal immutable receipt binds the complete chain and no load-bearing blocker remains. |

```text
Closed(task) =
    IdentityValid
  AND ScopeClosed
  AND DirectionResolved
  AND AuthorityValid
  AND MutationCompliant
  AND ProtectionSatisfied
  AND EpistemicStateSafe
  AND ProofDischarged
  AND ResultArtifactsFresh
  AND CompletionReceiptValid
```

A score, model confidence, successful command, clean diff, or passing subset of
tests is never sufficient by itself.

---

## What Sensei provides today

### Repository architecture and behavioral memory

Sensei can extract, compile, validate, and query:

- components and dependencies
- source files and symbols
- tests and coverage anchors
- contracts and authority surfaces
- invariants and failure modes
- forbidden fixes
- project intent
- historical decisions
- proof obligations
- portable architectural meta-principles

### Agent briefings and preflight

Before an agent edits a file or begins a task, Sensei can return:

- applicable invariants
- known failure modes
- forbidden fixes
- required tests
- relevant contracts
- authority information
- missing or contested knowledge
- risk and impact

### MCP, hooks, and editor integration

Sensei works with Claude Code, Codex, Cursor, and other MCP-compatible agents.

It provides:

- MCP query tools
- a bundled Sensei Architect skill
- generated `CLAUDE.md` and `AGENTS.md` guidance
- Claude Code pre-edit hooks
- Cursor rules
- a VS Code architecture view

### CI governance

Sensei can evaluate a diff against the governed architecture and report or block
violations in GitHub Actions.

### Architectural closure program

The closure protocol is being implemented as a fail-closed sequence rather than
a single optimistic verdict:

```text
bindings
→ authority
→ admission
→ observed mutation
→ scope verification
→ proof requirements
→ evidence and certification
→ deterministic result reconstruction
→ recorded result transition
→ terminal completion
→ revocation and migration
```

The graph, MCP, briefing, extraction, audit, CI, task sessions, and the terminal architectural-closure protocol are fully implemented and usable today.

---

## A briefing before an edit

Without governed architecture, an agent may rediscover the same dangerous
shortcut that caused the last incident.

> **Agent:** "I will set `paid = true` when the callback arrives."

With Sensei, the agent asks first:

```text
CRITICAL  payments.paid_state_requires_processor_confirmation

"paid" is money truth. It must come from the processor's verified
confirmation, never from a local cache or an untrusted callback payload.

Forbidden fix:
  trust the local callback payload

Required test:
  TestPaidStateRequiresVerifiedConfirmation
```

> **Agent:** "I need to verify the processor confirmation through the owner path
> before changing payment state."

Sensei does not make the decision for the agent. It makes the architecture
impossible to ignore quietly.

---

## Install

Prebuilt releases are self-contained and require no Docker or external database.

### Linux and macOS

```bash
curl -fsSL https://raw.githubusercontent.com/globulario/sensei/main/install.sh | sh
```

### Windows PowerShell

```powershell
irm https://raw.githubusercontent.com/globulario/sensei/main/install.ps1 | iex
```

### Package managers

```bash
brew install globulario/tap/sensei
```

```powershell
winget install Globulario.Sensei
```

### Build from source

Requires Go 1.25 or newer.

```bash
git clone https://github.com/globulario/sensei.git
cd sensei
./scripts/install.sh
export PATH="$PWD/bin:$PATH"
```

---

## Quick start

From the repository you want Sensei to understand:

### 1. Wire the agent surfaces

```bash
sensei init --mcp
```

This installs or updates the managed Sensei integration surfaces without
overwriting unrelated project configuration:

- `.sensei/skills/sensei-architect/`
- `.agents/skills/sensei-architect/`
- `.claude/skills/sensei-architect/`
- `CLAUDE.md`
- `AGENTS.md`
- `.cursor/rules/sensei.mdc`
- `.claude/hooks/`
- `.mcp.json`

Re-running initialization is idempotent. Locally edited managed files are
preserved unless explicitly forced.

### 2. Extract the repository architecture

```bash
sensei bootstrap --repo .
```

This creates repository-owned governed sources under `docs/awareness/`.

### 3. Start the local graph service

```bash
sensei serve -no-seed &
sensei build
```

Sensei runs locally with Oxigraph as an embedded RDF store.

### 4. Evaluate the repository

```bash
sensei repo-eval
sensei audit --domain github.com/your-org/your-repo
```

### 5. Ask the agent to use Sensei

A useful first instruction is:

> Before editing, use Sensei to preflight the task and request a briefing for
> every affected file. Obey applicable invariants, forbidden fixes, authority
> boundaries, and required proof.

---

## MCP tools

The MCP bridge exposes the core query and feedback surfaces:

- `awareness_metadata`
- `awareness_preflight`
- `awareness_briefing`
- `awareness_impact`
- `awareness_resolve`
- `awareness_query`
- `awareness_edit_check`
- `awareness_propose`

The generated Sensei Architect skill teaches an agent when to call them and how
to treat absence, uncertainty, contradiction, and authority honestly.

A manual Claude Code MCP configuration looks like this:

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

---

## CI: gate pull requests

```yaml
# .github/workflows/sensei.yml
name: Sensei architectural review

on:
  pull_request:

permissions:
  contents: read
  security-events: write

jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: globulario/sensei-action@v1
        with:
          mode: advisory
```

Modes:

- `advisory` reports findings without blocking
- `enforce` fails on blocking findings and fails closed when the diff cannot be verified

Repository policy can re-level or silence rules through
`.sensei/gate-policy.yaml`.

---

## A real codebase: Caddy

Sensei was tested against a pristine checkout of
[Caddy](https://github.com/caddyserver/caddy).

It mapped repository structure and recovered architecture laws from project
history, including rules grounded in specific pull requests and reversions rather
than invented from model intuition.

One recovered rule described why a streaming reverse-proxy copy must honor
context cancellation, linking the failure and its correction to the repository's
own history.

[Read the reproducible Caddy case study](docs/case-studies/caddy.md).

---

## Why a very large context window is not enough

A large-context model may read the entire repository, documentation, and history.
That improves candidate extraction and reasoning.

It still does not provide:

- authoritative source ownership
- legal mutation mechanisms
- single-use admission
- exact result identity
- deterministic reconstruction
- complete proof accounting
- atomic transition recording
- immutable completion
- revocation without history rewrite

A model can say, "I found no blocker."

Sensei asks:

```text
Were all mandatory sources consulted?
Was every blocker accounted for?
Did the ledger move during evaluation?
Does the proof bind the exact result?
Was the result independently reproduced?
What makes this transition authoritative?
```

A stronger model makes Sensei more capable. It does not make the protocol
unnecessary.

---

## Core design laws

Sensei development is governed by a small set of non-negotiable laws:

1. **Base binding and result binding are different objects.**
2. **The graph is a compiled projection, not authority by existence.**
3. **Task prose, model output, and user role do not create authority.**
4. **A verified diff proves scope, not correctness.**
5. **An evidence requirement is not an evidence receipt.**
6. **Certification is recomputed from records, never accepted from a caller boolean.**
7. **Missing, stale, conflicting, or incompatible knowledge never becomes PASS.**
8. **Historical receipts are immutable; later invalidation is recorded through revocation, not history rewriting.**

These laws matter more than any one model, agent, editor, or storage engine.

---

## Adopt Sensei progressively

| Level | Add | Gain |
|---|---|---|
| **1. One invariant** | Protect one dangerous file | The agent receives the rule before editing it |
| **2. One scar** | Record one incident and its forbidden fixes | The same failure becomes harder to reintroduce |
| **3. Agent consultation** | Enable MCP and the Sensei Architect skill | Agents query architecture before changing code |
| **4. Enforcement hooks** | Add pre-edit guards | Consultation becomes enforced behavior |
| **5. CI governance** | Add the Sensei GitHub Action | Violating changes are reported or blocked |
| **6. Architecture extraction** | Run `sensei bootstrap` | Components, contracts, symbols, tests, and risks become queryable |
| **7. Governed task protocol** | Use bindings, authority, admission, and proof records | Agent work becomes an accountable transition rather than an unstructured patch |
| **8. Architectural closure** | Complete the result, proof, completion, and revocation lifecycle | Exact tasks can earn terminal closure under explicit policy |

You do not need to model the entire organization before receiving value. Start
with the scar that keeps returning.

---

## Who is this for?

Sensei earns its place when:

- AI agents make locally correct but globally dangerous changes
- senior engineers repeat the same architectural warnings
- important rules live in Slack, post-mortems, and memory
- regressions return after team turnover
- code review depends on one or two people who know the history
- onboarding requires months of repository archaeology
- a regulated or critical system needs evidence of why a change was accepted
- a large codebase needs agents to operate without silently inventing authority

Sensei is not only for large distributed systems. A single dangerous invariant
in a small repository is enough to justify it.

---

## What Sensei is, and is not

| Sensei is not | Sensei is |
|---|---|
| another code-generation model | a governance and closure protocol around any coding agent |
| generic RAG over repository text | typed, repository-owned architectural knowledge |
| a linter with hard-coded rules | a graph of project-specific contracts, scars, authority, and proof |
| a confidence score | explicit legal states for known, unknown, blocked, certified, and completed |
| a replacement for tests | a system that derives which tests and evidence are required |
| a hosted authority service | local, open source, and versioned with the repository |
| an attempt to make an LLM deterministic | a deterministic acceptance envelope around probabilistic work |

---

## Architecture

```text
docs/awareness/*.yaml
code annotations
tests and contracts
approved decisions
        │
        ▼
 deterministic extraction + compilation
        │
        ▼
 Oxigraph RDF architecture graph
        │
        ├── briefing
        ├── impact
        ├── preflight
        ├── closure assessment
        └── proof requirement extraction
        │
        ▼
 task protocol and append-only ledger
        │
        ├── identity and scope
        ├── authority and admission
        ├── observed mutation
        ├── deterministic result pipeline
        ├── evidence and proof
        ├── certification
        └── completion and revocation
```

The CLI, gRPC server, MCP bridge, hooks, editor extension, and CI action are
interfaces over the same governed model.

No arbitrary SPARQL is exposed to agents. Query modes use closed typed surfaces.

---

## Command overview

The [full CLI reference](docs/cli-reference.md) documents every command and flag.

| Command | Purpose |
|---|---|
| `sensei init --mcp` | Install repository and agent integration surfaces |
| `sensei bootstrap --repo .` | Extract architecture into governed sources |
| `sensei serve` | Start Oxigraph and the gRPC server |
| `sensei build` | Compile governed sources into the graph |
| `sensei repo-eval` | Evaluate repository architecture and awareness quality |
| `sensei audit --domain <repo>` | Detect graph drift, gaps, and inconsistencies |
| `sensei briefing --file <path>` | Return the architecture governing a file |
| `sensei briefing --task "<task>"` | Return task-relevant architecture |
| `sensei impact` | Return structured affected nodes |
| `sensei preflight` | Evaluate risk before a task begins |
| `sensei edit-check` | Check proposed content against governed rules |
| `sensei gate --diff <range>` | Evaluate a Git diff in CI |
| `sensei propose --kind ...` | Stage a new architectural scar or rule candidate |
| `sensei check` | Validate governed source documents |
| `sensei validate` | Run deeper structural validation |
| `sensei demo` | Start a disposable stack and return a real briefing |

The architectural closure protocol commands are fully integrated into both the CLI and MCP server. The agent can use the task orchestration tools (`prepare-change`, `task-status`, `advance-task`, `task-briefing`) to navigate the change session from planning to terminal closure, or use low-level protocol commands (`admit-change`, `verify-admission`, `assess-closure`, `advance-convergence`, `complete-task`, `certify-change`) for customized or offline workflows.

---

## VS Code

Sensei has a companion VS Code extension:
[Sensei Awareness](https://marketplace.visualstudio.com/items?itemName=globulario.sensei-awareness).

```bash
code --install-extension globulario.sensei-awareness
```

The extension reads the local Sensei graph and exposes the architecture governing
the current file plus a project-level architecture dashboard.

---

## Repository layout

```text
cmd/awg/                         Sensei CLI
cmd/awareness-mcp/               MCP stdio bridge
cmd/yaml2nt/                     governed YAML to N-Triples compiler

golang/server/                   gRPC service
golang/extractor/                repository extraction and scanners
golang/architecture/             bindings, claims, closure, ledger, authority,
                                 admission, proof, result pipeline, certification

proto/                           gRPC contracts
ontology/                        RDF vocabulary
docs/awareness/                  governed architectural knowledge
docs/                            references, guides, case studies, and design notes
```

Build and test:

```bash
go build ./...
go test ./...
make sensei
make sensei-smoke
```

---

## Project status

Sensei has two deliberately distinct maturity surfaces.

### Usable now

- repository extraction
- governed architectural knowledge
- RDF graph compilation
- MCP tools
- agent skills and hooks
- briefings, impact, preflight, and edit checks
- repository evaluation and audit
- CI diff governance
- editor integration
- complete task orchestration (`prepare-change`, `task-status`, `advance-task`, `task-briefing`)
- deterministic result-transition recording (`certify-change`, `complete-task`, `task-ledger`)
- proof execution, validation, and discharge integration
- terminal completion receipts
- revocation, migration, and learning lifecycle
- self-hosted closure of Sensei changes

Sensei does not call an intermediate result "architecturally closed." The term is
earned only when the exact result, proof, freshness, and immutable completion
boundaries all hold.

---

## Origin

Sensei was born from [Globular](https://github.com/globulario), a distributed
platform developed with extensive AI-agent assistance.

The agents were productive, but they repeatedly rediscovered architecture
failures:

- two components claiming authority over the same truth
- stale state presented as current reality
- recovery paths depending on the system they must recover
- local fixes violating owner boundaries
- successful intermediate states presented as completed outcomes
- the same incident returning under a different filename

The response was not another prompt file.

The incidents became invariants. The bad fixes became forbidden moves. The real
repairs became required mechanisms. The required confidence became proof
obligations. Their recurring shapes became portable meta-principles.

Sensei grew from memory into governance, and from governance toward closure.

---

## Documentation

- [Quick start](QUICKSTART.md)
- [CLI reference](docs/cli-reference.md)
- [API reference](docs/api-reference.md)
- [Meta-principles](docs/meta-principles.md)
- [Caddy case study](docs/case-studies/caddy.md)

Contributors working on the closure protocol should treat frozen schemas,
fixtures, and design laws as higher authority than convenience or local
implementation shortcuts.

---

## License

GNU Affero General Public License, Version 3 (AGPLv3).

See [LICENSE](LICENSE) and [NOTICE](NOTICE).

Sensei may be used, modified, and redistributed under the terms of the AGPLv3.
