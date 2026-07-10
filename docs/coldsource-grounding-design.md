# Cold-source grounding: a verification phase between cited draft and acceptance

*Design only — a proposal, not implemented. No engine, cage, corpus, graph, or
principle changes are made by this document. It records how cold-source should
**verify that a cited invariant is actually encoded in the target tree** before
the candidate is accepted, and how a confidence threshold could let the model
auto-decide the low-blast-radius calls while new-principle creation stays a human
zoom-out.*

> **Why this doc exists.** A k8s dry-run produced eight fully *cited* candidates.
> Reading the actual source showed two of them — including a proposed *new*
> meta-principle — cited symbols and line numbers that **were not in the tree at
> all**; they had been drafted from PR *review comments* (proposals), not from
> invariants the codebase encodes. The citation cage had passed them because it
> verifies that citations *resolve*, not that the cited line *holds the claimed
> invariant*. Grounding closes that gap.

---

## 1. The gap in today's cage

`coldsource/citation_check.go` (`CheckCitations` / `checkOne`) verifies, per
citation:

- `file:<path>[:line]` — the file exists, and if a line is given it is **within
  range** (`line <= lineCount`);
- `commit:<sha>` — resolves via `git rev-parse`;
- `pr:<id>` — provenance **preserved but not verified** (kept for the human).

Three things it does **not** do, each of which let a wrong candidate through:

1. **No symbol check.** `line <= lineCount` is satisfied by *any* line that
   exists. The k8s `validation.go:3474` citation resolved — but line 3474 was
   `ValidatePortNumOrName`, unrelated to the claimed feature-gate rule. "The line
   exists" is not "the line says what the draft claims."
2. **No provenance tier.** A `pr:` comment (someone *suggesting* a change) is
   treated as evidence equal to a landed commit or a test. The k8s
   `churnCancels` rule cited a review suggestion for code that never landed; the
   symbol is absent from the whole tree.
3. **No drift detection.** Line numbers move. A citation captured at draft time
   can point at different code by acceptance time, with no signal that it
   slipped.

Grounding is the phase that turns "the citation resolves" into "the invariant is
present, and here is how strongly."

## 2. Where grounding sits in the loop

```
signals
  → triangulate
  → draft (with citations)
  → GROUND                       ← new phase
       • re-resolve each citation's file:line / symbol against the target tree
       • classify evidence provenance per citation
       • detect drifted lines / missing symbols / review-only claims
  → ACCEPT
       • accept only if grounded at >= landed_commit
       • test_encoded is gold; review_suggestion-only is rejected/segregated;
         unresolved is rejected
  → MAP
       • propose related EXISTING meta.* principles (advisory)
       • never create a new principle automatically
  → HUMAN
       • label: load-bearing | borderline | shallow | wrong
       • decide: sibling evidence | pilot candidate | new-principle candidate |
         unclassifiable
```

Grounding is **mechanical** — git, file reads, lightweight symbol matching. It
adds **no** LLM call and **no** key use. (The optional ensemble in §6 is the only
part that would, and it is explicitly opt-in.)

## 3. Provenance tiers

Each citation is classified into exactly one tier. A candidate's overall tier is
the **strongest** tier among its citations (one solid test outweighs three weak
comments), but the *distribution* is recorded for the human.

| Tier | Meaning | How it is established |
|------|---------|-----------------------|
| `test_encoded` | A test asserts the invariant | cited file is a test (`*_test.go`, `test/...`, language-equivalent) **and** the claimed symbol resolves there, **or** a test references the symbol the rule protects |
| `landed_commit` | The invariant is in shipped code | `commit:<sha>` resolves **and** the claimed symbol is present at the cited `file:line` (or within a small window of it) in the current tree |
| `review_suggestion` | Only a PR discussion proposes it | evidence is `pr:` comments and/or a `file:line` whose **symbol is absent** from the tree (the suggestion did not land, or landed differently) |
| `unresolved` | Citation does not resolve | file/commit missing, line out of range, unknown citation form |

**Tier order (strict):**

```
test_encoded  >  landed_commit  >  review_suggestion  >  unresolved
```

