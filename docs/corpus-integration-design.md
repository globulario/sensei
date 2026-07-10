# Corpus integration: turning grounded findings into corpus truth — human-gated

*Design only — a proposal, not implemented. No engine, corpus, graph, seed, pack,
or principle changes are made by this document. It designs the **safe, human-gated
path** from a reviewed, grounded dry-run finding to an official AWG corpus entry.*

> **The trust rule this whole design protects:**
> **Reports can be generated automatically. Corpus truth cannot.**
> Coldsource, intent-mine, grounding, and the `>80%` router all produce *reports*.
> A report becomes corpus truth only after a human selects it, and only through a
> reviewed PR. There is no path from a dry-run to an active rule that skips a human.

---

## 0. The pipeline

```
grounded candidate (a dry-run finding)
  → HUMAN-REVIEWED SELECTION        (a person picks which findings, per §7 checklist)
  → YAML corpus entry               (authored under docs/awareness/…, status: candidate)
  → MINIMAL owned triples           (only the new entry's triples appended to the seed)
  → generated seed / pack freshness (build-awareness-graph.sh --check; pack only if meta.*)
  → PR                              (docs-only or owned-triples-only; scope-guarded)
  → MERGE                           (human approval; status may advance to reviewed/active)
```

Every arrow after the first is human-driven or human-gated. The machine's job ends
at "grounded candidate"; everything past it is editorial.

## 1. What CAN be integrated (after human selection)

| Candidate type | Integrates as | Notes |
|----------------|---------------|-------|
| **strong_intent** | `intent` (+ `invariant` link) | doc + code/test agree — the clean case |
| **hidden_intent** | `invariant` + a doc note | code/test encode it; integration *adds the missing doc* |
| **missing_invariant** | `candidate_principle` or `invariant` (scoped) | scars imply it; needs a real anchor before `active` |
| **coldsource load-bearing** | `invariant` / `forbidden_fix` / `failure_mode` + `required_test` | the verified scar-rules |
| **sibling evidence** | `sibling_evidence` on an existing `meta.*` | corroboration, **never** a new principle |
| **pilot candidate with real citations** | domain-scoped `pilot rule` | only if citations resolve (cage-verified) |
| **stale_intent** | `drift_warning` **only** | evidence of drift, *not* an active rule |
| **ambiguous_owner** | conflict note **only** | contested truth, *not* an active rule |

`stale_intent` and `ambiguous_owner` are admitted **as findings, never as truth** —
they record *that* something is wrong, not *what the rule is*.

## 2. What must NOT be integrated automatically (ever)

- **ungrounded_claim** — no anchor proves it.
- **review_suggestion-only** findings — a proposal, not an encoded rule.
- **unresolved evidence** — citations that don't resolve.
- **fabricated / uncited drafts** — rejected by the cage; never reach selection.
- **stale_intent / ambiguous_owner as *active* truth** — drift/conflict evidence only.
- **novel meta-principles** — a new `meta.*` is *always* a human zoom-out (the
  `>80%` router auto-maps to **existing** principles only).
- **repo-specific candidates into the shared/global domain** — without an explicit,
  human-set scope, a repo's rule stays in that repo's domain.

These are not "discouraged" — there is no code path that integrates them without a
human, and the safe-generation gates (§6) plus the review checklist (§7) exist to
keep it that way.

## 3. Corpus entry types

| Type | Purpose | Can be `active`? |
|------|---------|------------------|
| `intent` | a stated architectural intent, grounded | yes (when grounded ≥ landed_commit) |
| `invariant` | a rule the system must preserve | yes |
| `failure_mode` | a way the system breaks | yes |
| `forbidden_fix` | a fix that looks right but is wrong | yes |
| `required_test` | a test that must exist/pass | yes |
| `sibling_evidence` | corroboration for an existing `meta.*` | attaches to an active principle |
| `candidate_principle` | a proposed new `meta.*` | **no** — review_only until human-promoted |
| `pilot rule` (domain-scoped) | a foreign-repo rule, scoped | yes, *within its domain only* |
| `drift_warning` / `stale_intent note` | recorded divergence | **no** — advisory evidence only |

