# Intent mining: extracting architectural intent, then grounding it

*Design only — a proposal, not implemented. No engine, cage, corpus, graph,
seed, or principle changes are made by this document. It designs how Sensei could
mine **stated architectural intent** from a project's explicit human sources,
ground that intent against what the code actually encodes, and surface where the
two diverge — without ever letting a doc or an LLM become trusted truth on its
own.*

> **The distinction this whole design protects:**
> **The LLM may propose intent. Sensei must ground it. Humans approve meaning.**
> Docs can *propose* a rule; docs alone are never final truth.

---

## 1. Problem statement

Coldsource learns from **scars** — regressions, reverts, fixes, repeated
failures. It answers *"what burned?"* But a large part of an architecture is
**stated intent** that was written down precisely so it would *not* burn: the
four-layer truth model, "etcd is the sole source of truth," "MinIO is not for
packages," "the controller decides, node-agents execute." That intent lives in
ADRs, design docs, proto comments, CLI help, error messages, README constraints,
and the tests that quietly encode it.

Two failure modes follow from never mining that intent:

1. **Silent drift.** A stated rule erodes one "simple fix" at a time. The doc
   still says X; the code now does not-X; nobody notices until it fails
   expensively. This is exactly the drift the awareness graph exists to prevent —
   but today Sensei only catches it *after* a scar.
2. **Hidden law.** A test or a landed fix encodes a real rule that no doc
   explains. New contributors (and agents) re-derive it by breaking it.

Intent mining reads the **charter**, grounds it against the **reality**, and the
*most valuable output is the disagreement* — stated-intent vs encoded-reality
mismatches are the architecture-drift detector, surfaced before the scar.

Coldsource finds **what burned**. Intent mining finds **what the system was meant
to preserve** — and whether it still does.

## 2. Source types and trust tiers

Intent is mined from explicit human sources, ranked by how strongly the source
*proves* a rule (not how confidently it *states* one). A README can assert a rule
in capital letters and still be Tier 4.

| Tier | Name | Sources | Evidence strength |
|------|------|---------|-------------------|
| **1** | **Executable truth** | tests, CI gates, type/proto/schema constraints, generated contract checks | a machine enforces it |
| **2** | **Landed behavior** | implementation code, landed commits, regression fixes, revert/repair commits | the system does it |
| **3** | **Maintainer intent** | ADRs, design docs, PR-review explanations by maintainers, issue decisions | a human with authority meant it |
| **4** | **Descriptive docs** | README, tutorials, user guides, code comments | a human described it |
| **5** | **Weak hints** | naming, folder structure, examples, conventions | a pattern suggests it |

**This reuses, and extends downward, the coldsource grounding spine**
([`coldsource-grounding-design.md`](coldsource-grounding-design.md)). Coldsource
grounds *code* evidence: `test_encoded > landed_commit > review_suggestion >
unresolved`. Intent mining maps onto the same vocabulary and adds the intent-only
tiers below landed code, because **stated intent is weaker evidence than a scar**:

```
test_encoded / ci_gate / schema_constraint   (Tier 1)   ─┐
landed_commit                                (Tier 2)    │  grounded
─────────────────────────────────────────────────────────┤
maintainer_intent                            (Tier 3)    │
docs_only                                    (Tier 4)    │  proposed, not grounded
weak_hint                                    (Tier 5)    │
─────────────────────────────────────────────────────────┤
conflicting    (a Tier-3/4 claim contradicted by Tier-1/2 reality)
unresolved     (no anchor of any tier)                  ─┘  not trusted
```

**The grounding bar is Tier 2.** An intent is *grounded* only if it reaches
`landed_commit` or above. Tier 3–5 evidence can *propose* an intent and *raise*
its priority for review, but never grounds it alone.

## 3. Pipeline

```
docs / ADRs / comments / tests / PRs / commits / schemas
  → EXTRACT candidate architectural intent          (LLM proposes; cited)
  → LINK intent to code symbols / files
  → LINK intent to tests or landed commits
  → LINK intent to existing invariants / meta-principles
  → GROUND: classify grounding strength (Tier 1–5 / conflicting / unresolved)
  → CLASSIFY output class (§6)
  → produce reviewable candidates                   (nothing written to a graph)
  → HUMAN decides: accept | reject | map to existing | create new principle
```

Extraction is LLM-backed (proposing intent from prose is a reasoning task).
Everything after extraction is **mechanical grounding** reusing the coldsource
GROUND phase — git, file reads, symbol resolution — plus a divergence check
(does the code agree with the stated intent?). The human gate is unchanged from
every other Sensei path.

## 4. Grounding model

Intent mining inherits the coldsource grounding mechanics verbatim
(`GroundCandidate`: symbol re-resolution, `commit-touches-file`, test detection,
drift flags) and adds two intent-specific operations:

**(a) Anchor classification.** Each piece of evidence cited for an intent is
tiered (§2). The intent's *grounding tier* is the strongest anchor; its *stated
tier* is the strongest doc/ADR source. Both are recorded — the relationship
between them is what produces the output class.

