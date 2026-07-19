# Phase 9.4 contract — CI / GitHub completion gate (advisory → enforce)

*Governed opening contract for Phase 9.4. Design + scope only — no gate logic,
engine, or graph change is made by this document. It records what 9.4 wires,
what it must never do, and the bounded sub-slices, each of which earns its own
reviewed checkpoint before implementation.*

## Grounding (what already exists vs. the gap)

Phase 9.4 does **not** build a gate. The gate infrastructure is already present:

- `.github/actions/sensei-gate/action.yml` — a composite CI action that builds
  Sensei, starts the store/server, and runs `sensei gate --diff <range> --domain
  <d> --mode advisory|enforce`, reading the exit code (0 pass / 1 blocked /
  2 cannot-verify) into the job summary.
- `cmd/awg/cmd_gate.go` — `sensei gate`: a **read-only** gate over a git diff's
  *added lines*, one `EditCheck` RPC per changed file, with `--mode
  advisory|enforce`, `--report-only` (fail-open), `--policy <.sensei/gate-policy.yaml>`,
  `--json`/`--sarif`, and exit codes `0` pass / `1` blocked / `2` cannot-verify
  (fail-closed under `--enforce`, fail-open under `--report-only`).
- `cmd/awg/cmd_runtime_gate.go` — `sensei runtime-gate`: the **structural
  precedent** for 9.4. A fail-closed gate that reads a *typed verdict* from a
  report and authorizes only on a whitelisted verdict, adding policy without
  re-deriving. Its vocabulary is runtime-repair, not completion closure.
- `cmd/awg/gate_policy.go` — per-repo enforcement policy at
  `<repo>/.sensei/gate-policy.yaml`, the existing per-domain opt-in mechanism.

**The gap 9.4 closes.** *No existing gate reads a task's completion or closure
verdict.* The completion owners (`golang/architecture/completion`) are consumed
only by non-gate surfaces (`complete-task`, `inspect-terminal`,
`recover-projections`, `task-status`, the MCP). And no gate resolves a *task/PR
identity* — `sensei gate` keys on a working-tree diff, never a task. Phase 9.4
wires the Phase-8 completion **closure verdict** into the existing gate path and
action, keyed to a task.

`docs/hard-gate-design.md` is the model 9.4 inherits (advisory→enforce, opt-in
per repo/domain, audited, fail-open on unavailability) — but note it governs the
*EditCheck-rule* gate and explicitly states "No fail-closed." The completion gate
adds a **verdict** axis on top of that availability axis; §"The fail-open /
fail-closed reconciliation" below is where the two are made consistent.

## Intended outcome

A CI job — the existing `sensei-gate` action plus a new completion mode of
`sensei gate` — reads a task's closure verdict via the canonical read view and:

- **advisory (default):** reports the verdict + the three distinctions, exit 0,
  annotates the job; blocks nothing.
- **enforce (opt-in per repo/domain):** fails closed on a *computed pathological*
  closure verdict; degrades (fail-open) when the verdict cannot be computed.

It is **read-only**: it appends no ledger event, writes no receipt, mutates no
repository or governed source, and never becomes terminal completion authority.
It is the Phase-9 slice that unlocks the CI/GitHub completion gate
(`closure.phase9_surfaces_locked_until_a_reviewed_slice`).

## The consume-don't-reinterpret contract

- The gate reads the closure verdict through the **canonical read view** —
  `completion.BuildCompletionProjection` (which wraps
  `VerifyCompletionClosure`), never raw ledger/receipt files
  (`closure.completion_projection_is_the_single_canonical_read_view`,
  `closure.phase9_surfaces_consume_completion_truth_never_reinterpret_it`).
- It re-derives nothing: no readiness, correctness, question-resolution, or
  terminal-cardinality recomputation; the owner's `ClosureVerdict` is authority.
- It never treats the projection or the Phase-8 closure report as the terminal
  fact (`closure.phase9_projections_are_not_terminal_authority`;
  forbidden `phase9_treat_projection_or_closure_report_as_completion_authority`).
- It appends nothing and writes no receipt (forbidden
  `phase9_surface_appends_completed_or_writes_receipt`).

## The closure-verdict → gate mapping

The closed `completion.ClosureVerdict` vocabulary maps to gate outcomes:

| ClosureVerdict | Meaning | Advisory | Enforce |
|---|---|---|---|
| `authoritative_completion` | whole durable conjunction holds | report PASS | **pass** |
| `not_completed` | no completion, no residue (in progress) | report | **pass by default**; block only under an explicit `require_completion` policy |
| `broken_completion` | completed event but conjunction fails | report | **fail closed** |
| `contradictory_terminal_history` | conflicting terminal facts | report | **fail closed** |
| `unsupported` | result world/ledger could not be established | report | **fail closed** (a *computed* non-establishable world, distinct from Sensei being unavailable — see below) |
| *(verdict not computable)* | Sensei/store/owner unreachable, task dir unresolved | report **degraded** | **fail open** (degraded pass) |

`not_completed` is not corruption — it is a legitimate in-progress state — so it
is advisory by default; "a task must be authoritatively completed to merge" is an
opt-in `require_completion` policy, never the default. This matches §9.4's
"fails closed on broken/contradictory/unsupported" (which omits `not_completed`).

## The fail-open / fail-closed reconciliation (the crux)

Two distinct axes must never be conflated:

