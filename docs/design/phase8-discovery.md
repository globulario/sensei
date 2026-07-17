# Phase 8 Discovery — Terminal Completion

**Status:** discovery only. No implementation, no new ledger event, no awareness
mutation. This document reconstructs the Phase 8 contract from repository evidence
(frozen `closureprotocol` types, inline ownership comments, the `architectural-closure-v1`
design doc, existing tests, and governed sources). It is authored for the
`SENSEI PHASE 8 DISCOVERY CHECKPOINT` and stops at a proposal for architect review.

## 1. Preflight & briefing evidence (recorded honestly)

- `sensei preflight --task "Phase 8 discovery…" --file certification/ledger.go --file
  closureprotocol/model.go --file tasksession/advance_result.go --domain
  github.com/globulario/sensei --mode standard` →
  **`PREFLIGHT_STATUS_EMPTY`, Risk `UNKNOWN_IMPACT`, Confidence `LOW`**, `coverage
  sufficient=false anchors=0 (no anchors fired, no files indexed — coverage thin
  for this area)`. Authority is authoritative/certified (live 91,124 triples).
- `sensei briefing --file <path>` for `certification/ledger.go`,
  `closureprotocol/model.go`, `tasksession/advance_result.go` → **all
  `No direct awareness anchors found` (EMPTY)**.

**Interpretation (not permission):** the live authoritative awareness graph is built
from an earlier main commit and contains **no anchors** for the architectural-closure
packages. EMPTY here means *this area is unannotated in the governed graph*, not "no
rules apply." Discovery therefore rests on **source evidence**: the frozen
`closureprotocol` contracts, the `docs/design/architectural-closure-v1.md` design law,
inline phase-ownership comments, `docs/awareness/invariants.yaml`, and tests. This
coverage gap is itself an architect question (§9, Q1).

## 2. What the repository already fixes about Phase 8

The post-`proving` region of the lifecycle is already named and partly built:

- **Ledger vocabulary** (`closureprotocol/vocabulary.go:129-135`): after
  `result_transition_recorded` come `evidence_recorded`, `proof_discharged`,
  `certified`, `completed`, `revoked`, `migration_executed`.
- **Task phases** (`vocabulary.go:107-118`): `proving → certified → completed`, plus
  `revoked`, `uncertifiable`, `abandoned`, `waiting_evidence`, `waiting_architect`.
- **Frozen completion contract** (`closureprotocol`): `CompletionReceipt` (`model.go:276`),
  `ValidateCompletionReceipt` (`validate.go:416`), `CompletionReceiptDigest`
  (`canonical.go:127`), `TaskTerminalStatus` {`completed`, `completed_with_exception`,
  `refused`, `abandoned`, `revoked`} (`vocabulary.go:27-31`, `validTerminalStatus`).
- **Explicit ownership hand-off to Phase 8** (direct quotes):
  - `certification/model.go:18-20`: the certification engine "makes no claim of
    result-graph freshness (Phase 7), **completion, or terminal closure (Phase 8)**."
  - `certification/ledger.go:41`: "No path in this package ever appends a `completed`
    event — **completion is Phase 8's transaction**."
  - `certification/receipt.go:46-47`: "**Phase 8 must call this** [`VerifyReceipt`] (and
    recompute the digest from the persisted bytes) before trusting a receipt reference."
  - `cmd/awg/cmd_certify_change.go:41`: "This command never appends a `completed` event
    (**Phase 8 owns completion**)."
  - `certification/adversarial_test.go` `TestCertificationPackageNeverTouchesCompletion`:
    "completion is Phase 8's transaction, structurally out of reach here."
- **Design law** (`architectural-closure-v1.md`):
  - "Reasoning closure and terminal completion are **distinct**." (l.103)
  - "Only a valid `CompletionReceipt` may establish terminal completion." (l.109)
  - Legal transitions: `certified → revoked`, `completed → revoked`; illegal:
    `scope_verified → completed`, `admitted → certified`, `revoked → completed`,
    `completed → admitted` (l.660-684).
  - Frozen rules (l.686-694): `completed` is terminal for one session; a completed task
    is never silently reopened; revocation is a **new** ledger event and usually a
    remediation task; waiting states do not erase prior receipts.
  - Closure predicate (l.696-723): ten dimensions incl. `completion`; for a completed
    mutation task the mandatory dimensions (identity, scope, authority, mutation,
    epistemic, freshness, **completion**) may not be `not_applicable`;
    `pass_with_exception` (→ `completed_with_exception`) requires a valid, unexpired
    waiver applying to the exact dimension.
- **No completion owner exists yet:** there is no `golang/architecture/completion/`
  package, no `sensei complete-task` CLI, and no operational producer that appends a
  `completed` event. **This absence is the Phase 8 gap.**

