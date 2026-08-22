# Real model-provider closure and evaluation handoff (#256 -> #131)

Status: implementation relay contract

Refs #256 and #131.

This document begins from the code merged by PR #257. It does **not** reopen the model-execution architecture that #257 proved. It names the remaining capability boundary, then defines the one-way handoff into evaluation.

The ordering is normative:

```text
#256 production capability
  -> real provider adapter + independent proof
  -> optional live model acquisition
  -> frozen content-addressed artifact
#131 evaluation
  -> deterministic scoring of that artifact
  -> independent human reference set
```

The benchmark must never create, weaken, or redefine the production capability it measures.

## Status of this relay

This section records the decision actually taken during implementation, so the
document and the code do not disagree.

```text
Stage A implementation and hermetic proof   complete
Stage A successful live-model witness       outstanding
                                            blocks #256 closure
                                            does NOT block Stage B implementation
Stage B implementation                      complete
Real model measurement in #131              blocked until the live witness succeeds
```

The live witness was attempted and failed for a reason outside the
architecture: the adapter reached the real model API over the network and the
service answered `HTTP 400: credit balance too low`. That was recorded as a
typed `errored` outcome rather than smoothed over, which is the behaviour the
contract wanted — but it is not a model producing an artifact, so #256 stays
open.

Stage B was then explicitly authorised to proceed. The refinement matters and
is deliberate: the ordering rule exists so that **the benchmark cannot create
the capability it measures**. That risk is absent here, because the capability
is real, hermetically proven, and reachable in production code; only an
external billing condition blocks one supplementary proof. An external service
must not get to decide whether an evaluator may be built, and it must equally
not get to decide that a proof step was optional. So both halves are kept:
Stage B proceeded, and #256 did not close.

Sections B and D below are the ORIGINAL contract text and are left unedited on
purpose. Where they say Stage B follows a fully green Stage A, read them
against this status block.

## Current state after #257

PR #257 established and mutation-tested the hard part of #256:

- `modelexec.Config` cannot configure a terminal status;
- `modelexec.Execute` constructs terminal status from observed execution;
- `resolved` requires provider/model/request/artifact/nondeterminism identity;
- unavailable/refused/errored/invalid/resolved are machine-distinct;
- request identity covers the evidence excerpt bytes actually supplied;
- returned artifacts cannot cite unsupplied evidence or attribute claims to files they were not shown;
- authority-shaped model output is rejected;
- binding/receipt agreement covers the duplicated model identity;
- deterministic HOW and WHY entry points remain byte-compatible with pinned pre-model documents;
- orchestration exposes `OrchestrateWithModel` while the deterministic entry point remains model-free;
- the proof matrix is hermetic and independent of `cmd/eval-arms`.

That is a real execution **owner and port**. One completion item from the design contract remains explicit: wire one real provider adapter last.

The repository must not close #256 merely because a fake provider can prove the port. The fake proves the authority contract. A real adapter proves the capability can cross the process/network boundary it was designed to govern.

## A. Close #256 with one real provider adapter

### Recommended shape: direct-argv JSON stdio adapter

Prefer a vendor-neutral adapter owned by `golang/architecture/modelexec` (or a small sibling package if import direction requires it):

```text
Sensei modelexec.Request
        |
        | canonical request envelope on stdin
        v
explicit executable + argv
        |
        | structured model artifact on stdout
        v
Sensei Artifact parser/validator
        |
        v
existing modelexec.Execute outcome construction
```

The adapter is transport. It does **not** own Sensei status semantics and cannot declare `resolved`.

A command adapter is recommended over a vendor SDK for the first implementation because it:

- keeps Sensei free of provider-specific SDK/auth dependencies;
- can front ChatGPT, Claude, a local model, or a future provider through a tiny bridge;
- is hermetically testable with a fixture executable;
- can be smoke-tested against a real model without making CI depend on credentials;
- preserves explicit provider selection rather than ambient credential discovery.

If Claude finds an existing provider abstraction that already satisfies these properties, reuse it rather than creating a parallel port.

### Required command-provider contract

The adapter must:

1. receive an explicit provider ID and provider version;
2. receive a direct executable path/name and argv, with **no shell interpolation**;
3. run under the caller context and honor cancellation/timeouts;
4. serialize exactly one closed request envelope to stdin;
5. parse exactly one closed artifact envelope from stdout;
6. keep stderr diagnostic-only and out of semantic artifact identity;
7. distinguish provider refusal from transport/execution error without prose matching;
8. never discover a provider/model merely because credentials exist in the environment;
9. never write status fields into `investigation.ModelBinding` itself;
10. never expose repository roaming or an implicit tool surface beyond the bounded request.

