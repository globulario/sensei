# Prospective authoring-recall protocol v1

Status: protocol only, no answers

Refs #259. Sibling of `phase10-reference-protocol-v1.md`, and deliberately separate from it.

This document defines how the human applicability reference for prospective authoring recall is selected, adjudicated, frozen, and scored. It contains **no expected facts, applicability labels, answer keys, scores, or interpretation of Sensei output**.

It exists to prevent the system from deciding which of its own knowledge was applicable.

## 1. What is being measured, and why it is not extraction precision

Phase 10.8 measures what Sensei can **recover** from bounded evidence. This protocol measures a different faculty: whether Sensei **surfaces already-known architectural law at the moment a change is being authored**.

The two can diverge completely. During the #258 review loop the governing law was already present in the graph, and structurally similar violations were still introduced and caught by independent review rather than by authoring-time awareness. Preflight over newly introduced files reported degraded or absent anchors.

That observation is a hypothesis, not a finding:

> prospective recall may degrade sharply at newly-created or otherwise unanchored seams, even when the governing law is already known elsewhere in the graph.

It is written down here so that it can be falsified, and so that a result which contradicts it is still reportable.

### The retrieval question

> I know nothing ABOUT this file yet. But what do I know that could GOVERN a file being created here, with these imports, in this package, touching this contract, next to these components?

This is not `BY_FILE`. A protocol that scored `BY_FILE` would measure a query that cannot answer the question, and would report the wrong faculty as failing.

## 2. Authority boundary

This is an evaluation instrument. It may not:

- rewrite authored awareness;
- promote candidates;
- weaken invariants or precision accounting to raise recall;
- become admission permission;
- add benchmark-only retrieval channels that production Sensei cannot use;
- be generated from Sensei output and then used as truth about that same output.

A low score is a finding about calibration. It is **not** permission to change production retrieval so that a benchmark turns green. Any production follow-up is filed separately, after measurement.

## 3. Freeze order

```text
protocol v1 merged
      |
      v
pinned changes + world binding
      |
      v
frozen candidate inventory (per stratum, content-addressed)
      |
      v
deterministic sample manifest (strata A/B/C/D)
      |
      v
eligible-knowledge corpus frozen
      |
      v
human applicability labels frozen
      |
      v
production retrieval run + scoring
      |
      v
interpretation
```

No later step may silently modify an earlier one after seeing a result.

The fifth arrow is the one that carries the whole protocol: **Sensei's authoring-time retrieval output must not exist, or must not be visible, when applicability is decided**. If the adjudicator reads what Sensei surfaced first, every miss becomes invisible, because the candidate answer has already framed what to look for.

A correction to protocol, sample, corpus, or labels creates a new version and invalidates downstream scores.

## 4. Unit of evaluation

One **proposed change**, evaluated from its pre-authoring or pre-merge state.

Applicability is a property of the change, not of the repository in the abstract. The same scar may be applicable to one change and irrelevant to another in the same file.

Each pinned change must carry:

- the exact diff or new-file contents;
- the exact base revision or tree digest it applies to;
- the repository domain;
- the graph generation/status available at that moment.

A change that cannot state what the graph knew when it was authored cannot be scored, because prospective recall is a claim about that moment.

## 5. Strata

Strata are frozen **before labels exist**, and reported separately. A macro average may be secondary; it may never be the headline, because a large anchored stratum would erase a weak new-seam one — which is precisely the effect this protocol exists to detect.

### A. New file / new architectural seam, no graph facts yet

The graph holds no facts about the file itself. Retrieval has only the proposed change plus repository context: package, directory, imports, owners, contracts, neighbours, global scars.

### B. Existing file, no usable anchors

The file already exists. It may have history, neighbouring relationships, imports, directory ownership and prior repository evidence, even though current preflight resolves no usable file anchors.

### C. Existing graph-anchored file

Sensei holds usable graph facts for the edited surface before the change.

### D. Mixed / cross-boundary change

One change touches both anchored material and a new or unanchored seam.

A and B are deliberately separate. Collapsing them would hide the single most actionable distinction available here: whether the problem is **creation-time context** or **missing file anchors generally**. If A and B differ, that difference is the finding.

## 6. The independent applicability reference

