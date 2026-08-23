# Phase 10.8 v2 — blinded human-adjudication package (#131, Step 10)

Frozen from the merged head. **Contains no labels.** Every judgment field in
`labels/` is null: protocol §14 reserves support labels, expected recall facts,
usefulness ratings and disagreement resolutions for a human, so they were not
generated.

## Provenance

| | |
|---|---|
| protocol | `phase10-reference-protocol-v2` |
| protocol document digest | `6afaf6ec1cebc73c72df43cb8c6e4842e2ea46356cc64ced61252a8ad620929d` |
| evaluator + world 1 pinned at | `ee3fda0a9dd853337b4edd987c7a9346b31ca3d7` |
| selection seed | `phase10-v2-pass1` (committed before any label existed) |
| captured-at | `2026-08-22T15:00:00Z` |

World bindings:

| world | repository | revision | observations |
|---|---|---|---|
| 1 sensei | `github.com/globulario/sensei` | `ee3fda0a9dd8` | 242,548 |
| 2 Globular | `github.com/globulario/Globular` | `48784d096039` | 20,168 |
| 3 **gin** | `github.com/gin-gonic/gin` | `34dac209ffb6` | 16,504 |
| 4 mutant suite | synthetic, composed tree digest | — | 347 over 12 sites |

## The sample manifest has TWO digests. They are not interchangeable.

- **file digest** `04e74ffd08c1301f83a89632b8c757877b29882ea82e2de6e7a5b3c99f985871`
  — sha256 of the bytes on disk, listed in `DIGESTS.txt`.
- **declared identity** `1621f3e89e6829f2ab0efb330f04231177ece580b01a5c3f7cb32946bad29beb`
  — the manifest's own `digest_sha256` field, computed over its content. This is
  what every label file binds to and what §17 means by "sample manifest digest".

Quote the declared identity when recording what a score consumed.

## Contents

```
sample/sample-manifest.json    805 items, 37 strata (§15)
sample/blind/*.json            9 blinded views — what the adjudicator reads (§12)
labels/*.labels.json           9 empty containers, one record per item (§16)
                               container schema v2 — see "Container schema" below
adjudicator-overlap.json       §13 second-adjudicator subset, 150 items
worlds/*.json                  the four world reports and the run index
make-adjudication-package.py   regenerates labels/ and the overlap from the manifest
DIGESTS.txt                    sha256 of every file above
```

## Item counts — all 805 remain unlabelled

| world | precision | recall_unit | challenge | total |
|---|---|---|---|---|
| 1 sensei | 210 | 12 | — | 222 |
| 2 Globular | 210 | 12 | — | 222 |
| 3 gin | 210 | 7 | — | 217 |
| 4 mutant suite | 120 | 12 | 12 | 144 |
| **total** | **750** | **43** | **12** | **805** |

`adjudicator-overlap.json` marks 150 precision items (exactly 20% per world:
42/42/42/24) for a second adjudicator. The subset was selected from the frozen
manifest **before any label exists**, which is stronger than §13's requirement
that it be fixed before labels are compared — it cannot be chosen after the fact
to flatter an agreement rate.

## How to complete

1. Work from `sample/blind/*.json`. These hide the extractor/provider label and,
   in the challenge lane, the producing lane; the claim and its evidence anchors
   stay visible because §12 says blinding must never strip what makes an item
   judgeable.
2. Record each judgment in the matching `labels/<world>.<lane>.labels.json`
   record, keyed by `item_key`. Fill `adjudicator_id`, `label`,
   `evidence_ids_inspected`, `adjudicated_at`, `adjudicated_at_source`,
   `blinded_at_decision_time`, `required_correction`, and optionally
   `rationale` and `active_seconds`. On a challenge item also fill
   `action_taken`. Update `labelled_count`.
3. **Recall lanes first, and in order.** §12 requires Sensei's output for a
   recall unit to stay hidden until the expected facts are frozen. Write the
   expected facts into that unit's `expected_facts` — each entry `{"id": …,
   "state": expected_supported | expected_ambiguous | expected_outside_scope,
   "matched": null}` — before looking at what Sensei produced for it. Only
   afterwards set each `matched`. §5.2's primary recall is matched
   `expected_supported` facts over total `expected_supported` facts, so a unit
   with no expected set has no recall, and a set written after seeing Sensei's
   output is not a blind one.
4. For the 150 overlap items, have the second adjudicator label independently
   from the same blinded views. Do not reconcile in place: §13 requires both
   original labels preserved and any resolution recorded as its own decision.
   If no second adjudicator is available, record `second_adjudicator_unavailable`
   rather than manufacturing agreement.
5. A label file becomes immutable once it is part of a scored reference set.
   Corrections append a new reference-set version (§16).

## Container schema

The containers are **v2** (`sensei.eval_labels.v2`). v1 carried only
`label`, so §5.2's expected-fact set, §10's action field and §11's correction
flag had nowhere to be recorded, and three of the six metrics §4 calls out as
requiring human truth could not have been produced from a fully labelled set.

v2 adds `required_correction` and `active_seconds` to every record,
`expected_facts` to recall units, and `action_taken` to challenge items.

The change was made while `labelled_count` was 0 in all nine containers. §16
makes a label file immutable once scored and §21 freezes the protocol at the
first label, so this shape change cost nothing then and would have cost a new
reference-set version and re-adjudication of all 805 items later. Nothing else
moved: the sample manifest and blinded views are untouched, the item counts are
unchanged, `adjudicator-overlap.json` is byte-identical, and adding a field an
adjudicator fills in later cannot tell them anything, so the blinding order is
unaffected.

`cmd/eval-phase10-score` reads either schema and reports which metrics a given
container version can produce.

## Not yet computable — stated rather than left to surface during scoring

- **§18 model delta.** All 12 sampled challenge items came from the
  deterministic lane; zero model-lane items were drawn under this seed, and no
  model provider was bound for this run (`phase10_composition_model_bound`:
  `not_run`). The delta has no second population to compare against and should
  be reported unavailable, not as zero.
- **§17 reference-set digest.** Not frozen here, deliberately. It is
  content-addressed over the label file digests, which do not exist until the
  labels do. Freezing it now would content-address a set of empty containers.
  `cmd/eval-phase10-score` computes it at the moment a score consumes the set,
  over: protocol digest, the manifest's declared identity, the label file
  digests, the world binding digests, and `adjudicator-overlap.json`.
- **`briefing_and_impact_surfaces`.** Reported
  `unavailable_by_authority_model`, not failed: the operational surfaces need an
  admitted, current, published graph, and publishing a synthetic mutant domain is
  refused by registry admission. Neither wall was weakened to obtain a number.

## Replay identity

The evaluator was run twice from identical pinned inputs at `ee3fda0a`. All 17
protocol artifacts — four world reports, the run index, both composition arm
reports, the sample manifest and all nine blinded views — are byte-identical.

`run_envelope.json` is the only artifact that differs, in exactly 8 fields:
`started_at_unix`, five per-arm elapsed timings, `peak_heap_bytes` and
`total_allocated_bytes`. It records what a run cost, never what it concluded.
The difference was enumerated field by field rather than assumed.
