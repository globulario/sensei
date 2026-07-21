# Sensei v2 Architecture Direction

## Status

This document defines the architectural direction for Sensei v2 after the closure of the first complete Sensei system.

Sensei v1 establishes a deterministic, governed architecture-reasoning substrate. Sensei v2 must not replace that substrate with probabilistic authority. It must extend Sensei from enforcing known architectural laws toward discovering candidate laws, adversarial questions, missing contracts, and structural counterexamples.

The governing direction is:

> Sensei v1 is the court, record, and rules of evidence. Sensei v2 begins building the investigator.

The investigator may ask, rank, connect, and challenge. It may never decide architectural truth by itself.

---

## 1. What Sensei v1 established

Sensei v1 proves that architecture can be represented and governed as an executable system rather than maintained as informal intent.

The completed v1 substrate includes:

- explicit architectural claims and governed records;
- stable semantic ownership and authority boundaries;
- distinction between desired, observed, actual, inferred, and unknown state;
- deterministic extraction from source, tests, documentation, history, and governed artifacts;
- immutable evidence and content-addressed receipts;
- contradiction preservation instead of first-result-wins resolution;
- guarded agent admission and bounded change execution;
- exact change-to-task and completion binding;
- separation between completion and correctness;
- architect feedback and governed claim promotion;
- runtime-boundary evidence and assessment;
- control-panel projection without semantic recomputation;
- actionable incompleteness rather than optimistic absence;
- proportional rigor for changes of different architectural weight.

These are not implementation details to be replaced in v2. They are the constitutional layer that makes v2 safe.

### 1.1 The central epistemic laws

Sensei v2 inherits the following non-negotiable laws:

1. Absence is not safety.
2. Observation is not authority.
3. Traffic is not architecture.
4. Confidence is not evidence.
5. A candidate is not a canonical claim.
6. Completion is not correctness.
7. Transport may carry a verdict but may not recompute it.
8. A skipped computation may not improve a result.
9. Contradictions must remain visible until governed resolution.
10. Generated truth must be regenerated from authored truth, never hand-reconciled.

Any v2 subsystem that weakens these laws is architecturally invalid, even if its outputs appear more intelligent.

---

## 2. What v1 teaches us

Sensei v1 is not only a product foundation. It is the first instrument for learning how architectural knowledge behaves under real agent-driven development.

### 2.1 V1 teaches which facts are already recoverable deterministically

Before learned inference is introduced, Sensei must measure what can already be extracted from:

- package and module structure;
- type and symbol relationships;
- call and dependency edges;
- tests and fixtures;
- comments and architecture documents;
- pull-request and commit history;
- failure modes and incident records;
- task and completion receipts;
- runtime evidence;
- architect answers and governed claims.

This deterministic baseline is essential. Without it, a neural layer can appear useful merely by rediscovering information that the system could have obtained more reliably.

### 2.2 V1 teaches where the real unknowns remain

V1 can identify several distinct forms of incompleteness:

- evidence is unavailable;
- evidence exists but identities cannot be bound;
- multiple interpretations remain contested;
- a contract is implied by implementation but not governed;
- an ownership relation is missing;
- a failure pattern has no encoded invariant;
- a valid rule interacts badly with another valid rule;
- the repository contains an architectural assumption that no artifact names;
- a candidate cannot be promoted without architect judgment.

These unresolved regions define the legitimate input space for v2 intelligence.

### 2.3 V1 teaches which human questions carry the most value

During the construction of v1, the highest-value advances often came from adversarial questions such as:

- Can a weaker narrow surface override a broader strict surface?
- Can a projection accidentally become a semantic owner?
- Can replay strengthen evidence?
- Can missing telemetry be interpreted as compliance?
- Can a generated artifact be manually reconciled without corrupting truth?
- Can a runtime witness be mistaken for a contract or crossing identity?
- Can a lower-rigor declaration downgrade contact with a higher-rigor owner?

V2 should learn to produce questions of this form as governed hypotheses.

The target is not generic curiosity. The target is structured adversarial inquiry grounded in the actual repository graph.

### 2.4 V1 teaches the cost of governance itself

V1 also reveals governance inflation:

