# Phase 10: Architectural Investigation and Knowledge Recovery

## Status

Opening design contract for Phase 10.

This document defines the implementation boundary for the next Sensei phase after the completed Phase 9 program. It is intentionally design-first. The opening commit adds no runtime authority, no new closure stage, and no model-dependent verdict.

Phase 10 moves architectural investigation into the current Sensei program rather than reserving the entire capability for a future Sensei v2 boundary.

The governing distinction is:

> Phase 10 builds the investigator on top of the completed Sensei constitution. A future learned layer may improve candidate recall, but Phase 10 owns the evidence model, deterministic investigation pipeline, candidate contracts, and governed review path.

`docs/architecture/V2_ARCHITECTURE_DIRECTION.md` remains useful as the longer-term learned-intelligence direction. This Phase 10 plan supersedes its scheduling implication that the investigator itself must wait for v2. The optional learned or GNN implementation remains deferred until the deterministic Phase 10 baseline is measurable and proven.

---

## 1. Phase 10 objective

Phase 10 improves the architectural and code knowledge Sensei can recover from a repository.

The phase must let Sensei answer four distinct questions without collapsing them:

1. **HOW** is the software structurally and behaviorally assembled?
2. **WHY** did the architecture acquire its current shape?
3. **WHAT MAY BE MISSING** from the current governed architecture?
4. **WHAT EVIDENCE WOULD PROVE OR REFUTE** a proposed architectural claim?

The target outcome is not autonomous architectural truth.

The target outcome is:

> Sensei reconstructs a deeper evidence-bound model of a repository, identifies what it still does not know, produces a small set of falsifiable architectural candidates and questions, and routes every proposed promotion through the existing governed authority path.

Phase 10 succeeds when it improves architectural grounding and review results without weakening any Phase 1 through Phase 9 law.

---

## 2. Why this is a phase, not a version boundary

The completed Sensei system already contains the constitutional machinery required to host investigation safely:

- deterministic fact extraction;
- explicit provenance and limitations;
- candidate claims with human review required;
- contradiction preservation;
- architect questions and governed answers;
- exact repository, tree, graph, task, and result bindings;
- proof requirements and evidence receipts;
- admission and closure that remain fail-closed;
- runtime-boundary observation without projection ownership;
- proportional rigor and actionable unknowns.

Architectural investigation is therefore the next bounded capability of the existing architecture. It does not need to become a new source of truth or a replacement system.

A future learned layer remains useful, but it must plug into Phase 10 interfaces as an optional candidate generator. It must not define those interfaces, own canonical claims, or become necessary for closure.

---

## 3. Architectural position

Phase 10 introduces a separate investigation pipeline beside the existing result pipeline.

```text
exact repository tree
        |
        +--> deterministic HOW extraction
        |      components
        |      runtime flows
        |      state access
        |      boundaries
        |      contracts and seams
        |      tests and guards
        |
frozen external evidence snapshots
        |
        +--> WHY investigation
               source-control history
               pull requests and issues
               design documents
               incidents
               runtime observations
               architect dialogue
        |
        v
coverage map + raw evidence receipts
        |
        v
normalized observations
        |
        v
candidate synthesis and adversarial challenge
        |
        +--> candidate invariant
        +--> candidate contract
        +--> candidate owner or boundary
        +--> candidate failure mode
        +--> candidate evidence request
        +--> counterexample
        +--> architect question
        |
        v
deterministic validation
        |
        v
existing governed review and promotion path
        |
        v
canonical graph, claims, admission, proof, closure
```

### 3.1 The result pipeline stays unchanged

Phase 10 must not become an eleventh mandatory stage of `golang/architecture/resultpipeline`.

The result pipeline is exact-result-bound, pure, offline, deterministic, and fail-closed. Raw Git history, external issue systems, team chat, mutable runtime telemetry, and learned inference can change independently from an exact result tree. They therefore cannot be silently inserted into result-local truth.

Only deterministic tree-local facts may flow into ordinary fact extraction and inferred claims.

