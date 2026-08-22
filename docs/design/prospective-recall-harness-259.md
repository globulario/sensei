# Prospective authoring-recall harness — implementation handoff (#259)

Status: handoff, no answers
Target repository: `globulario/sensei`
Refs #259, #131. Sibling of the #256/#257/#258 relay chain.

`docs/evaluation/prospective-recall-protocol-v1.md` is already merged and frozen.
Its SHA-256 is `ade91a42c8c0c421d0e6ba84ce2a547712c248b7cfef491ecc6ff8b12ff90d8d`.
This document does not amend it. It bounds the **instrument** that obeys it.

## 1. What is left, and who owns each part

The protocol's freeze order (section 3) partitions the remaining work by owner.
That partition is the whole point of this handoff: the parts an agent may build
are exactly the parts that cannot decide the answer.

| step | owner | status |
|---|---|---|
| protocol v1 merged | done | merged |
| pinned changes + world binding | agent (slice 1) | not started |
| frozen candidate inventory, per stratum, content-addressed | agent (slice 1) | not started |
| deterministic sample manifest (A/B/C/D) | agent (slice 1) | not started |
| eligible-knowledge corpus frozen | agent (slice 1) | not started |
| blind adjudication package emitted | agent (slice 1) | not started |
| **human applicability labels frozen** | **human only** | blocked on slice 1 |
| production retrieval run + scoring | agent (slice 2, runs after labels) | not started |
| interpretation | human | blocked |

Slice 2 is *written* before labels exist and *run* after. That order is required,
not incidental: a grader authored after seeing scores is not a grader.

## 2. Existing seams to reuse — do not reinvent

- `cmd/eval-arms/protocol.go` — the `protocolVersion` registry: ID, path,
  `DigestSHA256`, world bindings, domains, remotes, pinned revisions, and
  fail-closed lookup. Register the prospective protocol the same way.
- `cmd/eval-arms/sample.go` — inventory enumeration from a mechanical rule,
  content addressing, seeded selection.
- `golang/architecture/evalsample` — `candidates.go`, `select.go`, `sample.go`.
- `golang/architecture/evalharness` — baseline/composition conventions.
- `docs/evaluation/phase10-v2-reference-set/` — the on-disk shape of a frozen
  reference set: `DIGESTS.txt`, `sample/`, `labels/`, `worlds/`,
  `adjudicator-overlap.json`, `make-adjudication-package.py`.

A prospective reference set mirrors that layout under
`docs/evaluation/prospective-v1-reference-set/`.

## 3. Architect-owned decisions — resolve before the worker starts

These are not worker choices. A worker that picks one silently has decided what
the experiment measures.

3.2 is now **decided** and recorded below. 3.1 and 3.3 remain open and are the
architect's to resolve — and only those two. The instruction standing over this
work is to surface a new architectural decision only where this design does not
already determine the answer, so a worker meeting a question already settled here
follows the document rather than escalating it.

### 3.1 Which production surface is the authoring-time retrieval path

Candidates that exist today: `sensei briefing --task`, `sensei briefing --file`,
`sensei preflight --file/--task`, `sensei impact`, `sensei prepare-change`, and
the MCP equivalents `awareness_briefing` / `awareness_preflight` /
`awareness_impact`.

Protocol section 1 states the measured query is **not** `BY_FILE`. So:

- the chosen surface is recorded in the manifest by name and invocation;
- if production exposes no prospective channel for a file that does not yet
  exist, the runner records `no_prospective_channel` for that change and reports
  it as a retrieval-status class. It does **not** substitute `BY_FILE` and it
  does not add a benchmark-only channel (protocol section 2).

A run that is honestly mostly `no_prospective_channel` on stratum A is a valid
result and probably the finding.

### 3.2 The mechanical inventory rule and world binding — DECIDED

This one is settled and is no longer an open architect question. It was resolved
by the human owner and is recorded here rather than left to the worker.

