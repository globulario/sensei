# Provider-Neutral Execution Port O2

Status: architecture contract

## Purpose

Define how the deterministic O1 session owner (`golang/architecture/synthesis`) requests intelligence from interchangeable providers without granting providers repository, session, evaluation, admission, or completion authority.

O1 defined the canonical, revision-bound session contract and the pure deterministic state machine that decides what happens next. O1 does not know how to reach a provider, and it must not learn how — the state machine stays a pure function of state and command regardless of which provider, if any, produced the artifact carried on that command. O2 defines the boundary a provider crosses to reach O1: how a request leaves the session owner, how a response — or a typed failure — comes back, and how that response is proven untrusted until an existing O1 artifact accepts it.

## Architectural position

O2 sits between the governed synthesis session and any intelligence provider:

```text
governed synthesis session (O1)
  -> provider-neutral execution port (O2)
       -> capability discovery (untrusted self-description)
       -> one capability-driven Provider.Execute(Request, Observer) call
       -> [ interchangeable provider, out of process or in process ]
       -> Result (completed | typed terminal outcome)
       -> provider execution receipt (request/capability/response/observation digests)
  -> pure capability-specific mapper (validate envelope + declared/computed digest)
  -> candidate O1 artifact + O1 command
  -> synthesis.Transition decides acceptance (O1, unchanged)
```

O2 composes with O1; it does not replace or duplicate the FSM, the closed document schemas, the digest scheme, or the terminal-disposition rules O1 already owns. Every provider result remains a plain, untrusted value — a distinct Go type from any O1 artifact, never a type alias or embedding of one — until it is mapped into a closed O1 artifact (Interpretation, Plan, Attempt, or Evaluation) and accepted by `synthesis.Transition`. Mapping failure is an O2 rejection, not an O1 transition.

## Hard laws

1. Provider proposes; O2 validates and receipts; O1 remains the only session-transition authority.
2. Capability claim is not authority grant. A capability document is provider self-description and therefore untrusted observation — it proves only that a provider *claims* support. It grants nothing beyond eligibility to be asked; it never grants mutation, admission, completion, transition, or routing authority. Actual eligibility to invoke a provider belongs to the caller/configuration layer, which stays out of O2's scope.
3. Provider response is not accepted Interpretation, Plan, Attempt, or Evaluation. A response only becomes one of those closed O1 artifacts through an explicit, pure, validated mapping step — never by construction, never implicitly.
4. Every request binds exact O1 session identity, the exact parent artifact digest, and the expected plan generation or attempt number. A provider must not choose or alter plan generation, attempt number, retry budget, replan budget, or session identity — those stay O1-owned inputs the request merely carries forward.
5. Declared digest must equal recomputed digest for every O2 governed document — capability snapshot, request, response/result, observation aggregate, and receipt — before acceptance, the same integrity law O1 already enforces (`invariant.synthesis.declared_digest_must_equal_computed_content_digest`) applied across the O2 boundary. The provider must never produce an authoritative digest unchecked; O2 recomputes and rejects on mismatch.

Every provider result must remain untrusted until converted into a closed O1 artifact and accepted by `synthesis.Transition`.

## O2 scope

### In scope

- **One capability-driven provider port**, not four authority-shaped provider interfaces. A single provider-neutral execution interface, conceptually:

  ```go
  type Provider interface {
      Describe(ctx context.Context) (Capabilities, error)
      Execute(ctx context.Context, Request, Observer) (Result, error)
  }
  ```

  `Request.Operation` is a closed capability enum (`interpretation`, `planning`, `generation`, `evaluation-observation`). Capability-specific payloads are closed/versioned envelopes carried on the shared `Request`/`Result` shape. This keeps cancellation, deadlines, observations, receipts, identity, and failure semantics single-owned instead of duplicated across four interface families.
- Closed request and response/result envelopes, each binding at minimum: synthesis session digest; repository/base revision identity already carried by the O1 session; operation/capability; exact parent artifact digest; expected plan generation or attempt number where applicable; request ID and schema version; precommitted deadline/observation limits; semantic request digest.
- Capability discovery, explicitly modeled as untrusted provider self-description (see hard law 2) — never eligibility, never authority, never a routing input O2 itself acts on.
- Provider identity as observation only — never as an authority or routing key the port itself acts on.
- Deadlines, cancellation, and bounded streaming observations with **precommitted count and byte limits**, fixed before execution and never enlargeable by the provider. Observations carry a monotonic sequence number; duplicates and gaps are handled explicitly. Observations are evidence attached to the final `Result`/receipt — they never drive an O1 transition directly.
- A closed terminal-outcome model as data, not ambiguous Go errors, for every *expected* provider outcome:
  - completed;
  - unavailable;
  - timed-out;
  - cancelled;
  - invalid-output;
  - unsupported-capability.

  A non-nil Go `error` is reserved for local contract/programming/infrastructure failures where no valid O2 result or receipt could be constructed at all. Every normal terminal provider outcome — including every typed failure above — produces an inspectable execution receipt.
