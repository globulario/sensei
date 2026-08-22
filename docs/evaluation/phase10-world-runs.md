# Phase 10.8 evaluation world runs

Refs #131.

This records that every evaluation world #131 defines has now been run from pinned inputs by one reproducible command, and what those runs measured. It records **measurements only**. No score, no interpretation of whether Sensei did well, and no adjudicated fact appears here — those need the frozen reference set, which does not exist yet.

## The command

```bash
scripts/eval-phase10-worlds.sh <out-dir> <captured-at RFC3339> [selection-seed]
```

Worlds 2 and 3 had previously only ever been run by hand. A measurement that exists only as something somebody typed once is not the reproducible evidence #131 asks for, so the invocation lives in the script.

The script **clones** each external world rather than measuring the checkout in place. Both reasons matter:

- a working checkout carries untracked tooling (`.sensei/`, `.claude/`, an authored `docs/awareness/`), which makes the tree dirty; `eval-arms` then falls back to a tree digest, so the run identifies itself by a hash nobody can look up instead of by a revision anybody can check out;
- for world 3 that is worse than untidy. World 3 is the *independent* calibration, and Sensei-authored awareness sitting in the measured tree is exactly the "Sensei-specific ontology becoming the hidden answer key" the issue forbids.

## Worlds

| world | repository | why this repository |
|---|---|---|
| 1 | `github.com/globulario/sensei` | self-evaluation, measured in place |
| 2 | `github.com/globulario/Globular` | the runtime/historical world |
| 3 | `github.com/gin-gonic/gin` | independent calibration — see below |

### Why gin, and why not SQLite or Caddy

**SQLite cannot serve as world 3.** The extraction lane is Go-only (`gosemantics`). Pointed at a C repository it runs and reports zero observations with the honest note *"the repository holds no files in the Go observation surface; the empty result describes the surface, not the repository."* That behaviour is correct, but the result is a world that measures nothing rather than an independent calibration. Verified by running it, not inferred.

**Caddy was rejected too**, despite being Go. Its checkout carries an `awareness: onboard AWG + close invariant→test gaps` commit — Sensei has already shaped the repository that was supposed to calibrate it independently.

**gin qualifies**: Go, independently maintained, and its HEAD is an upstream commit with no Sensei authorship in it.

## Run of 2026-08-22

Captured at `2026-08-22T10:00:00Z`, selection seed `phase10-v1-pass1`.

| world | revision | status | observations | receipts | files cited |
|---|---|---|---|---|---|
| 1 sensei | tree `0635d288ce83` | unavailable | 241,776 | 241,775 | 1,541 |
| 2 Globular | `48784d096039` | resolved | 20,168 | 20,167 | 170 |
| 3 gin | `34dac209ffb6` | resolved | 16,504 | 16,504 | 97 |

World 1 reports a tree digest rather than a revision because the working tree was dirty when the run was made — the script it invokes was still uncommitted. That is the binding working as intended: a dirty tree is not the commit it names, and the run says so rather than claiming a revision it did not measure.

### Mechanically decidable integrity

These need no adjudicator and are reported as measured:

| world | absence claims | unexplained | searched-without-proof | anchors resolved | unresolved |
|---|---|---|---|---|---|
| 1 | 0 | 0 | 0 | 241,775 | 1 (`no_anchor`) |
| 2 | 0 | 0 | 0 | 20,167 | 1 (`no_anchor`) |
| 3 | 0 | 0 | 0 | 16,504 | 0 |

No observation in any world cites a file that is missing, is not a regular file, or names a line range outside its file. World 2 carries 190 non-blocking limitations; worlds 1 and 3 carry none.

### Frozen sample

661 items across 30 strata (three worlds × seven provider strata plus three lanes).

| lane | strata | outcome |
|---|---|---|
| precision | 21 | sampled |
| recall_unit | 3 | 2 sampled, 1 `sampled_all_available` |
| contradiction | 3 | `population_empty` — undrawn, awaiting a functional-predicate declaration |
| challenge | 3 | `population_empty` — the deterministic lane produced no counterexample or candidate question |

The contradiction lane is **undrawn, not clean**. Nobody has declared which predicates may hold a single object, so no pair of observations can be shown to disagree rather than to describe a multi-valued relation.

## Reproducibility

Worlds 2 and 3 produce byte-identical report digests across independent runs (`2713b450cf0a`, `7fb6f2566a8c`), because their pinned inputs did not change between them. World 1's digest differs between runs made against different working trees, which is the same property seen from the other side: the report tracks what was measured.

## Deliberately not committed here

The derived artifacts — world reports, sample manifest, blinded adjudication views — are regenerable by the single command above and are not checked in.

Two reasons. World 1's report describes whatever tree it was run against, so a committed copy is stale the moment the repository changes. And the sample's selection seed must be **committed before labels exist**; choosing that seed is the evaluation owner's call, not the harness's. A seed committed here would pre-empt it, and changing it later creates a new sample version rather than a correction.

## What this does not establish

Nothing about precision, recall, grounding quality, or whether any observation is true. Those require the human reference set (protocol §6, §14), which forbids Sensei from producing it. This page establishes only that the worlds run, from inputs that can be named, by a command anyone can re-run.
