# Phase 8 Discovery — Terminal Architectural Closure

**Status:** discovery only. No implementation, no new ledger event, no CLI, no
validator, no test, and **no awareness/graph mutation**. This document
reconstructs the Phase 8 contract from repository evidence (frozen
`closureprotocol` types, inline ownership comments, the `architectural-closure-v1`
design law, existing tests, and governed sources). It stops at a proposal for
architect review.

**Round 2 revision.** Round 1 reconstructed Phase 8 as *terminal completion*
only. Per the DISCOVERY REPAIR REQUIRED review, Phase 8 is **terminal
architectural closure**, comprising **two distinct owner-controlled slices**:

- **Slice 8.1 — architectural question disposition and governed promotion** (the
  epistemic feedback loop Phase 7's mandatory `architect_questions` stage created).
- **Slice 8.2 — terminal completion** (`certified → completed`), which now
  *verifies* the loop is closed but mutates none of it.

Slice 8.1 is ordered **before** 8.2: completion consumes an authoritative
question-resolution summary and fails closed if the loop is open.

## 1. Preflight & briefing evidence (recorded honestly — a BLOCKER)

- `sensei preflight --task "Phase 8 discovery…" --file certification/ledger.go
  --file closureprotocol/model.go --file tasksession/advance_result.go --domain
  github.com/globulario/sensei --mode standard` → **`PREFLIGHT_STATUS_EMPTY`,
  Risk `UNKNOWN_IMPACT`, Confidence `LOW`, `coverage sufficient=false anchors=0`.**
- `sensei briefing --file <path>` for `certification/ledger.go`,
  `closureprotocol/model.go`, `tasksession/advance_result.go` → **all
  `No direct awareness anchors found` (EMPTY).**

Interpretation (not permission): the live authoritative awareness graph is built
from a commit that predates the whole closure stack, so it has **no anchors** for
these packages. EMPTY = *this area is unannotated governed knowledge*, not "no
rules apply." Per the architect ruling on Q1, this EMPTY coverage is a **blocker,
not a follow-up chore**: Phase 8 must author governed invariants, failure modes,
forbidden fixes, and required tests **before production implementation**. This
discovery therefore rests entirely on *source* evidence.

## 2. Owner / boundary separation (do not collapse these stores)

The feedback loop and completion span four distinct owners; Phase 8 must respect
the separation and never fold them into one store:

| Owner | Responsibility | Existing anchor |
|---|---|---|
| **Task ledger** (`ledger`, per-task append-only chain) | records the *question lifecycle and disposition* for one task/session/result | `ledger` Store; today it carries no question/answer event — a gap (§5) |
| **Governed-knowledge owner** (`sensei propose` → `docs/awareness/*.yaml`; candidates → `knowledgeadoption`/`adoption.Receipt`) | controls *reusable architectural truth*; CLI-guarded | `cmd_propose.go:87`; `boundary.candidate_promotion`, `decision.candidate_promotion_remains_cli_guarded` |
| **Graph builder** (`graphbuild` + `sensei rebuild`/`build`) | rebuilds the *deterministic knowledge projection* (`embeddata/awareness.nt`), ownership-aware freshness gate | `cmd_rebuild.go:26`, `graphbuild.Compile/Stamp`, `seedfreshness.go`, invariant `awareness.seed_is_generated_author_in_yaml` (`invariants.yaml:288`) |
| **Briefing** (`server.Briefing`) | *reads* the rebuilt graph, strictly task-local/domain-scoped | `server/briefing.go:35`, `server/scope.go:77` `InScope`, invariant `awareness.task_claim_scoping_is_task_local_and_explainable` (`invariants.yaml:434`) |
| **Completion** (Phase 8.2, unbuilt) | *verifies* the loop is closed; mutates none of the above | — |

## 3. Slice 8.1 — architectural question disposition and governed promotion

### 3.1 The loop, and the machinery that already exists

Phase 7's mandatory `architect_questions` stage produces the questions; the
missing operation is carrying an authorized answer back into governed memory.

1. **Question identity (canonical, stable).** `questiongen.Generate`
   (`questiongen.go:268`) walks every current closure blocker (exactly-one
   disposition per blocker, `questiongen.go:798`) and emits `OpenQuestion`
   (`dialogue.go:62-88`) with `BlocksClosureBlockers` (`questiongen.go:377`),
   `BlocksClosureDimension`, `BlocksClaims`, `ArchitectRequired`, `Status`
   (`open/awaiting_architect/answered/resolved/accepted_unknown/superseded`,
   `dialogue.go:33-39`). Identity is content-addressed:
   `StableOpenQuestionID(q)` = `"question."+hash(repo | domain |
   blocks_closure_dimension | question_text | blocks_claims | hypothesis-ids |
   template)` (`dialogue.go:125-153`) — stable across regenerations, independent of
   timestamp/status. Bundled as the stage-8 `ArchitectQuestionsBundle`
   (`resultpipeline/stages.go:72-94`) with `ArchitectQuestionsActionable`.
2. **Answer record + disposition (exists, but offline).** `ArchitectAnswer`
   (`dialogue.go:95-113`): `AnswersQuestions`, `Author AnswerAuthor{Role,ID}`,
   `Statement`, `Classifications` (accepted answer types incl.
   `governed_decision_candidate`, `exception_authorization`,
   `unknown_acknowledgement`, `dialogue.go:41-49`), `Scope`, `EvidenceRefs`,
   **`GovernanceStatus`** — the disposition axis:
   `recorded/awaiting_evidence/awaiting_governance/accepted_for_question/rejected/superseded`
   (`dialogue.go:50-55`). Deterministic `StableArchitectAnswerID`
   (`dialogue.go:155`). Ops `RecordAnswer`/`AdjudicateAnswer`
   (`questiongen/dialogue_ops.go:80,158`); CLI `sensei record-answer` /
   `adjudicate-answer` (`cmd_architecture_dialogue.go`). **These mutate an offline
   `--dialogue` artifact only — not the task ledger and not governed sources.**
3. **Governed promotion owner (exists, CLI-guarded).** `sensei propose`
   (`cmd_propose.go:87`) appends typed entries to `docs/awareness/<file>`;
   `contract_unknown` routes to `candidates/…` review-only with **no rebuild**
   (`cmd_propose.go:420-428,333-341`); other kinds rebuild and re-stage the seed
   (`cmd_propose.go:350-364`). Candidate evaluation: `knowledgeadoption.Run`
   (`engine.go:164`) stages / machine-adopts / rejects — "it **never creates
   governed knowledge**" (`engine.go:3-6`) — writing a bundle to
   `.sensei/project/knowledge`, not `docs/awareness`. The promotion/provenance
   receipt type is **`adoption.Receipt`** (`adoption/receipt.go:27-45`):
   `PromotionStatus` (`candidate/machine_adopted/governed`), `AssertionOrigin`,
   `DecisionActor/Context/Policy/Timestamp`, `ValidForRevision`,
   `ValidForGraphDigest`, `ReviewStatus`, `SourceReceipts`, `RevocationConditions`;
   gate `ValidateMachineAdoption` fails closed unless snapshot-bound
   (`receipt.go:109-139`). Promotion is CLI-guarded law:
   `boundary.candidate_promotion`, `boundary.corpus_cli_mutation`,
   `decision.candidate_promotion_remains_cli_guarded`, forbidden fix
   `ui_direct_candidate_promotion_without_guarded_backend_api`.
4. **Deterministic graph rebuild (exists).** `sensei rebuild` (`cmd_rebuild.go:26`)
   / `build` → `graphbuild.Compile`+`Stamp` (`cmd_build.go:360`) →
   `embeddata/awareness.nt` (ownership-aware freshness gate `seedfreshness.go`;
   Oxigraph PUT *replaces* the default graph so a promoted+removed candidate is
   cleanly reflected, `cmd_rebuild.go:533-536`).
5. **Briefing read path (exists, strictly scoped).** `server.Briefing`
   (`briefing.go:35`) assembles domain-scoped sections via `InScope`
   (`scope.go:77`) / `briefingScope` (`scope.go:89`); guarded against foreign-domain
   leak (`TestInScope*_NoForeignLeak`). Task-local claims stay task-local
   (invariant `task_claim_scoping_is_task_local_and_explainable`; failure mode
   `behavioral.task_claim_scope_blocker_explosion`, `failure_modes.yaml:306`).

### 3.2 The genuine gaps Slice 8.1 must bridge (negative evidence)

- **No bridge from an accepted `ArchitectAnswer` to governed promotion.**
  `record-answer`/`adjudicate-answer` mutate only the offline dialogue;
  `propose`/`knowledgeadoption`/`promotion_gate` never read answers (grep for
  `ArchitectAnswer`/`accepted_for_question`/`AnswerType*` in those files → zero
  hits). An answer reaching `accepted_for_question` feeds **no** promotion path.
- **No task-ledger record for question disposition.** The six post-proving ledger
  events do **not** include a question/answer event; the dialogue artifact holds
  answers but no ledger event records the lifecycle/disposition. Slice 8.1 must
  define one (a new task-ledger event + its payload validator).
- **No `PromotionReceipt` type** (closest is `adoption.Receipt`); **no
  `AnswerDisposition`/`DispositionRecord`** type (the disposition axis is
  `ArchitectAnswer.GovernanceStatus`).
- **Answers/questions are not surfaced in briefings.** `OpenQuestion`/
  `ArchitectAnswer` are `explicit query only` RDF classes (`vocab.go:61-62`),
  absent from `briefing.go` (only counted in `metadata.go:167`). The "briefing
  feedback" leg does not close — promoted knowledge must reach future briefings
  with provenance back to the originating question/answer.
- **The pipeline's `unresolved` set is status-blind** (`build.go:635-640`: every
  `ArchitectRequired` question, no status filter), so answering unblocks proving
  only indirectly, via a graph rebuild that removes the backing blocker —
  reinforcing that **promotion + rebuild is the real unblock path**, and it is
  currently disconnected from the answer record.

