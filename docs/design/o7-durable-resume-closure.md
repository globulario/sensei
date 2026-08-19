# O7 Durable Resume and ARCHER Closure Contract

Status: **architect decision accepted; implementation required**

Issue: #149

This document is the completion contract for the part of O7 that the original
`bounded-synthesis-driver-o7.md` deliberately deferred. It does not replace the
accepted O1-O6C owners, O5A/O5B authority, or the existing `sensei
synthesis-run` semantics. It defines the missing durable-resume boundary and
resolves the architectural questions exposed by the real #149 end-to-end run.

The implementation is not complete merely because the types or CLI switches
exist. #149 closes only when the implementation and the declared proof matrix
are both demonstrated.

## 1. Why this contract exists

The current O7 implementation has a precise limitation:

```go
Run(ctx, initial, config)
```

drives one fresh synchronous process lifetime. The driver carries accepted
Interpretation, Plan, candidate identity, and accumulated trace in local Go
variables. It explicitly refuses an externally supplied `evaluating` state
because the O3 -> O4 handoff is process-local.

That was a valid first checkpoint. It is no longer sufficient for #149.

A governed synthesis session may outlive a process. The terminal, machine,
provider subprocess, or Sensei process can disappear without changing the
architectural identity of the work. Restarting from scratch is not equivalent
to continuing the same governed session because it can reset step accounting,
repeat already-accepted transitions, lose evidence, or silently cross graph,
task, closure, workspace, or base-revision drift.

O7 therefore needs a durable continuation contract:

```text
fresh run
  -> durable boundary
  -> interruption
  -> load exact checkpoint
  -> revalidate the mutable execution boundary
  -> continue the same O1 session

or

  -> typed drift refusal
```

The governing invariant is:

> Resume may continue execution, but it may never reinterpret completed
> history.

Anything already accepted into O1 or recorded by an existing owner remains
immutable. Resume decides only whether the same execution may continue from the
last durable boundary.

## 2. Decisions this contract makes

### Decision A: application and post-application verification are separate facts

An O5B application receipt closes when materialization succeeds. It is the
consumption record for that application and must not be rewritten later merely
because verification now exists.

The existing `admission.Verification` is already an immutable, self-digested
owner document. The missing piece is an immutable O5B-side binding record that
says which already-recorded application the admission verification describes.

The new shape is:

```text
candidate
  -> O5A request / decision
  -> O5B application receipt          immutable, closed at apply
  -> admission.Verification           immutable, produced afterward
  -> O5B verification binding record  immutable link between the two
```

Do **not** solve this by deleting the application receipt and re-applying. Do
**not** overwrite the application receipt. Do **not** make the applier generate
its own admission verification.

`candidateapply.AttachVerification`, whose current behavior returns a modified
copy of the application receipt, is not the production continuation model for
new applications after this contract. Historical v1 receipts containing the
optional verification fields remain readable; compatibility is not permission
to keep rewriting new application receipts.

The implementation should add a closed document with semantics equivalent to:

```text
sensei.candidateapply.verification-record.v1
```

It must bind, at minimum:

- O5B application receipt digest;
- O5B request digest;
- admission decision digest;
- candidate artifact digest;
- patch digest;
- admission verification digest;
- admission verification status;
- completion observation time;
- its own semantic digest.

The binding owner must independently validate the decision/application/
verification lineage already enforced by `candidateapply.AttachVerification`:
matching admission ID, decision digest, patch digest, and binding. It must not
judge correctness beyond the status the admission owner produced.

The CLI needs a **second post-application step** that records this binding from
an already-produced `admission.Verification`. A suitable surface is:

```text
sensei synthesis-record-verification
```

or an equivalently explicit name. It must read the existing O5B application
receipt; it must never require the candidate to be unconsumed; it must never
apply files; and it must never overwrite a different prior verification record.
Multiple later verifications may be represented as multiple immutable records
keyed by verification digest rather than by mutating one historical document.

This makes the proof-matrix row `verification scope violation` reachable after
real application.

### Decision B: downstream immutable evidence does not require a live graph

