# Model-assisted investigation execution #256

Status: implementation relay contract

Refs #256. This document is intentionally not the implementation and must not be used as evidence that model-assisted investigation exists. Its purpose is to pin the existing seams, the authority boundary, the failure vocabulary, and the proof required before `ModelStatusResolved` can mean that a model genuinely ran.

## Purpose

Sensei already records a model status in investigation documents, but the evaluated execution path has no model provider. The current state is therefore a recorded capability slot, not a capability.

#256 adds one optional execution lane:

```text
pinned deterministic investigation inputs
        |
        +--> existing deterministic extraction / WHY evidence
        |       remains independently reproducible
        |
        +--> explicitly bound optional model execution
                |
                +--> typed execution outcome
                +--> content-addressed model artifact
                +--> derived evidence / candidate / challenge only
```

The model lane may improve investigation. It never acquires architectural authority.

The ordering from #256 remains normative:

```text
capability issue (#256)   builds the capability
#131 evaluation           measures the capability
reference set             supplies independent truth
benchmark                 never creates either one
```

Do not modify the #131 evaluation harness merely to make its optional-model arm report a number in this implementation PR.

## Source map on main

The implementation must start from these existing owners rather than inventing a parallel model contract.

| Source | Existing responsibility | #256 implication |
| --- | --- | --- |
| `golang/architecture/investigation/binding.go` | `ModelBinding` and the closed model-status vocabulary | extend this identity instead of creating a second model-binding type |
| `golang/architecture/investigation/validate.go` | fail-closed validation for model status and resolved identity | strengthen this before any executor can emit `resolved` |
| `golang/architecture/investigation/receipt.go` | run receipt, model artifact digest, nondeterminism declaration | execution evidence belongs here and must agree with the document binding |
| `golang/architecture/howextract/compose.go` | deterministic HOW extraction; currently records `ModelStatusDisabled` | keep this deterministic lane model-free |
| `golang/architecture/whyinvestigation/orchestrate.go` | captures immutable WHY provider snapshots, executes registered evidence providers, composes one document | this is the first concrete orchestration seam where an optional model can be added without teaching deterministic providers to call models |
| `golang/architecture/whyinvestigation/compose.go` | composes normalized WHY document and currently records `ModelStatusDisabled` in both binding and receipt | model outcome must be threaded explicitly into composition; `resolved` cannot be manufactured here from configuration |
| `golang/architecture/whyinvestigation/contracts.go` | deterministic evidence-only provider contract | do not silently turn an evidence provider into an LLM provider; the model lane has different nondeterminism and receipt obligations |
| `golang/architecture/investigator/*` | deterministic candidate/challenge composition | preserve its deterministic contract; model output may feed a separately identified derived lane, not rewrite deterministic composition as if it were the same source |

Two current facts matter to the design.

First, `investigation.ModelBinding` is currently only:

```go
type ModelBinding struct {
    Status            string
    ModelName         string
    ModelDigestSHA256 string
}
```

Second, `RunReceipt` already has `Model`, `ModelArtifactDigestSHA256`, and `NondeterminismDeclaration` fields. The binding is therefore too weak for #256 even though the receipt has places for some of the evidence. A real model execution must bind the provider and request identity, not only a model label.

## Authority law

The model is an evidence producer, never an authority producer.

A model result may become:

- derived evidence with explicit provenance;
- a candidate claim requiring the existing review/promotion path;
- a candidate question;
- a challenge or counterexample;
- a limitation or typed absence.

A model result must never directly:

- mutate authored awareness or canonical graph knowledge;
- promote a candidate;
- weaken or reinterpret an invariant;
- reinterpret an owner verdict;
- create admission permission;
- satisfy proof merely because the model said it was satisfied;
- erase contradictory deterministic evidence;
- replace deterministic extraction output.

A consumer must always be able to distinguish deterministic material from model-derived material.

## Model binding v2 requirements

Extend the existing `investigation.ModelBinding`. Do not add a second identity struct in another package.

The exact Go field names are an implementation detail, but a resolved binding must carry at least these semantic identities:

- status;
- provider ID;
- provider version;
- model ID/name;
- model identity digest when the provider exposes one, or an explicit typed absence if such a digest cannot exist for that provider;
- exact request digest;
- exact returned/normalized model-artifact digest;
- nondeterminism declaration.

The request digest must cover every semantic input the model was allowed to observe. At minimum this includes the exact repository binding, graph identity/status, policy/profile identity where applicable, deterministic investigation document/input digests, selected target/query, provider/model identity, and the prompt/schema/template contract used to obtain structured output.

Credentials, hostnames, temporary paths, wall-clock timings, and other non-semantic runtime details must not be smuggled into semantic identity unless changing them can change what the provider is asked to decide.