External WHY evidence and model-generated candidates remain separate investigation artifacts until governed promotion writes repository-owned authored knowledge that the normal graph build can consume.

### 3.2 Investigation is advisory until promotion

An investigation may:

- observe;
- search;
- cite;
- rank;
- challenge;
- propose;
- ask;
- identify missing evidence.

It may not:

- create canonical truth directly;
- mutate governed awareness sources directly;
- weaken or remove an invariant;
- authorize a mutation;
- certify correctness;
- complete a task;
- reinterpret an owner verdict;
- convert confidence into authority.

---

## 4. Non-negotiable Phase 10 laws

These laws must be represented as governed awareness records before implementation is considered complete.

1. `investigation.model_output_must_never_become_governed_truth_directly`
2. `investigation.how_evidence_must_not_prove_historical_why`
3. `investigation.external_evidence_requires_an_immutable_snapshot_binding`
4. `investigation.unsearched_evidence_must_not_be_reported_as_absent`
5. `investigation.confidence_must_not_substitute_for_proof_strength`
6. `investigation.contradictions_must_survive_synthesis_without_authority`
7. `investigation.raw_evidence_must_survive_normalization`
8. `investigation.repeated_deviation_may_contest_architecture_but_not_weaken_it`
9. `investigation.result_pipeline_must_remain_model_independent`
10. `investigation.learned_layer_unavailability_must_not_change_canonical_truth`
11. `investigation.scope_must_not_expand_beyond_bound_evidence`
12. `investigation.external_provider_failure_must_be_visible_as_coverage_state`
13. `investigation.reproduction_requires_exact_plan_extractor_and_evidence_bindings`
14. `investigation.candidate_ranking_must_not_change_epistemic_status`
15. `investigation.promotion_must_use_the_existing_governed_answer_and_claim_path`

The central epistemic distinction is:

```text
searched and found no evidence
              !=
evidence source was not searched
```

No report, UI, agent briefing, or candidate composer may erase that distinction.

---

## 5. Phase 10 information model

Phase 10 begins with contracts, not extractors.

Create the canonical package:

```text
golang/architecture/investigation/
    model.go
    binding.go
    coverage.go
    evidence.go
    proof_strength.go
    receipt.go
    normalize.go
    validate.go
    digest.go
```

### 5.1 Investigation document

```go
type Document struct {
    SchemaVersion string `json:"schema_version" yaml:"schema_version"`
    GeneratedBy   string `json:"generated_by" yaml:"generated_by"`
    Mode          Mode   `json:"mode" yaml:"mode"`

    Binding Binding `json:"binding" yaml:"binding"`
    Plan    Plan    `json:"plan" yaml:"plan"`

    Coverage     []CoverageEntry       `json:"coverage" yaml:"coverage"`
    RawEvidence  []EvidenceReceipt     `json:"raw_evidence" yaml:"raw_evidence"`
    Observations []architecture.Fact   `json:"observations" yaml:"observations"`

    CandidateClaims    []architecture.Claim        `json:"candidate_claims,omitempty" yaml:"candidate_claims,omitempty"`
    CandidateQuestions []architecture.OpenQuestion `json:"candidate_questions,omitempty" yaml:"candidate_questions,omitempty"`
    Counterexamples    []Counterexample             `json:"counterexamples,omitempty" yaml:"counterexamples,omitempty"`

    Limitations []architecture.Limitation `json:"limitations,omitempty" yaml:"limitations,omitempty"`
    Receipt     RunReceipt                `json:"receipt" yaml:"receipt"`
}
```

The exact field spelling may be refined during implementation, but the semantic separation is mandatory:

- binding;
- plan;
- source coverage;
- raw evidence;
- normalized observations;
- non-authoritative outputs;
- limitations;
- immutable run receipt.

### 5.2 Investigation modes

```go
type Mode string

const (
    ModeHow          Mode = "how"
    ModeWhy          Mode = "why"
    ModeArchitecture Mode = "architecture"
    ModeBlastRadius  Mode = "blast_radius"
    ModeChallenge    Mode = "challenge"
)
```