### 3.3 Candidate Slice-8.1 shape (proposal — do not build)

Two small, distinct owner-controlled operations, each mirroring the accepted
patterns:

1. **Record an authorized disposition into the task ledger** (a new task-ledger
   transaction, standalone CLI e.g. `sensei disposition-question` — **not** hidden
   in `complete-task`). It consumes a stable `question.<hex>` identity from the
   Phase-7 result's `architect_questions` bundle; records disposition ∈ {answer,
   dismiss, defer, task-local} bound to task/session/result/question identity, an
   **authority-verified** answering actor (reuse `closureprotocol.ActorBinding` +
   `authority.VerifyActorBinding`, never a generic "AI answer" authority),
   rationale, effective scope, and answer provenance; distinguishes **task-local**
   resolution (stays in the ledger/dialogue) from **reusable** truth. The payload
   embeds/derives the frozen `ArchitectAnswer` + its `GovernanceStatus`; a new
   payload-validator branch requires it. Dismissed/deferred questions remain
   durable and explainable in the chain.
2. **Promote a reusable answer through the governed-knowledge owner** — the answer
   flows into `sensei propose` (governed source mutation) → `rebuild`
   (`graphbuild`) → seed, bound to the exact canonical claim/decision/invariant via
   a governed-promotion record. Reuse **`adoption.Receipt`** as the promotion
   receipt (bind `AssertionOrigin=promoted`, `DecisionActor`,
   `ValidForRevision/GraphDigest`, `SourceReceipts` = the certified result +
   originating question/answer). Promotion stays **CLI-guarded** (never a direct
   graph write). Future briefings must then surface the promoted node with
   provenance `PropAnswersQuestion`/`PropAuthoredIn` back to the question — which
   requires wiring `OpenQuestion`/`ArchitectAnswer` (or the promoted node's
   provenance) into `briefing.go`'s scoped sections.

