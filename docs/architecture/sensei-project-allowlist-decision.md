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

---

# Resolution (2026-09-13): Option D approved, and the slice turns out to be empty

The owner approved **Option D** — `.sensei/project` stays an ignored local
working/cache/staging area; publish a narrow derived slice rather than admitting the
tree — and asked for the minimum sufficient slice to be determined **empirically**,
starting from `protection-coverage.yaml` and adding `graph.nt` only if required.

Determined by tracing the consumers rather than by assuming. **No consumer requires any
published slice.**

## The consumer that hits the W3 knowledge limit does not read the graph

The gap is raised by `unexaminedCoverageGap` in sensei-code
(`internal/workflow/authority.go`), closed only by `action.DerivedCoverage`, which comes
from `Engine.coverageAtWorld`:

```
docs/awareness/derived_recipes.json      (TRACKED, already in the corpus)
  -> derived.AnchorsFor
    -> CLI.Revalidate  ->  sensei derive -kind <recipe> -repo-root <root> -revision <world>
```

`sensei derive` is a **source-code analysis at a git revision**. The recipes are
`field_access_under_lock`, `state_mutation_confined_to_owner`,
`command_invocation_confined_to` — mechanical questions about source, not graph queries.
Nothing on this path reads `.sensei/project`, and nothing on it reads the graph.

So the W3 limit — *"no anchors fired, no files examined"* — is a **recipe-coverage gap**:
no recipe's anchor covers the planned files. Publishing anything from `.sensei/project`
cannot move it.

## And the facts are already published anyway

| class | served sensei-code `:7882` | served sensei `:7881` | `.sensei/project/graph.nt` |
|---|---|---|---|
| `CodeSymbol` | 3,031 | 7,573 | 7,571 |
| `SourceFile` | 196 | 1,760 | 1,141 |

The code-symbol coverage is **already graph-resident**. `graph.nt` holds essentially the
same symbols the live graph already serves.

`protection-coverage.yaml` is likewise not a publication input: `checkProtectionCoverage`
calls `protection.Derive(repoRoot)`, which derives coverage **from the repository on
demand**. The file in `.sensei/project` is a cache of that derivation, not its source.

## Answers to the questions asked

| question | answer |
|---|---|
| minimum sufficient derived slice | **empty** — no identified consumer reads a published slice |
| is `protection-coverage.yaml` sufficient? | it is not *insufficient*, it is **unnecessary** — recomputable from the repo |
| is `graph.nt` necessary? | **no** — its symbols are already served |
| which facts are missing? | **none are missing from the graph.** What is missing is *recipes* in the tracked `derived_recipes.json` whose anchors cover the W3 planned files |

Building a publication slice now would export bytes nothing reads, and the plan's own
rule is *"semantic sufficiency, not maximum exported data."* So the slice is not built.

## What was implemented instead: the decision given teeth

Option D's negative half was **unenforced**. `importWouldDefeatItself` refuses a run whose
own extraction dirties a root the domain publishes, and it consulted
`extractionWriteRoots`, which named only `docs/awareness` — while import writes
`graph.nt`, `claims.yaml`, `knowledge/adoption-report.yaml`, `protection-coverage.yaml`,
the `project-invalid-<txID>` quarantine directory and `project.lock` into
`.sensei/project` on every run.

So an operator who adopted **Option A** by editing one registry line would have gotten
**no refusal at all**, and the import would have published a directory it rewrites as it
runs — the exact self-defeat that guard exists to prevent, in the one root it could not
see. Nine near-duplicate intermediate claim sets at ~79 MB each would have entered the
graph.

`.sensei/project` is now named in `extractionWriteRoots`, so admitting it as corpus is
mechanically refused unless the domain also permits a dirty worktree — which every
registered domain deliberately does not. The architectural decision is now an invariant
rather than a convention.

## What remains for the W3 coverage limit

Authoring recipes in `docs/awareness/derived_recipes.json` whose anchors cover the files
W3 plans to touch. That is corpus authoring in a tracked file, needs no allowlist change,
no publication change, and no graph refresh.

