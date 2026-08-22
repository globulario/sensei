# Phase 10.8 evaluation world runs

Refs #131.

This records how the evaluation worlds are run from pinned inputs by one reproducible command, what those runs measured, and why world 3 is currently blocked on a decision that is not the harness's to make.

It records **measurements only**. No score, no interpretation of whether Sensei did well, and no adjudicated fact appears here — those need the frozen reference set, which does not exist yet.

## The command

```bash
GLOBULAR_SRC=<checkout> GLOBULAR_REV=<commit> \
  scripts/eval-phase10-worlds.sh <out-dir> <captured-at RFC3339> [selection-seed]
```

Worlds 2 and 3 had previously only ever been run by hand. A measurement that exists only as something somebody typed once is not the reproducible evidence #131 asks for, so the invocation lives in the script.

### Three things the script refuses

**An output directory inside this checkout.** `eval-arms` materializes synthetic Go mutant repositories under `<out>/mutants`, and world 1 measures this repository by recursively scanning its `.go` files. An output directory inside the tree feeds the harness's own generated mutants into the self-evaluation and dirties the very tree world 1 is trying to bind. Refused rather than silently relocated: a run that quietly writes somewhere other than where it was told is its own defect.

**An external world with no pinned revision.** `git clone` alone takes whatever the source HEAD happens to name, so the same command with the same seed would silently evaluate a different world once that checkout advanced. Recording the revision afterwards does not make it a pinned input — it only makes the drift legible after the fact. The revision is required, checked out, and verified against what `HEAD` resolved to.

**A revision the source does not contain.** Fails loudly rather than falling back to HEAD.

### Why the external worlds are cloned

A working checkout carries untracked tooling (`.sensei/`, `.claude/`, an authored `docs/awareness/`), which makes the tree dirty; `eval-arms` then falls back to a tree digest, so the run identifies itself by a hash nobody can look up instead of a revision anybody can check out. A clean clone at a pinned commit carries only what upstream tracks.

## World 3 is blocked, and this script will not unblock it by itself

The frozen protocol names world 3 as the **independent SQLite calibration**.

There is a measured obstacle to running it as written. The extraction lane is Go-only (`gosemantics`). Pointed at a C repository it runs and reports zero observations with the honest note *"the repository holds no files in the Go observation surface; the empty result describes the surface, not the repository."* That behaviour is correct; the result is a world that **measures nothing** rather than an independent calibration. Verified by running it against a C repository, not inferred from reading the code.

**The script does not substitute another repository on its own authority.** `eval-arms` stamps the v1 protocol digest into every sample manifest, so a run that quietly swapped world 3 would produce samples claiming compliance with a world definition they did not follow. Protocol §3 is explicit that a correction creates a new version rather than a silent amendment, and §2 forbids modifying the evaluation to make a result obtainable.

So until a protocol version names a reachable world 3, `eval-arms` records it as `not_run` with a reason — which is the honest state, and visibly incomplete rather than quietly complete.

### What a decision would need to weigh

Candidate replacements are not interchangeable:

- **Caddy** is Go but is contaminated: its checkout carries an `awareness: onboard AWG + close invariant→test gaps` commit, so Sensei has already shaped the repository that was supposed to calibrate it independently.
- **gin** is Go, independently maintained, and its HEAD is an upstream commit with no Sensei authorship in it. It measures cleanly — a trial run produced 16,504 observations over 97 files at revision `34dac209ffb6`, with every anchor resolving.

An operator who has made that decision can bind a world explicitly with `WORLD3_SRC`, `WORLD3_REV`, `WORLD3_DOMAIN` and `WORLD3_NAME`. The default name is `world3_operator_bound`, which deliberately does **not** claim the v1 world-3 slot; claiming it requires amending the protocol so the manifest's stamped digest and its world definition agree.

## Run of 2026-08-22

Captured at `2026-08-22T10:00:00Z`, selection seed `phase10-v1-pass1`, world 2 pinned to `48784d096039`.

| world | revision | status | observations | receipts | files cited |
|---|---|---|---|---|---|
| 1 sensei | tree digest | unavailable | 241,776 | 241,775 | 1,541 |
| 2 Globular | `48784d096039` | resolved | 20,168 | 20,167 | 170 |
| 3 | — | `not_run` | — | — | — |

World 1 reports a tree digest rather than a revision because the working tree was dirty when the run was made — the script it invokes was still uncommitted. That is the binding working as intended: a dirty tree is not the commit it names, and the run says so rather than claiming a revision it did not measure.

### Mechanically decidable integrity

These need no adjudicator and are reported as measured:

| world | absence claims | unexplained | searched-without-proof | anchors resolved | unresolved |
|---|---|---|---|---|---|
| 1 | 0 | 0 | 0 | 241,775 | 1 (`no_anchor`) |
| 2 | 0 | 0 | 0 | 20,167 | 1 (`no_anchor`) |

No observation in either world cites a file that is missing, is not a regular file, or names a line range outside its file. World 2 carries 190 non-blocking limitations; world 1 carries none.

### Frozen sample

444 items across 20 strata (two worlds × seven provider strata plus three lanes).

| lane | outcome |
|---|---|
| precision | sampled, 30 per provider per world |
| recall_unit | sampled |
| contradiction | `population_empty` — **undrawn, not clean** |
| challenge | `population_empty` — the deterministic lane produced no counterexample or candidate question |

The contradiction lane is undrawn because nobody has declared which predicates may hold a single object, so no pair of observations can be shown to disagree rather than to describe a multi-valued relation.

## Reproducibility

World 2 produces a byte-identical report digest (`2713b450cf0a`) across independent runs at the same pinned revision. World 1's digest differs between runs made against different working trees. Both halves are the same property seen from two sides: the report tracks exactly what was measured.

## Deliberately not committed here

The derived artifacts — world reports, sample manifest, blinded adjudication views — regenerate from the single command above and are not checked in.

World 1's report is stale the moment the repository changes, so a committed copy would describe a tree that is no longer there. And the sample's selection seed must be **committed before labels exist**, which makes choosing it the evaluation owner's call rather than the harness's; a seed frozen here would pre-empt it, and changing it later creates a new sample version rather than a correction.

## What this does not establish

Nothing about precision, recall, grounding quality, or whether any observation is true. Those require the human reference set (protocol §6, §14), which forbids Sensei from producing it. This page establishes only that the worlds it can reach run, from inputs that can be named, by a command anyone can re-run — and that world 3 is not one of them yet.
