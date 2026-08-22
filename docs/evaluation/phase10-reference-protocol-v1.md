# Phase 10.8 external reference protocol v1

Status: protocol only, no answers

Refs #131.

This document defines how the human reference sets for Phase 10.8 are selected, adjudicated, frozen, and scored. It intentionally contains **no expected facts, labels, answer keys, scores, or interpretation of Sensei output**.

The protocol exists to prevent the evaluator from grading itself.

## 1. Authority boundary

The reference set is external evaluation evidence. It is not canonical architecture and may not:

- rewrite authored awareness;
- promote candidates;
- weaken invariants;
- reinterpret owner verdicts;
- become admission permission;
- be generated from Sensei output and then used as truth about that same output.

A low score is a finding. It is not permission to edit the answer key or production behavior inside the scoring run.

## 2. Freeze order

The following order is mandatory:

```text
protocol v1 merged
      |
      v
pinned evaluation worlds
      |
      v
deterministic sample manifest
      |
      v
human adjudication files
      |
      v
reference-set digest frozen
      |
      v
scoring implementation/run
      |
      v
interpretation
```

No later step may silently modify an earlier one after seeing a score.

A correction to protocol, sample, or labels creates a new version and invalidates/recomputes downstream scores.

## 3. Evaluation worlds

The protocol consumes the exact worlds already bound by #131:

1. Sensei self-evaluation;
2. Globular runtime/historical evaluation;
3. independent SQLite calibration;
4. architectural mutant suite.

Every sampled item must carry the exact world binding used by the evaluator, including repository domain, revision/tree, graph status/digest where applicable, provider/profile identities, and the relevant source/evidence identities.

## 4. Metrics requiring human truth

The reference set exists for metrics that cannot be manufactured mechanically from grounding alone:

- grounded observation precision;
- grounded observation recall;
- unsupported/fabricated claim rate;
- contradiction-preservation rate;
- challenge/counterexample usefulness;
- operator review burden.

Mechanically decidable metrics such as dangling evidence references, replay identity, provider coverage, runtime, memory, and artifact size remain machine-measured and do not need human labels.

## 5. Separation of precision and recall

Precision and recall require different evidence procedures and must not share a shortcut.

### 5.1 Precision

Precision starts from a bounded Sensei-produced item and asks whether the claim is supported by the pinned evidence available to the adjudicator.

Allowed labels:

- `supported`;
- `unsupported`;
- `ambiguous`;
- `outside_scope`;
- `cannot_adjudicate`.

Only `supported` and `unsupported` enter the primary precision denominator. Every excluded label is still reported as a count and rate.

Primary precision:

```text
supported / (supported + unsupported)
```

Do not silently count ambiguity as either success or failure.

### 5.2 Recall

Recall **must not begin from Sensei output**.

For each selected bounded module/component, the human adjudicator first writes the expected architectural facts from the pinned source/document/history evidence without seeing Sensei's extraction for that unit.

Only after the expected set is frozen may Sensei output be compared with it.

Allowed expected-fact states:

- `expected_supported`;
- `expected_ambiguous`;
- `expected_outside_scope`.

Primary recall:

```text
matched expected_supported facts / total expected_supported facts
```

This blind-first procedure is load-bearing. If the adjudicator reads Sensei's output first, omissions become invisible because the candidate answer has already framed what to look for.

## 6. Provider-stratified precision sampling

Observation-count weighting is not the headline measure.

The current world-1 distribution is highly skewed, with `state_extractor` producing the majority of observations and architecture-relevant thin lanes such as `contract_extractor` producing far fewer. A uniform sample over raw observations would mostly measure the largest provider.

### 6.1 Primary sampling rule

Sample independently by provider.

For the first v1 calibration pass:

- target 30 observations per provider per applicable world when at least 30 exist;
- if a provider has fewer than 30 observations, sample all;
- for a thin provider with at most 200 observations in a world, the protocol permits sampling all if adjudication cost is acceptable;
- never reduce a thin architecture-relevant provider merely because a large provider dominates the corpus.

The exact selected IDs are determined by stable hashing, not manual cherry-picking.

### 6.2 Stable selection

For each `(world binding, provider id, observation id)` compute a stable selection key from a committed seed and sort ascending. Take the first N required by the stratum.