Unknown modes fail validation.

### 5.3 Binding

```go
type Binding struct {
    Repository architecture.ClaimDocumentBinding `json:"repository" yaml:"repository"`

    EvidenceSnapshotDigestSHA256 string `json:"evidence_snapshot_digest_sha256,omitempty" yaml:"evidence_snapshot_digest_sha256,omitempty"`
    InvestigationPlanDigestSHA256 string `json:"investigation_plan_digest_sha256" yaml:"investigation_plan_digest_sha256"`
    ExtractorProfileDigestSHA256  string `json:"extractor_profile_digest_sha256" yaml:"extractor_profile_digest_sha256"`

    Model ModelBinding `json:"model" yaml:"model"`
}
```

The repository binding and evidence snapshot binding are not interchangeable.

`ModelBinding` must represent at least:

- `disabled`;
- `not_requested`;
- `unavailable`;
- `resolved`;
- `invalid`.

A model digest is required only when the model status is `resolved`.

A resolved model may produce only candidate output.

### 5.4 Evidence categories

```go
type EvidenceCategory string

const (
    EvidenceSourceCode        EvidenceCategory = "source_code"
    EvidenceTests             EvidenceCategory = "tests"
    EvidenceDocumentation     EvidenceCategory = "documentation"
    EvidenceSourceControl     EvidenceCategory = "source_control"
    EvidenceIssues            EvidenceCategory = "issues"
    EvidenceDesignDocuments   EvidenceCategory = "design_documents"
    EvidenceTeamChat          EvidenceCategory = "team_chat"
    EvidenceRuntime           EvidenceCategory = "runtime_observability"
    EvidenceErrorTracking     EvidenceCategory = "error_tracking"
    EvidenceProductAnalytics  EvidenceCategory = "product_analytics"
    EvidenceArchitectFeedback EvidenceCategory = "architect_feedback"
)
```

The vocabulary must remain closed and versioned. Providers may not invent category strings at runtime.

### 5.5 Coverage states

```go
type CoverageStatus string

const (
    CoverageSupporting    CoverageStatus = "searched_supporting"
    CoverageRefuting      CoverageStatus = "searched_refuting"
    CoverageMixed         CoverageStatus = "searched_mixed"
    CoverageNoResult      CoverageStatus = "searched_no_result"
    CoverageUnavailable   CoverageStatus = "unavailable"
    CoverageNotConfigured CoverageStatus = "not_configured"
    CoverageSkipped       CoverageStatus = "skipped_with_reason"
    CoverageInvalid       CoverageStatus = "invalid"
)
```

A coverage entry must identify:

- provider identity and version;
- category;
- target or query digest;
- searched time range when applicable;
- immutable source snapshot digest;
- result evidence IDs;
- coverage status;
- reason when unavailable, skipped, or invalid;
- limitations.

`searched_no_result` requires proof that the provider actually executed against the bound snapshot.

### 5.6 Raw evidence receipt

```go
type EvidenceReceipt struct {
    ID string `json:"id" yaml:"id"`

    Category EvidenceCategory `json:"category" yaml:"category"`
    Provider ProviderBinding   `json:"provider" yaml:"provider"`

    SourceIdentity string `json:"source_identity" yaml:"source_identity"`
    SourceDigestSHA256 string `json:"source_digest_sha256" yaml:"source_digest_sha256"`
    ContentDigestSHA256 string `json:"content_digest_sha256" yaml:"content_digest_sha256"`

    CapturedContent string `json:"captured_content,omitempty" yaml:"captured_content,omitempty"`
    ContentLocation string `json:"content_location,omitempty" yaml:"content_location,omitempty"`

    Scope architecture.ClaimScope `json:"scope" yaml:"scope"`
    CapturedAt string `json:"captured_at" yaml:"captured_at"`
}
```

The exact payload storage mechanism may use inline content or content-addressed side artifacts. Normalization must never destroy the original receipt.

### 5.7 Proof-strength ladder