- owner layers may multiply;
- read-only projections may inherit authority-level ceremony;
- guards may outlive the failure they were created to prevent;
- multiple artifacts may encode overlapping truth;
- a low-risk change may trigger disproportionate proof work.

V2 must therefore reason about two systems simultaneously:

1. the software architecture;
2. the governance architecture that protects it.

A mature Sensei must be able to identify both architectural drift and governance overgrowth.

---

## 3. The v2 objective

The primary objective of Sensei v2 is:

> Generate high-quality, evidence-grounded architectural questions and candidate laws from the residual uncertainty left after deterministic Sensei analysis.

This objective has four parts:

1. detect structural gaps and risky interactions;
2. propose falsifiable architectural hypotheses;
3. rank the hypotheses by expected value;
4. feed reviewed results back into the governed v1 substrate.

V2 is successful when it reduces the amount of adversarial insight that must originate exclusively from a human architect without transferring authority to the model.

---

## 4. Architectural position of v2 intelligence

V2 intelligence begins only after the v1 deterministic pipeline has produced its best available state.

```text
source + tests + history + runtime + governed knowledge
    -> deterministic extraction
    -> canonical graph and claims
    -> contradictions, unknowns, and unresolved candidates
    -> v2 adversarial inference
    -> candidate questions, candidate laws, and counterexamples
    -> deterministic evidence checks
    -> architect or governed-owner review
    -> canonical claim, rejection, or continued contest
```

The v2 layer is downstream of deterministic extraction and upstream of governed promotion.

It is neither the graph owner nor the claim owner.

### 4.1 Required separation

The architecture should preserve three distinct roles:

- **Deterministic substrate:** establishes what is known, observed, missing, contested, or invalid.
- **Learned investigator:** proposes interpretations, risks, questions, and counterexamples.
- **Governed authority:** accepts, rejects, refines, or contests the proposal.

No component may collapse these roles.

---

## 5. Core v2 capabilities

### 5.1 Adversarial question generation

The investigator should generate questions that attempt to falsify the current architecture.

Examples:

- Which stricter owner can be bypassed by a more specific weaker rule?
- Which package imports create an undeclared authority path?
- Which runtime interaction has no governed contract?
- Which claim is supported only by evidence from the same source that asserted it?
- Which completion proof does not cover the architectural risk introduced by the change?
- Which invariants are individually valid but jointly inconsistent?
- Which unknown has the highest blast radius if its optimistic interpretation is wrong?
- Which historical incident has no corresponding preventative law?

Each generated question must bind to exact graph nodes, files, claims, receipts, or runtime observations.

### 5.2 Candidate invariant discovery

V2 should propose candidate invariants from repeated evidence patterns, incidents, and reviewer interventions.

A candidate invariant must include:

- stable candidate identity;
- model and model-version identity;
- exact input graph digest;
- supporting evidence references;
- counterevidence references;
- confidence as ranking metadata only;
- a falsifiable statement;
- expected failure prevented;
- affected owners and surfaces;
- proposed proof obligations;
- promotion status;
- rejection or review history.

Confidence must never substitute for evidence or authority.

### 5.3 Missing-contract and missing-boundary discovery

V2 should identify implementation relationships that appear structurally meaningful but lack governed representation.

Examples include:

- repeated caller-callee interaction without a declared contract;
- direct access to owner-controlled state;
- runtime crossings that cannot be mapped to a governed boundary;
- package dependencies that contradict documented ownership;
- tests that imply an invariant not present in the graph;
- history showing repeated repair around the same undocumented assumption.

The output is a contract or boundary candidate, never an automatically created contract.

### 5.4 Interaction-risk analysis

The strongest v2 capability should examine interactions among valid rules.

Many serious defects do not violate one isolated rule. They emerge from composition:

- overlapping path or package classifications;
- authority fallback combined with stale identity;
- retry logic combined with non-idempotent mutation;
- runtime repair combined with unavailable owner state;
- two individually safe policies producing an unsafe combined state.

The investigator should search for counterexamples across rule intersections and produce minimal reproducing structures.

### 5.5 Governance-debt analysis

V2 should identify places where governance mechanisms are heavier than the failure they protect against.