## 4. Required metadata (every integrated entry)

```yaml
id: <stable, domain-prefixed>
domain: <fqdn>            # repo / source_set scope; "shared" only by explicit review
repo: <owner/name>
source_set: <pilot/… or repo path>
status: candidate | reviewed | active          # §5
grounding:
  tier: test_encoded | landed_commit | maintainer_intent |
        docs_only | review_suggestion | unresolved
evidence_citations: [file:…, commit:…, pr:…]   # the exact, resolving citations
source_run: <coldsource|intent-mine run id, if available>
related_symbols: […]     # code symbols/files the rule protects
related_tests: […]
related_invariants: […]
related_meta_principles: […]                    # advisory mappings (human-confirmed)
provenance: coldsource | intent_mine | manual | pilot
review:
  label: load-bearing | shallow | wrong | duplicate | drift
  reviewer_note: "<why accepted / scoped / held>"
promotion:
  promoted_at: <date>     # when it became reviewed/active
  promoted_in: <commit>   # the merge that activated it
```

`grounding.tier` and `evidence_citations` are **carried from the dry-run** — the
corpus entry inherits exactly the provenance the grounder computed, so the entry
can never claim stronger evidence than the run produced.

## 5. Promotion / status rules

- **`candidate`** — stored, **not authoritative**. Does not influence briefings,
  edit-checks, or gates. Most integrations land here first.
- **`reviewed`** — a human accepted it as useful knowledge (correct, well-scoped),
  but it is still not enforcing anything.
- **`active`** — allowed to influence briefings / checks / gates. Requires:
  grounding ≥ `landed_commit`, a resolving citation set, correct domain, and an
  explicit human promotion.
- **stale / conflict findings never become `active` rules** — they live as
  `drift_warning` evidence; resolving the drift is separate work.
- **domain-scoped by default** — an entry is active only within its `domain`.
- **shared / global** (a rule that applies across repos, e.g. a `meta.*`) requires
  **explicit human review** and the coverage discipline of §6.

## 6. Safe generation rules

The seed (`golang/server/embeddata/awareness.nt`) and the generated principle pack
(`cmd/awg/templates/awareness/meta_principles.yaml`) are **generated artifacts**.
Integration must not let them drift.