The provider ID/version are semantic assertions and must be non-empty. Do not infer a trustworthy provider version from an executable filename.

### Exact-request rule

The adapter must not introduce any material shown to the model that is absent from `modelexec.Request` identity.

This is especially important for prompt construction.

If the adapter or bridge expands a prompt template, the exact template/contract bytes must be pinned by the request's `PromptContractDigest` (or a stronger exact-content binding if implementation review shows the existing digest is insufficient). A provider adapter must not prepend hidden repository text, system hints, file contents, current time, or other semantic material outside the request identity.

The #257 review already proved the governing principle: a caller-supplied digest is a claim about content, not the content. The real adapter must not reintroduce the same bug one layer later.

### Wire schema

Do not casually add JSON tags or cosmetic fields to `investigation.ModelBinding`, `Binding`, or `RunReceipt` while implementing the adapter.

PR #257 proved that serialized compatibility is load-bearing:

- a cosmetic `reason` on disabled changed every deterministic document digest;
- a nested struct with `omitempty` still serialized empty members because `encoding/json` does not omit non-pointer structs.

Any persisted schema change requires a pinned pre-change fixture and an explicit compatibility decision. Prefer a private adapter wire type over changing an existing persisted type merely to make stdin prettier.

Add this to the review-traps list in the existing #256 design document:

> A cosmetic field on a persisted serialized type is a semantic change if it changes bytes or validation of historical documents. `omitempty` must be proven on the concrete Go type, not assumed from the tag.

### #256 closure evidence

Do not close #256 until all of these are true:

- existing 12-row proof matrix remains green;
- a command/real adapter has hermetic tests for direct argv, cancellation, malformed stdout, refusal, and transport error;
- a fixture adapter invocation reaches `modelexec.Execute` and earns `resolved` only from a validated artifact;
- a **supplementary real-provider smoke** has invoked one actual model through the adapter and recorded the resulting terminal binding and artifact digest;
- the real smoke does not need to run in CI and must not put credentials into receipts or committed artifacts;
- deterministic disabled/not-requested fixtures remain byte-pinned;
- repository gates remain green.

The real smoke proves reachability, not model correctness. Correctness belongs to #131.

## B. Handoff into #131 only after A is proven

> Refined during implementation — see "Status of this relay" above. Stage A's
> implementation and hermetic proof are what gate Stage B; the supplementary
> live-model witness gates #256's CLOSURE. The two were separated once the
> witness turned out to be blocked on billing rather than on engineering.

Once the production adapter exists and the independent proof above is green, `cmd/eval-arms` may stop reporting `not_implemented_in_evaluated_path`.

Do not replace that string merely because #257 merged. The evaluated path must itself know how to request the now-existing capability.

### Model arm configuration

The model arm should accept an explicit provider configuration sufficient to construct the production adapter. Exact flag names are implementation details, but the semantic inputs are:

- provider ID;
- provider version;
- model name;
- model digest, or the typed statement that the provider exposes none;
- executable + argv / adapter endpoint configuration;
- prompt-contract identity;
- output schema version;
- tool policy;
- bounded target/evidence selection.

No flag may set `resolved`, artifact digest, request digest, or another terminal outcome. Those remain execution evidence.

When no model provider is configured, arm 3 is **implemented but not run**. Record `not_run` with a reason such as `optional model capability is available; this run did not bind a provider`. Do not preserve `not_implemented_in_evaluated_path` after the evaluated path can actually invoke the capability.

When a provider is requested, preserve its `modelexec` terminal status in the arm report. A model refusal or error is an evaluation result, not a silent deterministic fallback.

### Acquisition is not scoring

A live model run is nondeterministic acquisition. The benchmark must not pretend otherwise.

Split arm 3 conceptually into two artifacts:

```text
LIVE ACQUISITION
  exact deterministic baseline binding
  exact model request digest
  provider/model identity
  terminal model binding
  accepted artifact digest
  captured model artifact
          |
          v
FROZEN SCORING INPUT
  immutable content-addressed acquisition bundle
          |
          v
DETERMINISTIC SCORING
  compare with reference set
  compute model delta
```

A repeated live model call may produce a different artifact. That is not replay failure if nondeterminism was declared honestly.