**(b) Divergence check.** When the stated intent names a symbol/behavior, the
grounder checks whether the code/test **agrees, is silent, or contradicts**:
- agrees → corroboration;
- silent (no code anchor) → `hidden`/`docs_only`, not grounded;
- contradicts (code encodes not-X) → `conflicting`.

### Rules (binding on the design)

- **No intent without evidence.** Every candidate cites at least one source;
  uncited extraction is dropped (the citation cage, unchanged).
- **No invariant without a code/test/commit anchor.** A candidate may only be
  proposed *as an invariant* if it reaches Tier 2+. Tier 3–4-only candidates are
  proposed as *intent*, explicitly marked ungrounded.
- **No new meta-principle without human review.** Extraction may *map to an
  existing* `meta.*` (advisory, per the grounding §6 router); it may never mint a
  new principle. New principles are a human zoom-out.
- **Docs can propose intent, but docs alone are not final truth.**
- **Docs vs code disagree → mark `stale`/`conflicting`, never accept.**
- **Tests/code prove a rule docs don't explain → mark `hidden_intent`.**
- **Repeated failures imply a rule with no docs/tests → mark `missing_invariant`**
  (this is the hand-off point from coldsource — see §8).

## 5. Candidate schema

```yaml
intent_id: repository.release_state_owned_by_repository
claim: >-
  Desired release state is owned by the repository service and must be read
  through repository APIs, not via direct storage access.
category: ownership            # see category enum below
sources:                       # WHERE the intent was stated (Tier 3–5)
  docs:    ["docs/architecture/four-layer.md#desired", "CLAUDE.md#prime-rules"]
  comments: ["golang/repository/.../desired.go: // owner of desired state"]
evidence:                      # WHAT grounds it in reality (Tier 1–2)
  code:    ["golang/repository/.../desired_state.go:GetDesiredService"]
  tests:   ["golang/repository/..._test.go:TestDesiredReadViaRPC"]
  commits: ["<sha> repository: route desired reads through owner RPC"]
related_invariants:
  - four_layer.truth_read_via_owner_rpc_not_direct_storage
related_meta_principles:
  - meta.storage_is_not_semantic_authority      # advisory mapping; human-confirmed
grounding:
  stated_tier:  maintainer_intent               # strongest doc/ADR source
  grounding_tier: test_encoded                  # strongest code/test anchor
  tier: test_encoded | landed_commit | maintainer_intent |
        docs_only | weak_hint | conflicting | unresolved
output_class: strong_intent                     # §6
status: candidate                               # never auto-promoted
provenance:
  extracted_by: llm:<model>                     # proposer is recorded
  decided_by: human                             # required before any promotion
```

`grounding.tier` is the **overall verdict** (the relationship of `stated_tier`
and `grounding_tier`, resolved through the output class). `status` is always
`candidate`; nothing here enters a graph without the human step.

### Intent categories (enum)

`ownership` · `lifecycle` · `compatibility_rollback` · `security_auth_boundary` ·
`concurrency_identity` · `failure_response` · `api_contract` ·
`ui_truth_presentation` · `operational_deployment`

These mirror the meta-principle families, so a grounded intent maps cleanly to
the corpus (e.g. `ownership` → `meta.storage_is_not_semantic_authority`;
`concurrency_identity` → `meta.code.identity_bound_state_must_not_be_copied`).

## 6. Output classes

The class is a function of two axes — **was it stated?** and **is it encoded?** —
plus owner-conflict and no-anchor. The *divergence* classes are the point.

| Class | Stated (Tier 3–4) | Encoded (Tier 1–2) | Meaning / action |
|-------|:---:|:---:|------|
| **strong_intent** | yes | yes, agrees | doc + code + test agree → high-value, ready for human accept |
| **stale_intent** | yes | yes, **contradicts** | docs say X, code/tests do not-X → fix the doc or the code; never accept as-is |
| **hidden_intent** | no | yes | tests/code encode a rule no doc explains → propose documenting + an invariant |
| **missing_invariant** | no | no, but **scars imply it** | repeated failures imply a rule with no doc/test → propose an invariant + test (coldsource feed) |
| **ambiguous_owner** | ≥2 sources | imply **different owners** | two sources claim different owners of one truth → resolve ownership before trusting |
| **ungrounded_claim** | yes (or LLM) | no anchor | a doc/comment/LLM asserts intent with nothing proving it → hold as a lead, not knowledge |

`strong_intent` is the only class that is a clean "accept" candidate.
`stale_intent`, `hidden_intent`, `missing_invariant`, `ambiguous_owner` are
**findings** — drift the graph wants surfaced. `ungrounded_claim` is segregated
exactly like a coldsource review-only lead.

## 7. Examples from Globular / Sensei