**Critical nuance — "no test" must not mean "no candidate."** A `landed_commit`
grounds a real invariant. The k8s `apps/v1` marker rule (#2) was grounded by a
*revert commit* with no unit test and is genuinely load-bearing; Caddy's
`dispenser.Errf` and several etcd rules were commit-grounded. Requiring a test
for everything would discard real laws. Test evidence is **strongest**, not
**mandatory**.

## 4. Accept / reject threshold

```
accept            iff overall tier >= landed_commit
mark "gold"       iff overall tier == test_encoded
segregate         iff overall tier == review_suggestion   (do not silently drop;
                                                            hold for human as a
                                                            "review-only" lead)
reject            iff overall tier == unresolved
```

`review_suggestion`-only candidates are **segregated, not deleted** — a recurring
review theme is a real lead about where a rule might belong, but it is *not* an
invariant the code currently guarantees, so it must never enter the accepted set
that feeds promotion or meta-mapping. It surfaces in a separate "review-only
leads" list for the human.

## 5. How citations are re-resolved

For `file:<path>:<line>` with a claimed symbol (the draft already names the
function/type/field it is about):

1. **Path** must exist (today's check, kept).
2. **Symbol presence:** search the file for the claimed symbol. If found within a
   small window of the cited line → `landed_commit` (or `test_encoded` if the
   file is a test). If found elsewhere in the file → record **drift** (resolved,
   but the line moved — still grounded, flagged for review). If absent from the
   file → demote toward `review_suggestion`/`unresolved`.
3. **Test detection:** the file path matches a test convention, **or** a sibling
   test file references the symbol → eligible for `test_encoded`.

Symbol matching starts deliberately **shallow** (identifier / declaration-line
match via the existing surface parser), not full AST resolution — cheap, language
-agnostic, and enough to have caught every k8s miss. AST-grade resolution is a
later refinement, not a precondition.

For `commit:<sha>`: keep the resolve check; additionally confirm the commit
**touches the cited file/symbol** (so a real-but-unrelated SHA cannot launder a
claim). For `pr:<id>`: unchanged — preserved, never sufficient alone, and it
**caps** a candidate at `review_suggestion` if it is the only evidence.

## 6. Confidence-gated auto-decision (the ">80%" rule)

The loop makes decisions of very different blast radius. They should not share one
gate.

| Decision | Blast radius | Auto-decide at high earned correlation? |
|----------|--------------|------------------------------------------|
| **Label** load-bearing / shallow | advisory triage | **yes** |
| **Map** to an *existing* `meta.*` (sibling evidence) | low, reversible, principle already human-vetted | **yes** |
| **Mint a *new* principle** | high — a generative claim about the whole codebase | **no — always human** |

**The threshold is a router, not an override.** A *new* principle is by
definition the candidate that **correlates with no existing principle** — it is
the low-correlation residue. So "auto-decide above 80%" and "new principles stay
human" are the **same rule from two ends**: high correlation means "this is
another instance of a law we already have" (safe to auto-attach); low /
unclassifiable correlation is exactly the new-law signal that needs a human.

**Correlation must be *earned*, never self-reported.** The drafter's own
`conf=high` is worthless as a gate — the k8s `validation.go` draft was
`conf=high` and fabricated. The 80% must come from signals the model cannot
assert into existence:

- **Grounding (precondition):** correlation is only computed for candidates
  already accepted at `>= landed_commit`. No correlation on ungrounded evidence —
  full stop.
- **Ensemble agreement:** the map decision is run by N independent judges each
  prompted to **refute** the mapping (distinct lenses, not N identical calls);
  `>= 80%` must *concur*.
- **Similarity:** measurable closeness between the candidate invariant and the
  principle's canonical statement.
- **Independent corroboration:** count of distinct PRs / commits / files that
  converge.

**Guardrails:**

- **Auto is not silent.** Every auto-decision writes a durable, audited record
  with `decided_by: auto`, is reversible, and appears in a batch the human can
  review.
- **Auto-decide the attachment, never the consequence.** An auto-mapped sibling
  may be recorded as corroboration; it must **not** strengthen enforcement,
  promote a rule, or change a principle's status without a human.
- **Back-test before trusting the number.** Calibrate the threshold against the
  already-labeled Caddy / etcd / vite / k8s outcomes; adopt 80% only if the
  human-confirmed decisions actually cluster there. The number is an empirical
  result, not a guess.

Run the k8s cases through it: **#1** (ungrounded, single citation cluster, would
not survive refute-lenses) scores *low* earned correlation → routed to human,
never auto-minted. **#6** (test-encoded, clean match to
`write_creates_completion_obligation`) scores *high* → auto-attached as sibling
evidence. The rule, correctly defined, does the right thing on both.

## 7. Why new-principle creation stays human-gated

- A new principle asserts something **generative** — that it predicts siblings in
  code that has not broken yet. One high-confidence draft cannot establish
  generativity; the value is realized only when the principle is used as a lens
  across the corpus.
- The model's confidence is **in-distribution** to the draft it just wrote.
  Minting a law from it is the exact over-reach the whole cold-source arc has
  been disciplined against (the cage exists *because* confident drafts lie).
- The extraction protocol already names this step a human zoom-out: *"if none
  fits → flag UNCLASSIFIABLE → zoom out with human."* Grounding feeds that
  decision better evidence; it does not replace the decider.

## 8. How grounding would have changed the k8s run

| # | claim | today | with grounding |
|---|-------|-------|----------------|
| 1 | validation must not gate on feature state (**new principle**) | accepted `conf=high` | `unresolved` (symbol absent at cited line) → **rejected**, never routed to new-principle minting |
| 5 | scheduler `churnCancels` cleanup | accepted | `review_suggestion` (symbol absent from tree) → **segregated** as a review-only lead |
| 7 | status_manager 404 flood | accepted as live failure | regraded: real code + `TestSyncPodIgnoresNotFound` → `test_encoded`, **reframed** as a fixed+guarded invariant |
| 2 | apps/v1 markers must not flip | accepted | `landed_commit` (revert resolves, markers present) → **accepted**, no test required |
| 4 | pod-status condition merge | accepted | `test_encoded` (`pod_status_patch_test.go` + 2 commits) → **accepted, gold** |
| 6 | subpath cleanup obligation | accepted | `test_encoded` (`trackingSubpath` forces partial-failure cleanup) → **accepted, gold** |

