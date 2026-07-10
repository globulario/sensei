# Cold-source hit-rate experiment

**Experiment only.** Everything here is input for measuring whether the
`coldsource` LLM drafter produces *load-bearing* awareness candidates from cold
day-0 signals. It never promotes, never writes active knowledge, and never
mutates the graph — candidates land as `status: candidate` files under
`docs/awareness/candidates/`, and the recommended runs below all use
`--dry-run` (no files written at all).

## Purpose

Answer the single question that decides SaaS vs. services for Sensei:

> Can an LLM draft awareness candidates from a repo's *existing* signals
> (PR review comments + revert/regression commits) that a human accepts as
> **load-bearing** — at a high enough rate to be worth productizing?

This package makes that a **one-command** run once you supply an API key, and
gives you a sheet to label the output.

## Required inputs

1. A target git repo (`--repo`) whose history is scanned for revert/regression
   commits.
2. A `--pr-comments` JSON fixture: an array of review comments
   (`{PRID, CommentID, Path, Line, Body}`). The fixtures here are **hand-authored
   experiment input** (realistic rules on real files), not scraped PRs — see the
   optional `gh` path below to pull real comments.
3. `ANTHROPIC_API_KEY` in the environment — required for `--drafter llm`.

A candidate is only drafted when a **theme** (component/dir) has **≥2 distinct
source types** (a PR comment *and* a revert/regression commit on the same
component). So a fixture only produces candidates when its file paths line up
with the target repo's revert history. The provided fixtures are tuned to the
`globulario/services` history.

## Command examples

Offline fixture is the default, recommended path. One command:

```bash
ANTHROPIC_API_KEY=sk-ant-... \
awg cold-bootstrap \
  --repo /path/to/services \
  --since HEAD~400..HEAD \
  --drafter llm \
  --pr-comments golang/extractor/coldsource/experiment/fixtures/services_pr_comments.json \
  --dry-run \
  --max 10
```

Or via the convenience runner (builds `awg` if needed):

```bash
ANTHROPIC_API_KEY=sk-ant-... \
./run.sh --repo /path/to/services --since HEAD~400..HEAD
```

No API key yet? Smoke-test the full pipeline (extract → triangulate → validate →
citation-check → report) with the deterministic echo drafter — **no key, no model
call**:

```bash
awg cold-bootstrap --repo /path/to/services --since HEAD~400..HEAD \
  --drafter echo --pr-comments .../fixtures/demo_pr_comments.json --dry-run
```

Optional — pull **real** review comments via GitHub (opt-in; needs `gh` + `jq`;
never required, never used by CI):

```bash
ANTHROPIC_API_KEY=sk-ant-... ./run.sh --repo /path/to/services --gh globulario/services
```

## Fixtures

| File | Target repo | Notes |
|------|-------------|-------|
| `fixtures/services_pr_comments.json` | services | 4 realistic rules on real files with revert history — the primary fixture |
| `fixtures/demo_pr_comments.json` | services | 2-entry minimal template (copy this shape) |
| `fixtures/awareness_graph_pr_comments.json` | awareness-graph | optional; may not triangulate — this repo has little revert history |
| `fixtures/caddy_pr_comments.json` | caddyserver/caddy | **real scraped sample** — 600 recent review comments across 133 PRs. The cross-repo benchmark (see below). |

## Caddy benchmark (theme-clustering regression case)

`fixtures/caddy_pr_comments.json` is the standing benchmark on a mature foreign
Go repo. Unlike the hand-authored fixtures, it is a **bounded real sample** of
caddy's most-recent PR review comments, aligned with the `HEAD~500..HEAD` window.
Use it to detect regressions in extraction/triangulation/theme-clustering.

```bash
# clone the benchmark repo (reverts come from real git history)
git clone --depth 600 https://github.com/caddyserver/caddy.git /tmp/caddy

awg cold-bootstrap --repo /tmp/caddy --since HEAD~500..HEAD \
  --pr-comments golang/extractor/coldsource/experiment/fixtures/caddy_pr_comments.json \
  --drafter echo --dry-run --max 10
```

**Expected after file-concept theme clustering** (the win this guards): the
broad component directories split into per-concept bundles instead of mega-
buckets. Concretely, `modules/caddyhttp` (was one ~47-citation bundle) and
`modules/caddyhttp/reverseproxy` (was one ~64-citation bundle) now triangulate
as sharp sub-bundles — `…reverseproxy.reverseproxy`, `…reverseproxy.streaming`,
`…reverseproxy.httptransport`, `…reverseproxy.fastcgi.fastcgi`, plus
`…caddyhttp.server`, `…caddyhttp.matchers`, `…caddyhttp.autohttps` — while the
genuinely coherent `…encode.encode`, `…rewrite.rewrite`, and `…logging.filewriter`
bundles survive intact. If a code change collapses these back into giant
buckets, theme-conflation has regressed.

> The unit-test form of this guard (no clone needed, runs in CI) is
> `TestTriangulate_SplitsBroadDirectoryByConcept`.

## Expected scoring output

A dry-run prints a funnel plus a per-candidate scoring sheet, e.g.:

```
AWG cold-bootstrap — EXPERIMENT report
drafter:                   llm:claude-opus-4-8
mode:                      DRY-RUN (no files written)

Funnel
  total signals found:      546
  themes found:             59
  triangulated themes:      4
  held back (single-source):55
  drafts attempted:         4
  rejected (malformed):     0
  rejected (no citation):   1
  rejected (shallow):       0
  rejected (duplicate):     0
  candidates accepted:      3
  ...
Scoring sheet (label each: load-bearing | shallow | wrong | duplicate)
#   class        theme                                conf    cits  label
1   forbidden_fix golang.repository.upstream          high    6     ______
...
Candidate evidence (for labeling)
[1] golang.repository.upstream  (class=forbidden_fix conf=high)
    reason: ...
    citations (6): ...
```

## Labeling

For each accepted candidate, assign one label:

- **load-bearing** — a real architectural rule a senior engineer would protect
  (authority, invariant, forbidden-fix, failure-mode). The win condition.
- **shallow** — true but generic/obvious (style, "validate input", restating a
  signature). Not worth a graph node.
- **wrong** — misstates the architecture, or the cited evidence doesn't support
  it. The dangerous class — keep this rate low.
- **duplicate** — restates an existing candidate/known rule.

Record results in `scoring_sheet_template.csv` (one row per candidate).

## Go / No-go

- **Go** (productize the drafter): **≳30%** of accepted candidates are
  *load-bearing* **and** the *wrong* rate is low. Cold-source bootstrap is real →
  AWG is SaaS-shaped.
- **No-go** (stop / treat as services): output is mostly shallow, duplicate, or
  wrong. The knowledge genuinely lives in heads, not derivable signals — price it
  as consulting with a tool, not SaaS.

## Guardrails (do not change as part of running the experiment)

- Drafter logic and the citation contract are fixed — don't relax them to make
  numbers look better.
- No promotion, no active-graph writes. Keep `--dry-run` for labeling runs.
- Don't add extractors to chase a better rate before the first read — that's the
  next decision, gated on this result.