`Binding.Model` and `Receipt.Model` must agree. If `RunReceipt.ModelArtifactDigestSHA256` remains separate, it must equal the artifact identity represented by the resolved model binding. Two fields must not be allowed to tell two stories about the same execution.

### `resolved` is earned, not configured

`ModelStatusResolved` is legal only after all of these are true:

1. an explicitly requested provider was resolved;
2. the provider was actually invoked;
3. the exact request was content-addressed;
4. a response artifact was returned;
5. the artifact passed structural validation and grounding rules for the model lane;
6. the normalized artifact digest was computed;
7. provider, model, request, artifact, and nondeterminism identities were recorded.

A caller setting `status: resolved`, a CLI flag naming a model, or a configuration object carrying a model name is not evidence of execution. The execution owner must construct the terminal binding from observed execution outcome.

## Typed outcome vocabulary

The existing statuses are:

- `disabled`
- `not_requested`
- `unavailable`
- `resolved`
- `invalid`

#256 additionally requires refusal and execution failure not to collapse into another absence. Prefer extending the closed vocabulary with `refused` and `errored` unless a stronger compatibility reason is established in code review. If a different encoding is chosen, it must still be machine-distinguishable without parsing human prose.

Required semantics:

| Outcome | Provider call? | Meaning |
| --- | ---: | --- |
| `disabled` | 0 | model capability intentionally disabled |
| `not_requested` | 0 | no model execution requested |
| `unavailable` | 0 | execution requested but provider/model could not be resolved or reached before invocation |
| `refused` | normally 1 | provider was invoked but explicitly refused the request |
| `errored` | normally 1 | provider execution/transport failed after invocation began |
| `invalid` | 1 | an artifact returned but failed the model-artifact contract or grounding validation |
| `resolved` | exactly 1 | provider genuinely ran and produced one accepted, content-addressed artifact |

Every non-success state that needs explanation carries a typed reason/code. Human-readable text may accompany it, but callers must not infer state by matching error strings.

Do not silently fall back from a requested model execution to the deterministic-only path and then present the run as model-assisted. The deterministic result may still be returned, but the model lane must record its actual typed outcome.

## Provider execution seam

Use explicit dependency injection or an explicit registry keyed by provider ID. Do not use ambient global credentials/provider discovery as the semantic selection mechanism.

A minimal conceptual boundary is:

```go
// Names are illustrative, not a required API.
type ModelProvider interface {
    Identity() ModelProviderIdentity
    Execute(context.Context, ModelRequest) (ModelArtifact, error)
}
```

The executor, not the provider, owns the Sensei receipt semantics. A provider adapter may report a structured refusal or structured execution failure, but it cannot declare itself `resolved` in Sensei's record.

The first integration target should be the investigation orchestration layer, with `whyinvestigation.Orchestrate` the concrete seam to evaluate first. It already owns immutable capture, provider outcomes, and final document composition. Do not put network/model calls inside `howextract`'s deterministic extraction functions or inside the existing deterministic WHY evidence providers.

If import direction or reuse makes a small sibling package preferable, keep `investigation.ModelBinding` the canonical serialized contract and keep the orchestration dependency one-way. The package name is less important than preserving authority direction.

## Request construction

The executor must construct the model request from already captured, bounded inputs. The model must not roam the repository or network outside that request through an implicit tool surface.

A request should bind:

- schema/request version;
- repository domain and exact revision/tree identity;
- graph digest/status actually consulted;
- HOW document digest and, for WHY-assisted work, the WHY/evidence snapshot identity;
- exact target observation/evidence IDs or query identity;
- exact evidence excerpts/receipts supplied to the model;
- provider identity/version and requested model;
- structured output schema/prompt contract digest;
- resource/tool policy if the provider can use tools.

Normalize and hash the request before invocation. The digest in the terminal binding must be that exact request, not a later reconstruction.

## Artifact construction

Do not deserialize provider output directly into canonical architecture objects and call that acceptance.

The model response first becomes a model artifact with its own schema and provenance. Validate it before mapping any item into Sensei's existing derived structures.

At minimum validation must fail closed on:

- malformed output;
- unknown/duplicate fields where a closed schema is promised;
- references to observation/evidence IDs that were not supplied in the request;
- repository/file/symbol scope outside the bound request;
- attempts to set canonical/promotion/authority fields;
- missing provider/model/request identity;
- empty or unhashable artifact;
- contradictory artifact identity between binding and receipt.

Model-derived claims continue to require the existing candidate/review path. A valid model artifact proves only that Sensei can attribute and validate what the model returned. It does not prove the returned claim is true.

## Deterministic preservation

The strongest regression rule for #256 is simple:

> With the model disabled or not requested, existing deterministic outputs remain byte-identical for the same pinned inputs.