Prospective recall alone is gameable by flooding: a system that surfaces everything scores perfect recall. The denominator must therefore be external to the system being graded.

For each frozen sampled change:

1. freeze the exact proposed change and its world binding;
2. freeze the corpus of known scars, invariants and contracts eligible for adjudication;
3. a human adjudicator, **blind to Sensei's prospective retrieval output**, decides which eligible items are applicable to that exact change;
4. freeze those applicability labels;
5. only then run and expose Sensei's authoring-time retrieval;
6. compare surfaced items against the frozen applicability set.

Sensei must not define which items were applicable, generate the answer key, or resolve human disagreement.

### 6.1 Applicability labels

Each (change, eligible item) pair an adjudicator considers receives exactly one label:

- `applicable` — this item governs this change, and an author who did not see it could plausibly violate it;
- `not_applicable` — this item does not govern this change;
- `ambiguous` — the adjudicator cannot decide whether it governs without knowing something the frozen evidence does not settle;
- `outside_scope` — the item is not the kind of thing this experiment is measuring;
- `cannot_adjudicate` — an explicit decision that the frozen evidence does not settle the question.

Only `applicable` enters the recall denominator. Every other label is still reported as a count and a rate.

`ambiguous` and `cannot_adjudicate` are separate on purpose, as they are in the extraction protocol: the first says the item might govern and the evidence stops short, the second is a considered judgement that the question cannot be answered at all. Collapsing either into `not_applicable` would silently shrink the denominator and inflate recall, which is the cheapest available way to make this instrument lie.

### 6.2 The eligible corpus is part of the ruler

The corpus is frozen by digest before labels exist. It bounds what an adjudicator could have marked applicable, so a recall denominator computed against a different corpus is a different experiment.

Growing the corpus later — even by adding genuinely relevant law — creates a new reference-set version. It does not correct the old one.

## 7. Metrics

### 7.1 Applicable-item prospective recall

```text
human-adjudicated applicable known items surfaced
--------------------------------------------------
human-adjudicated applicable known items
```

Reported **per stratum first**.

### 7.2 Nuisance / over-alert rate

The denominator is the **resolved** labels only:

```text
surfaced items labelled not_applicable
--------------------------------------------------------
surfaced items labelled applicable + labelled not_applicable
```

The restriction is load-bearing, and an earlier draft of this document got it wrong in a way worth recording. Writing the denominator as "surfaced items adjudicated" admits `ambiguous`, `outside_scope` and `cannot_adjudicate` into the bottom while they can never reach the top. A retriever could then drive its reported nuisance rate **downward by flooding**, provided the extra alerts were unresolvable rather than clearly inapplicable — defeating the one check this metric exists to perform.

So three numbers are reported, never one:

- **primary nuisance**, over resolved labels only, as above;
- **unresolved surfaced rate**: `(ambiguous + cannot_adjudicate + outside_scope) / all surfaced items`, which is what a flooding strategy actually inflates;
- **conservative nuisance**, counting every unresolved surfaced item as nuisance — the upper bound on how much noise the author was asked to absorb.

A run where primary nuisance is low and conservative nuisance is high has not demonstrated precision. It has demonstrated that most of what it surfaced could not be judged, and the two must never be collapsed into one headline.

Recall and nuisance are reported together, always, and neither may be presented alone. High recall bought with high nuisance means the system can recover relevant law only by flooding the author, which is a different result from recovering it precisely — and an operator experiences the two very differently.

### 7.3 Retrieval status truthfulness

For each change, record what production retrieval reported about itself: `resolved`, degraded, no anchors, unavailable. A miss accompanied by an honest `degraded` is a different defect from a miss reported as a confident empty result, and only the second also launders absence into apparent coverage.

### 7.4 Context availability

Record which context classes were actually available to production retrieval before it ran: change contents, package/module identity, imports, directory ownership and risk classification, neighbouring anchored components, touched contracts, global scars, history.

This is descriptive. It exists so a low stratum-A score can be attributed to missing context rather than to reasoning, or shown not to be.

## 8. Sampling

### 8.1 The candidate inventory is frozen first

A stable hash makes selection unbiased **within an already-fixed population**. It says nothing about how that population was assembled, so a curator could choose a favourable set of changes and then comply with the hashing rule exactly. The seed would be honest and the result still biased.

