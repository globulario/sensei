# Prospective authoring-recall reference set v1

**Status: frozen, pre-label. No applicability labels exist and no retrieval has been run.**

Refs #259. Produced by `cmd/eval-prospective freeze` under
`docs/evaluation/prospective-recall-protocol-v1.md`
(`sha256:ade91a42c8c0c421d0e6ba84ce2a547712c248b7cfef491ecc6ff8b12ff90d8d`).

This is the artifact a human adjudicates against. It contains no answer key, and
nothing in it may decide which known law was applicable to which change.

## Identity

| | |
|---|---|
| world | `world1_sensei_self` — `github.com/globulario/sensei` |
| pinned revision | `eac9603e332e2393815fb702c7aa1a105302ee20` |
| graph digest | `def94857a06a997412c56c682c39481b226f1834f93a4173425852965367b912` |
| selection seed | `259-v1` |
| sample manifest | `a6fc72d75ef6fc080f129ed2de06c85742a35617487321f041904ac48d8c0364` |
| blind corpus | `6334f400bcb805f6787c353a280859753a74390dce2a53fd2c389a4aaedbdfe4` |
| retrieval surface | `sensei.preflight.file_and_task.v1` |

Changing any of these produces a different reference set, not a corrected one.

## Files

| file | what it is |
|---|---|
| `sample-manifest.json` | the frozen selection: strata, per-stratum population digests, items, exclusion counts, the retrieval-surface decision |
| `blind-corpus.json` | **what an adjudicator reads.** 866 eligible items, exactly `id`/`class`/`title`/`statement` |
| `packages/*.json` | 48 adjudication packages, one per sampled change: the change and its world binding, referencing the blind corpus by digest |
| `corpus.json` | the full frozen corpus — reproducibility evidence only. **Not for adjudication:** it carries anchors, materialization provenance and per-class accounting |
| `inventory.json` | the complete classified candidate population at the pinned revision |
| `anchor-index.json` | the anchored-path set the strata were cut with, and how it was produced |
| `overlap-subset.json` | the deterministic second-adjudicator subset, fixed before any label exists |
| `DIGESTS.txt` | ledger of file-byte digests — verify with `sha256sum -c DIGESTS.txt` |

## What an adjudicator may see

`packages/*.json` and `blind-corpus.json`. Nothing else.

The blind corpus omits each item's anchors **by construction** rather than by
blanking them. Anchors are Sensei's own account of which files an item governs,
so showing them would hand over the answer key and turn applicability
adjudication into agreement with the system being graded. For the same reason a
package carries no stratum and no anchored/unanchored partition: both are
statements about what Sensei already knows.

`corpus.json` holds that withheld material and is kept separately as evidence
that the blind corpus is reproducible. It must not be opened while adjudicating.

## Population

```
983 candidate changes at the pinned revision, 151 excluded
  no_single_base_revision  150   (merge and root commits: no single pre-authoring state)
  no_paths_touched           1

866 adjudicable eligible items, 0 excluded, 0 beyond the query row cap
  invariant 292 · forbidden_fix 265 · meta_principle 164
  failure_mode 122 · contract 18 · incident_pattern 5

A_new_seam             209 population   12 sampled
B_unanchored_existing  361 population   12 sampled
C_anchored              38 population   12 sampled
D_mixed                375 population   12 sampled
```

Every class reconciles exactly against the graph's own total. 705 corpus items
were materialized from their own node; 161 carry only an IRI and a class in the
graph, and their statements are composed from the governing law that relates to
them — quoting governing classes only, never `source_file` or `test` relations.

## Second adjudicator

`second_adjudicator_unavailable`, recorded as protocol section 10 requires. The
overlap subset is fixed anyway, before any label exists, so it is available if a
second human becomes so. No AI may stand in for one, and none did.

## Next step

A human adjudicates applicability, blind, before any retrieval output exists or
is visible. Retrieval execution and scoring are Slice 2 and come after.