`synthesis-admit` and `synthesis-apply` operate on a candidate that has already
reached a terminal O1 candidate-ready state and on the immutable evidence
captured under that synthesis session. They must continue to validate their own
existing task/closure/base bindings, but they are **not** required by this
contract to contact a live graph server merely to re-decide the graph identity
that was already captured.

That preserves offline admission/application of durable evidence.

This decision is intentionally different for a mid-session O7 resume. Resume
claims that the mutable synthesis execution itself is still the same execution.
That claim requires a current authority observation.

Therefore:

```text
terminal candidate -> downstream O5A/O5B
    captured graph identity may be used offline

nonterminal O7 session -> resume
    current workspace/graph authority must be re-observed and compared
```

Do not "fix" graph drift by adding an unconditional live-Metadata dependency to
all downstream commands.

### Decision C: resume revalidates the execution boundary, not completed history

Resume must re-observe and compare the mutable identities whose continuity is
required to claim that the same synthesis session can continue.

At minimum it revalidates:

1. **Repository domain** - same repository domain as the checkpoint.
2. **Base revision** - same revision identity; the base tree still digests to
   the candidate/session input identity where that identity exists.
3. **Workspace identity** - recomposed through the same live Metadata authority
   used by fresh `synthesis-run`; semantic identity must match the checkpoint.
4. **Graph authority identity** - graph authority digest captured by the O1
   session must match the newly observed authoritative workspace identity.
5. **Task identity** - same task ID/session binding.
6. **Task control state** - same current control-state digest/generation.
7. **Closure state** - same closure report semantic digest resolved with that
   control generation.
8. **O1 session identity and budgets** - the checkpoint must contain the exact
   O1 state; retry/replan budgets and their already-consumed counters are never
   reset.
9. **O7 step budget** - restart must not reset `max_steps`; the checkpoint must
   preserve steps already consumed.
10. **Checkpoint integrity and evidence lineage** - every embedded or referenced
    Interpretation, Plan, O2 receipt, O3 receipt/handoff, O4 receipt, O1 event,
    candidate digest, and previous checkpoint reference must validate before a
    resumed owner call is allowed.

Resume does **not** call a provider to reinterpret an accepted
Interpretation. It does not ask O8 to re-plan a Plan already accepted by O1. It
does not rerun an evaluator merely to "refresh" a completed O4 decision. It
continues from the typed O1 phase captured at the last durable boundary.

A change to any required current identity above produces a typed refusal and no
O1 transition.

### Decision D: pre-onboarded repository is an explicit O7 precondition

O7 does not own repository onboarding.

A fresh synthesis execution is eligible only when Sensei can establish all of
these facts from existing owners:

- a repository checkout and repository domain;
- a live, usable `workspacecontract.Identity` from graph authority;
- a real prepared `tasksession.Session`;
- a current task-control generation;
- the closure report resolved for that same generation;
- the base revision bound by workspace identity;
- the authored Interpretation/interpretation-closure prerequisites already
  required by `synthesis-run`.

This contract names that boundary **RepositoryExecutionReadiness**. The name is
architectural; it does not require a new package if the existing owners can
compose and test the same typed result cleanly.

If the repository is not onboarded, O7 refuses with a typed readiness stop. It
must not fabricate a session, generate zero digests, silently start an import,
or weaken graph/task/closure requirements. Onboarding is a separate workflow.

The existing `--force-thin-coverage` exception remains an explicit caller
choice for the fresh-run case. A checkpoint records the exact identity under
which that choice was made. Resume requires continuity with that recorded
identity; improvement or deterioration of the graph is still drift and requires
starting a new synthesis session rather than silently changing the premises of
the old one.

## 3. Durable O7 checkpoint

Add a closed, self-digested checkpoint document. Suggested schema identity:

```text
sensei.synthesisdriver.checkpoint.v1
```

The exact Go names may follow repository conventions, but the semantic content
is required.

A checkpoint must carry enough state to continue without reconstructing
accepted evidence from provider prose or zero values:

- O1 `synthesis.SessionState`;
- accepted Interpretation, when one exists;
- accepted Plan, when one exists;
- latest sealed candidate digest, when one is part of completed history;
- the accumulated O7 trace/evidence required to produce an honest final run
  receipt after a later process resumes;
