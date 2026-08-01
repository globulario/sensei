# Governed Evaluator Composition O4

Status: architecture contract, checkpoint 1

## Purpose

Compose the O1 governed synthesis state machine (`golang/architecture/synthesis`), the O2 provider-neutral execution port (`golang/architecture/providerport`), and the O3 governed runner (`golang/architecture/runnercomposition`) into one bounded evaluation stage.

O4 evaluates exactly one O3-sealed candidate and produces either:

- one closed `synthesis.Evaluation` accepted by `synthesis.Transition`; or
- one explicit evaluator-unavailable terminal transition when required evaluation cannot be performed.

O4 does not decide admission, completion, mergeability, promotion, or repository mutation. A passing evaluation means only that O1 may produce `candidate-ready-for-admission`, which O5 may later submit to the existing admission and verification owners.

The central problem is not merely running tests. O4 must prove that every observation belongs to the exact accepted attempt and exact sealed candidate, preserve each evaluator's limitations, and derive the final recommendation through deterministic composition rather than trusting any evaluator or provider to choose session outcome.

## Architectural position

```text
O1 SessionState in PhaseAttempting
  + exact O3 generation evidence
      - providerport.Request
      - providerport.Result
      - providerport.Receipt
      - runnercomposition.RunnerReceipt(disposition=verified)
      - runnercomposition.CandidateArtifact

  -> generation handoff validation
      - declared digest == recomputed digest for every document
      - O3 receipt references the exact O2 request/result/receipt
      - candidate artifact is loaded by the exact digest in the O3 receipt
      - O2 generation result maps to RecordAttemptCommand
      - synthesis.Transition accepts the exact Attempt

  -> O1 SessionState in PhaseEvaluating

  -> governed evaluator composition
      - fresh evaluator surfaces materialized from the sealed artifact
      - mechanical tests
      - Sensei edit/diff audit
      - incident/scar matching
      - proof-obligation checks
      - optional external evaluator observations through O2
      - each result remains independently attributable and limited

  -> deterministic recommendation composition
      - one O1 synthesis.Evaluation
      - or EvaluatorUnavailableCommand for required evaluation unavailability

  -> synthesis.Transition decides:
      candidate-ready-for-admission | retry | replan | architect-review | abort

  -> STOP
```

O4 does not execute the next retry, replan, admission, or provider-selection step. It returns the resulting O1 state and events to its caller.

## The generation-handoff seam

O3 deliberately stopped before `providerport.MapToCommand` and `synthesis.Transition`. O4 must close that seam without reconstructing any value from digests or from the sealed candidate.

The current O3 `RunnerReceipt` references O2's Request, Result, and Receipt by digest. Digests are identity references, not storage. O4 therefore requires an exact generation handoff carrying the original O2 values together with the O3 receipt and candidate artifact identity.

`VerifiedGenerationHandoff` is the one canonical carrier. There is no alternative shape:

```go
type VerifiedGenerationHandoff struct {
    Request       providerport.Request
    Result        providerport.Result
    O2Receipt     providerport.Receipt
    RunnerReceipt runnercomposition.RunnerReceipt
}
```

Durability: this carrier is process-local, not crash-resume durable. Today, `providerport.Run` returns `Request`/`Result`/`O2Receipt` as plain Go values, and O3's `Run` returns `RunnerReceipt` as a plain Go value -- neither package writes any of the four to a store. Only the sealed `CandidateArtifact` bytes are durable (`CandidateArtifactStore`). `VerifiedGenerationHandoff` therefore exists only for the span of one caller's synchronous O3-`Run` -> O4-handoff-validation -> first-`Transition` call chain within a single process invocation. A crash anywhere in that span is recovered by re-running O3's `Run` from scratch against the same Session/Plan/identity, never by resuming a persisted handoff -- O3's evidence forge is deterministic and content-addressed, so re-running it is safe and produces the same sealed artifact. Durable, crash-resume handoff persistence is out of scope through checkpoint 5 and would require its own explicit design decision if ever added.

It must satisfy all of these laws:

1. The Request, Result, and O2 Receipt are the exact values O3 observed, never reconstructed from `RunnerReceipt` digests.
2. O4 recomputes and verifies every document digest again because mutable Go values may have changed after O3 validated them.
3. `RunnerReceipt.Disposition` must be `verified`.
4. `RunnerReceipt.RequestDigestSHA256`, `ResultDigestSHA256`, and `O2ReceiptDigestSHA256` must match the recomputed O2 documents.
5. `RunnerReceipt.CandidateArtifactDigestSHA256` must be present and must address a valid artifact returned by the canonical `CandidateArtifactStore` acceptance path.
6. `providerport.MapToCommand` must produce `synthesis.RecordAttemptCommand` from this exact Request/Result pair.
7. `synthesis.Transition` must accept that command from the current `PhaseAttempting` state and move to `PhaseEvaluating`.
8. O4 must never synthesize an Attempt from `CandidateArtifact`, `RunnerReceipt`, or matching field values.

`RecordAttemptCommand`'s Transition call does not unconditionally enter `PhaseEvaluating`: when the accepted Attempt's own `TerminalProviderStatus` is `invalid_output`, `synthesis.Transition` short-circuits directly to a terminal `Failed`/invalid-provider-output receipt, bypassing `PhaseEvaluating` entirely. This is legitimate existing O1 behavior, not an O4 failure -- a `verified` O3 disposition only proves O3's own evidence integrity and says nothing about `TerminalProviderStatus`. O4 must check the resulting phase after this first Transition call and, when the session has already terminated, stop and return that state and its events without attempting candidate loading, evaluator materialization, evaluator composition, or a second Transition call.

A handoff failure is a contract/programming failure. O4 returns a non-nil Go error and leaves O1 state unchanged. It is not an evaluator observation and must not be converted into a recommendation.

## Candidate identity and evaluator surfaces

The canonical evaluation input is the sealed `runnercomposition.CandidateArtifact`, loaded by the exact digest referenced by the verified O3 receipt.

O4 validates all cross-bindings before any evaluator runs:

- candidate repository domain and base revision equal the O1 Session;
- candidate workspace identity digest equals the O1 Session;
- candidate session digest equals the O1 Session digest;
- candidate plan digest and generation equal the accepted Attempt and current SessionState;
- candidate attempt number equals the accepted Attempt and current SessionState;
- candidate input digest equals `Attempt.InputCandidateDigestSHA256`;
- candidate proposed-change digest equals `Attempt.ProposedChangeDigestSHA256`;
- candidate artifact digest equals the O3 receipt reference and its own recomputed digest.

Evaluators never receive the O3 temporary workspace. It no longer exists and was never authoritative after sealing.

Each evaluator receives a fresh disposable materialization derived from the sealed manifest. A materialization may be writable when a real build or test tool needs to create generated files, caches, or temporary outputs, but:

- writes are confined to that evaluator's disposable surface;
- writes never alter the sealed artifact or another evaluator's surface;
- evaluator-generated files are observations, not candidate changes;
- no evaluator may replace the candidate identity with a post-test tree digest;
- cleanup outcome is recorded independently from evaluation outcome;
- retained handles are revoked before backing directories are removed.

## Evaluator model

O4 uses one provider-neutral evaluator interface. Evaluator kind is data, not interface shape.

Conceptually:

```go
type Evaluator interface {
    Describe(ctx context.Context) (EvaluatorDescriptor, error)
    Evaluate(ctx context.Context, EvaluationInput) (EvaluatorResult, error)
}
```

`EvaluatorDescriptor` is closed and includes at minimum:

- stable evaluator ID;
- evaluator kind;
- evaluator version;
- supported check IDs or check classes;
- deterministic/nondeterministic declaration;
- required runtime capabilities;
- known limitations;
- descriptor digest.

`EvaluationInput` binds at minimum:

- O1 Session digest;
- accepted Attempt digest;
- candidate artifact digest;
- repository domain and base revision;
- plan generation and attempt number;
- disposable evaluator-surface handle;
- precommitted deadline and evidence limits;
- required proof-obligation digests relevant to this session.

`EvaluatorResult` is an O4 observation document, not an O1 `Evaluation`. It includes:

- evaluator descriptor digest;
- exact input digest;
- terminal evaluator outcome;
- ordered check observations;
- evidence references and evidence digests;
- classified failure observations;
- limitations;
- cleanup truth;
- result digest.

An evaluator never returns an authoritative O1 Recommendation. It may report classifications or a suggested action as untrusted evidence, but the O4 composer owns the only mapping into `synthesis.Evaluation.Recommendation`.

## Initial evaluator classes