The seed is committed in the sample manifest before labels exist.

Changing the seed creates a new sample version.

### 6.3 Headline aggregation

Primary headline precision is the **macro average across provider strata** that have adjudicable samples.

Report alongside it:

- every provider's own precision;
- micro/observation-weighted precision as a secondary metric;
- sample size and excluded-label counts per provider.

A high-volume provider must not mathematically erase a weak thin provider.

## 7. Recall unit selection

Recall is evaluated over bounded units rather than individual extracted observations.

A unit should be small enough for a human to understand from pinned evidence without relying on Sensei output, for example:

- package/module;
- service/component;
- bounded cross-component interaction;
- documented architectural decision with implementation surface.

Selection must be deterministic from an independently defined inventory, not chosen because Sensei did well or poorly there.

For each world, v1 targets up to 12 bounded units. If fewer than 12 independently meaningful units exist, use all and report the smaller denominator.

The unit inventory and selection keys are frozen before expected facts are written.

## 8. Contradiction-preservation set

A contradiction case requires at least two pinned evidence items that genuinely disagree or remain unresolved.

The human reference must describe the expected **epistemic state**, not force a single winner where the evidence does not establish one.

Allowed expected states:

- `contradiction_must_be_preserved`;
- `one_side_superseded_with_authoritative_evidence`;
- `insufficient_to_resolve`.

Primary contradiction-preservation rate:

```text
cases where Sensei preserves the required epistemic state / adjudicable contradiction cases
```

A system that confidently chooses one side of unresolved evidence fails this metric even if the chosen side later turns out to be correct by coincidence.

## 9. Unsupported/fabricated claim rate

Every adjudicated candidate/model-derived claim is classified using the precision labels above.

Primary unsupported rate:

```text
unsupported / (supported + unsupported)
```

Report deterministic and model-derived lanes separately.

Do not let a model-assisted lane improve recall while hiding an increased unsupported-claim rate inside a combined score.

## 10. Challenge and counterexample usefulness

Usefulness is a human-operational metric, not architectural authority.

Each sampled challenge/counterexample receives one rating:

- `0` no useful action or question;
- `1` weak/redundant but related;
- `2` useful, would cause a meaningful check;
- `3` high-value, exposes a plausible missed risk/assumption.

Also record whether the challenge caused:

- no action;
- evidence lookup;
- code/test inspection;
- correction of a candidate conclusion;
- escalation to a human authority decision.

Report distribution, median, and mean. Do not compress usefulness into pass/fail alone.

## 11. Operator burden

For each adjudication batch record:

- number of items reviewed;
- number requiring evidence lookup;
- number requiring correction;
- number marked ambiguous/cannot-adjudicate;
- active review time if the adjudicator can record it reliably.

Primary burden measures:

- corrections per 100 reviewed items;
- evidence lookups per 100 items;
- ambiguous/cannot-adjudicate rate;
- optional median active seconds per item.

Timing is secondary because human timing is noisy. Do not let missing timing invalidate otherwise useful adjudication.

## 12. Blinding rules

### Precision

Where practical, hide the originating extractor/provider label from the adjudicator until after the support label is recorded. The claim and evidence anchor remain visible because they are necessary to judge support.

### Recall

Sensei output for the selected unit must remain hidden until expected facts are frozen.

### Optional model arm

When comparing deterministic vs model-assisted output, the adjudicator should not be told which lane produced an item during first-pass support labeling if the artifact format allows it without losing required provenance. Provenance is revealed after labeling for analysis.

Blinding is an anti-bias tool, not a reason to strip evidence needed for adjudication.

## 13. Second-adjudicator check

At least 20% of human-labeled precision and contradiction items should be independently adjudicated by a second human when a second adjudicator is available.

The overlap subset is selected deterministically before either adjudicator's labels are compared.

Report:

- raw agreement rate;
- disagreement counts by label pair;
- resolved vs unresolved disagreements.

Do not silently overwrite one adjudicator with the other. A resolution, if performed, is a separate recorded decision with both original labels preserved.

If only one human is available, record `second_adjudicator_unavailable` rather than manufacturing agreement through an AI reviewer.

## 14. AI use during adjudication

An AI system, including Sensei itself, may be used to navigate or retrieve pinned evidence only if its retrieval output is treated as a pointer and the human verifies the underlying source.

