# Phase 9 — Governed Contract & Decomposition (Slice 9.0)

> **Status: roadmap only.** This document is the govern-first contract for Phase 9.
> It defines the objective, owners, authorities, artifacts, consumption rules, and
> bounded slices. **No Phase-9 implementation is authorized until this contract is
> reviewed and accepted.** It introduces no implementation code beyond the governed
> records and the machine-checkable `phase9_contract_test.go`.

## 0. Grounding — what the repository already establishes

Phase 9 is **not** a new closure-lifecycle phase. The task state machine is terminal
at `completed`: `closureprotocol/vocabulary.go` allows `certified → {completed,
revoked}` and `completed → {revoked}` only — there is no phase after completion.
Phase 8 established and proved the *complete completion architecture* (readiness →
authoritative mutation → durable-conjunction verification → reconstruction →
projection recovery → end-to-end owner re-verification), and its closure report
keeps three claims distinct: implementation closure ≠ completion of one task ≠
repository-wide perfection.

What the repository shows is still **missing** — and what therefore grounds Phase 9:

- **Completion truth is unwired downstream.** The completion owner
  (`golang/architecture/completion`) exposes `CompleteTask`, `InspectTerminalState`,
  `RecoverProjections`, `VerifyCompletionClosure`, and `BuildPhase8ClosureReport`,
  but **no server, CLI, MCP, or briefing surface reads terminal state** to display or
  act on it. There is **no `complete-task` CLI subcommand** and **no `complete_task`
  MCP tool**, though `taskcontrol` already projects a *desired* `ActionCompleteTask`.
- **Product direction (README.md, docs/api-reference.md, docs/adoption-plan.md):**
  Sensei is "architectural memory for AI coding agents" delivered as a local
  queryable graph whose surfaces — "the MCP bridge, a CI step, the editor extension"
  — are **thin clients** of the graph/owners. The CI gate (`.github/actions/
  sensei-gate`) and the VS Code extension (`editor/vscode/`) already exist as thin
  clients; `docs/hard-gate-design.md` designs graduating the gate from advisory to
  enforcing.
- **GNN / embedding-similarity memory has no repository evidence.**
  `docs/design/memory-correctness-tradeoff.md` argues *against* it. Phase 9 must not
  invent a GNN capability; it is placed **out of scope** (see §6) absent new,
  reviewed repository evidence.

## 1. Objective and exact outcome

**Phase 9 makes Sensei's terminal-completion truth operationally consumable and
invocable through the product's thin-client surfaces — the CLI, the MCP bridge, the
CI/GitHub gate, and the editor — where every surface is either a read-only
projection of, or a delegating invocation to, the Phase-8 completion owner.**

The measurable outcome: an agent (via MCP/CLI), a CI job, and the editor can each (a)
**read** a task's honest terminal state and end-to-end closure verdict, and (b)
**invoke** completion only by delegating to the single authorized owner path — with
**no surface reinterpreting, re-deriving, or re-authorizing completion truth**. The
completion owner remains the sole terminal authority; every surface is a thin client.

## 2. Non-goals (explicit)

- **No new completion authority or mutation path.** Phase-9 invocation surfaces
  delegate to `completion.CompleteTask` (which resolves `grant.sensei.
  terminal_completion` and `OperationComplete`). Phase 9 mints no second completer.
- **No reinterpretation of Phase-8 truth.** No surface re-derives readiness,
  correctness, question resolution, or terminal cardinality; no surface treats a
  projection or the closure report as terminal authority.
- **No GNN / ML capability.** Out of scope; not repo-grounded (§6).
- **No new closure-lifecycle phase.** The state machine remains terminal at
  `completed`.
- **No product surface ships without its own reviewed slice** (§4, §5).

## 3. Owners, authorities, artifacts, and projections

### 3.1 Authorities (unchanged — reused, never duplicated)
- **Terminal completion:** `authority.sensei_terminal_completion` /
  `grant.sensei.terminal_completion` / `mutation_path.terminal_completion` /
  `OperationComplete` — owned by `completion.CompleteTask` only. Phase-9 invocation
  surfaces **delegate** to it; they declare **no** completion authority.
- **Read/observation:** Phase-9 read surfaces resolve `observation_path.
  repository_read` semantics (read-only); they declare no mutation authority.