- **No full seed regen drift.** Append only the **minimal owned triples** for the
  new entry — never a full `awg rebuild` (which produces large unrelated churn and
  removes triples it doesn't own). Extract the entry's triples from
  `awg build --input docs/awareness/<dir>` and append exactly those.
- **Generated pack changes only when meta-principles change.** A non-`meta.*` entry
  (an invariant/intent/failure_mode) must leave `meta_principles.yaml` untouched.
- **`scripts/build-awareness-graph.sh --check` must pass** — the ownership-aware
  seed-freshness gate (only owned triples must be current; services lead/lag is
  tolerated).
- **`scripts/sync-principle-pack.py --check` must pass** *if the pack is affected*
  (a meta-principle was added/changed).
- **N-Triples validation must pass** — the appended triples must parse.
- **Coverage ratchets must be satisfied for any new meta-principle** — a new
  `meta.*` needs a tier in AWG's `docs/awareness-control/meta_principle_coverage.yaml`
  (auto code_scanner, or a registry entry), respecting
  `enforcement_ratchet.max_review_only`. A `candidate_principle` that can only be
  `review_only` must bump the budget deliberately, with justification.
- **Cross-repo checks validate integration only** — declaration and artifact
  gates may still read external project data, but ownership repairs happen in
  awareness-graph first. A new AWG principle must not require a companion PR in
  another repo just to classify its enforcement tier.

## 7. Human review checklist

Before selecting a finding for integration, a reviewer answers:

1. **Does the evidence really support the claim?** (read the cited file/test/commit)
2. **Is the grounding tier strong enough** for the intended status? (`active` needs
   ≥ `landed_commit`)
3. **Is the domain correct?** (repo-specific vs shared)
4. **Is this repo-specific or general?** (default to repo-specific)
5. **Is there a test or landed commit** behind it, or only docs/review?
6. **Is the claim already covered** by an existing invariant/principle? (then it's
   a duplicate or sibling, not a new entry)
7. **Should this be sibling_evidence instead of a new principle?** (almost always
   yes, for anything resembling an existing `meta.*`)
8. **Could this become an unsafe scanner/detect rule if overgeneralized?** (a rule
   that would fire on healthy code is worse than no rule — keep scope tight)

A "no" or "unsure" on 1, 2, or 8 means **do not integrate** (or integrate only as
`candidate` / `drift_warning`).

## 8. Non-goals

- **No auto-promotion** — nothing becomes `reviewed`/`active` without a human.
- **No auto-minting of new meta-principles.**
- **No hard-gate changes, no CI behavior changes.**
- **No graph mutation in this (or the implementing) dry-run path.**
- **No LLM or key use** — integration is editorial + mechanical generation.
- **No promotion of existing dry-run results** — the k8s/echo/LLM runs to date stay
  reports until separately reviewed.
- **No enforcement / detect rules** — a `detect` block is a separate, later,
  explicitly-gated decision (see [`hard-gate-design.md`](hard-gate-design.md)).

## 9. Proposed future CLI shape (design only — not implemented)

```
awg corpus plan        --from <report.yaml>        # show what COULD integrate, by §1/§2
awg corpus materialize --selected <ids> --status candidate   # author YAML + minimal triples
awg corpus validate                                 # run the §6 generation gates locally
```

`plan` is read-only and classifies a run's findings into integrate / hold / never.
`materialize` writes **only** YAML corpus entries + minimal owned triples for the
**human-selected** ids, always at `status: candidate`. `validate` runs the
freshness/pack/ntriples/ratchet checks. None of these promote; promotion to
`reviewed`/`active` is a separate, reviewed edit. **None are built yet.**

## 10. Examples

**A. strong_intent from Globular docs/tests → intent + invariant link.**
A `docs,tests` run grounds `repository.desired_state_owned_by_repository` at
`test_encoded` (the owner RPC + its test). Reviewer confirms evidence, scopes it to
`globular-services`, integrates an `intent` entry + a `related_invariants` link to
`four_layer.truth_read_via_owner_rpc`. Eligible for `active` (≥ landed_commit).

**B. k8s `status_manager` badly-cited finding → drift/re-derive note, not truth.**
The grounded finding cited a line + a PR comment, not the real test → segregated.
Integration path: a `drift_warning` recording "amplification invariant likely real,
this draft cited it badly — re-derive against `TestSyncPodIgnoresNotFound`." It is
**not** an active rule and carries no enforcement.

**C. etcd transcript-derived pilot candidate → candidate only.**
The `pilot/etcd` candidates are transcript-derived; their commit/PR ids were never
re-verified. Integration admits them at `status: candidate` only, with
`grounding.tier: review_suggestion`, and the reviewer_note "must re-derive and
verify citations against the etcd repo before any promotion" — exactly the honesty
already in `pilot/etcd/README.md`.

**D. Caddy verified candidate → domain-scoped reviewed rule.**
The `pilot/caddy` candidate has **real PR-comment ids** (cage-verified). Integration
admits a `pilot rule` scoped to `github.com/caddyserver/caddy`, `status: reviewed`,
`provenance: pilot` — active **within the Caddy domain only**, never leaking into a
Globular briefing.

---

*Status: design proposal. No code, graph, seed, pack, or principle changes were
made in producing this note. The implementing work (the `awg corpus` commands and
the YAML→minimal-triples generator) is a separate, explicitly-approved step.
Related: [`coldsource-grounding-design.md`](coldsource-grounding-design.md) (the
grounding spine), [`intent-mining-design.md`](intent-mining-design.md) (the intent
loop), [`hard-gate-design.md`](hard-gate-design.md) (warning → block), and
[`cold-bootstrap-adoption.md`](cold-bootstrap-adoption.md).*