```go
type ProofStrength string

const (
    ProofAssertionOnly       ProofStrength = "P0_assertion_only"
    ProofStaticSource        ProofStrength = "P1_static_source_citation"
    ProofStructuralPath      ProofStrength = "P2_structural_path_demonstrated"
    ProofDeterministicTest   ProofStrength = "P3_deterministic_test_executed"
    ProofIntegrationRuntime  ProofStrength = "P4_integration_runtime_observed"
    ProofProductionObserved  ProofStrength = "P5_production_observation"
)
```

Confidence and proof strength must remain separate fields.

Confidence ranks a candidate.

Proof strength constrains what the evidence can legitimately support.

### 5.8 Run receipt

Every investigation run must record:

- schema version;
- generated-by identity;
- repository binding;
- graph digest when available;
- plan digest;
- extractor profile digest;
- evidence snapshot digest;
- model binding and model artifact digest when used;
- deterministic post-processing version;
- output document digest;
- output candidate IDs and digests;
- timestamp source;
- resource limits;
- nondeterminism declaration.

A later run may supersede a previous result. It may not rewrite the previous receipt.

---

## 6. HOW extraction

Create:

```text
golang/architecture/howextract/
    extract.go
    topology.go
    flow.go
    state.go
    boundaries.go
    contracts.go
    tests.go
    compose.go
```

HOW extraction answers only structural and behavioral questions.

Initial deterministic investigators:

| Investigator | Required output |
|---|---|
| topology | packages, components, dependency edges |
| runtime flow | entrypoint-to-effect paths |
| state ownership observation | readers, writers, mutation paths |
| boundary | cross-component and cross-domain crossings |
| contract seam | interfaces, RPCs, schemas, stable seams |
| test protection | tests, guards, CI gates, fixtures |
| data shape | types and serialized forms crossing boundaries |

The implementation must reuse the current Go AST and semantic extractors under `golang/architecture/factextract` and `golang/architecture/gosemantics`. It must not create a second competing parser pipeline.

HOW extraction may support a fact such as:

```text
package X writes state Y
```

It may not by itself prove:

```text
package X was intentionally selected as the authoritative owner of Y
```

That second statement requires authored authority or WHY evidence.

---

## 7. WHY investigation and frozen evidence providers

Create:

```text
golang/architecture/whyinvestigate/
    investigate.go
    provider.go
    snapshot.go
    source_control.go
    documents.go
    incidents.go
    runtime.go
    dialogue.go
```

### 7.1 Provider interface

```go
type Provider interface {
    ID() string
    Version() string
    Category() investigation.EvidenceCategory

    Snapshot(
        context.Context,
        Request,
    ) (
        SnapshotReceipt,
        []investigation.EvidenceReceipt,
        investigation.CoverageEntry,
        error,
    )
}
```

Providers report coverage even when they fail or find no evidence.

### 7.2 First built-in providers

The first implementation must remain reproducible without live SaaS dependencies.

Implement:

1. Git log, blame, and local commit history
2. Repository documentation
3. ADRs and governed decisions
4. Tests and fixtures
5. Sensei incident and failure-mode records
6. Existing architect dialogue
7. Imported runtime-evidence snapshots
8. Task, completion, and evidence receipts

GitHub PRs, issues, Slack, Jira, Sentry, observability, and analytics should initially enter through imported immutable snapshots.

Examples:

```text
sensei evidence import github-prs.json
sensei evidence import issue-export.json
sensei evidence import runtime-observations.json
sensei evidence import error-tracking-export.json
```

The connector or agent may collect external material. Sensei core verifies, hashes, binds, and interprets the snapshot.

---

## 8. Candidate synthesis and adversarial challenge

Create:

```text
golang/architecture/investigator/
    compose.go
    rules.go
    challenge.go
    counterexample.go
    ranking.go
    postprocess.go
    receipt.go
```

Inputs:

- HOW observations;
- WHY evidence;
- evidence coverage;
- current graph;
- current claims;
- closure unknowns and blockers;
- existing architect questions;
- historical review outcomes.