O4 defines composition mechanics and typed ports. Concrete adapters are added only for existing Sensei-owned surfaces that can be mapped without duplicating authority.

The initial composition must support these classes:

1. **Mechanical checks**
   - bounded build, test, vet, lint, schema, freshness, or repository-defined commands;
   - exact command, environment policy, working surface, exit status, and captured evidence are receipted;
   - command success is an observation, never proof of admission or architectural correctness.

2. **Sensei edit/diff audit**
   - evaluates the exact O3 proposed-change identity and sealed candidate content through the existing per-file/per-diff advisory surface (`sensei edit-check`, and `sensei gate --diff <range> --enforce` for its diff-scoped enforce-mode form) -- not `sensei audit`, which self-audits the awareness-graph corpus (embeddata freshness, YAML validity, coverage) and has no candidate-diff scope;
   - must not rebuild a different diff from an unrelated checkout and call it equivalent; `sensei gate` currently sources its diff from a live git ref range (`git diff --unified=0 <range>`), so the exact seam by which it consumes a sealed, disposable candidate materialization instead of a live checkout is an open integration question, to be resolved no later than checkpoint 4, not by this document;
   - `edit-check`'s RPC and `gate`'s report-only mode are advisory and fail-open by original design (built for interactive authoring, where an unreachable server must never block a developer). O4 must treat an unreachable or degraded result from either as `CheckUnavailable`, never as a passing check, per hard law 10;
   - forbidden fixes and binding invariants remain visible in check evidence.

3. **Incident and behavioral-scar matching**
   - matches evaluator failures, diagnostics, or stack traces first against this session's own already-governed, already-digested `synthesis.Interpretation.KnownFailureModes` / `ForbiddenFixes` (bound into Session identity at interpretation time), then, for anything not already captured there, against the generic awareness-graph query surface over `failure_mode` graph nodes. There is no separate purpose-built "incident memory" component today, and O4 must not create one;
   - a match classifies evidence and may strengthen retry, replan, review, or abort rationale;
   - memory output is advisory evidence unless an existing invariant or contract gives it blocking force.

4. **Proof-obligation checks**
   - verifies the exact proof obligations referenced by `synthesis.Session.ProofObligationDigests` against the existing `closureprotocol.ProofDischarge` owner, matched by `DischargeDigestSHA256`;
   - absence, staleness, wrong candidate binding, or unverifiable proof is never silently treated as discharged;
   - O4 does not redefine the proof owner or proof semantics.

5. **External evaluator observations**
   - may run through O2 `OperationEvaluationObservation`;
   - the O2 provider's completed `synthesis.Evaluation` payload is still untrusted observation input;
   - O4 must not pass that provider-produced Evaluation directly through `providerport.MapToCommand`, because doing so would allow a provider to choose O1's final Recommendation;
   - O4 validates and projects its checks, classifications, and limitations into an `EvaluatorResult`, then composes them with every other evaluator.

Before implementation, each concrete evaluator adapter must name the existing Sensei owner it calls. An adapter may translate an owner; it may not recreate that owner's policy.

## Evaluation policy

Evaluator selection and recommendation rules are precommitted before execution in one closed, digest-addressed O4 policy document.

**Authority is fixed, not deferred**: the O4 *caller* -- the same authority that owns invoking O3's `Run` and driving the session -- selects and supplies one immutable, self-digested policy document before O4's first `Transition` call. O4 validates that policy (schema, self-consistency of its own declared digest against its recomputed content digest, and that it binds the exact Session/Attempt/candidate identity in play) and applies it. O4 never constructs a default policy, never fills in an absent policy, and never amends one after validating it. A policy that fails validation is a contract/programming failure (a Go error, same category as a handoff failure), never an evaluator observation.

The policy includes at minimum:

- exact Session and Attempt identity;
- exact candidate artifact identity;
- ordered evaluator specifications;
- required versus optional evaluator status;
- deadlines and evidence limits;
- required check IDs where applicable;
- deterministic failure-class-to-recommendation rules, narrowing (never reordering -- see "Initial recommendation precedence" below) the fixed precedence this contract establishes;
- policy digest.

Providers and evaluators cannot add evaluators, mark themselves optional, enlarge limits, alter recommendation precedence, or rewrite the policy after observing results.

Policy is execution configuration, not canonical architectural truth. O4 must not infer policy from provider capability claims.

### Initial recommendation precedence

