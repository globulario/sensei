# Slice 2 — the instrument that will grade the run

**Status: written, not run. No sampled change has been executed, no label
exists, and the frozen reference set is untouched.**

Slice 2 is the runner, scorer and reporter of
`docs/design/prospective-recall-harness-259.md` section 5. The design requires
it to be *written before labels exist* and *run after* — "a grader authored
after seeing scores is not a grader" — so this document records that the
instrument now exists while the numbers do not.

Verify the claim rather than take it: `eval-prospective run` refuses without a
frozen answer key, and `docs/evaluation/prospective-v1-execution/execution-identity.json`
still records `sampled_changes_executed: 0`.

```
$ eval-prospective run --labels prospective-labels.json …
eval-prospective: frozen labels: open prospective-labels.json: no such file or directory
```

## What it is

| command | what it does |
|---|---|
| `eval-prospective run` | replays the frozen retrieval surface over the pinned changes and writes `sensei.prospective_run.v1` |
| `eval-prospective score` | compares that run with the frozen labels and writes `sensei.prospective_score.v1` |
| `eval-prospective report` | renders the protocol section 12 report from a score |

`run` refuses to execute unless a frozen labels file exists, hashes to the
digest it carries, and binds to this exact sample manifest and blind corpus.
There is no flag that bypasses it, and a test asserts the flag set contains
none — a skip flag is how the one ordering constraint the whole experiment
rests on gets undone by somebody in a hurry.

It also refuses a world that has drifted off the pinned revision, and a server
whose graph is not the pinned digest, not `AUTHORITY_VERDICT_AUTHORITATIVE`, or
not `GRAPH_FRESHNESS_STATE_CURRENT` — the three conditions the execution
identity already named.

## Decisions this required, recorded before any number exists

### The task text is mechanical, and it is a lower bound

The frozen packages carry the diff and the touched paths. They carry no commit
subject and no author intent, because the inventory was built from diffs. So
the runner composes the `--task` text from the paths and git's own status
letters, under the rule id `paths_and_git_status.v1`:

```
add cmd/new/main.go; modify golang/server/reload.go; delete golang/server/old.go
```

The exact string is recorded per change in the run artifact.

**This understates what a real author supplies.** Someone typing *"make the
reload path fail closed when the marker is missing"* hands retrieval far more
than *"modify golang/server/reload.go"* does. The alternative was worse:
inventing richer intent would be a benchmark-only channel, and the score would
then measure this harness's paraphrasing rather than production's retrieval.
A low stratum-A result under this rule is evidence about retrieval **given
path-level intent**, and the report must be read that way.

### The status mapping is fixed here, not chosen later

```
invocation failed, every path is new  → no_prospective_channel
invocation failed otherwise           → unavailable
PREFLIGHT_STATUS_DEGRADED             → degraded
PREFLIGHT_STATUS_EMPTY                → empty
PREFLIGHT_STATUS_OK, no direct anchor → no_anchors
PREFLIGHT_STATUS_OK                   → resolved
anything else                         → unavailable
```

The last line is deliberate. An unrecognised status is not evidence of
coverage, and a harness that guessed at one would be inventing a result.

### MetaPrinciple items reach production through the invariant partition

The corpus enumerates by class and names items `class:id`. Production returns a
node's own class — and `MetaPrinciple` nodes are dual-typed `meta.*` invariants
that surface in the invariant partition, which `golang/server/impact.go` states
outright.

A strict qualified match would therefore have scored every one of the corpus's
**164 meta_principle items as permanently unsurfaceable**: a systematic zero
that would read as a retrieval failure rather than as a vocabulary mismatch in
the ruler. So a qualified match is tried first, and an unqualified match is
used only when the short id is unique across the whole corpus. Ambiguous short
ids resolve to nothing, because a wrong hit inflates recall silently while a
recorded miss does not. Every hit records which rule matched it, and the report
prints both counts.

### What is surfaced but unscorable is reported, not hidden

Production also surfaces required tests, intents and architectural nodes that
are not in the eligible corpus. They have no label and can be neither a hit nor
a nuisance, so they are counted as `surfaced_outside_corpus` and reported. That
number is part of what an author actually absorbs, and dropping it would flatter
the nuisance figures.

## What the scorer will not do

- collapse `ambiguous`, `outside_scope` or `cannot_adjudicate` into
  `not_applicable`; only `applicable` reaches the recall denominator;
- report one nuisance number — primary, unresolved-surfaced and conservative
  travel together, because primary alone can be driven down by flooding with
  unjudgeable items;
- report `0.0` for a rate with an empty denominator; an absent metric is
  absent, and the macro average names the strata it covers;
- merge strata A and B, anywhere;
- drop a change from the denominator because retrieval had no channel for it,
  or because the runner never executed it;
- select which misses to show — the missed set is complete;
- generate interpretation text from the numbers.

Each of those is a test, not a promise: `golang/architecture/prospectivescore`
and `cmd/eval-prospective`.

## What still gates the run

The human applicability labels. That is the only remaining input, and no part
of this instrument can supply it.

## Relationship to #282

The execution identity requires `AUTHORITY_VERDICT_AUTHORITATIVE` before a run
may proceed. #282 showed that verdict rested on a triple count, which a
count-preserving mutation passes. The runner now checks a verdict that means
more than it did: content verification was added at load, publication and
server startup, and the freshness detail no longer claims the store matches the
artifact when only its marker and count were compared.

The pinned store itself was re-verified during that work — content digest
recomputed from the live serialization, matching `def94857…` exactly — so this
reference set's world is intact and unmutated.