Before any seed is applied, therefore:

1. the **complete** candidate-change inventory is enumerated from a stated, mechanical rule — for example every change in a bounded commit or PR range against the pinned repository — not from a hand-picked list;
2. eligibility and exclusion rules are written down before the inventory is built, and each exclusion is recorded **with its reason and count**, so a shrinking population is visible rather than silent;
3. each change is assigned to exactly one stratum by a stated rule applied to its pre-authoring state;
4. the per-stratum inventory is content-addressed and its digest published in the sample manifest.

Only then is the seed applied.

A recall figure computed over an inventory whose digest is not published is not reproducible, and cannot be defended against the objection that the population was chosen after the fact. Changing the inventory — including by widening the commit range — creates a new sample version.

This is the same discipline the extraction protocol applies to its recall units, and for the same reason: whoever gets to decide what enters the denominator decides the score.

### 8.2 Drawing from the frozen inventory

Sample independently by stratum, never uniformly over changes.

For each stratum, target up to 12 changes for v1. If a stratum has fewer, use all and report the smaller denominator rather than borrowing from another stratum to reach a round number.

Selection uses a stable hash of a committed seed and the change identity, sorted ascending. The seed is committed in the sample manifest before labels exist. Changing it creates a new sample version.

The selection may not see whether Sensei did well on a change. Sampling changes because they look interesting is how a calibration run becomes an anecdote.

## 9. Blinding

- **Applicability**: Sensei's surfaced set stays hidden until the applicability labels are frozen. This is not a courtesy; it is the difference between measuring recall and confirming output.
- **Nuisance**: judged over the surfaced set, so it is necessarily unblinded to the fact that Sensei surfaced the item. The adjudicator judges applicability to the change, not whether Sensei was right to surface it.
- Where an item's provenance could bias applicability judgement, it is withheld until the label is recorded.

## 10. Second adjudicator

At least 20% of applicability items should be independently adjudicated by a second human when one is available. The overlap subset is selected deterministically before either adjudicator's labels are compared.

Report raw agreement, disagreement counts by label pair, and resolved versus unresolved disagreements. Do not overwrite one adjudicator with the other; a resolution is a separate recorded decision preserving both original labels.

If only one human is available, record `second_adjudicator_unavailable` rather than manufacturing agreement through an AI reviewer.

## 11. AI use during adjudication

An AI system, Sensei included, may help navigate or retrieve frozen evidence only if its output is treated as a pointer and the human verifies the underlying source.

An AI system must not:

- decide which items are applicable;
- generate the eligible corpus and then be scored against it;
- resolve adjudicator disagreement;
- create the answer key and then grade itself with it.

## 12. Reporting

Every report must expose:

- per-stratum recall and all three nuisance numbers, with A and B never merged;
- the candidate-inventory digest per stratum, with exclusion counts and reasons;
- sample sizes, excluded and ambiguous counts per stratum;
- macro summary with each stratum still visible;
- exact change, world, corpus and reference-set identities;
- second-adjudicator status and agreement;
- retrieval status distribution;
- context classes available;
- examples of applicable items missed on A and B versus C and D.

Examples are frozen with the labels. Selecting illustrative misses after seeing the scores is editing the answer key with extra steps.

## 13. Interpretation boundary

These are different findings and must not be collapsed into one score:

| pattern | supported reading |
|---|---|
| high C, low A | a **new-seam coverage** problem, not generic retrieval failure |
| low across all strata | a broader prospective-relevance problem |
| high recall, high nuisance | law is recoverable only by flooding the author |
| low primary nuisance, high conservative nuisance | most of what was surfaced could not be judged; precision is unproven, not demonstrated |
| A and B differ | the problem is creation-time context, not missing anchors generally |
| A and B alike, both low | the problem is missing file anchors generally |

A negative result is preserved and reported. The purpose of this instrument is to find out whether the hypothesis in section 1 is true, not to confirm it.

## 14. Relationship to #131 and #258

This protocol is separate from `phase10-reference-protocol-v1.md` and does not modify it.

#258 closed the Phase 10.8 measuring instrument for extraction. This measures an authoring-time faculty that extraction metrics can miss entirely, and it must not be folded into that ruler — a single blended score would let strong extraction hide weak prospective recall, which is the exact failure that motivated this issue.