- **Projection maintenance:** derived-projection recovery is
  `completion.RecoverProjections` (derived-state only, no `OperationComplete`); a
  Phase-9 surface may call it but adds no new mutation path.

### 3.2 Durable artifacts (owned by Phase 8 — Phase 9 only reads)
The `completed` ledger event, the terminal completion receipt
(`completion.terminal_receipt/v1`), the correctness certification, the
question-resolution certificate, the readiness assessment (frozen in the receipt),
and the governed manifest digest. **Phase 9 writes none of these.**

### 3.3 Non-authoritative projections (Phase 9 may produce)
Read models only, each explicitly non-authoritative and reconstructed from the owner:
- a terminal-state view (`InspectTerminalState`) surfaced in task-status/briefing;
- an end-to-end closure verdict (`VerifyCompletionClosure`) surfaced to CI/editor;
- the Phase-8 closure report (`BuildPhase8ClosureReport`) surfaced as *implementation*
  evidence, never runtime authority.
None of these is durable terminal truth; the durable event/receipt conjunction —
reconstructed by the owner — is the sole terminal fact.

## 4. Phase-8 consumption contract

**Phase 9 MAY consume:** `InspectTerminalState`, `VerifyCompletionClosure`,
`BuildPhase8ClosureReport`, `RecoverProjections`, and the read-only `completed`
event + terminal receipt — through the owner's exported APIs.

**Phase 9 MUST NEVER:** append a `completed` event; produce, replace, or supersede a
completion receipt; re-derive readiness/correctness/question-resolution/terminal
cardinality in a surface; treat a projection, read model, or the closure report as
terminal completion authority; collapse implementation closure / task completion /
repository-wide perfection; or normalize contradictory terminal history at a surface
(it re-reports what the owner classified).

## 5. Slice decomposition (one slice open at a time)

Each slice is govern-first, has its own acceptance criteria + failure modes + proof
matrix, and unlocks exactly its own surface. **Only the slice the architect opens is
authorized.** Suggested order (the architect fixes the order at open time):

### Slice 9.1 — Completion read projection (server / task-status)
- **Surface:** `golang/server` + `cmd/awg` `task-status`/`task-briefing` read path.
- **Does:** surfaces `InspectTerminalState` + `VerifyCompletionClosure` output as a
  non-authoritative terminal-state view. Read-only.
- **Acceptance:** every terminal state (`not_completed…unsupported`) and closure
  verdict is surfaced honestly; a projection never claims `committed` without the
  durable conjunction; the view is deterministic and mutates nothing.
- **Failure modes:** projection claims completed on residue/broken; surface
  reinterprets or re-derives state; projection treated as authority.
- **Proof matrix:** each terminal state → surfaced faithfully; broken/contradictory →
  not authoritative; determinism; zero mutation.

### Slice 9.2 — Completion invocation surface (`complete-task` CLI + MCP tool)
- **Surface:** `cmd/awg` `complete-task` + `cmd/awareness-mcp` `complete_task` tool.
- **Does:** thin client that **delegates** to `completion.CompleteTask` and reports
  its closed outcome set. No completion logic of its own.
- **Acceptance:** the surface only calls `CompleteTask`; surfaces
  `committed/exact_replay/not_ready/…`; never bypasses authority/readiness; idempotent
  on replay; refusals surfaced honestly and write nothing.
- **Failure modes:** CLI/MCP manufactures completion; bypasses the owner or its
  authority; caller-supplied status accepted.
- **Proof matrix:** delegation only; every outcome surfaced; replay idempotent;
  refusal writes nothing; no caller can manufacture completion.

### Slice 9.3 — Recovery & inspection surface (`inspect-terminal` + `recover-projections`)
- **Surface:** `cmd/awg` + MCP, delegating to `InspectTerminalState` /
  `RecoverProjections`.
- **Does:** read + derived-state maintenance only. Recovery rebuilds projections from
  a valid conjunction; never appends or blesses.
- **Acceptance/failure/proof:** mirror 8.2c's recovery boundary at the surface layer;
  recovery never appends `completed`, never normalizes contradiction, never blesses
  residue; idempotent.

### Slice 9.4 — CI / GitHub completion gate (advisory → enforce)
- **Surface:** `.github/actions/sensei-gate` + `golang/server`/`cmd/awg` gate path;
  grounded in `docs/hard-gate-design.md`.