## 3. Phase-boundary ownership (Phases 3–7) — what Phase 8 must NOT duplicate

| Phase | Package(s) | Ledger event(s) it owns | Phase transition | Must not be re-done by Phase 8 |
|------|-----------|------------------------|------------------|-------------------------------|
| 3 (admission v2) | `admission`, `authority`, `identity`, `tasksession` producers | `authority_resolved`, `admission_decided`, `admission_consumed`, `change_observed`, `scope_verified` | …→ `scope_verified` | typed authority/admission/scope decisions |
| 4 (evidence) | `evidencereceipt` | (`evidence_recorded` — see Q2) | — | EvidenceReceipt validation, coverage, freshness, conflict detection |
| 5 (proof) | `proofdischarge` | (`proof_discharged` — see Q2) | — | proof-discharge computation, waivers, compatibility |
| 6 (certification) | `certification` (+ `sensei certify-change`) | `certified` | `proving → certified` | the four-lane engine — **sole writer of `CorrectnessCertified`** (`invariants.yaml:540 closure.only_certification_engine_establishes_correctness`) |
| 7 (result transition) | `resulttransition` (contract), `resultpipeline` (builder), `resultrecording` (recorder), `tasksession.AdvanceResultTransition` (owner) | `result_transition_recorded` | `scope_verified → proving` | building/recording the result binding; the advance-result orchestration |

**Phase 8 (this proposal): completion.** Event `completed`; transition
`certified → completed`; establishes terminal closure via `CompletionReceipt`. It
consumes — never reproduces — the certification receipt.

## 4. Candidate Phase 8 contract

### State transition & authority owner
- **Owner:** a new `golang/architecture/completion` package, mirroring `certification`:
  a pure `Evaluate` (decides the terminal outcome, produces a `CompletionReceipt`) plus a
  ledger-integrated `CompleteTask` (the sole side-effecting entry). A thin
  `sensei complete-task` CLI adapter carries only operational inputs (task dir,
  expected head, clock), exactly as `certify-change` does for Phase 6.
- **Transition:** `certified → completed` only. `scope_verified/proving → completed` is
  illegal; a task with no `certified` predecessor fails closed.

### Authoritative inputs (consumed, never fabricated)
- The recorded `certified` event's `CertificationReceipt`, re-validated via
  `certification.VerifyReceipt` (recompute digest from persisted bytes) — the design
  explicitly requires this. `CompletionReceipt.CertificationDigestSHA256` binds it.
- The upstream chain digests already carried through: base binding, result binding,
  closure assessment, authority resolution, admission decision/verification, and the
  proof-discharge / evidence-receipt digests the certification lanes consumed
  (`CompletionReceipt` fields at `model.go:276-296`).
- The `CompletionPolicy` (already threaded into the base binding
  `tasksession/session.go:684` = `completion.architectural_closure.v1` and the admission
  decision `CompletionPolicyID`) and a completing actor.

### Output (the only thing Phase 8 appends)
- One `completed` ledger event carrying a validated `CompletionReceipt`
  (`ValidateCompletionReceipt` + self-excluding `CompletionReceiptDigest`), with
  `TerminalStatus ∈ {completed, completed_with_exception}` for the primary path.

### Projection effects
- The status projection advances to `PhaseCompleted` / `TerminalCompleted` (a terminal
  projection). No projection is written directly; projections rebuild from the verified
  chain, as in Phases 6/7.

### Replay / idempotency, stale-head, post-commit (mirror the accepted Step-8 / certify semantics)
- **Expected-head protection:** `CompleteTask` requires an expected head and refuses a
  stale head (mirrors `certification.CertifyTask` `ErrStaleExpectedHead`).
- **Replay:** an exact re-completion of an already-`completed` task appends no second
  event and reports the current terminal state.
- **Post-commit:** a durable `completed` entry whose HEAD/projection reconciliation
  fails exposes the committed identity and permits an exact retry (mirrors
  `resultrecording` `ErrEntryDurable` / `PostCommitError`).

### Forbidden outputs (boundary)
- Never set / imply `CorrectnessCertified` (only `certification` does) and never
  re-run the certification lanes.
- Never append `certified`, `evidence_recorded`, `proof_discharged`, `revoked`, or
  `migration_executed`.
- Never accept a caller-supplied terminal status, verdict, or completion truth — the
  engine derives it.
- Never reopen a `completed` task (`completed → admitted/…` illegal); revocation is a
  separate event/phase.

## 5. Boundary tests Phase 8 must carry
- A non-certified task cannot be completed (fail closed; `admitted/scope_verified/
  proving → completed` refused).
- Completion binds the exact certification receipt by digest; a forged/mismatched
  `CertificationDigestSHA256` or a receipt that fails `VerifyReceipt` refuses.