What must replay byte-identically is the scorer over the same frozen acquisition bundle and frozen reference set.

This lets exact-head CI verify evaluation logic without credentials while preserving the fact that the original model acquisition was nondeterministic.

### Do not mix model output into deterministic truth

Arm 3 must report the deterministic baseline and model-derived additions separately.

At minimum report:

- deterministic observation/candidate counts and digests;
- model terminal status;
- model request/artifact identities;
- accepted model-derived item counts by kind;
- grounding/invalid/refusal/error outcomes;
- score of deterministic lane against the frozen reference set;
- score of model-derived additions against the same set;
- combined operator-visible delta as a derived comparison only.

Never rewrite the deterministic document with model output and then score the merged object as though every item had the same provenance.

## C. Freeze the reference protocol before answers

The human reference protocol is defined separately in:

`docs/evaluation/phase10-reference-protocol-v1.md`

That document intentionally contains no labels, expected facts, adjudicated answers, or benchmark results.

The protocol must merge **before**:

- selecting or revealing the final adjudication sample;
- writing human labels;
- interpreting scores;
- changing production behavior because of a score.

This is the externality boundary Claude called out correctly: the system may implement the protocol, but it must not author its own answer key after seeing its output.

## D. Implementation order for Claude

Use this order. Do not collapse it for convenience.

1. Add the real provider adapter and adapter-only tests.
2. Re-run the existing #256 proof matrix and the pinned pre-model serialization fixture.
3. Perform one supplementary real-model smoke and record only non-secret binding/digest evidence.
4. At that point, update #256 with completion evidence and close it.
   **Not yet done.** The live witness has not succeeded, so #256 remains open;
   steps 5-7 proceeded under the refinement recorded in "Status of this relay".
5. Add arm-3 provider configuration and call the **production** `OrchestrateWithModel` / `modelexec` path. Do not duplicate model execution in the evaluator.
6. Add live-acquisition bundle serialization and deterministic frozen-bundle scoring.
7. Change arm 3 from `not_implemented_in_evaluated_path` to `not_run` only when the harness really has a reachable provider path.
8. Merge the reference protocol before generating any human answer files.
9. Generate the frozen sample manifest deterministically from pinned worlds.
10. Human adjudication happens outside the scoring implementation.
11. Add scoring code only after labels are frozen.
12. Run the exact-head protocol and report failures as results, not as reasons to alter truth.

## E. Required tests added by this PR's implementation

### Provider adapter

- direct argv, no shell interpretation;
- provider identity/version required;
- context cancellation terminates invocation;
- malformed stdout -> invalid/error as appropriate, never resolved;
- explicit provider refusal stays refusal;
- process/transport failure stays errored;
- stderr cannot alter artifact digest;
- hidden/unbound prompt material cannot be introduced;
- fixture success earns resolved only through existing `modelexec.Execute` validation;
- disabled and not-requested never launch a process.

### Evaluation arm

- no configured model -> implemented `not_run`, not `not_implemented`;
- configured fake/fixture provider -> production model path invoked exactly once;
- model outcome is copied into evaluation report without reinterpretation;
- deterministic baseline digest is identical whether arm 3 is configured or absent;
- acquisition bundle content-addresses deterministic baseline + request + binding + artifact;
- scorer over a frozen acquisition bundle is byte-identical on replay;
- a live re-acquisition with a different model artifact receives a different acquisition identity, not a false replay failure;
- evaluation code cannot configure terminal model status;
- no model-derived item enters canonical awareness or deterministic numerator.

### Mutation expectation

At least these guards must have tests that fail when deliberately neutered:

- process launch on disabled/not-requested;
- shell-free argv boundary;
- request/prompt identity coverage;
- artifact grounding/scope validation;
- terminal status ownership;
- deterministic byte fixture;
- acquisition-bundle identity;
- frozen scorer replay identity.

## F. Completion truth

### #256

#256 is complete when a real explicitly bound provider can cross the adapter boundary into the already-proven execution owner, `resolved` remains earned from validated execution, every failure is typed, deterministic output remains byte-compatible, and one real-model smoke proves the capability is not only a fake-provider abstraction.

### #131

This PR does **not** by itself close #131.

It removes the last optional-model plumbing gap and freezes the independent grading protocol. #131 closes only after human reference sets are created under that protocol, scores are computed over all required pinned worlds, failures remain visible, and one exact-head evaluation proves the complete protocol.