This contract fixes the precedence among the four non-accept recommendations now; it is not deferred to checkpoint 5. When more than one applies to the same evaluation, the highest-precedence recommendation wins, evaluated in this fixed order regardless of evaluator completion order (hard law 18):

1. **`abort`** -- reserved for evidence that no retry or replan of this session could possibly resolve: a blocking Sensei audit/forbidden-fix violation, or a proof obligation classified as permanently undischargeable against this candidate.
2. **`architect-review`** -- evidence that is ambiguous, policy-boundary, or otherwise not safely automatable: an incident/scar match flagged as concerning but not itself blocking, or a required-evaluator failure classification the policy does not map cleanly to `retry-generation` or `replan`.
3. **`replan`** -- evidence that the current *plan* (not merely this attempt) cannot reach the objective as structured: a proof obligation that is structurally undischargeable under the plan's current step sequence, or a mechanical/audit failure the policy classifies as plan-level rather than attempt-level.
4. **`retry-generation`** -- the default, lowest-severity non-accept outcome: evidence that is attempt-local and plausibly resolved by a fresh generation attempt against the same plan (e.g. a mechanical test failure with no plan- or policy-level classification).

A policy may narrow this precedence for its own session (e.g. disable a class entirely by declaring no evaluator that can produce it), but must not reorder it, and must not introduce a fifth outcome. `accept-candidate` sits outside this precedence entirely -- it is legal only when the unanimous "Recommendation hard floor" below holds, never by falling through an empty precedence chain.

## Deterministic composition

O4 produces exactly one normalized `synthesis.Evaluation` when every required evaluator reached an evidenced terminal result.

The final artifact uses:

- `EvaluatorKind`: the governed O4 composer identity, not one component evaluator;
- `EvaluatorVersion`: the O4 contract/implementation version;
- `Checks`: the normalized union of component check observations;
- `ClassifiedFailureReasons`: deterministic normalized classifications from component evidence;
- `Recommendation`: derived only by the accepted O4 policy;
- `Limitations`: the normalized union of component and composition limitations;
- `EvaluationDigestSHA256`: freshly computed and self-verified.

Canonical ordering is required. At minimum:

- evaluator results ordered by evaluator ID;
- checks ordered by evaluator ID then check ID;
- evidence references sorted and deduplicated without losing their owning evaluator binding;
- failure classifications sorted and deduplicated;
- limitations canonically ordered;
- identical validated inputs produce byte-for-byte equivalent Evaluation and O4 receipt identity.

## Recommendation hard floor

Regardless of configurable non-success policy, `accept-candidate` is legal only when all of the following are true:

- every required evaluator completed;
- every required check passed;
- no required check was skipped or unavailable;
- every required proof obligation was discharged against this exact candidate;
- no blocking limitation remains;
- Sensei audit reports no blocking invariant, contract, forbidden-fix, or scope violation;
- the final Evaluation and every referenced evaluator result pass declared-versus-computed digest validation.

A required skipped or unavailable check is never equivalent to pass.

Optional evaluator unavailability remains visible as a limitation and may not silently increase confidence.

The mapping among evidenced failure classes and `retry-generation`, `replan`, `architect-review`, or `abort` follows the fixed precedence in "Initial recommendation precedence" above, narrowed (never reordered) by the caller-supplied policy. No language-model prose may choose among them implicitly.

## Required evaluator unavailable, and every other non-evaluated O4 stop

When a required evaluator cannot produce a valid result at all, O4 does not fabricate a partial passing Evaluation. The same governed-terminal principle applies to every failure that occurs once O4 owns the session -- `candidate-load-failure`, `materialization-failure`, `required-evaluator-unavailable`, and `composition-failure` all produce an O4 receipt carrying the precise disposition, then all four map that receipt's bounded detail (the disposition and its `FailureDetail`) into `synthesis.EvaluatorUnavailableCommand.Detail`. O1 performs its one existing terminal transition (`ReasonEvaluatorUnavailable`) using its existing semantics -- unconditionally, with no retry/replan budget check, exactly as `transitionEvaluatorUnavailable` already behaves today.

This closes what checkpoint 1's first review round flagged as an infinitely-retryable "parked in `PhaseEvaluating`" limbo: none of these four failures may leave SessionState parked with no O1-recorded consequence. Every one of them ends the session through the same governed terminal command, distinguishable afterward only by the O4 receipt's own disposition and by `EvaluatorUnavailableCommand.Detail`, never by silence.