Outputs remain non-authoritative:

- candidate invariant;
- candidate contract;
- candidate boundary;
- candidate owner;
- candidate failure mode;
- candidate evidence request;
- adversarial question;
- minimal counterexample;
- governance-debt candidate.

### 8.1 Deterministic post-processing

Every generated candidate must pass deterministic checks:

- every file exists in the bound tree;
- every symbol or graph node exists;
- every evidence reference resolves;
- evidence digests match;
- candidate scope does not exceed evidence scope;
- supporting and refuting evidence remain distinct;
- contradictions remain visible;
- human review remains required;
- promotion status remains `candidate`;
- confidence remains ranking metadata only;
- no governed source is modified.

### 8.2 Candidate identity

Candidate identity must not include confidence.

Stable identity should bind:

- repository;
- proposition;
- scope;
- candidate kind;
- generator version;
- input graph digest;
- evidence snapshot digest.

The same proposition may be reranked without becoming a different architectural proposition.

### 8.3 Ranking

Rank candidates by:

- architectural blast radius;
- authority sensitivity;
- contradiction density;
- incident recurrence;
- task relevance;
- runtime frequency;
- evidence independence;
- cost of obtaining missing evidence;
- expected reduction in future agent mistakes.

Ranking must never change candidate status or proof strength.

---

## 9. Architect questions and governed promotion

The existing `golang/architecture/questiongen` and architecture dialogue models remain the only canonical question and answer path.

Phase 10 adds new question source kinds without creating a parallel dialogue system:

```go
type QuestionSourceKind string

const (
    SourceClosureBlocker         QuestionSourceKind = "closure_blocker"
    SourceInvestigationCandidate QuestionSourceKind = "investigation_candidate"
    SourceCounterexample         QuestionSourceKind = "counterexample"
    SourceEvidenceGap            QuestionSourceKind = "evidence_gap"
    SourceDeviationPattern       QuestionSourceKind = "deviation_pattern"
)
```

Add stable templates:

- `question.structural_why.v1`
- `question.missing_contract_candidate.v1`
- `question.owner_candidate.v1`
- `question.failure_mode_candidate.v1`
- `question.counterexample_validation.v1`
- `question.evidence_request.v1`
- `question.governance_debt.v1`
- `question.repeated_deviation.v1`

Every question must state:

- what was observed;
- what remains unknown;
- why it matters;
- supporting evidence;
- refuting evidence;
- what would falsify the candidate;
- which owner appears responsible for answering;
- what happens if unresolved.

Promotion must continue through accepted architect answers, governed proposal, authored source mutation, graph rebuild, ordinary inference, and ordinary closure.

---

## 10. Architectural deviation receipts

Create:

```text
golang/architecture/deviation/
    model.go
    record.go
    cluster.go
    candidate.go
```

A deviation receipt records when implementation friction exposes a possible gap in the current architecture.

Example kinds:

- `implementation_required_undeclared_parameter`
- `implementation_bypassed_owner_path`
- `implementation_needed_repeated_locking`
- `implementation_added_same_escape_hatch`
- `implementation_could_not_satisfy_boundary`
- `implementation_discovered_missing_state`

One deviation is local evidence.

Repeated independent deviations of the same shape may create a candidate that contests an existing architectural claim.

They may not automatically weaken or remove that claim.

---

## 11. CLI and MCP surfaces

### 11.1 CLI

Planned commands:

```text
sensei investigate how
sensei investigate why
sensei investigate architecture
sensei investigate blast-radius
sensei investigate challenge
sensei investigate validate

sensei evidence snapshot
sensei evidence import
sensei evidence coverage

sensei candidates list
sensei candidates show
sensei candidates review
```

### 11.2 MCP

Keep the MCP surface coherent rather than creating one tool per extractor.

Planned tools:

- `awareness_investigate`
- `awareness_evidence_coverage`
- `awareness_candidates`
- `awareness_challenge`

`awareness_propose` remains the governed proposal bridge. No Phase 10 MCP tool may promote knowledge directly.

