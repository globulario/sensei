# Phase 10.8 scoring instrument — and the gap it found in time

**Status: the scorer exists, the reference set is still unlabelled, and the
label containers were extended — while every one of them was still empty — to
hold the three human-truth metrics they had nowhere to record.**

`docs/evaluation/phase10-v2-reference-set/` has been frozen since 2026-08-22:
805 items, 37 strata, nine label containers, all empty. Nothing read those
containers. `cmd/eval-phase10-score` now does, and implements protocol v2
sections 5, 8, 9, 10, 11, 13, 17 and 20.

Run it against the unlabelled set and it answers honestly:

```
$ eval-phase10-score --reference-set docs/evaluation/phase10-v2-reference-set
0 of 805 sampled items carry a label.
…
| world1_sensei_self | … | `no_adjudicable_sample` | … | `no_adjudicable_sample` |
```

Every rate with an empty denominator reports its availability rather than
`0.000`. A scorer that printed zero precision for an unlabelled reference set
would be describing the system it grades, not the ruler.

It also computes the section 17 reference-set digest, which the reference set's
README correctly declined to freeze at generation time — it content-addresses
over label file digests, so it means something only at the moment a score
consumes them. That is when this tool computes it.

## The gap that was found, and closed in time

Three metrics protocol section 4 lists as *requiring human truth* had no field
to be recorded in:

| metric | protocol | what container v1 carried | what it needed |
|---|---|---|---|
| grounded observation **recall** | §5.2 | one `label` per recall unit | the frozen expected-fact set, each fact's state, and whether Sensei matched it |
| challenge **action** distribution | §10 | `label` (the 0–3 rating) | whether the challenge caused no action, an evidence lookup, code inspection, a correction, or an escalation |
| corrections per 100 items | §11 | nothing | whether an item required correction |

Recall is the serious one. §5.2 defines primary recall as

```
matched expected_supported facts / total expected_supported facts
```

and the container holds a single label per unit. There is nowhere to write the
expected facts, which means there is nowhere to write the thing §5.2 makes
load-bearing: **the human's expected set, frozen before Sensei's output for that
unit is visible.** Without it, "recall" would have to be reconstructed from
whatever the adjudicator happened to put in `rationale` — prose, unscoreable,
and no longer blind-first in any checkable way.

## Why it had to be now

Protocol §16: *"The label file is immutable once included in a scored
reference-set version."* §21: *"Once the first human label is recorded, this
protocol is frozen for that reference-set version."*

`labelled_count` was 0 in all nine containers, so the change cost nothing:
`make-adjudication-package.py` regenerates them from the manifest. After the
first label the same change would have required a new reference-set version and
re-adjudication of all 805 items.

The evaluation owner made the call. Container schema **v2** adds
`required_correction` and `active_seconds` to every record, `expected_facts` to
recall units, and `action_taken` to challenge items.

Nothing else moved, and that is checkable rather than asserted:

- the sample manifest and all nine blinded views are byte-identical;
- `adjudicator-overlap.json` is byte-identical — the 150-item second-adjudicator
  subset did not shift;
- item counts are unchanged: 210/12, 210/12, 210/7, 120/12/12, totalling 805;
- `labelled_count` is still 0 everywhere;
- `sha256sum -c DIGESTS.txt` passes against the regenerated ledger.

Adding a field an adjudicator fills in later cannot tell them anything, so the
blinding order is unaffected.

## The scorer reads both container versions

v1 and v2 are different findings and the report keeps them apart:

```
v1 recall  → not_captured_by_label_container   (the ruler has no field for it)
v2 recall  → no_adjudicable_sample             (nobody has adjudicated it yet)
```

Attributing the first to the second would blame the ruler for the human's
pending work; the second to the first would blame the human for a schema gap.
A container version this scorer does not know is refused outright rather than
interpreted positionally.

Against the reference set today, every uncomputable metric is now either
`no_adjudicable_sample` (awaiting labels) or `not_sampled` (worlds 1–3 draw no
challenge lane; no contradiction lane or model lane was drawn under this seed).
Nothing is `not_captured_by_label_container` any more.

## What this does not establish

Nothing about precision, recall, grounding or usefulness. Those need the human
labels, which do not exist. This document establishes only that the instrument
that will consume them exists, was written while every container was still
empty, and reports the shape of its own blind spots.