- original O7 steps consumed and immutable `max_steps`;
- original run start observation, if retained for reporting;
- repository domain and base revision;
- workspace identity semantic digest;
- graph authority digest;
- task ID/session digest;
- task control-state digest/generation;
- closure-report digest;
- previous checkpoint digest, when this is not the first checkpoint;
- checkpoint sequence number;
- its own semantic digest.

Do not store ambient credentials, vendor login material, secret environment
values, or mutable filesystem paths as authority. Runtime paths may be recorded
as observation/convenience only when the semantic owner identity is also
present.

The checkpoint digest must exclude observation timestamps in the same spirit as
`RunReceiptDigest`; authority/evidence references remain part of identity.

### Checkpoint history

Checkpoints are append-only evidence. A filesystem adapter should write
content-addressed checkpoint documents and may atomically move a small
`latest` pointer/reference after the new checkpoint is durable.

Do not overwrite the only copy of the previous checkpoint.

A crash between the content-addressed write and latest-pointer update leaves a
recoverable older pointer, not a corrupted history.

## 4. Safe durable boundaries

O7 checkpoints only at owner boundaries where completed history is
unambiguous.

Required resumable O1 phases are:

- `created`;
- `planning`;
- `planned`;
- `attempting`;
- `retry`;
- `replan`.

`Succeeded` and `Failed` are terminal and are not "resumed" into new synthesis
work. A caller may reload/report them, but no owner transition follows.

`Evaluating` remains **not externally resumable** in this contract. The existing
O4 evaluation engine owns the accepted verified-generation handoff and resolves
`Evaluating -> {Succeeded|Retry|Replan|Failed}` within one owner call. Do not
serialize a half-consumed process-local O3/O4 handoff and pretend it is durable.

If a process dies while a provider/O3/O4 owner call is in flight, the next
process resumes from the last checkpoint that preceded that owner call. The
unfinished call is not completed history. Any unreferenced content-addressed
artifact left by an interrupted call is non-authoritative unless a later valid
checkpoint references it.

This rule avoids inventing transactional semantics the existing owners do not
have.

## 5. Driver API shape

Keep fresh execution and resumed execution visibly distinct at the API boundary.
A shape equivalent to the following is expected:

```go
func Run(ctx context.Context, initial synthesis.SessionState, config Config) (Result, error)
func Resume(ctx context.Context, checkpoint Checkpoint, current ResumeBinding, config Config) (Result, ResumeAssessment, error)
```

Exact names can follow local style. What matters:

- `Run` starts a fresh O1 session and may persist checkpoints through an
  injected checkpoint sink/store;
- `Resume` never accepts an arbitrary `synthesis.SessionState` as sufficient
  authority;
- `Resume` first verifies the checkpoint, then the current binding, then
  continues the same internal driver loop;
- no resumed owner call occurs when the assessment refuses;
- the resume assessment itself is immutable evidence.

The architecture package must stay independent of gRPC, CLI parsing, and
repository-specific path discovery. The CLI composes current workspace/task/
closure/base observations through existing owners and supplies a typed
`ResumeBinding` (or equivalent) to O7.

A checkpoint storage abstraction belongs behind an injected interface. Provide
an in-memory implementation for deterministic tests and a filesystem adapter
for the CLI. O7 must not hard-code task-directory layout into the orchestration
owner.

## 6. Resume assessment receipt

A refusal must be auditable, not only an error string.

Add a closed resume assessment document, suggested identity:

```text
sensei.synthesisdriver.resume-assessment.v1
```

It binds:

- checkpoint digest;
- session digest;
- checkpoint phase;
- checkpoint sequence/steps consumed;
- expected repository/base/workspace/graph/task/control/closure identities;
- observed current identities;
- disposition: `allowed` or `refused`;
- one typed refusal reason when refused;
- its own semantic digest.

Required refusal vocabulary includes at least:

- `checkpoint-invalid`;
- `checkpoint-not-resumable`;
- `repository-domain-drift`;
- `base-revision-drift`;
- `workspace-identity-drift`;
- `graph-authority-drift`;
- `task-identity-drift`;
- `task-control-drift`;
- `closure-drift`;
- `evidence-lineage-invalid`;
- `step-budget-exhausted`.

Do not collapse these into generic `resolution failure` in JSON output. CLI exit
codes may group some operator actions if necessary, but machine-readable output
must preserve the exact typed reason.