---

## 12. Phase decomposition

### Phase 10.0: Contract and boundary freeze

Deliver:

- this design contract;
- explicit Phase 10 laws;
- package ownership map;
- no implementation authority;
- no generated artifacts represented as complete.

### Phase 10.1: Investigation contracts

Deliver:

- `golang/architecture/investigation`;
- normalized document and binding;
- evidence categories;
- coverage states;
- evidence receipts;
- proof-strength vocabulary;
- run receipts and semantic digests;
- strict validation and property tests.

### Phase 10.2: Deterministic HOW extraction

Deliver:

- topology, runtime-flow, state-access, boundary, seam, test, and data-shape observations;
- reuse of current AST and semantic extractors;
- no historical-intent inference from code alone.

### Phase 10.3: WHY evidence framework

Deliver:

- provider interface;
- immutable evidence snapshots;
- coverage map;
- built-in Git, docs, incidents, dialogue, receipt, and runtime-import providers.

### Phase 10.4: Candidate composer and challenge engine

Deliver:

- candidate claim, question, evidence request, failure mode, contract, boundary, and counterexample outputs;
- deterministic grounding validation;
- ranking separate from epistemic status.

### Phase 10.5: Governed question integration

Deliver:

- investigator-backed question source kinds;
- new stable templates;
- existing dialogue and answer path reused;
- governed promotion only.

### Phase 10.6: Deviation learning

Deliver:

- deviation receipts;
- pattern clustering;
- repeated-deviation candidates;
- no automatic weakening of architecture.

### Phase 10.7: Agent surfaces

Deliver:

- CLI commands;
- four MCP tools;
- agent skill updates;
- human-readable and JSON output;
- honest coverage and proof strength.

### Phase 10.8: External evaluation

Deliver:

- Sensei self-evaluation;
- Globular runtime and historical evaluation;
- SQLite independent calibration;
- architectural mutants;
- baseline versus Phase 10 metrics.

### Deferred beyond Phase 10 deterministic baseline

- GNN or learned candidate generator;
- cross-repository training;
- automatic training from architect answers;
- live SaaS provider dependency in core;
- mandatory model execution for closure;
- automatic canonical promotion.

---

## 13. First implementation PR boundary

The first implementation slice after this opening contract is **Phase 10.1: Investigation contracts**.

It must remain narrow.

### Required new files

```text
golang/architecture/investigation/model.go
golang/architecture/investigation/binding.go
golang/architecture/investigation/coverage.go
golang/architecture/investigation/evidence.go
golang/architecture/investigation/proof_strength.go
golang/architecture/investigation/receipt.go
golang/architecture/investigation/normalize.go
golang/architecture/investigation/validate.go
golang/architecture/investigation/digest.go
```

Tests may be split by concern.

### Required behavior

1. Canonical normalization is deterministic.
2. Semantic digest is stable across map order and input slice order where order is not semantic.
3. Every enum uses a closed vocabulary.
4. Unknown enum values fail closed.
5. Repository, plan, and extractor bindings are explicit.
6. Evidence snapshot binding is explicit when external evidence is declared.
7. Model-disabled and model-unavailable documents remain valid when no model output is claimed.
8. Model-resolved documents require a model artifact digest.
9. Raw evidence has stable identity and digest.
10. Coverage distinguishes searched-no-result from unavailable, skipped, and not-configured.
11. `searched_no_result` requires an executed provider receipt.
12. Candidate scope must not broaden beyond observation or evidence scope.
13. Confidence and proof strength remain separate.
14. Candidate claims remain human-review-required and `promotion_status=candidate`.
15. Contradictory evidence is valid and preserved.
16. Run receipts bind the exact output digest.
17. No package imports resultpipeline, closureprotocol, admission, or a model implementation.
18. The package remains pure and offline.

### Required tests

