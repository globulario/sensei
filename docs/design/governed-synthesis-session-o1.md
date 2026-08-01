# Governed Synthesis Session O1

Status: architecture contract

## Purpose

Introduce the first bounded, deterministic orchestration layer for governed software evolution.

This work borrows the useful pattern demonstrated by ARCHER: separate interpretation, planning, generation, evaluation, and bounded retry. Sensei must adapt that pattern to its stronger authority model rather than copy a regulation-to-Python pipeline.

The result of O1 is not an autonomous coding agent. It is a canonical, revision-bound session contract and deterministic state machine that can coordinate existing or future agents without granting them mutation, completion, promotion, merge, or architectural authority.

## Language and process decision

The canonical orchestration core MUST be implemented in Go.

Reasons:

1. Sensei's current authority, admission, investigation, closure, workspace, CLI, MCP, appliance, and GitHub surfaces are Go-owned.
2. A Python core would create a second runtime, dependency graph, packaging path, error model, and security surface.
3. The useful idea is deterministic orchestration, not Python itself.
4. Go interfaces and generated protobuf types provide the required typed boundary.
5. The existing appliance and single-binary distribution remain materially simpler when orchestration stays in Go.

Python MAY later be supported as an out-of-process provider implementation behind the same versioned gRPC provider contract. It MUST NOT become the canonical session owner, state store, evaluator, admission owner, or receipt producer.

## Architectural position

The new layer sits after task-scoped architectural closure and before any governed mutation attempt:

```text
repository identity
  -> task identity
  -> architectural closure / briefing / proof obligations
  -> governed synthesis session
       -> interpretation snapshot
       -> plan candidate
       -> implementation candidate
       -> evaluation observations
       -> bounded retry or terminal disposition
  -> existing admission / verification owners
  -> existing completion truth
```

The session coordinates work. It does not replace closure, admission, verification, completion, or promotion.

## Hard laws

1. Intelligence may explore; authority remains deterministic.
2. No provider may mutate repository state through the session contract.
3. Every session is bound to exact repository, base revision, task, graph authority, and closure identity.
4. Every attempt is immutable and monotonically appended.
5. Retry budgets are explicit before execution and cannot be enlarged by a provider.
6. Replanning is distinct from regeneration and has an independent budget.
7. A passing evaluator is evidence, not admission, correctness, completion, approval, or merge authorization.
8. Existing admission and verification owners are delegated to, never reimplemented.
9. Provider output is untrusted input and must be closed-schema validated before use.
10. Terminal failure preserves all prior evidence and produces an inspectable receipt.
11. Session resumption must reject identity drift, graph drift, task drift, closure drift, and base revision drift.
12. Deterministic replay covers state transitions and digest computation, not nondeterministic model text generation.

## O1 scope

O1 establishes contracts and the pure orchestration state machine only.

### Required package

Create a focused Go package, recommended location:

`golang/architecture/synthesis`

The package owns:

- closed typed session documents;
- canonical normalization and semantic digest support;
- identity and cross-document validation;
- a pure deterministic transition function;
- retry and replan budget accounting;
- terminal disposition rules;
- deterministic fixtures and adversarial tests.

It must not invoke a model, run a process, edit a worktree, call GitHub, or perform admission.

### Required schema family

Define closed Draft 2020-12 schemas and generated/hand-maintained Go types for at least:

1. `sensei.synthesis.session.v1`
2. `sensei.synthesis.interpretation.v1`
3. `sensei.synthesis.plan.v1`
4. `sensei.synthesis.attempt.v1`
5. `sensei.synthesis.evaluation.v1`
6. `sensei.synthesis.receipt.v1`

The exact split may be adjusted only if the same authority and identity properties remain explicit and independently testable.

### Session identity

The session document must bind, directly or through exact digests, all of:

- repository domain;
- repository root or workspace identity through the existing canonical owner;
- base commit SHA;
- task identity and task-session identity;
- graph authority / graph marker identity;
- architectural closure or briefing artifact identity;
- proof-obligation identity;
- requested objective;
- initial retry budget;
- initial replan budget;
- provider-independent session ID;
- schema version.