Net: the two fabricated/ungrounded candidates are removed *before* they reach a
human or a principle, while every genuinely grounded candidate is preserved —
and the strongest ones are marked gold.

## 9. How it preserves the earlier wins

- **Caddy** (`dispenser.Errf`, commit + review): grounds at `landed_commit` /
  `test_encoded` — accepted, unchanged.
- **etcd** (proto-mutex copylock, raft aliasing, client dial contract): the
  load-bearing ones are commit-grounded — accepted; the transcript-only,
  unverified ones are exactly what `review_suggestion`/`unresolved` is meant to
  hold back until re-derived (consistent with `pilot/etcd`'s own "must re-verify
  before promotion" caveat).
- **Vite** (TS code candidates): grounded against the TS tree the same way;
  language-agnostic shallow symbol matching covers it.

Grounding tightens *what is trusted*; it does not discard the candidates that
were already real.

## 10. Non-goals

- **No engine implementation** — this is the contract, not the code.
- **No corpus, graph, seed, pack, or ontology changes.**
- **No new principles, no promotion, no graph mutation.**
- **No LLM or key use** in the mechanical core (the optional ensemble in §6 is
  explicitly opt-in and out of the base phase).
- **No hard gates, no CI behavior change** — grounding gates *acceptance inside a
  dry-run*, nothing in anyone's merge path.
- **No change to the human gate** — labeling and the sibling/new-principle
  decision remain human; grounding only feeds them cleaner evidence.

## 11. Validation — measured on the k8s LLM run (2026-06-15)

§8 *predicted* how grounding would change the k8s run. After the mechanical phase
shipped, an LLM dry-run (`drafter llm:claude-opus-4-8`, same repo/window) was
routed through the grounding gate to **measure** it. Analysis-only, dry-run,
nothing written.

**Grounding does not change triangulation.** Extraction is upstream and
untouched: identical signals/themes/triangulated before and after.

**It adds a strength axis** to the previously-binary accepted set —
`test_encoded > landed_commit > review_suggestion > unresolved`, plus `!`
(claimed symbol absent at a cited file) and `~` (cited line drifted) flags.

**k8s rerun (`kubernetes/kubernetes` @ `HEAD~500..HEAD`, `--max 8`):**

| stage | count |
|-------|------:|
| signals | 345 |
| themes | 148 |
| triangulated | 10 |
| drafted | 10 |
| **segregated (review-only)** | **2** |
| **accepted** | **8** |

Accepted tier distribution: **4 `test_encoded`, 4 `landed_commit`** (0
`review_suggestion`, 0 `unresolved` — the two review-only candidates were
segregated, not accepted).

**Headline finding.** The earlier feature-gate `validation.validation` candidate
— which the *old binary cage accepted at `conf=high`*, and which had to be
manually retracted as a fabricated new-principle — is now **auto-segregated** as
`review_suggestion` / `symbol_absent`. The gate caught, mechanically, exactly the
case that previously slipped through.

**Status-manager finding.** The `status_manager` candidate's underlying invariant
may well be real, but *this draft cited it badly* (a line + a PR comment, not the
actual test). Grounding correctly **segregated the weakly-cited draft** and asks
for re-derivation against the real test — the rule is not lost, its bad evidence
is.

**Strongest-anchor semantics validated — keep them.** A candidate is accepted on
its strongest anchor; a single drifted citation or a symbol name that appears in
prose but not the code does **not** sink a real landed/test-backed candidate —
it is accepted **with a `!`/`~` flag** that routes reviewer attention (e.g. the
`scheduler_perf` candidate kept its real cleanup commit and was flagged for the
absent `churnCancels` symbol). Tightening the threshold to reject flagged
candidates would discard real commits over LLM prose.

**No correctly-cited load-bearing candidate was lost.** The two segregations were
a fabricated candidate (correct) and a real-but-badly-cited one (correctly held
for re-derivation). The gate removed weak evidence, not real invariants.

---

*Status: design proposal. No code, graph, or principle changes were made in
producing this note. Implementation is a separate, explicitly-approved step.
Related: [`hard-gate-design.md`](hard-gate-design.md) (the draft→serve→**block**
design; this is its upstream sibling, the draft→**accept** design),
[`intent-mining-design.md`](intent-mining-design.md) (reuses this grounding spine
to mine **stated intent**, not scars),
[`cold-bootstrap-adoption.md`](cold-bootstrap-adoption.md), and
[`milestone-cold-bootstrap-v0.md`](milestone-cold-bootstrap-v0.md), and
[`corpus-integration-design.md`](corpus-integration-design.md) (the human-gated
path from a grounded finding to corpus truth).*
