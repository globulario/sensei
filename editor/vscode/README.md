# Sensei — VS Code extension

See the **invariants, failure modes, and intent that govern the file you are
editing** — read straight from your project's [awareness
graph](https://github.com/globulario/sensei).

Git shows you what *changed*. This shows you what is *true and must stay true*:
the architectural constraints that live in no diff. Open any file and the
**Awareness** view in the activity bar tells you which rules protect it, the
risk class of editing it, the forbidden fixes, and the tests that pin it.

## What it shows

For the file in the active editor it runs one `Preflight` query and renders:

- **Risk** — the edit-risk classification (low, architecture-sensitive,
  security, data-loss, …) with coverage detail in the tooltip.
- **Invariants / Failure modes / Intent** anchored to the file — each row links
  to its source anchor (YAML or annotated Go).
- **Forbidden fixes** and **Required tests** for this file.
- **Required actions** and **Blind spots** the preflight surfaced.

### Visible absence

An empty panel is ambiguous. When nothing is anchored to a file the extension
says so explicitly, and distinguishes:

- **"No rules anchor to this file"** — confident absence: the graph has
  sufficient coverage here and nothing governs the file.
- **"Coverage is thin here"** — absence that is *not* conclusive.
- **"Awareness degraded"** — the backend was partially unavailable; the answer
  is unreliable.

This is deliberate: a rule authored without a file anchor shows up as governing
*nothing*, instead of silently surfacing nowhere.

## Install

From the VS Code **Extensions** view, search **Sensei** — or from the command line:

```sh
code --install-extension globulario.sensei
```

Or install a packaged build directly:

```sh
code --install-extension sensei-0.1.0.vsix
```

You'll also need the `sensei` CLI itself — see the
[install options](https://github.com/globulario/sensei#install-one-line)
(one-line installer, Homebrew, winget, or from source).

## Requirements

A running `awareness-graph` server (the gRPC backend `sensei serve` starts):

```sh
sensei serve            # defaults to localhost:10120
```

The extension is a first-class **gRPC client** of that server — the same
`AwarenessGraph` service the `sensei` CLI uses. It does not shell out to a binary.

## Settings

| Setting | Default | Meaning |
| --- | --- | --- |
| `sensei.serverAddr` | `localhost:10120` | gRPC server address (`host:port`). |
| `sensei.domain` | `""` | Domain/repo scope (e.g. `github.com/owner/repo`). Required on a multi-domain graph. |
| `sensei.mode` | `standard` | Preflight detail level: `compact` or `standard`. |
| `sensei.enabled` | `true` | Query the graph as you switch files. |
| `sensei.requestTimeoutMs` | `10000` | Per-request gRPC deadline. |

## Develop

```sh
cd editor/vscode
npm install
npm run sync-proto     # vendor the canonical proto (run after a proto change)
npm run compile        # or: npm run watch
```

Press **F5** in VS Code to launch an Extension Development Host. The gRPC
contract is vendored at `proto/awareness_graph.proto` and loaded at runtime;
`npm run check-proto` (also enforced in CI) fails if it drifts from the
canonical `proto/awareness_graph.proto`.

## Project dashboard

Open **Sensei: Open Project Dashboard** (the graph icon in the "This File"
view header, or the command palette) for an architect's cockpit:

- **Control banner** from `Metadata` — totals per class + a state signal
  (in control / stale / dev-unstamped / unknown). Trust in five seconds. Two
  refresh controls: **Reload** re-pulls the served graph (cheap, always on);
  **Rebuild** runs `sensei rebuild` then reloads, with a before/after count
  (*"22,753 → 23,184 triples"*) — a gated local op (opt-in, see below).
- **Aspect navigation** — Invariants, Failure modes, Intents, Incident
  patterns, Files via `Query(by_class)`. Forbidden fixes and Tests have no
  standalone listing endpoint, so they show their count and surface as related
  nodes (not a fabricated list).
- **Detail** via `Resolve` — description, anchor, and related nodes grouped and
  clickable.
- **Focus graph** via `Query(related)` — the local causal chain around the
  selected node (intent → invariant → failure mode → forbidden fix; invariant →
  test → file). Deterministic layout, labeled edges, zoom/pan, depth 1–2. Never
  the whole graph.
- **Review** — an evidence-based project score + architecture proposals
  (see below), computed locally from `Metadata` and the candidate files.
- **Candidates** — a review→promote queue (see below): reads
  `docs/awareness/candidates/*.yaml`, renders each candidate as a review card,
  and — only when you opt in — can promote a reviewed candidate by driving the
  guarded `sensei promote` flow. Read-only by default.

The dashboard derives its diagram from queryable edges — the backend has no
diagram of its own. Reads are gRPC; the only write path is the opt-in local
`sensei promote` described below.

## Reviewing and promoting candidates

Candidates are knowledge an extractor or AI agent proposed (an invariant,
failure mode, intent…) that is **not yet trusted** — it must be human-reviewed
before it becomes part of the graph. **Candidate knowledge ≠ graph truth.** The
**Candidates** tab makes the *review → approve → promote → rebuild → reload*
loop usable without leaving the editor.

The trusted path is never bypassed:

> candidate → human review → approval → corpus/source file change → validate →
> deterministic rebuild → reload graph metadata

### Filling the queue (scan)

The **Scan codebase for knowledge** panel discovers candidates without leaving
the editor, via `sensei intent-mine --drafter echo` — **deterministic, no LLM, no
API key, no cost**:

- **Scan (preview)** — a dry-run that grounds architectural-intent proposals
  against the workspace tree and shows the report. **Nothing is written.**
- **Apply to queue** — re-runs it with `--apply`: grounded intents (≥0.80) are
  drafted to `docs/awareness/intent_*.yaml` and weaker proposals + findings are
  parked under `docs/awareness/candidates/` for review. Writes the working tree,
  **not the graph** — surfaces the `git diff`, refreshes the queue, and you
  commit. Discovery produces candidates, never facts.

### The review queue

- **Review card per candidate** — review status (candidate / approved /
  rejected), class, confidence, review label, evidence summary, source anchors,
  and the proposed target corpus file, parsed from
  `docs/awareness/candidates/*.yaml`.
- **Preview (dry-run)** — runs `sensei promote <id> --dry-run`: validates the
  candidate and shows the exact canonical YAML it *would* append. **Nothing is
  written.**
- **Approve / Reject** — a local staging decision (persisted in the workspace).
  It does **not** touch files or the graph; it only marks the candidate for the
  batch promotion. The auditable record is the promotion git diff.
- **Edit** — opens the candidate file at its entry in VS Code.

### Promote approved

One explicit action — **Promote approved (N)** — runs the guarded path for every
approved candidate:

1. confirmation summary listing the approved candidates;
2. `sensei promote <id> --no-rebuild` for each (validate → append canonical YAML →
   remove from queue), **stopping on the first validation failure**;
3. a single `sensei rebuild` (deterministic);
4. reload `Metadata` over the read-only gRPC server;
5. an operation summary: *"Promoted 3 candidates. Rebuilt graph: 22,753 → 23,184
   triples (+3 invariants). Changed files: …"*

Every command (with cwd, stdout/stderr, exit) is logged to the **Awareness
Operations** output channel; the dashboard shows a compact status with
expandable detail. Your working tree changes but is **not committed** — you
review the `git diff` and commit.

The same safety rule now applies to **single-candidate Promote**: the extension
does **not** rely on a plain `sensei promote` rebuild. It runs
`sensei promote <id> --no-rebuild` first, then performs the same project-aware
rebuild plan the dashboard uses for the main **Rebuild** action, so the
`awareness-graph` repo cannot accidentally clobber its combined seed with a
single-repo rebuild.

### Prerequisites & graceful degradation

Promotion needs: a workspace open, `sensei.enableLocalOperations: true`,
and the `sensei` CLI on `PATH` (or `sensei.senseiPath`). The tab detects each
and **degrades gracefully** — if local ops are off, `sensei` is missing, or the
folder isn't an Sensei project, it says exactly what's missing and shows the guarded
CLI to run by hand. It never fails silently.

### Safety model

- **The graph server remains read-only.** Promotion is performed locally through
  corpus files and deterministic rebuilds — no write RPC, no live triple insert.
- **The extension never validates or writes knowledge itself** — it drives
  `sensei`, which owns the guards (naming, status, confidence, evidence, anchoring).
  A candidate that fails validation cannot be promoted.
- **Opt-in & auditable.** Off by default; approve/reject is staging only;
  promotion is a git-diffable change you commit, and the graph only gains the
  knowledge through the same deterministic rebuild every other build uses.
- **Manual recovery.** Anything the dashboard does maps to a CLI command shown in
  the tab: `sensei promote <id>`, `sensei rebuild`, `sensei corpus validate`.

## Project review and architecture proposals

The **Review** tab answers two questions: *how healthy is this project's
architecture evidence?* and *what improvements does Sensei suggest, and why?*

- **Evidence-based, not a vanity score.** The 0–100 **Architecture Evidence
  Score** is computed in the extension from `Metadata` counts, graph provenance,
  and the candidate files — never from guesswork or an LLM. It measures how much
  architecture evidence Sensei can currently see and how useful that evidence is for
  project control; it is *not* a code-quality verdict. Every dimension, strength,
  risk, and proposal traces back to a fact the dashboard already holds, and the
  scoring is deterministic: the same metadata always yields the same score.
  (Later versions will deepen this with related-edge structure — `Resolve`,
  `Query(related)`, invariant-to-test edges, and per-file anchors.)
- **Six dimensions** feed the score: graph coverage, invariant/test evidence,
  drift/freshness, architecture-spine completeness, pattern risk, and agent
  readiness. A **confidence** label (High/Medium/Low) tracks graph freshness and
  evidence volume, so a stale or dev-unstamped build never reads as high-trust.
- **Evidence language only.** The review reports what Sensei can *see* ("Sensei sees
  83 invariants and 12 required tests; per-invariant coverage is not asserted
  here") rather than absolute verdicts. It does not claim exact test coverage
  from aggregate counts, and it does not fabricate file-specific findings.
- **Proposals are suggestions, not automatic changes.** Each card carries
  evidence, why it matters, a suggested next step, a confidence, and its source.
  Triggers are concrete (e.g. many invariants but few required tests; a non-zero
  pattern-misuse count; unstamped provenance; candidate files present).
- **Read-only.** The Review tab adds no mutation, no candidate promotion, no new
  RPC, and no network calls beyond the existing gRPC client. A **low score can
  mean "not enough graph evidence," not necessarily weak architecture** — it is a
  measure of how much Sensei can see, not a final judgement of the codebase.

## Status

v0.1 — "This File" panel + project dashboard: an evidence-based project review
and architecture proposals (read-only); a candidate loop that **scans** the
codebase to fill the queue and **reviews → approves → promotes** to drain it;
and **two-mode refresh** (Reload re-pulls the served graph; Rebuild runs
`sensei rebuild` then reloads). Every write goes through the opt-in local `sensei`
path (`sensei.enableLocalOperations`, off by default); the graph server
stays read-only. Planned next: preflight diagnostics (squiggles) on the edited
region, and direct test↔failure-mode edges.