Do not invent parallel repository, workspace, task, graph, closure, or admission semantics. Reuse existing typed owners and project their exact identity into this contract.

### Interpretation artifact

The interpretation artifact is Sensei's stronger equivalent of ARCHER's rule-interpretation document.

It must be derived from existing governed inputs and explicitly distinguish:

- objective;
- applicable intent;
- binding invariants;
- relevant contracts and authority boundaries;
- known failure modes and forbidden fixes;
- required proof obligations;
- assumptions;
- unresolved questions;
- limitations;
- source references and source digests.

Interpretation is evidence supplied to planning. It is not new canonical architectural truth and cannot promote candidates.

### Plan artifact

A plan is a provider proposal bound to one interpretation and one session generation.

It must include:

- ordered steps with stable IDs;
- intended file or symbol scope when known;
- expected evidence per step;
- explicit assumptions and risks;
- stop conditions;
- provider identity as an observation, not session authority;
- parent interpretation digest;
- plan generation number;
- semantic digest.

A replan creates a new immutable plan generation. It never rewrites the prior plan.

### Attempt artifact

Each attempt must include:

- attempt number;
- plan generation;
- parent plan digest;
- exact input candidate identity;
- provider observation identity;
- proposed patch or change-envelope digest, not an implicitly trusted mutation;
- produced evidence references;
- timestamps as excluded-from-semantic-digest observation fields where appropriate;
- terminal provider status;
- no admission or correctness claim.

### Evaluation artifact

Evaluation must represent observations from one or more evaluators without collapsing them into authority.

It must include:

- attempt identity;
- evaluator kind and version;
- checks performed;
- passed, failed, skipped, and unavailable observations;
- evidence references;
- classified failure reasons;
- recommendation: accept-candidate, retry-generation, replan, architect-review, or abort;
- limitations;
- semantic digest.

The pure state machine, not the evaluator, decides whether a recommendation is permitted by remaining budgets and session state.

### Receipt

A terminal receipt must state exactly why the session stopped:

- candidate-ready-for-admission;
- retry-budget-exhausted;
- replan-budget-exhausted;
- architect-review-required;
- identity-drift-refused;
- invalid-provider-output;
- evaluator-unavailable;
- explicitly-aborted;
- other closed enumerated reasons justified by the implementation.

`candidate-ready-for-admission` means only that a candidate may be submitted to the existing admission owner. It must not claim accepted, correct, complete, approved, mergeable, or merged.

## Deterministic state machine

Implement a pure transition API conceptually equivalent to:

```go
type Command interface{ synthesisCommand() }
type Event interface{ synthesisEvent() }

func Transition(state SessionState, command Command) (SessionState, []Event, error)
```

The exact API may follow repository conventions, but tests must prove:

- the same state plus same command produces byte-for-byte equivalent normalized state/events;
- illegal transitions fail closed;
- attempt numbers and plan generations are monotonic;
- budgets cannot underflow or increase;
- retry cannot occur without a classified retryable evaluation;
- replan cannot occur without a classified replan evaluation;
- candidate-ready cannot occur without a valid attempt and evaluation;
- terminal sessions cannot accept further commands;
- resumption rejects all bound-identity drift;
- malformed or unknown enum/schema fields are rejected;
- timestamps and provider prose cannot influence authoritative transition identity unless explicitly included by contract.

## Initial budget policy

O1 must provide explicit configurable budgets with conservative defaults, but policy ownership must be separated from mechanics.

Recommended initial defaults for fixtures and CLI prototypes only:

- generation retries: 3;
- replans: 1;
- total attempts: derived and hard-capped;
- no hidden automatic extension.

These values are not permanent product law. The contract requirement is that budgets are explicit, immutable for a session, and enforced deterministically.

## Required fixtures

Provide positive and adversarial fixtures including:

- happy path: interpretation -> plan -> attempt -> evaluation -> candidate-ready receipt;
- retry then success;
- replan then success;
- retry exhaustion;
- replan exhaustion;
- invalid provider output;
- graph identity drift on resume;
- base revision drift on resume;
- task or task-session drift on resume;
- closure digest drift on resume;
- attempt referencing the wrong plan generation;
- evaluation referencing the wrong attempt;
- provider trying to enlarge a budget;
- evaluator claiming correctness or admission;
- command submitted after terminal receipt.

Every committed fixture must pass real JSON Schema validation and package-level semantic validation.

## Awareness and documentation

Add governed awareness records for at least:

- `synthesis.intelligence_may_explore_but_authority_is_deterministic`;
- `synthesis.provider_output_is_untrusted_and_never_directly_mutates`;
- `synthesis.retry_and_replan_budgets_are_precommitted`;
- `synthesis.passing_evaluation_is_not_admission_or_completion`;
- failure mode for unbounded autonomous retry;
- forbidden fix for embedding provider-specific orchestration into the authority owner;
- required tests proving the transition and drift-refusal laws.

Refresh generated awareness and import-graph artifacts through existing canonical commands only.

## Forbidden scope

O1 must not add:

- model SDK dependencies;
- Python runtime dependencies;
- provider authentication;
- Claude, Codex, OpenAI, Gemini, or local-model adapters;
- gRPC/protobuf provider service;
- process or PTY execution;
- worktree creation;
- patch application;
- repository mutation;
- admission policy changes;
- completion policy changes;
- GitHub writes;
- Dashboard UI;
- automatic promotion of interpretation, plan, or evaluation content;
- claims that evaluation success proves correctness.

## Follow-on PR sequence

After O1 is merged and pinned, proceed through separate bounded PRs:

### O2: provider-neutral execution port

Add a versioned Go interface and optional gRPC contract for out-of-process providers. Define capability discovery, request/response envelopes, cancellation, deadlines, streaming observations, and provider receipts. No repository mutation.

### O3: governed runner composition

Compose the session state machine with the existing workspace/runner contracts. Execute providers in isolated worktrees or candidate buffers, capture exact patch digests and evidence, and preserve the rule that providers never own repository truth.

### O4: evaluator composition

Compose mechanical tests, Sensei edit/diff audit, incident matching, proof-obligation checks, and external evaluators into the evaluation artifact. Keep each evaluator's limitations visible.

### O5: admission bridge

Submit a candidate-ready receipt and exact candidate identity to the existing admission and verification owners. Produce a composed receipt without changing their semantics.

### O6: first provider adapters

Add provider adapters one at a time. Prefer in-process Go adapters where an SDK is stable and appropriate; use the gRPC provider port for Python or other external implementations. Provider choice must not alter session authority semantics.

## Acceptance criteria

The implementation is complete only when:

- all schemas are closed and validated by a real Draft 2020-12 validator;
- Go types, normalization, digest, and validation are deterministic;
- the transition engine is pure and race-free;
- all positive and adversarial fixtures pass;
- full repository build, vet, tests, freshness, generated-artifact, and Sensei dogfood checks pass;
- no forbidden runtime/provider/mutation scope appears;
- the exact implementation head receives an architect-review handoff with schema and fixture digests.

## Implementer protocol

Claude must use the repository's canonical Sensei briefing, preflight, task, admission, and verification workflow before editing governed files.

Claude must first map the existing typed owners for workspace identity, task identity, graph authority, architectural closure, proof obligations, admission, verification, and completion. It must compose those owners rather than approximating them.

When an identity field or authority mapping is unclear, post `ARCHITECT QUESTION` and stop that affected portion. Do not invent a parallel owner or weaken closedness to make progress.

Finish by posting `IMPLEMENTATION READY FOR ARCHITECT REVIEW` bound to the exact head SHA, including all schema digests, representative fixture digests, transition-law test evidence, full verification commands, and explicit confirmation that no provider execution or repository mutation was added.