This path is distinct from:

- a required evaluator completing with failed checks, which produces an Evaluation and a policy-derived non-accept recommendation (disposition `evaluated`);
- an optional evaluator being unavailable, which remains a limitation inside a completed Evaluation (also disposition `evaluated`);
- the accepted Attempt's own `TerminalProviderStatus` being `invalid_output`, which O1 already terminated directly on the *first* Transition call, before `PhaseEvaluating` was ever entered (disposition `invalid-output-terminated`, below) -- O4 makes no second Transition call for this case, because O1 has nothing left to be told;
- an O4 contract/programming failure where no valid O4 receipt can be constructed at all (e.g. the policy document itself fails validation), which returns a Go error and leaves state unchanged.

## O4 receipt

O4 introduces one closed receipt document binding the entire evaluation composition without replacing O1's `synthesis.Evaluation` or terminal `synthesis.Receipt`.

The receipt references at minimum:

- Session digest;
- accepted Attempt digest;
- candidate artifact digest;
- O3 RunnerReceipt digest;
- O2 generation Request, Result, and Receipt digests;
- evaluation-policy digest;
- ordered evaluator descriptor/result digests;
- final O1 Evaluation digest when produced;
- terminal O4 disposition;
- materialization cleanup truth;
- receipt digest.

Initial disposition vocabulary:

| Disposition | Meaning | Evaluation digest | O1 command |
|---|---|---:|---|
| `invalid-output-terminated` | The accepted Attempt's own `TerminalProviderStatus` was `invalid_output`. O1's first `RecordAttemptCommand` Transition call already terminated the session (`ReasonInvalidProviderOutput`) before `PhaseEvaluating` was ever entered. | nil | none -- O1 already terminated via the *first* Transition call; this disposition only binds O4's own evidence to that already-recorded O1 receipt |
| `candidate-load-failure` | The exact sealed artifact could not be loaded or validated after the generation handoff was accepted. | nil | `EvaluatorUnavailableCommand` |
| `materialization-failure` | A required evaluator surface could not be constructed from the sealed artifact. | nil | `EvaluatorUnavailableCommand` |
| `required-evaluator-unavailable` | A required evaluator could not produce a valid terminal result. | nil | `EvaluatorUnavailableCommand` |
| `composition-failure` | Evaluator results existed but could not be composed into a valid Evaluation under the accepted policy. | nil | `EvaluatorUnavailableCommand` |
| `evaluated` | A valid O1 Evaluation was composed and accepted by `synthesis.Transition`. | present | `RecordEvaluationCommand` |

Every disposition now ends in an O1-recorded consequence: `invalid-output-terminated` binds evidence to a termination O1 already recorded on the first Transition call; the four `EvaluatorUnavailableCommand` dispositions each produce their own governed O1 termination on the second Transition call; `evaluated` produces `RecordEvaluationCommand`. No disposition leaves SessionState parked with nothing O1-recorded -- there is exactly one Transition call for `invalid-output-terminated` and exactly two for every other disposition, never zero and never a dangling first call with no second.

The implementation may refine this vocabulary only during contract review. It must not collapse contract failure, evaluator unavailability, failed checks, and composition failure into one ambiguous error.

Cleanup success is orthogonal to disposition. A sealed candidate remains immutable even when disposable evaluator-surface cleanup fails. Cleanup failure must be visible and must not rewrite the Evaluation recommendation after composition.

## O1 transition boundary

O4 may call `synthesis.Transition` at most twice:

1. once to record the exact verified generation Attempt -- this either enters `PhaseEvaluating`, or (disposition `invalid-output-terminated`) O1 itself terminates the session immediately, in which case O4 makes no second call;
2. once, only when the first call entered `PhaseEvaluating`, to record either the composed Evaluation (`RecordEvaluationCommand`, disposition `evaluated`) or a governed unavailability termination (`EvaluatorUnavailableCommand`, dispositions `candidate-load-failure`, `materialization-failure`, `required-evaluator-unavailable`, or `composition-failure`).

O4 returns the resulting state and events. It does not call:

- `StartAttemptCommand` after a retry recommendation;
- `StartPlanningCommand` after a replan recommendation;
- any admission or verification owner after candidate-ready;
- any repository mutation or GitHub write.

The caller owns continuation beyond this one bounded evaluation stage.