- normalization idempotence;
- digest determinism;
- duplicate-ID refusal;
- invalid-vocabulary refusal;
- escaping file-path refusal;
- missing provider execution for `searched_no_result` refusal;
- model status and digest matrix;
- repository/evidence binding mismatch refusal;
- candidate scope expansion refusal;
- contradiction preservation;
- confidence does not alter candidate identity;
- proof strength does not derive from confidence;
- output receipt digest mismatch refusal;
- model-disabled canonical truth equivalence;
- fuzz or property tests for normalization and validation stability.

### Explicit exclusions for Phase 10.1

Do not add:

- extractors;
- Git traversal;
- network providers;
- MCP tools;
- CLI commands;
- model invocation;
- GNN code;
- question generation;
- governed source mutation;
- result-pipeline stages;
- closure blockers;
- production runtime probes.

The first slice closes only the language and evidence rules that later investigators must obey.

---

## 14. Evaluation strategy

Use three repositories:

1. Sensei for self-governance
2. Globular for historical and runtime complexity
3. SQLite for independent external calibration

Metrics:

- component recovery;
- boundary precision;
- dependency-direction accuracy;
- state-owner observation accuracy;
- runtime-flow accuracy;
- invariant candidate precision;
- contract candidate precision;
- citation correctness;
- proof-strength correctness;
- coverage completeness;
- contradiction preservation;
- useful-question rate;
- false-review cost;
- architect time saved;
- mutation detection rate;
- deterministic reproducibility.

Architectural mutants should include:

- owner-path bypass;
- direct filesystem access across the wrong layer;
- dependency inversion;
- missing transaction boundary;
- incompatible persisted-data mutation;
- retry around a non-idempotent mutation;
- runtime repair using stale owner state.

A successful finding must identify:

- which architecture is threatened;
- which evidence supports that architecture;
- which owner or boundary is implicated;
- what failure may result;
- what evidence would establish safety.

---

## 15. Definition of Phase 10 completion

Phase 10 is complete only when Sensei can demonstrate all of the following:

1. Reconstruct a typed HOW model from an exact repository tree.
2. Investigate WHY across a declared evidence-coverage map.
3. Preserve raw evidence, normalized observations, contradictions, and limitations.
4. Generate evidence-linked candidates and falsifiable questions.
5. Distinguish confidence from proof strength.
6. Refuse to promote candidates without governed review.
7. Detect at least one seeded architectural violation missed by ordinary tests.
8. Improve architecture extraction measurably over the current deterministic baseline.
9. Continue operating correctly with every model-dependent feature disabled.
10. Reproduce an investigation from its repository, graph, evidence, plan, extractor, configuration, and model receipts.

The completion claim is not:

> More candidate files exist.

The completion claim is:

> Sensei can recover deeper architectural knowledge, prove how that knowledge was obtained, preserve uncertainty and contradiction, and route every proposed architectural conclusion through the same governed system that protects existing truth.

---

## 16. Implementation instruction for the first agent

Begin with Phase 10.1 only.

Before writing code:

1. Run Sensei briefing and preflight over the proposed `golang/architecture/investigation` package.
2. Inspect the current `architecture.Fact`, `architecture.Claim`, `ClaimDocumentBinding`, dialogue, semantic-digest, normalization, and validation conventions.
3. Reuse existing canonicalization and binding vocabulary where semantics are identical.
4. Do not create a second generic evidence or claim type when an existing type is sufficient.
5. Document every deliberate semantic difference from existing architecture contracts.

During implementation:

- use closed typed vocabularies;
- normalize before identity, digest, comparison, or validation;
- preserve raw evidence separately from normalized facts;
- fail closed on unknown states;
- make empty output explicit rather than inferred from missing computation;
- keep the package pure, offline, and model-independent;
- add property tests before adding adapters.

Stop and report rather than extending scope when:

- an existing canonical owner must change;
- the design requires a new closure dimension;
- resultpipeline would need an additional stage;
- external evidence cannot be frozen reproducibly;
- candidate scope cannot be tied to evidence scope;
- a model output would need to become authoritative to make the design work.

The opening PR should remain draft until the Phase 10.1 contracts and tests satisfy the exact boundary above.
