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
  `<repo>/.sensei/gate-policy.yaml`. **The policy file and loader are the extension
  point, but the current schema does NOT express per-domain completion adoption:**
  it is a `Default string` + `Rules map[string]string` (EditCheck rule → level),
  with no domain map and no completion section, and a missing policy means
  "inherit each rule's declared level," not completion opt-in. 9.4b adds an
  explicit `completion:` section (below); the completion verdict is not another
  EditCheck rule and must not be forced through the `Rules` map.

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
  closure verdict **and on invalid required task identity**; degrades (fail-open)
  only on a *runtime outage after identity was established*.

It is **read-only with respect to authoritative state** — the task ledger,
terminal receipts, completion state, governed repository sources, and projections:
it appends no ledger event, writes no receipt, mutates none of them, and never
becomes terminal completion authority. It **may** emit non-authoritative
reporting: CI annotations, a job summary, and — like the existing gate's optional
`--event-log` — an external, best-effort audit record. That audit emission lives
**outside the task directory**, is best-effort (its failure is not itself a gate
failure), and can **never change the gate verdict** — "audit logging failed" is
not a back door into completion semantics. It is the Phase-9 slice that unlocks
the CI/GitHub completion gate (`closure.phase9_surfaces_locked_until_a_reviewed_slice`).

## The consume-don't-reinterpret contract

- The gate consumes the **typed availability envelope**
  `completion.BuildCompletionProjectionEnvelope` (the Phase-9.1 abstraction) and
  validates its **canonical publication form**. It never calls
  `BuildCompletionProjection` directly, never reads raw ledger/receipt files, and
  never rebuilds availability or verdict semantics from Go errors. The envelope's
  builder already converts owner errors into explicit *typed unavailability* and
  its available branch carries the computed projection (including a computed
  `unsupported` verdict) — so the gate's availability-vs-verdict decision is
  **structural**, read from the envelope's closed vocabulary, not re-derived
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

Only the envelope's **available** branch carries a `ClosureVerdict`; its
**unavailable** branch (a typed availability class) is split by *cause* into the
identity and runtime classes below. Verdict outcomes:

| ClosureVerdict (envelope available) | Meaning | Advisory | Enforce |
|---|---|---|---|
| `authoritative_completion` | whole durable conjunction holds | report PASS | **pass** |
| `not_completed` | no completion, no residue (in progress) | report | **pass by default**; block only under an explicit `require_completion` policy |
| `broken_completion` | completed event but conjunction fails | report | **fail closed** |
| `contradictory_terminal_history` | conflicting terminal facts | report | **fail closed** |
| `unsupported` | result world/ledger could not be established | report | **fail closed** (a *computed* non-establishable world — the owner returns it as part of the closed verdict vocabulary; distinct from Sensei being unavailable) |

Envelope **unavailable** outcomes (see the three outcome classes below), split by
cause — an absent identity is not an outage:

| Envelope unavailable, by cause | Advisory | Enforce (where completion applies) |
|---|---|---|
| **Identity invalid** — required task identity absent/ambiguous/malformed/mismatched | degraded report | **cannot verify → block** |
| **Runtime unavailable** — identity established, then Sensei/store/owner unreachable | degraded report | **degraded pass** |

`not_completed` is not corruption — it is a legitimate in-progress state — so it
is advisory by default; "a task must be authoritatively completed to merge" is an
opt-in `require_completion` policy, never the default. This matches §9.4's
"fails closed on broken/contradictory/unsupported" (which omits `not_completed`).

## The three outcome classes (the crux)

The gate's outcome is decided in **three** classes that must never be conflated.
In particular, a missing or invalid task identity is **not** the same failure as
a runtime outage and must never silently become a degraded pass — otherwise a
repo that opted into enforcement could bypass the gate merely by omitting or
breaking its task binding (repo enforces → PR breaks its binding → task
unresolvable → "unavailable" → degraded pass → enforcement disappears).