Semantics (mirror the accepted Step-8 discipline): exact promotion replay is
**idempotent** (same answer+claim → no duplicate governed node / no second ledger
event); a **contradictory** promoted answer, a **stale** graph digest, an
**unauthorized** promotion, or a **concurrent** promotion fails closed;
task-local answers must be kept out of unrelated repository-wide briefings
(reuse `InScope`); a **graph-rebuild failure** leaves governed sources and the
ledger consistent (fail closed, retryable) and never a partially-promoted world;
**post-commit recovery** exposes committed identity and permits exact retry.

## 4. Slice 8.2 — terminal completion (accepted findings + the new gate)

The Round-1 terminal-completion reconstruction is retained and accepted:

- **Owner:** a new `golang/architecture/completion` package — pure `Evaluate`
  (produces a `CompletionReceipt`) + ledger-integrated `CompleteTask` (sole side
  effect), plus a thin standalone `sensei complete-task` CLI (**not** folded into
  `AdvanceResultTransition`; architect Q5 ruling). Mirrors `certification.CertifyTask`.
- **Transition:** `certified → completed` only (design state machine, illegal
  `scope_verified/proving → completed`, `completed` terminal/never reopened).
  `completed`/`completed_with_exception` only (architect Q4); revocation,
  abandonment, refusal, migration remain distinct.