## Hard laws

1. **Exact generation evidence crosses the seam; digests alone are not storage.** O4 never reconstructs O2 Request, Result, Receipt, or Attempt from O3 digest references.
2. **Only O3 `verified` may be recorded as an Attempt.** Every other O3 disposition stops before `MapToCommand`.
3. **The sealed CandidateArtifact is the only candidate input.** No live checkout, O3 temp directory, provider path, or ambient working tree may substitute.
4. **Attempt and candidate identity are independently cross-verified.** Matching field names are not sufficient; every declared digest is freshly recomputed.
5. **Evaluator surfaces are disposable and isolated from each other.** No evaluator consumes another evaluator's mutated surface.
6. **Evaluator mutation is not candidate mutation.** Generated files and caches remain evaluation evidence only.
7. **Evaluator claims are observations, not authority.** No evaluator directly creates the final O1 Recommendation.
8. **External O2 evaluation output is not mapped directly into O1.** O4 composes it as one observation source.
9. **Selection and limits are precommitted, and policy is caller-supplied.** Evaluators cannot add themselves, enlarge budgets, or change required/optional status. The O4 caller selects and supplies one immutable, self-digested policy before O4's first Transition call; O4 validates and applies it but never constructs, defaults, or rewrites it.
10. **Required skipped/unavailable never means pass.** Absence of evidence cannot become positive evidence.
11. **Accept requires unanimous required success.** Every required check and proof obligation must bind to the exact candidate.
12. **Failure classification is deterministic.** Provider prose cannot silently choose retry, replan, review, or abort.
13. **Limitations survive composition.** A result cannot become stronger by losing the caveats of its sources.
14. **O1 remains transition authority.** O4 composes commands; `synthesis.Transition` decides budget and terminal consequences.
15. **Evaluation is not admission.** `accept-candidate` means candidate-ready-for-admission only after O1 accepts it.
16. **No autonomous continuation.** O4 stops after the evaluation transition.
17. **Cleanup truth is independent.** Cleanup failure is recorded without laundering or rewriting evaluator outcome.
18. **Same evidence, same result.** Normalization, composition, recommendation, receipt identity, and O1 commands are deterministic.
19. **Negative controls are mandatory.** Every authority, lineage, availability, and recommendation law must fail when its old bug is restored.
20. **No owner is approximated.** Existing audit, incident, proof, admission, and completion owners are called or referenced, never recreated.
21. **No disposition without an O1-recorded consequence.** Every disposition ends in an O1 Transition call that already recorded, or now records, a governed terminal or evaluation outcome. SessionState is never left parked in `PhaseEvaluating` with no O1-recorded trace of what happened.

## Required adversarial proofs

Implementation must prove at minimum:

- a RunnerReceipt with any disposition other than `verified` cannot reach `MapToCommand`;
- a self-consistent but unrelated O2 Result cannot be substituted for the result referenced by O3;
- a mutated Request, Result, O2 Receipt, RunnerReceipt, policy, evaluator result, Evaluation, or O4 receipt is rejected by declared-versus-computed digest validation;
- a valid CandidateArtifact from another session, plan, generation, or attempt is rejected;
- a candidate artifact with matching metadata but tampered manifest bytes is rejected;
- evaluation never reads the live repository checkout;
- two evaluators cannot observe each other's generated files;
- retained evaluator handles fail after revocation and revocation precedes directory removal;
- a required skipped or unavailable check cannot produce `accept-candidate`;
- an optional unavailable evaluator remains visible as a limitation;
- a provider-supplied `accept-candidate` recommendation cannot override mechanical failure;
- direct `providerport.MapToCommand` use on an external evaluation-observation result is absent or mechanically refused in O4;
- missing or stale proof obligations cannot be marked discharged;
- identical inputs with evaluators returned in different completion order produce identical normalized Evaluation and receipt digests;
- retry/replan budgets are changed only by O1 Transition, never by O4 policy or evaluator output;
- O4 performs no third transition and no autonomous continuation;
- an O4-constructed or O4-defaulted policy is rejected; only a caller-supplied, self-digest-verified policy is accepted, and no evaluator, provider, or O4 code path can rewrite it after validation;
- when multiple evidenced failure classes apply to one evaluation, the fixed precedence (`abort` > `architect-review` > `replan` > `retry-generation`) determines the recommendation regardless of evaluator completion or evidence-discovery order, and a policy that attempts to reorder rather than narrow this precedence is rejected;
- every disposition other than `evaluated` produces exactly one O1-recorded terminal consequence (either the first Transition call terminating directly for `invalid-output-terminated`, or a second `EvaluatorUnavailableCommand` call for the other four) -- no disposition leaves SessionState parked in `PhaseEvaluating` with zero O1-recorded trace;
- the old defective behavior is restored in each negative control and the new test fails for the exact claimed reason.