## 7. Step and budget semantics across restart

Restart is not a budget refill.

If a checkpoint says O7 has consumed `N` steps under immutable
`max_steps=M`, the resumed driver begins with `N` already consumed and has at
most `M-N` steps left.

Likewise O1's retry/replan counters remain exactly those in the checkpointed
`SessionState`. Resume must never reconstruct a new O1 `Session` merely to make
more budget available.

A resume whose O7 step budget is already exhausted produces a typed refused or
step-limit terminal assessment without calling an external provider.

## 8. CLI contract

Extend the production synthesis surface so an operator can deliberately resume
an exact checkpoint. Preferred UX:

```text
sensei synthesis-run --resume <checkpoint>
```

A separate `synthesis-resume` command is acceptable only if it preserves the
same semantics and shares the same O7 implementation rather than forking the
driver.

Fresh-run requirements such as `--interpretation` are not re-requested when the
checkpoint already contains the accepted Interpretation. Runtime provider
configuration still has to be supplied/resolved as required to execute the next
owner call, but changing runtime configuration must not mutate the checkpoint or
accepted O1 history.

The CLI must:

1. load and validate the checkpoint before using its contents;
2. resolve the exact task referenced by the checkpoint, not whichever task is
   merely active today;
3. resolve task control + closure through the same generation-safe owner used
   by fresh `synthesis-run`;
4. recompose live workspace/graph identity through Metadata authority;
5. re-read/verify the base revision from Git;
6. construct the typed current resume binding;
7. call O7 resume assessment;
8. persist the assessment receipt;
9. continue only on `allowed`;
10. persist every new checkpoint and the final O7 receipt/lineage using atomic,
    append-only semantics.

No resume path may admit, apply, commit, push, open a PR, approve, merge, or
promote knowledge.

## 9. Application-verification CLI correction

The current `synthesis-apply --verification` option is temporally backwards for
real production use: the verification describes the result after application,
but the command consumes the candidate during the application that must happen
before that verification can exist.

Implementation must provide a post-apply recording path as described in
Decision A.

Compatibility options:

- keep `--verification` only for historical/tests where a pre-produced
  verification already exists, but make the new second-step record the normal
  documented flow; or
- deprecate/remove the flag if repository compatibility policy permits it.

Whichever route is chosen, the ordinary real flow must be:

```text
synthesis-apply
  -> immutable application receipt
verify-admission
  -> immutable admission.Verification
synthesis-record-verification
  -> immutable application/verification binding
```

A `scope_violated` admission verification must be recordable against the
already-applied candidate without a second application and must yield a typed
non-success result from the record command.

## 10. Required implementation work

The implementer should inspect and modify the existing owners rather than build
parallel replacements. At minimum inspect:

- `golang/architecture/synthesisdriver/driver.go`;
- `golang/architecture/synthesisdriver/receipt.go`;
- `golang/architecture/synthesisdriver/schema.go` and `schemas/`;
- `golang/architecture/synthesisdriver/lifecycle_test.go`;
- `golang/architecture/synthesisdriver/e2e_test.go`;
- `cmd/awg/cmd_synthesis_run.go`;
- `golang/architecture/candidateapply/types.go`;
- `golang/architecture/candidateapply/apply.go`;
- `cmd/awg/cmd_synthesis_apply.go`;
- `cmd/awg/cmd_verify_admission.go`;
- `scripts/synthesis-run-smoke.sh`;
- `docs/design/bounded-synthesis-driver-o7.md`;
- `docs/design/archer-integration-closure.md`.

Expected new work includes:

- checkpoint type, normalization, semantic digest, strict validation, and JSON
  schema;
- resume-binding and resume-assessment types with strict validation and schema;
- refactoring `Run` so fresh and resumed execution share one internal typed
  dispatcher rather than two orchestration implementations;
- checkpoint persistence hook/store and deterministic in-memory tests;
- filesystem checkpoint adapter with atomic append-only writes;
- CLI resume flow and typed reporting;
- immutable candidate-application verification record and post-apply CLI;
- updates to the two existing design documents after implementation so they
  describe reality rather than this planned state;
- proof-matrix scenarios listed below.