1. **Availability (fail OPEN).** If the verdict *cannot be computed* — Sensei or
   its store is unreachable, the owner errors, the task directory is unresolved —
   the gate **passes with a "degraded" annotation**, honoring the platform rule
   that *AI is supplementary, never required* and *fail safe*. A Sensei outage
   must never halt every merge. This is the hard-gate availability policy and the
   existing `--report-only` fail-open path / exit-2-as-degraded.
2. **Verdict (fail CLOSED under enforce).** A *computed pathological* verdict —
   `broken_completion`, `contradictory_terminal_history`, `unsupported` — is a
   **positive signal of a real problem**, not an availability gap, and blocks. A
   corrupt or unestablishable closure is exactly what the gate exists to catch.

The load-bearing distinction is **"could not compute" (degrade) vs. "computed and
it is broken/unestablishable" (block).** `unsupported` is on the *verdict* axis:
Sensei ran and determined the result world cannot be established — that is a
computed fail-closed result, not the same as Sensei never running. Collapsing
these two — failing closed on an outage, or degrading on a computed broken
verdict — is the central failure mode this contract forbids.

## Task / PR identity (a named sub-problem)

- **Task-keyed (exists):** reuse the `inspect-terminal` pattern —
  `resolveTaskLedgerDir(repoRoot, taskDir, active)` (`--task-dir` explicit, or
  `--active`/`--repo` via `tasksession.LoadActivePointer` →
  `.sensei/tasks/active.yaml`) → `completion.Request{RepositoryRoot, TaskDirectory}`.
- **PR-keyed (gap):** there is no existing PR→task mapping. 9.4 must design how a
  CI run for a PR identifies its task (e.g., the PR's checkout carries an active
  pointer, or the task dir is passed to the action). This is an explicit open
  design point for sub-slice 9.4c, not something to invent silently.

## Failure modes (governed)

- gate **mutates** the repository or ledger (writes a receipt, appends an event).
- gate **claims completion** / becomes terminal authority.
- gate **collapses the three distinctions** (impl-closure vs. task-completion vs.
  repo-perfection).
- gate **enforces without per-domain opt-in** (imposes blocking on a repo that
  never adopted Sensei for its domain).
- gate **conflates unavailability with a broken verdict** — fails closed on a
  Sensei outage (violating fail-safe), or fails open on a *computed* pathological
  verdict (letting corruption merge).

## Proof matrix

- each `ClosureVerdict` surfaced faithfully (5 verdicts);
- advisory reports and blocks nothing; enforce blocks on
  broken/contradictory/unsupported;
- `not_completed` passes by default, blocks only under `require_completion`;
- **fail open** (degraded pass) when the verdict cannot be computed;
- **fail closed** (enforce) on a computed pathological verdict;
- enforce is opt-in per repo/domain (`.sensei/gate-policy.yaml`);
- the three distinctions are preserved and never collapsed;
- **zero mutation** — no ledger/receipt/repository write on any path;
- verdict read via `BuildCompletionProjection`, never raw ledger.

## Bounded sub-slices (one reviewed checkpoint each — do not begin later ones)

- **9.4a — read + report (advisory only).** A completion mode of `sensei gate`
  (e.g. `sensei gate --completion --task-dir <d>` / `--active`) that resolves the
  task, calls `BuildCompletionProjection`, and reports the verdict + the three
  distinctions in text/json/sarif. Exit 0 always (advisory). Mirrors
  `runtime-gate`'s read-a-typed-verdict shape. No enforce, no mutation.
- **9.4b — enforce + the fail-open/closed reconciliation + policy opt-in.** Add
  the enforce path: fail closed on a computed pathological verdict, fail open
  (degraded) on unavailability, the `require_completion` policy option, wired
  through `.sensei/gate-policy.yaml`. Adversarially prove the availability-vs-
  verdict distinction.
- **9.4c — action wiring + audit + PR/task identity.** Extend
  `.github/actions/sensei-gate/action.yml` with a completion job, emit the
  durable audit record (job log; optional Sensei audit entry), and resolve the
  PR→task identity.

## Governance

- 9.4 is governed by the phase-9 forward-declarations from 9.0 — it consumes
  completion truth read-only (`phase9_surfaces_consume_completion_truth_never_
  reinterpret_it`), is not terminal authority
  (`phase9_projections_are_not_terminal_authority`), preserves the three
  distinctions (`phase9_preserves_the_three_completion_distinctions`), and is the
  reviewed slice that unlocks the gate
  (`phase9_surfaces_locked_until_a_reviewed_slice`).
- This contract additionally forward-declares the **gate-specific** invariant,
  failure mode, and forbidden fixes that capture the availability-vs-verdict
  reconciliation and the per-domain-opt-in requirement (see the governed YAML +
  `golang/coverage/phase9_contract_test.go`).

## Surface locking

Only Slice 9.4 is opened. Slice 9.5 (VS Code completion cockpit) and Slice 9.6
(candidate briefing-feedback leg) remain **locked** until each receives its own
reviewed slice.

## Out of scope

- No auto-enforce — enforce is opt-in per repo/domain, never imposed.
- No change to the EditCheck-rule gate (a distinct gate); 9.4 adds a completion
  verdict mode, it does not alter forbidden-pattern enforcement.
- No PR-platform-specific integration beyond the generic composite action.
- No GNN / ML capability (`phase9_invent_a_gnn_or_ml_capability_without_
  repository_evidence`).