**Inventory.** Enumerate the *complete* candidate-change population that is
actually observable or reconstructable by Sensei at the pinned world revision.
Not a curated commit range, and not an arbitrarily sampled subset. Anything that
cannot be inventoried is **excluded explicitly**, with its reason and count, so a
population that shrinks does so visibly (protocol section 8.1). Eligibility and
exclusion rules are written down before the inventory is built.

This supersedes the earlier proposal to scope the inventory to the merged
#256/#257/#258 range. The reason the narrower rule was wrong is worth keeping:
picking the loop where the hypothesis was first observed selects for the
phenomenon being measured, and a seed applied afterwards would have been honest
about the draw while the population underneath it was already biased. Completeness
at the pinned revision is what makes the denominator defensible.

**World binding.** The harness binds to the exact repository and world revision
used to construct the inventory and the evidence. A result may not silently carry
across world drift: if the checkout resolves to a different revision than the
manifest pins, the run fails closed and requires a new observation, rather than
reporting old numbers against a moved world. The `Revisions` map on the protocol
registry already models exactly this — v2 pins World 3 to a commit for the same
reason — so bind through it rather than inventing a second mechanism.

### 3.3 The stratum classification rule

Strata are assigned by applying a stated rule to each change's **pre-authoring**
state. That rule consults production anchor resolution, which is the system under
test. Guardrail: the raw classification evidence is frozen and published per
change alongside the inventory digest, so a later production change cannot
retroactively re-stratify a frozen sample.

## 4. Slice 1 — freeze the sample, emit the blind package

New binary or subcommand family (`cmd/eval-prospective`, or `eval-arms
prospective`; the architect picks and the choice is recorded).

### Required behaviour

1. Register `prospective-recall-protocol-v1` with its path and the digest above.
   A mismatched digest fails closed; it does not warn.
2. Bind to the exact world revision named in the manifest. A checkout that
   resolves elsewhere fails closed; there is no "close enough" revision.
3. `inventory` — enumerate the complete candidate-change population observable
   or reconstructable at that pinned revision, per section 3.2. Emit per-change: exact diff or new-file contents,
   exact base revision or tree digest, repository domain, and the graph
   generation/status available at that moment (protocol section 4). A change that
   cannot state what the graph knew at authoring time is excluded, with a reason.
4. Classify each change into exactly one of A/B/C/D by the stated rule, carrying
   the raw classification evidence.
5. Content-address the per-stratum inventory and publish each digest.
6. `sample` — draw independently per stratum, up to 12 per stratum, by stable
   hash of a committed seed and change identity, sorted ascending. A stratum with
   fewer than 12 uses all of them and reports the smaller denominator. Never
   borrow across strata.
7. Emit a sample manifest carrying: protocol ID + digest, world binding, pinned
   revision, seed,
   per-stratum inventory digests, exclusion counts and reasons, and the retrieval
   surface chosen in 3.1.
8. `corpus` — freeze the eligible knowledge corpus (scars, invariants,
   contracts) by digest before any label exists.
9. `package` — emit the blind adjudication package for a human: change contents,
   world binding, and eligible corpus. It must contain **no Sensei retrieval
   output whatsoever**, and where item provenance could bias the judgement, that
   provenance is withheld (protocol section 9).
10. Emit the deterministic 20% second-adjudicator overlap subset before any labels
   are compared (protocol section 10).

### Required tests

- protocol digest mismatch is refused;
- a checkout at a revision other than the pinned one is refused, and the refusal
  names the drift rather than reporting an empty inventory;
- inventory enumeration is deterministic, and complete over the observable
  population at the pinned revision rather than over a supplied list;
- an item that cannot be inventoried appears as a counted exclusion, never as a
  silent absence;
