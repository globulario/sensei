# Phase 10.8 scoring instrument — and a gap that closes at the first label

**Status: the scorer exists, the reference set is still unlabelled, and three
of the six human-truth metrics cannot be produced from the containers as they
stand.**

`docs/evaluation/phase10-v2-reference-set/` has been frozen since 2026-08-22:
805 items, 37 strata, nine label containers, all empty. Nothing read those
containers. `cmd/eval-phase10-score` now does, and implements protocol v2
sections 5, 8, 9, 10, 11, 13, 17 and 20.

Run it against the unlabelled set and it answers honestly:

```
$ eval-phase10-score --reference-set docs/evaluation/phase10-v2-reference-set
0 of 805 sampled items carry a label.
…
| world1_sensei_self | … | `no_adjudicable_sample` | … | `not_captured_by_label_container` |
```

Every rate with an empty denominator reports its availability rather than
`0.000`. A scorer that printed zero precision for an unlabelled reference set
would be describing the system it grades, not the ruler.

It also computes the section 17 reference-set digest, which the reference set's
README correctly declined to freeze at generation time — it content-addresses
over label file digests, so it means something only at the moment a score
consumes them. That is when this tool computes it.

## The gap

Three metrics protocol section 4 lists as *requiring human truth* have no field
to be recorded in:

| metric | protocol | what the container carries | what it needs |
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

The scorer reads all three fields when present (`expected_facts`,
`action_taken`, `required_correction`, and an optional `active_seconds`), so a
container extended to carry them scores without further change. It reports them
as `not_captured_by_label_container` when absent, and lists each one in the
report's *metrics this reference-set version cannot produce* section rather than
omitting it — a metric that vanishes silently flatters by omission, which is the
failure section 20 exists to prevent.

## Why this is time-critical

Protocol §16: *"The label file is immutable once included in a scored
reference-set version."* §21: *"Once the first human label is recorded, this
protocol is frozen for that reference-set version."*

Right now `labelled_count` is 0 in all nine containers, so extending the
container schema costs nothing: `make-adjudication-package.py` regenerates them
from the manifest, the sample manifest and the blinded views are untouched, and
no answer key exists to invalidate. The blinding order is unaffected — adding a
field an adjudicator fills in later cannot tell them anything.

After the first label, the same change requires a new reference-set version and
re-adjudication of 805 items.

**This is the evaluation owner's decision, not the harness's.** It changes what
the ruler can measure, and §21 puts protocol changes with the owner. The scorer
is written to work either way: extend the containers and it reports recall,
actions and corrections; leave them and it reports precisely which metrics the
run cannot produce and why.

## What this does not establish

Nothing about precision, recall, grounding or usefulness. Those need the human
labels, which do not exist. This document establishes only that the instrument
that will consume them exists, was written while every container was still
empty, and reports the shape of its own blind spots.
