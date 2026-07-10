# Sensei cold-bootstrap v0: learn → validate → promote → serve → warn

*A milestone note: what the cold-bootstrap sequence proved, what it does not yet
claim, and where it could go next. Captured for the record — not a launch.*

This milestone closes the loop from a mature external repo's own history to a
future agent being warned before it repeats a class of mistake that repo already
paid for. It is a v0 proof, deliberately scoped.

---

## 1. What was proven

- **Sensei cold-source can mine real repo history** from mature external projects
  (PR review comments + revert/regression commits), not just hand-authored input.
- **Caddy and etcd both cleared the live LLM drafter quality bar.**
- **The validation cage held** on both scored runs:
  - no fabricated citations survived,
  - the *wrong* rate was **0**,
  - bad / unresolvable evidence was rejected rather than drafted.
- **Repo/domain-scoped promotion works** — an explicit, human-gated path admits a
  foreign rule into a served graph with its full provenance.
- **Foreign-repo rules can be served without polluting Globular's graph** — the
  embedded seed stays byte-identical; pilot rules live outside the scanned paths.
- **Briefing can show compact provenance** (repo · origin · review · bundle ·
  range · cites) so a foreign rule explains why it should be trusted.
- **EditCheck can warn on a bad future edit** — advisory, never blocking.
- **Cross-domain leakage was prevented** — a Caddy rule never appears in a
  Globular briefing or edit-check, and vice versa; multi-domain unscoped queries
  fail closed.
- **Meta-principle mapping works** for at least one promoted Caddy rule.
- **etcd analysis revealed a likely missing future meta-principle family** around
  identity-bound / aliased mutable state.

## 2. Evidence points

**Caddy live run**
- ~50–60% of accepted candidates judged **load-bearing**.
- **0 wrong.**
- **0 fabricated citations.**
- The model **narrowed noisy bundles** (split conflated mega-buckets into sharp
  per-concept rules) instead of amplifying them.

**etcd live run**
- ~40–60% **load-bearing**.
- **0 wrong.**
- **0 fabricated citations.**
- Produced **domain-specific** rules around proto mutex copy, raft message
  mutation, and the client dial/auth contract.

**PR arc**
- **#16/#17** — cold-source cage + LLM drafter
- **#19** — manual / session drafter tooling
- **#20** — file-concept theme clustering
- **#21** — dependency/build-noise exclusion + auto-window planning
- **#22/#23** — domain-scope consumer (scope resolver + serving isolation)
- **#24** — repo/domain/provenance producer + pilot promotion
- **#25** — warning-level enforcement (domain passthrough + provenance prose +
  EditCheck)

## 3. What is intentionally NOT claimed

- **Not SaaS-proven** across all languages/repos — two Go repos, not a general
  guarantee.
- **Not autonomous truth** — a drafter proposes; a human promotes.
- **No auto-promotion.**
- **No hard blocking.**
- **No CI enforcement.**
- **No broad product demo yet.**
- The **etcd Phase-D mapping is analysis-only**, based on the prior dry-run /
  scoring transcript — *not* committed graph truth (etcd candidates were never
  persisted as artifacts).
- The candidate meta-principle **`meta.code.identity_bound_state_must_not_be_copied`**
  is a **future observation**, not an authored corpus rule.

## 4. Why it matters

The core loop:

```
existing repo scars (bugs / reverts / review comments)
  → candidate rules (LLM drafter)
  → citation validation (the cage: cite or be rejected)
  → human promotion (explicit, gated)
  → domain-scoped serving (isolated per repo)
  → warning-level enforcement (EditCheck warns, never blocks)
  → meta-principle mapping (local rule → reusable law)
  → gap discovery (where no law yet exists)
```

**The central insight:** *Sensei can extract the laws a project already paid for in
bugs, reverts, and reviews — then make future agents aware of them before they
repeat the same class of mistake.* The Caddy `dispenser.Errf` rule and the etcd
aliasing findings are not invented; they are scar tissue, re-served as foresight.

The most valuable signal in v0 was not the rules that mapped to existing
principles (reuse across repos — expected and confirmed) but the two independent
etcd candidates that **converged on a principle that does not yet exist**. That
is the cold-source loop pointing at where the next law is hiding.

## 5. Phases since v0 (completed)

The v0 roadmap A–F is done, and two further phases followed.