- Provider execution receipts binding: request digest; capability snapshot digest used for eligibility checking; provider observation identity; terminal outcome; response digest when a response exists, otherwise null; ordered observation-stream digest or observation-set digest; started/completed timestamps as observation fields excluded from semantic identity where specified; limitations; receipt digest (O2-recomputed and verified, never provider-declared-and-trusted).
- A raw-result/O1-artifact separation with an explicit, pure mapping pipeline (see the architectural-position diagram): validate O2 envelope and declared/computed digest, run a pure capability-specific mapper, produce a candidate O1 artifact plus O1 command, and let `synthesis.Transition` decide acceptance. O2 never constructs an already-accepted O1 artifact.
- Deterministic mapping from provider outcomes into existing O1 artifacts and commands: the same validated request/result inputs always produce the same mapped O1 artifact and command.
- Test doubles and adversarial contract tests, including the required proof additions below.
- Optional protobuf/gRPC contract definitions, deferred: implement and stabilize the canonical Go contract first. Add protobuf/gRPC only if that Go contract is stable and an out-of-process boundary needs mechanical parity proof in this same PR — it must remain transport-neutral and contain no implementation adapter, and it must not enlarge O2 scope or delay closure.

### Explicitly out of scope

- Claude, Codex, OpenAI, Gemini, or local-model adapters.
- SDK dependencies or authentication.
- Prompt templates tied to one provider.
- Provider selection/routing policy.
- Worktrees, processes, PTYs, patch application, or repository mutation.
- Evaluation composition.
- Admission or completion bridging.
- Autonomous session-driving loops.

## Acceptance criteria

The implementation is complete only when:

- all request/response envelope schemas are closed and validated the same way O1's document schemas are (real Draft 2020-12 validation, no free-form fields);
- every provider outcome — completed, each typed terminal outcome, and cancellation — has an explicit, tested mapping into an O1 command or a rejection, with no path that lets provider output reach `synthesis.Transition` unmapped and unvalidated;
- capability discovery cannot be mistaken for or substituted for eligibility, authority, or routing anywhere in the type system or the tests;
- provider execution receipts carry exact request/capability/response/observation digests, computed the same way O1's `closureprotocol.SemanticDigest`-based digest scheme works, not a new scheme, and O2 rejects any declared receipt digest it did not itself recompute;
- test doubles and adversarial contract tests cover every typed terminal outcome and at least one capability-mismatch case;
- full repository build, vet, tests, freshness, generated-artifact, and Sensei dogfood checks pass;
- no forbidden scope (adapters, SDK dependencies, authentication, routing policy, mutation, evaluation composition, admission/completion bridging, autonomous loops) appears;
- the exact implementation head receives an architect-review handoff with schema and fixture digests.

### Required proof additions

Adversarial contract tests must additionally demonstrate:

- **digest integrity**: declared digest equals recomputed digest for capability, request, response/result, observations aggregate, and receipt — a mismatch on any one of these is rejected before the artifact is trusted anywhere downstream;
- **stale identity**: a request or result bound to a stale/wrong synthesis session digest, repository identity, or base revision is rejected before mapping;
- **wrong generation / wrong attempt**: a result referencing a plan generation or attempt number other than the one the session currently expects is rejected before `Transition` ever sees it;
- **race conditions**: cancellation and deadline races produce exactly one terminal outcome and exactly one receipt — never zero, never two;
- **budget smuggling**: no field on any provider request, result, or receipt can enlarge, reset, or otherwise influence `RetryBudget`/`ReplanBudget`/`RemainingRetryBudget`/`RemainingReplanBudget`; observation limits themselves cannot be enlarged by the provider;
- **command smuggling**: no provider result can cause a command other than the one the mapping layer explicitly constructs to reach `synthesis.Transition` — a provider cannot smuggle an O1 command, an admission claim, a repository-mutation instruction, or an alternate session identity through any field;
- **determinism**: the same validated request/result inputs always produce the same mapped O1 artifact and command, byte-for-byte.

## Implementer protocol

This document is the contract. Implementation must not begin until it is committed to this PR and reviewed by the architect.

Claude must use the repository's canonical Sensei briefing, preflight, task, admission, and verification workflow before editing governed files, exactly as during O1.

Claude must first map the existing typed owners this port composes with — the O1 session, command, and event types in `golang/architecture/synthesis` — rather than approximating or duplicating them. When an interface boundary or mapping is unclear, post `ARCHITECT QUESTION` and stop that affected portion. Do not invent a parallel session owner or weaken closedness to make progress.

Finish each bounded step by posting `IMPLEMENTATION READY FOR ARCHITECT REVIEW` bound to the exact head SHA, including all schema digests, representative fixture digests, contract-test evidence, full verification commands, and explicit confirmation that no provider adapter, SDK dependency, or repository mutation was added.