| Intent (illustrative) | Stated in | Encoded in | Class |
|---|---|---|---|
| `etcd.sole_source_of_truth` (no env vars / hardcoded addrs) | CLAUDE.md hard-rule #1 | config fallback chain errors out; `make check-services` (CI gate) | **strong_intent** |
| `cluster_controller.no_os_exec_syscall_systemctl` | CLAUDE.md security boundary | `make check-services` CI gate (Tier 1) | **strong_intent** |
| `proof_writer.anchors_installed_unix_to_pid_start` | *(undocumented at the time)* | code + `TestProof…` (INC-2026-0016) | **hidden_intent** → propose documenting |
| `node_stable_ip_owner` | `PrimaryIP()` vs `StableIP(clusterVIP)` both used | VIP-holder returns VIP from `PrimaryIP()` | **ambiguous_owner** → resolve owner |
| `diagnostic_output_must_be_bounded` | *(no doc/test before INC)* | doctor event amplification recurred (scars) | **missing_invariant** → coldsource feed |
| `minio.not_for_packages` | CLAUDE.md + memory | packages live in POSIX CAS; *if* any path read packages from MinIO | **stale_intent / conflicting** if found |
| `release_state_owned_by_repository` | four-layer docs | repository owner RPC + test | **strong_intent** |

The valuable rows are the non-`strong` ones: `hidden_intent` writes down a law
the codebase already obeys silently; `ambiguous_owner` catches a real
`PrimaryIP`/`StableIP` ambiguity *before* it evicts an etcd member;
`missing_invariant` is where coldsource and intent mining meet.

## 8. Interaction with coldsource grounding

Intent mining **complements** coldsource; it does not replace it. They share the
GROUND phase and the human gate, and differ only in where they start:

```
            ┌─────────────── coldsource ───────────────┐
   scars →  reverts / fixes / review-rule-language  →   candidate RULE ─┐
            └───────────────────────────────────────────┘               │
                                                                         ├─► GROUND (shared) ─► HUMAN gate ─► corpus
            ┌─────────────── intent mining ────────────┐                 │
   charter→ docs / ADRs / comments / tests / schemas →  candidate INTENT─┘
            └───────────────────────────────────────────┘
```

- **Coldsource → intent mining:** a coldsource cluster of scars with no doc/test
  is a `missing_invariant` intent finding — intent mining gives the scar a name
  and a home in the charter.
- **Intent mining → coldsource:** a `stale_intent` (doc says X, code does not-X)
  predicts where the *next* scar will be — a finder hint for coldsource.
- **Shared grounding:** both run `GroundCandidate`; intent mining adds the anchor
  tiers and the divergence check, but the symbol re-resolution, commit-touches,
  test detection, and `review_suggestion`/`unresolved` segregation are identical.
- **Shared human gate + §6 router:** the `>80% earned correlation` router maps a
  grounded intent to an *existing* `meta.*` automatically (audited, reversible);
  a *new* principle is always the human zoom-out. New intent never auto-mints.

Sensei's job after the human accepts: store the reviewed
**intent → invariant → test → code → meta-principle** links so the charter and
the reality stay tied, and the next divergence is detectable.

## 9. Non-goals

- **No engine implementation** — this is the contract, not the code.
- **No corpus, graph, seed, pack, or ontology changes.**
- **No new principles, no promotion, no graph mutation.**
- **No scanners, no hard gates, no CI behavior change.**
- **No LLM or key use** in this document; extraction is LLM-backed *when
  implemented*, and even then is dry-run, cited, and human-gated.
- **No auto-minting of meta-principles** — extraction may map to existing `meta.*`
  advisorily; minting stays human.

## 10. Future implementation phases (not started)

1. **Source adapters (read-only)** — extract candidate intent from one source
   type at a time (start with proto/schema comments + ADRs: highest
   intent-density, lowest noise). Dry-run, cited.
2. **Anchor + divergence grounding** — extend `GroundCandidate` with the Tier 1–5
   anchor classifier and the agrees/silent/contradicts check; emit output
   classes. Mechanical, no LLM.
3. **Output-class report** — a scoring sheet like coldsource's, grouped by class,
   with `stale`/`ambiguous`/`missing` surfaced first (the findings, not the
   confirmations).
4. **`sensei intent-mine`** *(design only here)*:
   ```
   sensei intent-mine --repo . \
     --sources docs,comments,tests,prs,commits,schemas --dry-run
   ```
5. **Corpus links** — on human accept, persist the
   intent→invariant→test→code→meta-principle edges (separate, gated PR).
6. **Coldsource bridge** — wire `missing_invariant` ↔ coldsource scars both ways.

Each phase is its own explicitly-approved step. This PR is phase 0: the design.

---

*Status: design proposal. No code, graph, or principle changes were made in
producing this note. Related:
[`coldsource-grounding-design.md`](coldsource-grounding-design.md) (the shared
grounding spine), [`hard-gate-design.md`](hard-gate-design.md) (draft → block),
[`cold-bootstrap-adoption.md`](cold-bootstrap-adoption.md),
[`milestone-cold-bootstrap-v0.md`](milestone-cold-bootstrap-v0.md), and
[`corpus-integration-design.md`](corpus-integration-design.md) (the human-gated
path from a grounded finding to corpus truth). Coldsource finds what burned;
intent mining finds what the system was meant to preserve.*
