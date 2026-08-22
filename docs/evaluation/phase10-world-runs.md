# Phase 10.8 evaluation world runs

Refs #131.

This records how the evaluation worlds are run from pinned inputs by one reproducible command, what those runs measured, and why world 3 is currently blocked on a decision that is not the harness's to make.

It records **measurements only**. No score, no interpretation of whether Sensei did well, and no adjudicated fact appears here — those need the frozen reference set, which does not exist yet.

## The command

```bash
SENSEI_REV=$(git rev-parse HEAD) \
GLOBULAR_SRC=<checkout> GLOBULAR_REV=<commit> \
  scripts/eval-phase10-worlds.sh <out-dir> <captured-at RFC3339> [selection-seed]
```

**Every** world is pinned, world 1 included — and so is the **evaluator itself**. `eval-arms` is built from a clone at `SENSEI_REV` rather than from the live tree, because pinning the measured world while the measuring binary stayed mutable left the same asymmetry one level up: the same revision and seed could produce different reports with nothing recording why. A self-evaluation whose instrument is unpinned is not re-measurable. The run prints the evaluator's revision. An earlier version passed the live checkout straight through on the reasoning that world 1 *is* this repository — which was wrong in the way that matters: `worldBinding` records the revision only *after* extraction, so the tree could be anything and the report would simply describe whatever it found. The same command and seed evaluated a different world whenever the working tree moved. Naming `SENSEI_REV` makes the choice of tree an explicit act rather than something the run discovers afterwards.

Worlds 2 and 3 had previously only ever been run by hand. A measurement that exists only as something somebody typed once is not the reproducible evidence #131 asks for, so the invocation lives in the script.

### Six things the script refuses

**An output directory inside this checkout.** `eval-arms` materializes synthetic Go mutant repositories under `<out>/mutants`, and world 1 measures this repository by recursively scanning its `.go` files. An output directory inside the tree feeds the harness's own generated mutants into the self-evaluation and dirties the very tree world 1 is trying to bind. Refused rather than silently relocated: a run that quietly writes somewhere other than where it was told is its own defect.

**An external world with no pinned revision.** `git clone` alone takes whatever the source HEAD happens to name, so the same command with the same seed would silently evaluate a different world once that checkout advanced. Recording the revision afterwards does not make it a pinned input — it only makes the drift legible after the fact. The revision is required, checked out, and verified against what `HEAD` resolved to.

**A revision the source does not contain.** Fails loudly rather than falling back to HEAD.

**A non-empty output directory.** `eval-arms` writes what this run produced and leaves whatever a previous run left, so a rerun without a seed keeps the old `sample/` tree beside an index recording the sample as `not_run`, and a rerun omitting a world keeps that world's stale report beside the new one. An operator could then adjudicate blinded views this command never produced, against an index that does not describe them — and nothing in the artifacts would say so.

**An output path that only looks external.** The containment check canonicalizes with `pwd -P`: a logical path keeps a symlink intact, so a directory symlinked back into the checkout would have passed the test and then had mutants written through it into the tree world 1 scans.

**A sample drawn over an operator-bound world under the default protocol.** See world 3 below.

A linked checkout made by `git worktree add` is accepted: its `.git` is a file rather than a directory, so the source is validated with `git rev-parse --is-inside-work-tree` instead of a directory test, which would otherwise have recorded a world the caller did supply as `not_run`.

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

An operator who has made that decision can bind a world explicitly with `WORLD3_SRC`, `WORLD3_REV`, `WORLD3_DOMAIN` and `WORLD3_NAME`. The default name is `world3_operator_bound`, which deliberately does **not** claim the v1 world-3 slot.

Renaming alone would not have been enough, and an earlier version of this script stopped there. `eval-arms` samples every world that ran and stamps the protocol digest into the manifest, so a bound replacement would still have been sampled under a protocol specifying a different world — the name only tells a reader, while the digest is what the artifact asserts. So binding a world **and** drawing a sample now requires `PROTOCOL_FILE` to name the protocol that governs it. The script cannot check that a protocol's text says what its binder believes, but it can refuse to stamp v1 over a world v1 does not define.

`PROTOCOL_ID` travels with it, and neither moves alone. The manifest records both the identity and the digest; forwarding a new document under the old identity would produce a manifest naming v1 while carrying another protocol's digest — a ruler that misstates what it is, which is worse than one that refuses to be built.

## Run of 2026-08-22

Captured at `2026-08-22T10:00:00Z`, selection seed `phase10-v1-pass1`, world 2 pinned to `48784d096039`.

| world | status | observations | report digest |
|---|---|---|---|
| 1 sensei | resolved, at `SENSEI_REV` | 241,978 | `0f4c01faf023` |
| 2 Globular | resolved, `48784d096039` | 20,168 | `2713b450cf0a` |
| 3 | `not_run` | — | — |

No sample was drawn from this run — see below.

### Mechanically decidable integrity

These need no adjudicator and are reported as measured:

| world | absence claims | unexplained | searched-without-proof | anchors resolved | unresolved |
|---|---|---|---|---|---|
| 1 | 0 | 0 | 0 | 241,775 | 1 (`no_anchor`) |
| 2 | 0 | 0 | 0 | 20,167 | 1 (`no_anchor`) |

No observation in either world cites a file that is missing, is not a regular file, or names a line range outside its file. World 2 carries 190 non-blocking limitations; world 1 carries none.

### The sample was refused, and that is the correct outcome

No sample was drawn, and the earlier version of this page reported one. That report was wrong in exactly the way this document spends its length warning about.

The default protocol consumes **every** world in `requiredWorlds`. World 3 did not run, so a sample drawn here would have carried the v1 identity while following a reduced world definition — the same false claim as substituting a world, arrived at by omission rather than replacement. That is precisely why it looked harmless: nothing was swapped, something was simply missing, and the manifest would have said v1 regardless.

`eval-arms` refuses, and the refusal **fails the run**:

```
frozen_sample_manifest  published_domain  failed  (refusing to draw under the default
protocol while mutant suite (its material is not represented in the sample manifest;
the sampler draws only from checkout worlds), world3_independent_calibration did not
run: the manifest would claim an identity whose world definition this run did not
follow. Run the missing world(s), or bind a protocol that defines the reduced set with
--protocol-file and --protocol-id.)
```

The command exits non-zero. `failed` rather than `not_run` is deliberate: a seed is a request to draw, so refusing it is a failure of what was asked for. Reported as `not_run` it would have left automation with a successful command that produced no manifest — silence indistinguishable from success. A run that supplies no seed still reports `not_run`, because nothing was requested.

The refusal lives in `eval-arms`, not in the wrapper script, because a rule enforced only by a wrapper is a rule a direct caller does not have. The same applies to `--protocol-id` and `--protocol-file`: the tool refuses either one alone, since an identity beside another protocol's digest is a ruler that misstates itself.

### The mutant suite is now the fourth sampled world

The protocol consumes the mutant suite as world 4. Checking that its arms *ran* was never the same as sampling it, and for several revisions of this page a v1 manifest would have carried three worlds while naming a protocol that defines four.

The suite's observations now reach `evalsample.Build` — 347 observations across 12 defect sites, carried from the arm that already extracts over them rather than from a second extraction, so the sample and the report describe the same run. Its recall inventory is the **defect sites**: the files each mutant actually changed, taken from the mutant definitions rather than from what extraction happened to observe. That independence matters more here than anywhere else — a denominator built from observed paths could only contain sites extraction already reached, so a site it missed entirely would be unmeasurable, which is precisely what a mutant suite exists to detect.

It is not a checkout, so it carries no revision and is bound by the suite's own composed digest. `runWorlds` neither runs nor reports it; telling an operator it "needs an external checkout" would send them looking for a repository that does not exist.

### World 3 remains the one open decision

With worlds 1, 2 and 4 running, world 3 is the only thing standing between this and a v1 sample — and the harness will not resolve it on its own authority. Attempting to bind gin under the v1 name is refused:

```
world3_independent_calibration  failed  (the protocol names this world but no
upstream identity is registered for it, so a checkout cannot be shown to be it;
bind it as an operator world instead)
```

That refusal is the design working. Registering a guess would let an arbitrary tree pass as the SQLite calibration, and the identity of world 3 is exactly the question that has not been answered. Amending it is a protocol version, not a harness default.

### Names are not identities

Completeness compares each required world's **repository domain** against the domain the protocol names, not just its label. A caller could otherwise point three arbitrary Go checkouts at `world1_sensei_self`, `world2_globular` and `world3_independent_calibration` and satisfy the check with repositories the protocol never named. "You did not run it" and "you ran something else and called it that" are reported as different problems, because they need different corrections.

## Reproducibility

Two independent runs at the same pinned revisions produce byte-identical report digests for **both** worlds — `0f4c01faf023` and `2713b450cf0a`.

World 1's reproducibility is the part that had to be earned. While it was measured from the live tree, its digest changed between runs whenever anything in the repository moved, including the script's own uncommitted edits. Pinning it made the self-evaluation as re-measurable as the external worlds, which is what "exact pinned inputs" has to mean if it means anything.

## Deliberately not committed here

The derived artifacts — world reports, sample manifest, blinded adjudication views — regenerate from the single command above and are not checked in.

World 1's report is stale the moment the repository changes, so a committed copy would describe a tree that is no longer there. And the sample's selection seed must be **committed before labels exist**, which makes choosing it the evaluation owner's call rather than the harness's; a seed frozen here would pre-empt it, and changing it later creates a new sample version rather than a correction.

## What this does not establish

Nothing about precision, recall, grounding quality, or whether any observation is true. Those require the human reference set (protocol §6, §14), which forbids Sensei from producing it. This page establishes only that the worlds it can reach run, from inputs that can be named, by a command anyone can re-run — and that world 3 is not one of them yet.