- every exclusion carries a reason and is counted;
- stratum assignment is a total function — no change lands in two strata or none;
- A and B are never merged by any code path;
- seeded selection is reproducible and changing the seed changes the draw;
- a stratum with fewer than N members reports the true denominator;
- the emitted adjudication package contains no retrieval output (assert on the
  serialized bytes, not on intent);
- overlap subset is deterministic and independent of label content;
- manifest binds protocol ID, digest, seed and every inventory digest.

### Exclusions

Do not add: retrieval execution, scoring, any label file, any expected-fact
material, any change to production retrieval, any new retrieval channel, any
promotion of candidates, any change to authored awareness.

## 5. Slice 2 — runner, scorer, reporter (written before labels, run after)

### Required behaviour

1. `run` — refuse to execute unless a frozen labels file exists and its digest
   matches the manifest. This is the mechanical guarantee of the protocol's
   fifth arrow; a flag that bypasses it must not exist.
2. Replay the chosen production authoring-time surface over each pinned change
   at its pinned base. Record the typed retrieval status per change: `resolved`,
   degraded, no anchors, unavailable, `no_prospective_channel` (section 7.3).
3. Record which context classes were actually available before retrieval ran:
   change contents, package/module identity, imports, directory ownership and
   risk class, neighbouring anchored components, touched contracts, global
   scars, history (section 7.4).
4. `score` — per stratum: recall over `applicable` labels only, and all three
   nuisance numbers — primary (resolved labels only), unresolved-surfaced rate,
   and conservative. Never one nuisance number. Never recall without nuisance.
5. Only `applicable` enters the recall denominator; `not_applicable`,
   `ambiguous`, `outside_scope` and `cannot_adjudicate` are each reported as a
   distinct count and rate, never collapsed (section 6.1).
6. `report` — per-stratum first, macro summary with every stratum still visible,
   inventory digests with exclusion counts, sample sizes, exact change/world/
   corpus/reference-set identities, second-adjudicator status and agreement by
   label pair, retrieval-status distribution, context-class availability, and the
   frozen missed-example set. Where only one human adjudicated, emit
   `second_adjudicator_unavailable`.
7. A negative result is emitted, not suppressed.

### Required tests

- scoring refuses to run without a frozen matching labels digest;
- there is no bypass flag (assert the flag set);
- recall denominator counts `applicable` only — a fixture with every other label
  present proves the others cannot enter the numerator or the denominator;
- all three nuisance numbers are emitted, and a fixture where flooding with
  unresolvable items drives primary nuisance down still shows it in the
  unresolved-surfaced and conservative numbers;
- no output path emits a single blended headline score;
- A and B remain separate through scoring and reporting;
- a `no_prospective_channel` change is scored as a miss with honest status, not
  silently dropped from the denominator;
- report contains every identity listed in protocol section 12.

### Exclusions

Same as slice 1, plus: no interpretation text generated from the numbers, and no
selection of illustrative misses after scores are visible.

## 6. What counts as closing #259

A documented near-zero stratum A is an **acceptable close**. The purpose of this
instrument is to establish the empirical distribution, not to manufacture
stratum-A cases.

If the real system produces essentially no stratum-A population, or produces one
on which nothing is surfaced, the report records:

- the result itself, unrounded and unsoftened;
- the search and inventory coverage that produced it — what was enumerated, what
  was excluded and why, and at which pinned revision;
- why the inventory is believed representative of the observable population.

What may **not** happen in that case is weakening the stratum definition to
populate it. A stratum A that has been redefined until it has members no longer
measures new-seam coverage, and the redefinition would be invisible in the score
it produces. If A and B must be re-cut, that is a new protocol version and a new
sample, not an amendment to this run.

## 7. Hard stop

The instrument stops at the blind package and resumes after the labels are
frozen. No agent, Sensei included, may decide applicability, generate the
eligible corpus and then be scored against it, or resolve adjudicator
disagreement (protocol section 11).

Any production retrieval improvement suggested by the result is filed as a
separate issue **after** measurement. It is not implemented inside the
calibration run.