An AI system must not:

- generate the expected recall facts;
- assign support/unsupported labels;
- resolve adjudicator disagreements;
- create the answer key and then score itself against it.

The reference set is human adjudication evidence.

## 15. Sample manifest

The frozen sample manifest is machine-readable and content-addressed. Each row/item must include at least:

- schema version;
- protocol version/digest;
- committed selection seed;
- world ID;
- exact world binding digest/reference;
- metric lane (`precision`, `recall_unit`, `contradiction`, `challenge`);
- provider ID where applicable;
- sampled observation/candidate/challenge/unit identity;
- deterministic selection key;
- source/evidence identities required for adjudication;
- blind payload digest if a blinded view is materialized.

The manifest contains **selection**, not answers.

## 16. Human label files

Human labels live separately from the sample manifest.

Each label record carries:

- sample item identity;
- adjudicator identity or stable pseudonymous ID;
- label/rating;
- evidence identities actually inspected;
- optional short rationale;
- adjudication timestamp source;
- whether the item was blinded at decision time;
- optional disagreement-resolution link.

The label file is immutable once included in a scored reference-set version.

Corrections append a new reference-set version; they do not rewrite published evidence invisibly.

## 17. Reference-set identity

A reference-set release is content-addressed over:

- protocol digest;
- sample manifest digest;
- label file digests;
- world binding digests;
- adjudicator-overlap manifest where applicable.

Scores must record the exact reference-set digest they consume.

Two score reports using different reference-set digests are not directly replay-identical even if they carry the same human-readable version label.

## 18. Optional-model delta

The optional model is additional derived evidence, never replacement deterministic truth.

Score three views separately:

1. deterministic lane;
2. model-derived additions;
3. operator-visible combined result.

The primary model delta is reported as differences, not as a new canonical score:

- precision delta;
- recall delta;
- unsupported-claim-rate delta;
- contradiction-preservation delta;
- challenge-usefulness delta;
- operator-burden delta.

The deterministic baseline used for the comparison must be the exact baseline bound into the model acquisition bundle.

A model run against a different deterministic baseline is a different experiment.

## 19. Frozen model acquisition

A live model call is nondeterministic acquisition, not deterministic replay.

For each scored model-assisted run, freeze a content-addressed acquisition bundle containing at least:

- exact deterministic baseline identities/digests;
- exact `ModelBinding`;
- exact request digest;
- accepted artifact digest and artifact bytes;
- provider/model identity;
- nondeterminism declaration;
- pinned world/reference inputs needed for scoring.

Do not commit secrets, credentials, authorization headers, or provider-private transport metadata.

Scoring over the same frozen acquisition bundle and reference-set digest must be byte-identical.

Re-running the live model may yield a new acquisition digest. That is a new run, not a failed replay, provided nondeterminism was declared honestly.

## 20. Scoring report requirements

Every final report must expose enough denominator detail to prevent flattering aggregation.

At minimum include:

- reference-set digest;
- exact world/run authority binding;
- per-provider precision sample sizes and scores;
- macro and micro precision;
- recall by selected unit and aggregate;
- unsupported claim rate;
- contradiction-preservation counts;
- challenge usefulness distribution;
- operator burden measures;
- model-assisted deltas where a real model acquisition exists;
- excluded/ambiguous/cannot-adjudicate counts;
- second-adjudicator agreement or typed absence;
- deterministic replay identity;
- runtime/memory/artifact-size metrics from the existing mechanical lane.

No single aggregate score is sufficient evidence for #131 completion.

## 21. Protocol-change rule

Once the first human label is recorded, this v1 protocol is frozen for that reference-set version.

A material protocol change requires:

1. new protocol version;
2. new protocol digest;
3. regenerated sample manifest if selection semantics changed;
4. re-adjudication where label semantics changed;
5. fresh score reports.

Do not patch a scoring rule after seeing an uncomfortable result and preserve the old version label.

## 22. Completion statement

This protocol is complete when it can produce an answer key without consulting the system being graded, preserve uncertainty and disagreement instead of erasing them, and bind every score to exact human and repository evidence.

Merging this document is **not** evidence that #131 is complete. It is the prerequisite that makes later human calibration evidence trustworthy.