- **Authoritative completion input (architect Q3 ruling):** an **explicit,
  authority-bound completion request/record** — a content-addressed
  `completion-request.yaml`-style locator following the certification-request
  pattern (`certification/source.go`). The completing actor must **not** be an
  arbitrary caller string nor a silent reuse of the certifying actor.
- **Consumes, never re-runs:** the persisted `CertificationReceipt` via
  `certification.VerifyReceipt` (`receipt.go:47`) + digest recompute
  (`CompletionReceipt.CertificationDigestSHA256`); the upstream chain digests; the
  `completion.architectural_closure.v1` policy. It never re-runs certification
  lanes and never writes `CorrectnessCertified` (Phase 6 sole writer).
- **Output/projection/semantics:** one `completed` event carrying a validated,
  content-addressed `CompletionReceipt` (`ValidateCompletionReceipt` `validate.go:416`,
  `CompletionReceiptDigest` `canonical.go:127`); status projection →
  `PhaseCompleted`/`TerminalCompleted` (rebuilt, never written directly); a new
  `completed` payload-validator branch in `ledger/event.go` (mirroring the
  `result_transition_receipt` rule); exact-replay idempotency, expected-head
  stale refusal, durable-entry post-commit recovery.

**New gate (the loop must be closed).** Completion consumes an authoritative
question-resolution summary and **fails closed** when:

- a closure-blocking `ArchitectRequired` question remains unresolved (today
  surfaced as `proofrequirements.ProvingBlocked` on
  `UnresolvedArchitectQuestionIDs`, `compose.go:377`);