A governance mechanism should be a simplification candidate when it cannot clearly name:

- the failure it prevents;
- the owner of the rule;
- the evidence that justified it;
- the cheapest proof that preserves safety;
- the consequences of removing or weakening it.

This capability must remain advisory. V2 may propose removing ceremony, but it may not remove guards or proof obligations automatically.

### 5.6 Question prioritization

Not every unknown deserves human attention.

V2 should rank questions by factors such as:

- architectural blast radius;
- authority sensitivity;
- likelihood of hidden coupling;
- recurrence in history;
- runtime evidence frequency;
- contradiction density;
- task relevance;
- cost of obtaining the missing evidence;
- expected reduction in future agent error.

Ranking is prioritization, not truth.

---

## 6. The learned layer

A GNN or related graph-aware model is a plausible implementation for v2, but the architecture must not depend on a particular model family.

The learned layer should be replaceable.

### 6.1 Inputs

Potential model inputs include:

- canonical architecture graph topology;
- typed node and edge classes;
- symbol and dependency graphs;
- claims and claim states;
- evidence and provenance edges;
- contradiction and contest edges;
- task, change, and completion relationships;
- historical incidents and fixes;
- runtime-boundary observations;
- ownership and authority structure;
- architect review outcomes.

All features must be derived from versioned, reproducible inputs.

### 6.2 Outputs

The model may emit only non-authoritative records such as:

- candidate invariant;
- candidate contract;
- candidate boundary;
- candidate owner;
- candidate failure mode;
- adversarial question;
- suspected contradiction;
- expected counterexample;
- candidate evidence request;
- ranking or similarity score.

The model may not emit canonical truth directly.

### 6.3 Inference receipts

Every inference run must produce an immutable receipt containing:

- model identity and version;
- model artifact digest;
- feature-extractor version;
- input graph digest;
- inference configuration;
- output candidate digests;
- timestamp;
- resource limits;
- deterministic post-processing version;
- any nondeterminism declaration.

A model update must never silently rewrite old candidate history.

### 6.4 Optionality

Sensei must remain fully operational without the learned layer.

When the model is absent, unavailable, incompatible, or disabled:

- deterministic extraction continues;
- canonical claims remain unchanged;
- closure truth does not degrade beyond the actual missing learned capability;
- the system reports learned inference as unavailable;
- no previously canonical claim is withdrawn merely because the model is absent.

The model is an optimization and recall amplifier, not a runtime dependency of architectural truth.

---

## 7. Learning and feedback

V2 may improve from architect review, but feedback must itself be governed.

### 7.1 Review outcomes

Candidate review outcomes should include:

- accepted;
- rejected;
- refined;
- contested;
- duplicate;
- insufficient evidence;
- deferred;
- invalid input;
- outside governed scope.

These outcomes form training or ranking signals only after provenance and authority checks.

### 7.2 No naive reinforcement loop

The system must not learn directly from every accepted or rejected candidate without context.

A review may reflect:

- repository-specific convention;
- temporary migration state;
- incomplete evidence;
- reviewer error;
- scoped exception;
- a genuine universal architectural law.

Training data must preserve these distinctions.

### 7.3 Repository-local and cross-repository learning

V2 should distinguish:

- repository-local patterns;
- organization-level conventions;
- language or framework patterns;
- candidate universal principles.

A local decision must not automatically become a global architectural rule.

---

## 8. Evaluation strategy

V2 must be evaluated against the deterministic v1 baseline.

### 8.1 Baseline questions

For every evaluation repository, measure:

- what v1 discovers without learned inference;
- which important unknowns remain;
- which candidates are generated only by v2;
- which v2 candidates are accepted by an architect;
- which are rejected or harmful;
- whether v2 identifies a real counterexample missed by baseline tests;
- whether v2 reduces architect questioning time;
- whether agent-produced changes improve under v2 guidance.

### 8.2 Core metrics

Recommended metrics include:

- deterministic candidate recall;
- neural-only valid candidate recall;
- candidate precision;
- false-positive review cost;
- architect questions avoided;
- high-value questions discovered;
- time to architectural grounding;
- prevented architectural mistakes;
- repeated incident reduction;
- model-unavailable degradation;
- reproducibility across model versions.