That rule applies to HOW extraction and to deterministic WHY capture/composition. Adding model capability must not perturb provider ordering, evidence capture, normalized deterministic facts, or deterministic receipt identity merely because model-capable code is now installed.

For a model-assisted run, nondeterminism must be explicit. Do not claim byte-identical replay of the model response unless the provider contract can actually guarantee it. What must replay deterministically is the identity envelope: exact request digest, provider/model identity, artifact digest, and the deterministic baseline against which the model was run.

## Implementation sequence for Claude

Implement in this order so each step can fail closed before the next obtains authority from it.

1. **Strengthen the serialized contract.** Extend `investigation.ModelBinding` and validation with provider/request/artifact/nondeterminism identity. Add typed `refused` and `errored` states, or document and test an equivalently machine-distinct encoding.
2. **Add model contract tests before an executor.** Prove `resolved` is rejected when any required identity is missing, artifact/binding digests disagree, or disabled/not-requested states carry execution evidence they cannot have.
3. **Define the narrow provider port and model request/artifact schema.** Provider selection must be explicit and testable with an in-process fake.
4. **Implement one execution owner.** It constructs and digests the request, invokes the provider once, validates the artifact, computes its digest, and constructs the terminal `ModelBinding` from observed outcome.
5. **Integrate at orchestration.** Start with the WHY orchestration seam unless concrete import/evidence constraints prove a better owner. Thread the model result into document composition instead of letting `compose.go` mint status from configuration.
6. **Map valid model output only into derived lanes.** Preserve provenance and existing human-review requirements. Never write canonical knowledge from this path.
7. **Preserve deterministic mode exactly.** Run existing HOW/WHY fixtures with the model path absent and assert no semantic-output change.
8. **Wire a real provider adapter last.** The contract and fake-provider tests must work without network access, API keys, benchmark harnesses, or a particular vendor.
9. **Only after #256 is independently proven may #131 replace `not_implemented_in_evaluated_path` with a measured arm.** That is a later evaluation change, not part of this capability PR.

## Required proof matrix

A PR claiming #256 complete must include tests for all rows below.

| Case | Expected proof |
| --- | --- |
| model disabled | provider called 0 times; deterministic output unchanged; status `disabled` |
| model not requested | provider called 0 times; deterministic output unchanged; status `not_requested` |
| unknown/unresolvable provider | provider called 0 times; typed `unavailable`; never `resolved` |
| provider refusal | one attempted invocation; typed `refused`; never `resolved` |
| provider execution error | one attempted invocation; typed `errored`; never `resolved` |
| malformed/ungrounded returned artifact | provider called once; typed `invalid`; never `resolved` |
| valid returned artifact | provider called exactly once; `resolved`; provider/model/request/artifact/nondeterminism identities all present and mutually consistent |
| caller tries to predeclare `resolved` | execution path refuses or overwrites it from observed outcome; configuration cannot manufacture success |
| model cites evidence not in request | artifact invalid; no derived claim reaches composition |
| model proposes canonical/promotion authority | rejected or stripped before derived mapping; no graph/promotion/admission mutation occurs |
| deterministic regression | model-disabled HOW and WHY outputs remain byte-identical on pinned fixtures |
| benchmark independence | tests execute with fake provider and no `cmd/eval-arms` dependency |

At least one negative test must be demonstrated to fail when each corresponding guard is deliberately neutered. A green test that cannot detect removal of its law is not proof of the law.

## Review traps

Do not accept any of these as completion:

- a CLI flag that changes `ModelBinding.Status` to `resolved` without an invocation;
- a provider name recorded without provider version/request digest;
- a successful HTTP/command exit treated as a valid model artifact without schema/grounding validation;
- a model response merged into deterministic observations with no provenance distinction;
- a requested model failure silently falling back to deterministic-only and reporting success;
- benchmark code changed to synthesize or assume the missing capability;
- a vendor-backed happy-path test that cannot run hermetically with a fake;
- a model-generated claim written directly into canonical awareness or accepted as admission evidence.

## Repository gates

Before merge, the implementation branch should demonstrate at least:

```text
gofmt -l <changed Go files>        -> empty
go vet ./...                       -> clean
go test ./...                      -> green
make import-graph-check            -> clean
scripts/build-awareness-graph-self.sh --check -> clean
```

If a real-provider smoke requires credentials, keep it supplementary. Hermetic fake-provider tests are the merge proof; an external service must not be required to establish the authority contract.

## Completion statement

#256 is complete only when an explicitly bound model can genuinely run through the investigation path, every outcome is typed and provenance-bound, `resolved` is impossible without observed successful execution and a validated artifact, deterministic investigation remains independently reproducible, and model output still has no route to canonical authority except through the pre-existing governed candidate/review mechanisms.