- **Does:** a CI job reads the closure verdict for a task/PR and reports it (advisory
  or enforcing). Read-only; never mutates the repository or the ledger.
- **Acceptance:** the gate reports the closure verdict + the three distinctions,
  supports advisory/enforce, fails closed on broken/contradictory/unsupported, and
  performs no mutation; enforce mode is opt-in per repo/domain (per hard-gate design).
- **Failure modes:** gate mutates; gate claims completion; gate collapses the
  distinctions; enforce without opt-in.
- **Proof matrix:** verdict surfaced; advisory vs enforce; fail-closed; zero mutation.

### Slice 9.5 — Editor (VS Code) completion cockpit
- **Surface:** `editor/vscode/`.
- **Does:** the editor thin-client displays terminal state + closure verdict from the
  server/owner. Read-only.
- **Acceptance/failure/proof:** the cockpit reads owner-reconstructed state only,
  never re-derives; displays the three distinctions; no mutation from the editor.

### Slice 9.6 (candidate, deferred) — Briefing-feedback leg
- **Surface:** `golang/server/briefing.go`.
- **Does:** surface promoted `OpenQuestion`/`ArchitectAnswer` provenance in briefings
  (the residual gap named in `phase8-discovery.md` §3.2/A4). Read-only projection.
- Kept as a candidate; opened only if the architect places it in a reviewed slice.

## 6. Surface-locking table

| Surface | State | Unlocked by |
| --- | --- | --- |
| Server read model / task-status view | **locked** | Slice 9.1 |
| `complete-task` CLI + MCP invocation | **locked** | Slice 9.2 |
| `inspect-terminal` / `recover-projections` CLI + MCP | **locked** | Slice 9.3 |
| CI / GitHub completion gate (enforce) | **locked** | Slice 9.4 |
| VS Code completion cockpit | **locked** | Slice 9.5 |
| Briefing-feedback leg | **locked (candidate)** | Slice 9.6 (if opened) |
| GNN / embedding-similarity memory | **out of scope** | not repo-grounded; requires a separate future authorization with evidence |

No locked surface may ship without its reviewed slice. GNN is excluded, not merely
deferred, until repository evidence justifies it.

## 7. Governed contract (machine-checkable, forward-declared)

Mirroring Slice 8.0, the Phase-9 contract is authored in the canonical governed
sources **before** implementation and asserted by
`golang/coverage/phase9_contract_test.go` (`TestPhase9GovernedContractPresent`):

- **Invariants** (`docs/awareness/invariants.yaml`):
  `closure.phase9_surfaces_consume_completion_truth_never_reinterpret_it`,
  `closure.phase9_invocation_surfaces_only_delegate_to_the_owner`,
  `closure.phase9_projections_are_not_terminal_authority`,
  `closure.phase9_preserves_the_three_completion_distinctions`,
  `closure.phase9_surfaces_locked_until_a_reviewed_slice`.
- **Failure modes** (`docs/awareness/failure_modes.yaml`):
  `closure.phase9_surface_manufactures_or_reinterprets_completion`,
  `closure.phase9_projection_or_report_treated_as_terminal_authority`,
  `closure.phase9_surface_shipped_without_a_reviewed_slice`.
- **Forbidden fixes** (`docs/awareness/forbidden_fixes.yaml`):
  `phase9_surface_appends_completed_or_writes_receipt`,
  `phase9_reinterpret_or_rederive_completion_truth_in_a_surface`,
  `phase9_treat_projection_or_closure_report_as_completion_authority`,
  `phase9_ship_a_surface_without_a_reviewed_slice`,
  `phase9_invent_a_gnn_or_ml_capability_without_repository_evidence`.

High-risk coverage (already established prefixes `golang/architecture/`,
`golang/server/`, `cmd/awg/`, and the governed YAMLs) guards the owner and the CLI/
server surfaces; each later slice adds its own surface path (e.g.
`cmd/awareness-mcp/`, `editor/vscode/`, `.github/`) to `high_risk_files.yaml` when it
opens.

## 8. Stop boundary

This is the map, not the territory. No Phase-9 implementation — no CLI subcommand, no
MCP tool, no server read model, no CI gate, no editor code, no GNN — is authorized
until this Phase-9.0 contract is reviewed and accepted, and thereafter only the one
slice the architect opens.