- a reusable answer was accepted (`accepted_for_question`) but its governed
  promotion/rebuild is **incomplete or stale** (adoption not `governed`, or the
  promoted node's `ValidForGraphDigest` ≠ the current seed digest);
- a **contradictory** promoted answer remains contested;
- required question **provenance does not bind the exact certified result**.

Completion **must not** itself answer questions or mutate governed knowledge; it
only verifies the separate question/promotion owner (Slice 8.1) completed its work.

## 5. Phase-boundary ownership (Phases 3–8)

| Phase | Package(s) | Event(s) owned | Transition |
|---|---|---|---|
| 3 admission | admission, authority, identity | authority_resolved, admission_decided, admission_consumed, change_observed, scope_verified | → scope_verified |
| 4 evidence | evidencereceipt | none (evidence artifacts) | — |
| 5 proof | proofdischarge | none (proof-discharge artifacts) | — |
| 6 certification | certification (+ `certify-change`) | **certified** — sole `CorrectnessCertified` writer | proving → certified |
| 7 result transition | resulttransition+resultpipeline+resultrecording+`AdvanceResultTransition`; questions via `questiongen` | **result_transition_recorded** (+ the `architect_questions` bundle) | scope_verified → proving |
| **8.1 question disposition + promotion** | **new** — a question-disposition ledger transaction + governed promotion via `propose`/`adoption`/`graphbuild`/`briefing` | **new task-ledger disposition event** (name TBD) | records dispositions; promotes reusable truth (does not change task phase) |
| **8.2 completion** | **new** `completion` (+ `complete-task`) | **completed** | certified → completed |

`evidence_recorded`/`proof_discharged`/`revoked`/`migration_executed` remain
reserved, non-operational vocabulary (architect Q2); Phase 8 neither requires nor
emits them.

## 6. Combined E2E matrix (proposed — proves BOTH the loop and completion)

**Feedback loop (Slice 8.1):**
1. A Phase-7 result with an `ArchitectRequired` open question → disposition
   `answer` recorded in the task ledger, bound to the exact question/result/actor
   identity; question status → resolved; a task-status projection reflects it.
2. A **reusable** answer → governed promotion via `propose` → `rebuild` → the
   promoted claim/decision/invariant appears in `docs/awareness/*` and the rebuilt
   seed, with an `adoption.Receipt` binding actor/revision/graph-digest.
3. A **future briefing** surfaces the promoted knowledge with provenance back to
   the originating question/answer; a **task-local** answer does **not** leak into
   an unrelated-domain briefing (`InScope`).
4. Dismissed/deferred questions remain durable and explainable.
5. Exact promotion replay is idempotent (no duplicate node/event); contradictory
   / stale-digest / unauthorized / concurrent promotion fails closed; a graph-
   rebuild failure is atomic + retryable (post-commit recovery).

**Terminal completion (Slice 8.2):**
6. A certified task whose loop is closed → `complete-task` → one `completed` event,
   independent reload/validate, `PhaseCompleted`/`TerminalCompleted`.
7. Fail-closed: an unresolved closure-blocking question → refused; an accepted-but-
   unpromoted (or stale-promotion) answer → refused; a contradictory promoted
   answer → refused; question provenance not binding the certified result → refused.
8. Not-certified task → refused; forged certification digest → refused; exact
   replay idempotent; stale head → refused; post-commit recovery; boundary proof
   (no `CorrectnessCertified` write; no
   certified/evidence/proof/revoked/migration event); thin-CLI machine+human
   output with no caller-supplied phase/status/verdict/terminal-status.

## 7. Architect rulings (folded) + remaining questions

Rulings applied: **Q1** govern-first is a blocker (§1); **Q2** evidence/proof
events reserved, never required/emitted; **Q3** explicit authority-bound
completion request, no arbitrary/certifying-actor reuse; **Q4** completion owns
only `completed`/`completed_with_exception`; **Q5** completion standalone, and
question disposition/promotion is its own owner-controlled transaction.

Remaining source-gaps → explicit architect questions:

- **A1 — the disposition ledger event.** No task-ledger event for question
  disposition exists. Confirm Phase 8 defines a new event (name? e.g.
  `architect_answer_recorded` / `question_disposition_recorded`) with a payload
  validator, distinct from the six reserved post-proving events; and confirm the
  answering-actor authority model (which role/grant authorizes an answer vs a
  promotion — reuse `authority.VerifyActorBinding`; do **not** invent an "AI
  answer" authority).
- **A2 — answer → promotion bridge.** There is no code linking an
  `accepted_for_question` answer to `propose`/governed promotion. Confirm the
  intended bridge: does Slice 8.1 add an `answer → propose` path (the answer's
  classification `governed_decision_candidate`/`exception_authorization` selecting
  the propose kind + target governed record), gated by the promotion CLI owner and
  `adoption.ValidateMachineAdoption`?
- **A3 — promotion receipt.** Confirm `adoption.Receipt` is the promotion receipt
  Phase 8 should bind (vs a new type), and how it binds to the originating
  question/answer + the certified result (via `SourceReceipts`).
- **A4 — briefing surfacing.** Surfacing promoted answers in briefings requires
  wiring `OpenQuestion`/`ArchitectAnswer`/the promoted node into `briefing.go`
  scoped sections (they are explicit-query-only today). Confirm this is in Slice
  8.1 scope and how provenance-back-to-question is presented without leaking
  task-local answers.
- **A5 — completion's question-resolution summary.** Confirm the authoritative
  source completion reads to verify the loop is closed: the Phase-7
  `ArchitectQuestionsBundle` + `proofrequirements.ProvingDisposition` on the
  certified result, cross-checked against governed promotion state
  (adoption `governed` + `ValidForGraphDigest` == current seed). Is the
  status-blind `unresolved` computation (`build.go:635-640`) acceptable, or must
  Phase 8 read answer `GovernanceStatus` too?
- **A6 — governed knowledge to author (Q1).** Which invariants/failure-modes/
  forbidden-fixes/required-tests must Slice 8.1/8.2 author before implementation
  (e.g. "only the governed-knowledge owner promotes reusable truth", "completion
  never mutates governed knowledge", "a completed task's blocking questions are all
  resolved+promoted-or-task-local")? Authoring them is itself an awareness mutation
  gated to a later reviewed step.

## 8. Confirmation

No Phase 8 production implementation has begun and **no governed/awareness
mutation has been made**: this change adds only this discovery document — no new
package, ledger event, CLI, validator, test, or graph rebuild. `CorrectnessCertified`
remains false; PRs #56/#57/#59 are untouched. The task ledger, governed-source
authority, and briefing projection remain three distinct stores in this proposal.