1. **Identity invalid — fail CLOSED under enforce.** The required task identity is
   absent, ambiguous, malformed, or mismatched: the change-to-task binding does
   not resolve to exactly one verified task for this change (see "Task/PR
   identity"). Under enforce where completion applies this is **cannot verify →
   block**; in advisory it is a degraded *report*. An absent identity is a caller
   failure, not a Sensei outage. The existing completion envelope groups "no
   active pointer" and "unreadable repository path" into `task_directory_unresolved` —
   correct for a read-only display, but the gate must add *policy context*: under
   enforce, unresolved required identity blocks.
2. **Runtime unavailable — fail OPEN, degraded.** Identity **was** established, but
   the verdict cannot be computed because Sensei / its store / the owner is
   unreachable or errors at evaluation time. The gate **passes with a "degraded"
   annotation**, honoring *AI is supplementary, never required* / *fail safe* — a
   Sensei outage must never halt every merge (the existing `--report-only` /
   exit-2-as-degraded path).
3. **Verdict available — apply verdict policy.** The envelope carries a computed
   projection whose closed `ClosureVerdict` is applied per the table above:
   `authoritative_completion` passes; `not_completed` passes by default (blocks
   only under `require_completion`); `broken_completion` /
   `contradictory_terminal_history` / `unsupported` **fail closed under enforce**
   — a computed pathological verdict is a positive signal, not an availability
   gap. `unsupported` is on the *verdict* axis: Sensei ran and determined the
   result world/ledger cannot be established.

The load-bearing distinctions: **absent identity (block under enforce) ≠ runtime
outage after identity (degrade) ≠ computed pathological verdict (block).**
Collapsing identity-invalid into runtime-unavailable, failing closed on a genuine
outage, or degrading on a computed pathological verdict are the failure modes this
contract forbids.

## Task / PR identity — a typed, verified change-to-task binding

The active pointer (`.sensei/tasks/active.yaml`) is **not** the canonical PR
identity. It is fine for the local CLI, a developer workstation, and advisory
inspection, but it is not inherently bound to the repository identity, the PR
number, the PR head SHA, the current result, or *exactly one* task — so it must
never be the enforce-mode identity source.

For 9.4c, the enforce path resolves identity from a typed, provider-neutral
**change-to-task binding** (schema designed in 9.4c), conceptually:

```yaml
schema_version: completion.change_task_binding/v1
change:
  provider: github
  repository: globulario/sensei
  change_id: "77"          # PR number
  head_sha: "<exact PR head>"
task:
  directory: .sensei/tasks/<task>
  id: "<task-id>"
  session_id: "<session-id>"
```

The gate must **verify**, before consuming any verdict, that:

- the binding's `repository` == the CI repository;
- the binding's `change_id` == the current PR;
- the binding's `head_sha` == the checked-out PR head;
- the task directory belongs to the repository (the repository/task one-world law);
- the task `id` / `session_id` match the *verified* ledger;
- exactly **one** task is selected;
- the task's result binding corresponds to *this* change.

The mapping **selects** the task; it does not prove completion — the completion
projection remains solely responsible for the verdict. Any of these checks failing
is **identity invalid** (class 1), not runtime-unavailable.

The gate is explicitly **forbidden** from inferring identity from: the branch
name, the PR title, the PR body prose, directory scanning, "the latest task", the
first matching task, or the active pointer alone. Those are identity-by-convenience
traps.

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
- gate **treats missing or invalid required task identity as runtime
  unavailability** — an absent, ambiguous, malformed, or mismatched task binding
  earns a degraded pass under enforce, erasing completion enforcement for that PR
  (the identity bypass).

## Proof matrix

- each `ClosureVerdict` surfaced faithfully (5 verdicts);
- advisory reports and blocks nothing; enforce blocks on
  broken/contradictory/unsupported;
- `not_completed` passes by default, blocks only under `require_completion`;
- **identity invalid** → degraded report in advisory, **cannot-verify block**
  under enforce (never a degraded pass);
- **runtime unavailable after identity** → degraded pass (fail open);
- **computed pathological verdict** → fail closed under enforce;
- enforce is opt-in per repo/domain via an explicit completion policy section;
- the three distinctions are preserved and never collapsed;
- the gate is **read-only WRT authoritative state** (ledger, receipts, completion
  state, governed sources, projections) — any audit emission is best-effort,
  outside the task directory, and cannot change the verdict;
- verdict consumed from `BuildCompletionProjectionEnvelope`'s validated canonical
  form, never raw ledger.

## Bounded sub-slices (one reviewed checkpoint each — do not begin later ones)

- **9.4a — read + report (advisory only).** A completion mode of `sensei gate`
  (e.g. `sensei gate --completion --task-dir <d>`) that consumes
  `BuildCompletionProjectionEnvelope`, validates its canonical form, and reports
  the availability class + verdict + the three distinctions in text/json/sarif.
  Exit 0 always (advisory). Mirrors `runtime-gate`'s read-a-typed-verdict shape.
  No enforce, no mutation.
- **9.4b — enforce + the three outcome classes + policy opt-in.** Add the enforce
  path: identity-invalid → block, runtime-unavailable → degraded pass, computed
  pathological verdict → block, `require_completion` as a separate option. Add an
  explicit `completion:` section to the gate policy (a new schema — the completion
  verdict is not an EditCheck rule and must not be forced through the `Rules`
  map). Adversarially prove all three classes are distinct, and that EditCheck
  rule inheritance cannot activate completion enforcement.
- **9.4c — action wiring + audit + the change-to-task binding.** Design the
  `completion.change_task_binding/v1` schema and verifier (above), extend
  `.github/actions/sensei-gate/action.yml` with a completion job, and emit the
  best-effort, non-authoritative audit record (job log; optional external audit
  entry) — never inside the task directory, never able to change the verdict.

## Completion policy — semantics locked now (schema designed in 9.4b)

9.4b adds an explicit `completion:` section to the per-repo gate policy (distinct
from the EditCheck `Rules` map), conceptually:

```yaml
completion:
  default: advisory
  domains:
    github.com/globulario/sensei:
      mode: enforce
      require_completion: true
```

The final schema is designed in 9.4b, but these semantics are **locked now**:

- **no completion entry → advisory** — nothing enforces without explicit adoption;
- **one domain cannot activate another domain's gate** — a foreign-domain verdict
  never gates this repo's CI;
- **`require_completion` is separate from pathological-verdict enforcement** —
  enforce blocks broken/contradictory/unsupported regardless of `require_completion`;
  `require_completion` *additionally* blocks `not_completed`;
- **EditCheck rule inheritance must not activate completion enforcement** — the
  completion gate is a distinct governance system, never reached through `Rules`;
- **malformed or contradictory completion policy fails loudly** — never silently
  defaults to enforce or to advisory.

## Governance

- 9.4 is governed by the phase-9 forward-declarations from 9.0 — it consumes
  completion truth read-only (`phase9_surfaces_consume_completion_truth_never_
  reinterpret_it`), is not terminal authority
  (`phase9_projections_are_not_terminal_authority`), preserves the three
  distinctions (`phase9_preserves_the_three_completion_distinctions`), and is the
  reviewed slice that unlocks the gate
  (`phase9_surfaces_locked_until_a_reviewed_slice`).
- This contract additionally forward-declares the **gate-specific** governed
  records (see the governed YAML + `golang/coverage/phase9_contract_test.go`):
  the availability-vs-verdict reconciliation
  (`closure.completion_gate_fails_open_on_unavailability_and_closed_on_a_computed_verdict`
  + `closure.completion_gate_conflates_unavailability_with_a_broken_verdict` +
  the enforce-without-opt-in / fail-closed-on-unavailability forbidden fixes), and
  the **identity requirement**
  (`closure.completion_gate_requires_explicit_identity_when_enforcement_applies` +
  forbidden `phase9_gate_treats_missing_required_task_identity_as_runtime_unavailability`).

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
