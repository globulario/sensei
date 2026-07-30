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
       -> capability discovery
       -> versioned request envelope
       -> [ interchangeable provider, out of process or in process ]
       -> versioned response envelope | typed provider failure
       -> provider execution receipt (request/response digests)
  -> deterministic mapping into O1 artifacts and commands
  -> synthesis.Transition (O1, unchanged)
```

O2 composes with O1; it does not replace or duplicate the FSM, the closed document schemas, the digest scheme, or the terminal-disposition rules O1 already owns. Every provider result remains a plain, untrusted value until it is mapped into a closed O1 artifact (Interpretation, Plan, Attempt, or Evaluation) and accepted by `synthesis.Transition`.

## Hard laws

1. Provider proposes; O2 validates and receipts; O1 remains the only session-transition authority.
2. Capability claim is not authority grant. A provider declaring support for a capability grants it nothing beyond eligibility to be asked; it never grants mutation, admission, completion, or transition authority.
3. Provider response is not accepted Interpretation, Plan, Attempt, or Evaluation. A response only becomes one of those closed O1 artifacts through an explicit, validated mapping step — never by construction, never implicitly.

Every provider result must remain untrusted until converted into a closed O1 artifact and accepted by `synthesis.Transition`.

## O2 scope

### In scope

- Versioned Go interfaces for:
  - interpretation;
  - planning;
  - generation;
  - optional provider-side evaluation observations.
- Closed request and response envelopes.
- Capability discovery.
- Provider identity as observation only — never as an authority or routing key the port itself acts on.
- Deadlines, cancellation, and bounded streaming observations.
- Typed provider failures:
  - unavailable;
  - timed out;
  - cancelled;
  - invalid output;
  - unsupported capability.
- Provider execution receipts containing exact request/response digests.
- Deterministic mapping from provider outcomes into existing O1 artifacts and commands.
- Test doubles and adversarial contract tests.
- Optional protobuf/gRPC contract definitions, only if they remain transport-neutral and contain no implementation adapter.

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
- every provider outcome — success, each typed failure, and cancellation — has an explicit, tested mapping into an O1 command or a rejection, with no path that lets provider output reach `synthesis.Transition` unmapped and unvalidated;
- capability discovery cannot be mistaken for or substituted for authority anywhere in the type system or the tests;
- provider execution receipts carry exact request/response digests, computed the same way O1's `closureprotocol.SemanticDigest`-based digest scheme works, not a new scheme;
- test doubles and adversarial contract tests cover every typed failure and at least one capability-mismatch case;
- full repository build, vet, tests, freshness, generated-artifact, and Sensei dogfood checks pass;
- no forbidden scope (adapters, SDK dependencies, authentication, routing policy, mutation, evaluation composition, admission/completion bridging, autonomous loops) appears;
- the exact implementation head receives an architect-review handoff with schema and fixture digests.

## Implementer protocol

This document is the contract. Implementation must not begin until it is committed to this PR and reviewed by the architect.

Claude must use the repository's canonical Sensei briefing, preflight, task, admission, and verification workflow before editing governed files, exactly as during O1.

Claude must first map the existing typed owners this port composes with — the O1 session, command, and event types in `golang/architecture/synthesis` — rather than approximating or duplicating them. When an interface boundary or mapping is unclear, post `ARCHITECT QUESTION` and stop that affected portion. Do not invent a parallel session owner or weaken closedness to make progress.

Finish each bounded step by posting `IMPLEMENTATION READY FOR ARCHITECT REVIEW` bound to the exact head SHA, including all schema digests, representative fixture digests, contract-test evidence, full verification commands, and explicit confirmation that no provider adapter, SDK dependency, or repository mutation was added.