- Re-completion is idempotent (no second `completed` event).
- A structural test that the `completion` package never references `CorrectnessCertified`
  and never appends `certified`/`evidence_recorded`/`proof_discharged`/`revoked`
  (mirror of `TestCertificationPackageNeverTouchesCompletion`).
- Stale-head refusal; post-commit recovery; and a boundary proof that no
  revocation/migration event is emitted.

## 6. Smallest implementable Phase 8 slice (proposal — do not build yet)
**Slice 8.1 — the terminal completion transaction.** Keep certification, completion,
revocation, and migration **distinct** (the repository evidence proves they are distinct
events/receipts; nothing shows them atomic).
- `golang/architecture/completion/`: `Evaluate(request, records, policy) → Result`
  (typed `CompletionReceipt` + verdict) and `CompleteTask(opts) → TaskCompleteResult`
  (ledger-integrated: verify → expected head → load certified receipt + upstream →
  `VerifyReceipt` → evaluate → store receipt content-addressed → append `completed` →
  rebuild projections → re-verify).
- Ledger payload validator for the `completed` event (require the completion-receipt
  artifact), added in `ledger`/`completion` alongside the existing per-event validators.
- Thin `sensei complete-task --task-dir --expected-head [--format]` CLI.
- **Typed shapes:** `CompleteTaskOptions{TaskDir, ExpectedHeadDigestSHA256, RepoRoot,
  ProducerID, ProducedAt}`; `TaskCompleteResult{Result, Appended, Head, ReceiptRef,
  Verification}` (mirrors `certification.TaskCertifyResult`).
- **Invariants to add (governed, in a later authored step — not during discovery):**
  only a `CompletionReceipt` establishes terminal completion; a `completed` event
  requires a prior `certified` event whose receipt re-verifies; `completed` is terminal
  and never reopened.
- **Failure modes:** completion-without-certification; certification-digest drift;
  reopened-completed; caller-supplied-terminal-status; stale-head; post-commit.
- **E2E matrix:** (1) certified task → complete → `completed`, reload/validate, terminal;
  (2) not-yet-certified → refused; (3) exact replay idempotent; (4) forged certification
  digest → refused; (5) stale head → refused; (6) post-commit recovery retries without a
  second event; (7) boundary proof: no `CorrectnessCertified` write and no
  certified/evidence/proof/revoked/migration event; (8) thin-CLI machine+human output.

## 7. Explicit architect questions (gaps not filled by invention)
- **Q1 — governed coverage gap.** The completion contract lives in source
  (`closureprotocol` + design doc) but is **absent from the live awareness graph**
  (preflight/briefings EMPTY). Should Slice 8.1 also author governed knowledge
  (invariants/failure modes/required tests) for completion, and if so, does that block
  implementation or follow it? (No awareness mutation was made during discovery.)
- **Q2 — evidence/proof operational producers.** No operational producer was found that
  appends `evidence_recorded` or `proof_discharged`. Does `certification.CertifyTask`
  obtain evidence and proof-discharge records from those **ledger events**, or from a
  `certification-request.yaml` / graph / YAML source? If the ledger events have no
  producer, there is a **gap between `proving` and `certified`** that precedes Phase 8 —
  is that gap in Phase 8's scope, a separate phase, or already satisfied by
  certification's record sources? *(Pending the reference sweep; stated as a question,
  not assumed.)*
- **Q3 — completion inputs.** Does completion require a `completion-request.yaml`
  (analogous to `certification-request.yaml`), or does it derive the policy + completing
  actor entirely from the certified event + base binding? Where does `CompletingActor`
  come from operationally (the enrolled agent identity)?
- **Q4 — terminal-status scope.** `TaskTerminalStatus` includes `refused`, `abandoned`,
  `revoked` besides `completed`/`completed_with_exception`. The design treats
  `abandoned`/`stale`/`uncertifiable` as side-transitions from any nonterminal state,
  and `revoked` as a distinct event. Confirm Slice 8.1 owns only
  `completed`/`completed_with_exception`, and that `abandoned`/`refused`/`revoked` are
  separate transitions/phases.
- **Q5 — advance-task vs. standalone.** Phase 6 exposed certification as a standalone
  `certify-change` CLI, not folded into `AdvanceResultTransition`. Confirm Phase 8
  completion should likewise be a standalone `complete-task` transaction (not part of the
  advance-result owner), preserving one-owner-per-transaction.

## 8. Confirmation
No Phase 8 production implementation has begun: this change adds only this discovery
document. No new package, no ledger event, no CLI, no validator, no test, and no
awareness/graph mutation were added. `CorrectnessCertified` remains false; PRs #56/#57/#59
are untouched.