## Bounded checkpoint sequence

### Checkpoint 1: architecture contract

This document only. No implementation.

Contract review must close:

- exact O3-to-O4 generation handoff shape;
- existing owner mapping for each initial evaluator adapter;
- evaluation-policy authority and initial recommendation rules;
- O4 disposition/evidence-presence matrix.

### Checkpoint 2: closed data model

Add only:

- evaluation policy;
- evaluator descriptor/input/result;
- O4 receipt;
- schemas, Go types, normalization, semantic digests, validators, fixtures, and adversarial schema tests.

No process execution, candidate materialization, O2 call, or O1 transition wiring.

### Checkpoint 3: exact generation handoff

Add:

- durable exact O2 value handoff from O3;
- cross-digest validation;
- candidate artifact loading and Attempt binding;
- `MapToCommand` plus first O1 transition into `PhaseEvaluating`.

No evaluator execution yet.

### Checkpoint 4: evaluator surfaces and mechanical ports

Add:

- fresh per-evaluator materialization;
- revocation and cleanup lifecycle;
- evaluator interface and deterministic test doubles;
- initial mapped Sensei/mechanical evaluator adapters approved in contract review.

No final composition or O1 evaluation transition yet.

### Checkpoint 5: deterministic composition

Add:

- required/optional handling;
- limitation preservation;
- failure classification and recommendation policy;
- final O1 Evaluation construction;
- O4 receipt;
- second O1 transition;
- complete positive and negative end-to-end matrix.

Stop after returning O1 state/events.

## Forbidden scope

O4 must not add:

- O5 admission or verification bridging;
- O6 generation-provider adapters;
- provider selection or routing policy;
- automatic retry or replan execution;
- autonomous session loops;
- repository mutation or patch application;
- GitHub writes;
- merge, completion, correctness, or approval claims;
- a second O1 Evaluation schema;
- a parallel proof, audit, incident, task, workspace, admission, or completion owner;
- direct trust in a provider-produced Recommendation;
- evaluation against a live checkout or unsealed candidate;
- shared mutable evaluator workspaces;
- hidden evaluator selection or hidden policy extension.

## Acceptance criteria

O4 is complete only when:

- every new schema is closed and validated with the repository's real Draft 2020-12 path;
- all declared digests are independently recomputed at every acceptance boundary;
- the exact O3 generation result reaches O1 without reconstruction;
- the exact sealed candidate is the sole evaluation input;
- all required evaluators and proof obligations have explicit, testable availability semantics;
- recommendation composition is deterministic and provider-independent;
- every limitation remains attributable after composition;
- O1 alone changes retry/replan budgets and terminal state;
- all positive and adversarial proofs pass, including restored-bug negative controls;
- race tests prove evaluator isolation, revocation, and single terminal receipt production;
- repository build, vet, tests, freshness, generated-artifact checks, and Sensei dogfood are green;
- no forbidden O5/O6/autonomous/mutation scope appears;
- the exact implementation head receives architect acceptance.

## Implementer protocol

This document is the contract. Implementation must not begin until checkpoint 1 is reviewed and accepted.

Before editing implementation files, the implementer must use Sensei briefing/preflight and map the exact existing owners for:

- edit/diff audit;
- incident/scar matching;
- proof-obligation verification;
- mechanical command execution and evidence capture;
- policy/configuration authority;
- candidate artifact storage;
- O1 state and O2 execution evidence.

When an owner or handoff is unclear, post `ARCHITECT QUESTION` and stop that affected slice. Do not invent a parallel owner or weaken identity binding to continue.

Finish each checkpoint by posting `IMPLEMENTATION READY FOR ARCHITECT REVIEW` bound to the exact head SHA, including schema/fixture digests, negative-control evidence, race evidence where applicable, full verification commands, and explicit confirmation that O5 admission, O6 adapters, autonomous continuation, repository mutation, and GitHub writes remain absent.