**A–F (the v0 roadmap):** product/demo README and adoption guide; hard-gate
design → v1 dry-run → CI report-only; non-Go validation (vite live, then Python
`pydantic`/`flask`, Rust `tokio`, and a full k8s pass); the identity/aliasing
meta-principle `meta.code.identity_bound_state_must_not_be_copied` formalized
(two-repo); and the etcd candidates persisted under `pilot/etcd`.

**Grounding — the draft → *accept* verification phase.** Design
([`coldsource-grounding-design.md`](coldsource-grounding-design.md)), mechanical
implementation, and a measured k8s validation (recorded as §11 of the grounding
design). It closes the cage gap the k8s run exposed: a citation must not merely
*resolve* — the cited invariant must be *encoded*. It introduces the provenance
tiers `test_encoded > landed_commit > review_suggestion > unresolved` and
**strongest-anchor acceptance with `!`/`~` flags**. Measured result: on the k8s
LLM run it auto-segregated the two weak candidates (including the feature-gate
one the *old binary cage had accepted at high confidence*) while keeping every
correctly-cited invariant.

**Intent mining — the complement to coldsource.** Design
([`intent-mining-design.md`](intent-mining-design.md)), a mechanical grounding
implementation, **and the LLM extraction (proposer) half** — so `awg intent-mine`
now runs the full loop. Coldsource finds **what burned**; intent mining finds
**what the system was meant to preserve** — it *gathers* rule-bearing excerpts
from a repo's stated charter (docs, ADRs, comments, tests, commits, schemas), an
**LLM proposes** cited intent candidates, an **intent cage** rejects any
fabricated source citation, and `GroundIntent` grounds them against what the code
encodes — surfacing the **divergences** (stale / hidden / missing / ambiguous).
It reuses the grounding spine, keeps the trust model — **the LLM proposes intent,
Sensei grounds it, humans approve meaning** — and applies the same `>80%` router:
auto-map to an *existing* invariant/meta-principle only; **creating new intent
stays human even at high certainty.** The LLM proposer is opt-in (key from the
environment only); the echo proposer runs the loop with no key. Demonstrated on
this repo: a `docs,schemas` LLM dry-run proposed cited candidates including a
`stale_intent` the symbol check caught and two that auto-mapped to existing
invariants.

**Coldsource ↔ intent-mining bridge.** Two mechanical converters wire the loops
together. *coldsource → intent:* a scar-mined candidate is lifted (`ScarsImply`,
no stated source) and grounding classifies it as **hidden_intent** (code encodes
a rule no doc explains) or **missing_invariant** (scars imply it, nothing encodes
it) — giving a scar a candidate home and asking whether the charter ever named it.
*intent → coldsource:* a `stale_intent`/`ambiguous_owner` finding emits a **finder
hint** at the divergent file — the likely next-scar site. `awg intent-mine
--from-coldsource` runs the bridge; dry-run, no LLM.

**Corpus integration — the human-gated path to corpus truth.** Design
([`corpus-integration-design.md`](corpus-integration-design.md)) and an
implementation: `awg corpus plan|materialize|validate`. This is where the trust
boundary is made structural — **reports can be generated automatically; corpus
truth cannot.** `plan` (read-only) classifies a findings report into
`integrate | hold | never`; `materialize` writes **only `status: candidate`** YAML
entries for human-*selected*, integrate-eligible findings, under a `candidates/`
tree — it refuses `never` findings, forces `candidate` even for active-eligible
ones, and never touches the seed, PUTs a graph, promotes, or mints a principle;
`validate` checks metadata, status, grounding tier, and citation resolution
(`drift_warning`/`candidate_principle` may never be active). Promotion to
`reviewed`/`active` and the minimal-owned-triples seed append remain separate
human/PR steps.

## 6. Still not started (explicitly deferred)

- **Tokio LLM gate run** — deferred after the controlled k8s validation.

The cold-bootstrap → grounding → intent-mining → bridge → corpus-integration arc
is now complete in code; the remaining item is a single validation run.

Each is its own explicitly-approved step. The trust model is invariant across all
of them: extraction/proposal may be machine-driven; **grounding is mechanical;
promotion and new meaning are human.**

---

*Status: milestone note, updated 2026-06-15 to record the grounding,
intent-mining, bridge, and corpus-integration phases. No code, graph, or principle
changes were made in producing this note. Related:
[`coldsource-grounding-design.md`](coldsource-grounding-design.md),
[`intent-mining-design.md`](intent-mining-design.md),
[`corpus-integration-design.md`](corpus-integration-design.md),
[`cold-bootstrap-adoption.md`](cold-bootstrap-adoption.md), and the Phase-D Caddy
and etcd mapping notes in ai-memory.*