Do not redesign O1, O2, O3, O4, O5A, admission, or commandprovider unless a
concrete failing contract test proves an existing owner cannot express the
required invariant. If that happens, stop and record the architectural blocker
rather than quietly moving authority into O7.

## 11. Required tests for O7 durable resume

These are contract tests, not optional examples.

### Checkpoint integrity

- checkpoint semantic digest is timestamp-independent where observations are
  explicitly non-authoritative;
- unknown fields/schema mismatch fail closed;
- changing O1 state, accepted Interpretation, Plan, trace receipt, candidate
  digest, binding digest, step count, or previous-checkpoint digest invalidates
  the checkpoint;
- checkpoint history is append-only and an older valid checkpoint is not
  overwritten by saving a newer one.

### Resume from each durable phase

With deterministic fake owners, interrupt and resume from:

- `created`;
- `planning`;
- `planned`;
- `attempting`;
- `retry`;
- `replan`.

Each test must prove the next owner call is the one selected by the captured O1
phase and that completed prior owners are not reinterpreted/replayed.

### Non-resumable states

- externally supplied `evaluating` remains refused;
- `succeeded` does not start more synthesis work;
- `failed` does not start more synthesis work;
- exhausted O7 step budget performs no provider call.

### Drift refusal

Construct each drift independently and prove no provider/runner/evaluator call
happens after refusal:

- repository domain;
- base revision/tree;
- workspace identity;
- graph authority digest;
- task ID/session;
- task control-state digest/generation;
- closure-report digest.

The refusal receipt must name the exact drift class.

### Budget continuity

- retry budget is not reset by resume;
- replan budget is not reset by resume;
- O7 `max_steps` is not reset by resume;
- retry/replan success after resume still follows existing O1 transitions;
- exhaustion after resume is identical to exhaustion without a process restart.

### Deterministic replay

With deterministic providers/evaluators and unchanged current bindings:

- a checkpoint resumed twice in isolated stores produces the same next typed
  request/transition chain;
- a completed transition recorded before a checkpoint is never recomputed into
  a different accepted identity.

Provider nondeterminism is not magically eliminated; the test proves O7's
orchestration and identity decisions are deterministic given deterministic
owners.

## 12. Required tests for post-application verification

- application receipt remains byte/digest identical after later verification is
  recorded;
- compliant verification binds to the exact application;
- scope-violated verification binds to the exact application and is surfaced as
  non-success without re-application;
- wrong admission ID refused;
- wrong decision digest refused;
- wrong patch digest refused;
- wrong binding refused;
- a second different record cannot overwrite the first;
- repeated recording of the exact same verification is either explicitly
  idempotent or explicitly refused, but never silently rewrites history;
- no command in the flow commits, pushes, opens a PR, approves, merges, or
  promotes knowledge.

## 13. #149 completion proof matrix

**Implemented.** The matrix now lives as data in `section13Matrix`
(`golang/architecture/synthesisdriver/lifecycle_matrix_test.go`), with a census
test that reads every file it cites and fails when a named proof stops being
declared. It reports **29 rows proven, 7 open**, each open row carrying the
reason it cannot be proven honestly. The list below remains the requirement;
the census is the current answer, and a prose list is no longer where coverage
is tracked.

The proof ladder is deliberate, and no layer impersonates another:

```text
unit contracts
      ↓
section 13 hermetic lifecycle matrix   ← always CI, normative
      ↓
scripts/synthesis-run-smoke.sh         ← one real-system witness
```

Every matrix row enters through the public Run/Resume dispatcher — the same
surface `cmd/awg` enters — with fakes only at owner boundaries, so it proves
lifecycle compositions rather than components. The smoke is deliberately NOT a
second copy of these rows: two matrices eventually disagree.

After implementation, deliberately construct and record the rows still missing
from `archer-integration-closure.md`. Do not mark a row proven because a fixture
contains a similar branch.

At minimum close the remaining rows currently identified as fixture-only or
unproven:

- O4 accept followed by admission refusal;
- admitted-with-conditions with exact acknowledgement binding;
- waiting admission;
- uncertifiable admission deliberately constructed, not merely observed;
- candidate artifact tampering;
- wrong O1 attempt/evaluation lineage;
- unsupported candidate operation refusal;
- apply digest mismatch;
- verification scope violation **after real application**;
- oversized provider output;
- provider timeout process-group cleanup;
- retry then success;
- replan then success;
- retry exhaustion;
- replan exhaustion;
- deterministic replay;
- resume-with-drift-refusal for every identity class in section 11.

Keep the already-proven real-CLI rows in the matrix and rerun them on the exact
implementation head so the final evidence is one coherent accepted head, not a
mosaic of unrelated commits.

If a real vendor CLI, graph service, or credential needed by a proof row is not
available in the implementation environment, do **not** fabricate the proof.
Leave the row open and state exactly what prevented it.

## 14. Completion criteria

This O7 completion contract is implemented only when all of the following are
true:

1. a nonterminal synthesis session can survive a process boundary through a
   strict self-digested checkpoint;
2. resume revalidates repository/base/workspace/graph/task/control/closure
   continuity before any new owner call;
3. every required drift becomes a typed auditable refusal;
4. O1 retry/replan budgets and O7 max-step budget survive restart unchanged;
5. completed Interpretation/Plan/O3/O4 history is never reinterpreted;
6. `evaluating` is still not serialized as a fake durable handoff;
7. downstream terminal-candidate admission/application remain capable of using
   captured graph evidence offline;
8. application receipts are immutable after application;
9. post-application verification is recorded as a separate immutable binding;
10. post-application scope drift is detected by the real verifier, and a
    verification describing drift outside the exact applied artifact is refused
    as unbound rather than recorded as an application verdict;

    > **Superseded wording, kept deliberately.** This criterion previously read
    > *"the real CLI can demonstrate a scope violation after application without
    > re-applying the candidate"*, and it is recorded here as changed rather than
    > quietly rewritten, because it did NOT pass as written and must not be read
    > as having done so.
    >
    > The real-CLI witness in #211 falsified it. O5A derives the admitted
    > envelope from the candidate's own diff and O5B applies exactly that
    > artifact, so `AppliedPatch == AdmittedPatch` and
    > `AdmittedScope == Scope(AdmittedPatch)`. An applied candidate therefore
    > cannot violate a scope computed from it unless some earlier invariant has
    > already failed. Manufacturing a bound scope violation would have required
    > deliberately weakening the exact-artifact boundary — making the system
    > worse in order to satisfy a sentence about it.
    >
    > What the witness found instead is the stronger property. Later workspace
    > drift IS detected (`verify-admission` genuinely returns `scope_violated`),
    > and the binding owner then refuses to attach that verification to the
    > application (exit 3), because it describes a tree containing changes the
    > application never made. Two independent safety properties, both firing.
    >
    > Of the three possible designs — (A) application can violate admitted
    > scope, (B) later drift can be recorded as if the application caused it,
    > (C) application is exact-scope by construction while later drift stays
    > detectable but unattributable — the implementation reached (C). The
    > document yields to the proved architecture, not the reverse.
11. the #149 completion proof matrix is deliberately exercised on the exact
    accepted head;
12. no automatic commit, push, PR, approval, merge, or promotion authority is
    introduced.

Only then update `docs/design/archer-integration-closure.md` to **closed** and
close #149.

## 15. Out of scope

Do not use this work to:

- implement external-repository onboarding;
- add automatic admission;
- add automatic application by default;
- add automatic commit/push/PR/approval/merge;
- revalidate the live graph for every downstream immutable-evidence command;
- serialize a process-local half-finished O3/O4 handoff;
- reset budgets on restart;
- solve #197 or other generation-boundary migrations;
- broaden the provider's authority.

Those are separate decisions.

## 16. Implementation handoff

Implement this contract on the branch/PR that contains this document.

Work contract-first. Add the closed types/schemas and failing tests before
wiring production behavior. Reuse existing owners and their validators. Every
new production branch that can refuse or continue must have a typed contract
test. Run package tests as slices land, then the complete Go suite, generated
artifact freshness checks, repository dogfood, and `scripts/synthesis-run-smoke.sh`
on the exact head.

When review or testing exposes a contradiction with this contract, do not
paper over it with a flag, a zero digest, an advisory warning, or a permissive
fallback. Record the contradiction in the PR and ask for an architect decision.
