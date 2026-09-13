# `.sensei/project` cannot publish anywhere — the decision, fully specified

This is the last open point of `oxygraph_usage.md` besides the live migration, and it is
an owner's decision about intent. Both options are specified here completely so the
choice costs one sentence.

## The measured situation

`.sensei/project` holds the reconstruction output — roughly **248,000 code-symbol
triples** on this machine. It is where `sensei import` writes what it extracts from a
checkout.

All three registered domains declare exactly one allowed corpus root:

```yaml
allowed_corpus_roots:
  - docs/awareness
```

So `.sensei/project` is **unpublishable by construction**: `AdmitPublication` refuses it
for every domain that exists. This is not a bug — the registry is doing precisely its
job, refusing to publish a directory no domain admits.

The consequence is the W3 knowledge limit: a governed run asking for code-symbol
coverage cannot get it, because the triples that would answer it can never reach a graph.

`import` is now honest about this (`50319cb5`): a run whose own extraction would defeat
its publication refuses up front instead of rewriting the checkout and then failing.
Refusing truthfully is not the same as being able to publish, which is why this decision
is still owed.

## Option A — admit `.sensei/project` as a corpus root

```yaml
# ~/.sensei/domains.yaml
  github.com/globulario/sensei-code:
    repository_identity: globulario/sensei-code
    allowed_corpus_roots:
      - docs/awareness
      - .sensei/project        # ← added
```

**What it means.** Generated reconstruction output becomes publishable corpus for that
domain.

**Consequences, measured and stated plainly:**

- `.sensei` is the **gitignored runtime state directory**. Publishing from it means the
  published graph contains content that is not in any commit, so a publication receipt
  would certify revision R while the bytes came from files git does not track. That is
  the exact authority mismatch the registry's `allow_dirty_worktree: false` default
  exists to prevent, arriving through a different door.
- The domain would then need `allow_dirty_worktree: true` in practice, because extraction
  rewrites those files on every run — and the registry's own comment explains at length
  why that flag must stay off until a receipt binds content identity rather than a
  revision.
- Nothing in the code needs changing. One registry line.

**Choose this if** code-symbol coverage is wanted now and the receipt weakening is
acceptable as a stated, temporary cost.

## Option B — move the reconstruction output under `docs/awareness`

Extraction writes to `docs/awareness/generated/` (a path that already exists and is
already an allowed root) instead of `.sensei/project`.

**What it means.** Generated output becomes tracked, reviewable, committed corpus.

**Consequences:**

- The output is version-controlled, so a receipt certifying revision R is true: the
  bytes are in R.
- ~248k triples of generated content enter the repository, and every extraction run
  produces a diff. That is a real cost in review noise and repository size.
- The corpus must be committed before publication, so `import` becomes a two-step
  operation: extract, commit, then publish. `importWouldDefeatItself` already refuses
  the one-step form for exactly this reason, so the refusal becomes the documented
  workflow rather than a wall.
- This needs a code change: `extractionWriteRoots` in `cmd/awg/cmd_import.go` and the
  extraction output path. Bounded — one constant and the writer.

**Choose this if** the graph's provenance must stay exactly as strong as it is now.

## Option C — neither, and say so

Leave it unpublishable and record that code-symbol coverage is deliberately out of the
graph. The W3 knowledge limit then stands permanently rather than pending, and a governed
run asking for coverage gets a truthful "this graph does not carry that" instead of an
open question.

**Choose this if** coverage was never meant to be graph-resident.

## Recommendation

**Option B**, with Option C as the honest fallback if the review noise is unacceptable.

The reason is the one this whole architecture front keeps arriving at: every other law
here refuses to let a receipt claim more than its evidence supports. Option A makes the
graph's provenance weaker to make a query answerable, which is the trade this front
exists to stop making. Option B pays a repository cost to keep the provenance exact.

I cannot make this choice: it is about what the graph is *for*, not about what is
correct. The authorizing sentence is one of:

> Option A: admit `.sensei/project` as a corpus root for sensei-code.
> Option B: move extraction output under `docs/awareness/generated`.
> Option C: coverage stays out of the graph; record the limit as permanent.
