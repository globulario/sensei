# Phase 9.4c — authoritative change-to-task binding

This opens Phase 9.4c, **design-first**, on top of the closed 9.4a (advisory report) and
9.4b (enforce decision + typed identity/runtime cause split). It contains the design
contract only — **no binding, validator, action, or enforcement code.** It operationalizes
the `completion.change_task_binding/v1` forward-declaration already sketched in
[`phase9.4-contract.md`](phase9.4-contract.md) ("Task / PR identity — a typed, verified
change-to-task binding").

## Objective

Replace the **operator-supplied** relationship between a repository change and a
`--task-dir` with a typed, verifiable publication:

```
completion.change_task_binding/v1
```

The binding must prove that the **exact change** being evaluated belongs to the **exact
canonical task identity** whose completion closure is being enforced. It must make this
impossible:

> a valid, authoritatively completed task supplied as the identity for an **unrelated**
> change (task A's green completion laundered onto change B).

In 9.4b the identity source is the explicit `--task-dir` — fine for local advisory
inspection, but a task directory **alone** is not proof that this change belongs to this
task. 9.4c binds them.

The load-bearing equation:

```
repository
    + bounded change (base..head)
    + exact head SHA
    + canonical task identity
    = one authoritative completion evaluation subject
```

## Proposed binding fields (`completion.change_task_binding/v1`)

The typed binding includes or canonically references at least the following. Every field
is exact and typed; none is inferred:

| Field | Meaning |
|---|---|
| `schema_version` | `completion.change_task_binding/v1` — the version identity |
| `repository.provider` | canonical provider (e.g. `github`) |
| `repository.identity` | canonical repository identity (e.g. `github.com/globulario/sensei`) — the completion-policy domain |
| `change.provider` | provider of the change (e.g. `github`) |
| `change.id` | change identity (e.g. PR number) |
| `change.head_sha` | the exact evaluated head commit SHA |
| `change.base_sha` | base/comparison SHA defining the bounded change `base..head` |
| `task.directory` | the task directory (`.sensei/tasks/<task>`) |
| `task.id` | canonical task id (must match the **verified** ledger) |
| `task.session_id` | canonical session id (must match the verified ledger) |
| `task.result_binding_digest` | digest tying the task's current result to this change |
| `issuer` | the authoritative producer/issuer of the binding (e.g. the CI event authority) |
| `publication.id` | publication identity (unique per issued binding) |
| `provenance` | how the binding was produced (event source, checkout provenance, tool + version) — sufficient to audit |
| `digest_sha256` | deterministic self-excluding binding digest over the canonical serialization |

Canonical serialization + a self-excluding `ChangeTaskBindingDigest` follow the closure
program's existing digest discipline (clear the digest field, canonicalize, hash). The
binding **does not replace** completion evidence — it binds that evidence (the projection
for `task`) to the change (`repository` + `base..head` + `head_sha`) under evaluation.

## Frozen identity rules (preserved from 9.4a/9.4b, extended)

Identity is **exact, typed, and fail-closed**. The binding and its verifier must never:

- select a task from a **branch name**;
- select a task from a **commit message**;
- infer a task by **nearest directory**;
- fall back to the **only available** task;
- match by **basename** or **prefix**;
- **case-fold** any identity;
- **whitespace-normalize** any identity;
- **guess from Git state** when an authoritative binding is absent;
- accept a **stale** binding created for another `head_sha`;
- **reuse** a binding across repositories;
- **silently rebind** after history changes (force-push / rewrite);
- rely on **human-readable error text** for any decision.

Any of these is a design defect, not a convenience.

## Required validity classes (typed, never a generic bool)

The verifier returns a typed outcome from a closed vocabulary — at least:

| Class | Meaning |
|---|---|
| `authoritative_binding` | the binding verifies: repository + change + head + base + task all match, exactly one task, provenance authoritative |
| `binding_absent` | no binding supplied where enforcement requires one |
| `binding_malformed` | structurally invalid / unparseable / unknown required field |
| `binding_stale_head` | the binding's `head_sha` does not match the evaluated head |
| `binding_repository_mismatch` | the binding's repository ≠ the CI/evaluated repository |
| `binding_task_mismatch` | the binding's `task.id`/`session_id` ≠ the verified ledger, or the task dir is out-of-world |
| `binding_change_range_mismatch` | the binding's `base_sha`/`head_sha` do not define the evaluated bounded change |
| `binding_contradictory` | more than one binding selects this change, or two changes share one binding |
| `binding_unsupported_version` | `schema_version` is not a supported version |
| `binding_unverifiable_provenance` | provenance/issuer cannot be established as authoritative |
| `binding_publication_invalid` | the publication (digest/canonical form) does not verify |

These map, at the gate, onto **identity-invalid** (block under enforce) — never onto the
9.4b runtime-degraded lane. An invalid binding identity is a caller failure, exactly like
9.4b's identity cause.

## Enforcement relationship with the closed 9.4b gate

9.4c composes **in front of** the 9.4b decision — it does not change it:

1. **Validate** `completion.change_task_binding/v1` → a typed validity class.
2. **Establish** the exact repository / change / task subject (only on `authoritative_binding`).
3. **Resolve** the completion policy for the exact canonical domain (= `repository.identity`).
4. **Build** the completion projection for the **bound** task (the 9.4b read-only envelope).
5. **Apply** the frozen 9.4b enforcement decision (`decideCompletionEnforcement`).
6. **Publish** the decision + binding provenance for audit.

Consequences the design fixes:

- a valid completion projection for the **wrong** task → **block** (binding fails at step 1/2, identity-invalid);
- a valid binding with **non-authoritative** completion → still follows the 9.4b table (e.g. `not_completed` under `require_completion` blocks);
- an **invalid binding** can never reach the 9.4b runtime degraded-pass lane — only a genuine runtime failure of the owner, *after* a valid binding established identity, may degrade.

## GitHub vs local execution — authority model

### GitHub execution (authoritative)

The Action produces the binding from the **authoritative event context**, binding:

- `repository.identity` (from the event repository);
- `change.id` (PR / change number from the event);
- `change.base_sha` and `change.head_sha` (from the event / checked-out change context);
- `task.*` (the canonical task selected for this change).

The `head_sha` **must** come from the authoritative event or the checked-out change and
be protected against a **stale or mismatched checkout** (the evaluated head must equal the
binding's head, or the binding is `binding_stale_head` → block). Design must specify how
the Action obtains and cross-checks the head (event payload vs. `git rev-parse HEAD`).

### Local execution (provisional, non-certifying)

Local creation of a binding from developer Git state is **provisional**: usable for local
advisory inspection, **never** granted the same authority as a reviewed pull-request
binding for certification purposes. The contract must state this explicitly and must not
silently elevate local Git state to authoritative identity. (Whether a locally-produced
binding is `provisional` or `unsupported for certification` is decided in this contract;
the default is **provisional, non-authoritative**.)

## Audit requirements

Every enforcement result must let an auditor determine, from **typed** records (never
prose alone):

- which binding was evaluated (publication id + digest);
- which repository and change it named;
- which head SHA it covered;
- which task it selected;
- whether the binding was `authoritative_binding` (or which invalid class);
- why it passed or blocked (the stable 9.4b + 9.4c reason codes);
- whether completion passed, blocked, or degraded;
- the exact stable reason codes produced.

Audit is best-effort and **non-authoritative** (as in the 9.4 contract): it never mutates
the task directory, never changes the verdict, and lives in the job log / an optional
external entry.

## Required adversarial design proofs (forward-declared)

The contract forward-declares tests (to be implemented in the checkpoints) for at least:

- completed task A bound to unrelated change B → block;
- correct task with a **stale head SHA** → block (`binding_stale_head`);
- correct head SHA in the **wrong repository** → block (`binding_repository_mismatch`);
- correct repository with a **wrong base SHA** → block (`binding_change_range_mismatch`);
- **rewritten / force-pushed** history → block (no silent rebind);
- **two bindings for one change** → block (`binding_contradictory`);
- **one binding reused for two changes** → block;
- malformed task identity → block (`binding_malformed`/`binding_task_mismatch`);
- **task directory substitution** after binding creation → block;
- **path alias / symlink substitution** → block (identity out-of-world);
- **branch-name spoofing** → ignored (never a selection input);
- **commit-message spoofing** → ignored (never a selection input);
- **duplicate publication** → block (`binding_contradictory`/invalid);
- **unknown schema version** → block (`binding_unsupported_version`);
- **missing provenance** → block (`binding_unverifiable_provenance`);
- invalid signature / digest (if the design adopts cryptographic verification) → block;
- **authoritative completion attached to an invalid binding** → block (identity wins over a green verdict);
- **runtime completion-owner failure attached to an invalid binding** → block (invalid binding identity never receives 9.4b runtime degradation).

**Invalid binding identity must never receive 9.4b runtime degradation.**

## Correctness certification

`CorrectnessCertified` remains **unchanged** during this design opening and throughout
9.4c until implementation **and** adversarial proof are complete. This contract must define
the later condition under which the binding *contributes* to certification — namely: an
`authoritative_binding` composed with an authoritative-completion 9.4b decision, over a
verified ledger, is a **necessary** input a future certifier could consider — but **no
certification flag is changed by 9.4c**, and Phase 6 remains the sole writer of
`CorrectnessCertified`.

## Proposed bounded checkpoints (each its own review + STOP)

### Checkpoint 1 — typed binding schema + validator (no enforcement wiring)
The `completion.change_task_binding/v1` schema, canonical serialization, self-excluding
digest, the closed **typed validity-class** vocabulary, and a strict parser/validator
(unknown fields / wrong types / duplicate fields / unsupported version fail closed). No
gate wiring, no producer.

### Checkpoint 2 — authoritative producer + Action wiring (no certification change)
Produce the binding from GitHub change identity (repository / base / head / change id),
canonical task selection, publication, and the audit information. Extend
`.github/actions/sensei-gate/action.yml` with a completion job that carries the head-SHA
cross-check. No final certification change.

### Checkpoint 3 — enforcement composition + adversarial closure
Compose binding validation **before** the 9.4b decision (the 6-step relationship above);
block wrong / stale / absent / contradictory / unverifiable bindings where required; the
full adversarial suite above; deterministic audit; race + platform proofs; and closure of
the governing records.

## Governing records (forward-declared)

9.4c is governed by the phase-9 forward-declarations it inherits plus new records to be
added **govern-first** at Checkpoint 1 (assertable by `golang/coverage/phase9_contract_test.go`):

- invariant: the enforce subject is a verified change-to-task binding, not a task
  directory alone (`closure.completion_gate_binds_change_to_task_before_enforcing` — name TBD);
- invariant: an invalid binding identity blocks and never degrades
  (extends `closure.completion_gate_requires_explicit_identity_when_enforcement_applies`);
- failure_mode: a completed task is accepted as identity for an unrelated change
  (`closure.completion_gate_accepts_a_task_completion_for_an_unrelated_change` — name TBD);
- forbidden_fix: infer the task from branch/commit/Git state / accept a stale-head or
  cross-repository binding (`phase9_gate_infers_change_task_identity_from_git_state` — name TBD).

Exact record ids/text are proposed and frozen at Checkpoint 1, govern-first, before code.

## Out of scope for this opening PR

No binding generation, no validator code, no Action changes, no CLI behavior changes, no
automatic task discovery, no branch/commit inference, no completion-policy changes, no
9.4b decision changes, no certification changes, no new degraded paths, no unrelated
repairs. This document is the reviewable design artifact only — Checkpoint 1 does not begin
until this opening contract is reviewed.