### 8.3 External-repository requirement

V2 cannot be considered validated while Sensei remains its primary proving ground.

The decisive test requires:

- repositories not designed by the Sensei architect;
- maintainers unfamiliar with the ontology;
- agents without prior project-specific instructions;
- real tasks with architectural consequences;
- independent review of candidate usefulness.

The target outcome is not complete automatic understanding.

The target outcome is:

> Sensei identifies what it does not understand, asks useful questions, improves from governed answers, and prevents at least one plausible architectural error.

---

## 9. Product direction

V2 intelligence must remain legible to unfamiliar users.

For every candidate or question, the system should explain:

- what was observed;
- what remains unknown;
- why the question matters;
- which evidence supports it;
- which evidence would falsify it;
- what owner must answer;
- what happens if it remains unresolved.

The system should never display model confidence as a substitute for architectural grounding.

A useful v2 experience is not a flood of clever guesses. It is a small queue of high-value, evidence-linked questions.

---

## 10. Phased implementation direction

### Phase A: Baseline and instrumentation

Before adding learned inference:

- measure deterministic extraction coverage;
- record unresolved candidate classes;
- capture architect questions and review outcomes;
- identify recurring unknown patterns;
- establish external repository evaluation fixtures;
- measure governance and review cost.

### Phase B: Deterministic adversarial analysis

Before introducing a GNN, implement deterministic counterexample generators where practical:

- overlapping ownership and rigor surfaces;
- contradictory authority paths;
- missing contract bindings;
- unguarded high-risk edges;
- rule-intersection checks;
- repeated incident without invariant;
- unsupported positive verdicts.

This establishes another strong baseline and produces training data.

### Phase C: Learned candidate ranking

Introduce an optional model that ranks existing deterministic and heuristic candidates.

The first learned feature should improve prioritization, not invent canonical facts.

### Phase D: Learned candidate generation

Allow the model to propose new candidate questions, invariants, contracts, and counterexamples.

All outputs remain non-authoritative and receipt-bound.

### Phase E: Governed feedback and adaptation

Use reviewed outcomes to improve ranking and generation while preserving repository, organization, and universal scopes.

### Phase F: External validation

Demonstrate measurable value on unfamiliar repositories and with unfamiliar maintainers.

---

## 11. Stop boundaries

The following are outside the initial v2 architecture direction:

- automatic promotion of model output to canonical claims;
- model-issued correctness certification;
- model-driven automatic repair without governed admission;
- replacing deterministic extraction with learned inference;
- treating confidence as evidence;
- suppressing contradictions because a model prefers one interpretation;
- silently retraining from ungoverned interaction data;
- requiring a GPU or remote model service for core Sensei operation;
- broad telemetry ingestion without architectural identity binding;
- optimizing benchmark scores by weakening Unknown or fail-closed behavior.

---

## 12. Architectural success criteria

Sensei v2 succeeds when:

1. v1 remains useful and trustworthy with v2 disabled;
2. v2 produces evidence-grounded questions that humans judge valuable;
3. accepted v2 candidates improve the governed graph without bypassing authority;
4. rejected candidates remain visible as learning evidence without polluting truth;
5. model changes do not rewrite historical decisions;
6. the system discovers interactions and counterexamples that deterministic extraction missed;
7. external maintainers receive value without first learning the entire Sensei ontology;
8. governance becomes more proportional rather than simply accumulating more machinery;
9. Sensei begins asking some of the adversarial questions previously supplied only by expert architects;
10. the distinction between candidate intelligence and architectural truth remains intact.

---

## 13. Final direction

Sensei v1 amplifies a strong architect by making architectural memory, authority, evidence, and completion executable.

Sensei v2 should amplify the architect's adversarial reach.

It should search the residual unknown, test the interaction between valid rules, propose missing laws, request decisive evidence, and learn from governed review. It must do all of this without becoming the authority it assists.

The direction can be summarized as:

```text
v1: preserve and enforce known architectural truth
v2: investigate what architectural truth may still be missing
```

The learned system starts where deterministic Sensei ends.

It does not replace the court.

It brings the court better questions.
