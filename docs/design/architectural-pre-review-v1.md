# Sensei Architectural Pre-Review Report — v1 (contract freeze)

Status: **frozen contract (Milestone PR-0)** — model, vocabularies, canonical
identity, disposition, and reviewer-attention ranking. Live source adapters
(diff, graph, ledger, result, certification, neural) and the CLI/GitHub surfaces
arrive in later milestones and do not change this contract.

Schema version: `architectural-pre-review.v1`
Attention policy version: `reviewer-attention.rank.v1`

## 1. What this is

A deterministic, evidence-linked report generated for a proposed repository
change *before* a human begins detailed review. It answers what changed, what it
means architecturally, which governed rules apply, whether the actor was
authorized, whether the observed patch stayed in scope, what is proven, what
evidence is missing, how the result architecture changed, and — above all — what
still requires human judgment.

It does **not** replace code review. It removes mechanical reconstruction and
already-provable questions so reviewers spend attention only where a human must.

## 2. Core law — projection, never authority

```
PreReviewReport != architecture fact
              != admission decision
              != proof discharge
              != certification
              != completion
```

The report may *display* authoritative verdicts sourced from receipts. It may
never create or upgrade one. There is no hidden certification, admission, or
completion engine in this package. Every verdict shown is traceable to a receipt
digest; a caller-supplied boolean or a pre-set disposition can never establish
one — `Finalize` always re-derives the disposition and summary from evidence.

## 3. Coverage levels

A report declares the evidence it had. A later level's absence never erases
earlier findings.

| level | adds | may claim |
|---|---|---|
| `advisory` | diff, graph briefing, protection findings | impact, applicable rules, questions — **not** governed mutation or correctness |
| `governed` | task/actor/authority/admission/consumption/observed-change/scope | authorization and scope — **not** correctness |
| `proof_bound` | result binding, evidence, proof discharge, certification receipt | an independently verified certification |
| `terminal` | fresh result graph/artifacts, completion receipt | terminal architectural closure |

## 4. Dispositions

Closed vocabulary, distinct from certification verdicts. Blocking states are
evaluated most-urgent-first; the two positive terminal states are honored only
from their receipts.

```
cannot_verify > scope_violation > governance_required > mechanical_repair_required
> evidence_required > architect_decision_required > ready_for_human_review
> certified > terminally_closed
```

- `certified` requires a certification receipt (`ProofSummary.Certification`
  with verdict `certified` **and** a receipt digest).
- `terminally_closed` requires a completion receipt **and** a valid
  certification.
- Scope verification alone never yields `certified` — scope is not correctness.

## 5. Epistemic states

Load-bearing statements are grouped and never merged into one confidence score:
`observed`, `governed`, `deterministically_inferred`, `model_candidate`,
`contradicted`, `unknown`, `stale`, `not_applicable`, `uncertifiable`. A
`model_candidate` may never appear as a governed impact/protection/proof finding
and may never block.

## 6. Reviewer attention — the flagship

Each item names one question only a human may legitimately answer, why it
matters, whether it blocks, its evidence, related files, allowed answers, and a
resolution path. Ranking is deterministic and versioned
(`reviewer-attention.rank.v1`) — never model confidence:

```
score = blocking(100 if blocking)
      + severity_rank * 10
      + clamp(task_relevance, 0, 3) * 5
      + clamp(architectural_reach, 0, 3) * 5
      + human_decision_bonus(20 for architect/unknown-direction/contradiction/authority-conflict)
      - mechanical_penalty(15 for result-graph-change/waiver-expiring/model-candidate)
```

Ties break blocking-first, then severity, then a fixed category order, then ID.
Equivalent questions (same normalized text, or same ID) collapse to the
highest-ranked occurrence. The full ranked set lives in JSON; Markdown shows the
top `DefaultMaxReviewerItems` (7) and notes the remainder.

## 7. Canonical identity

- `ReportID = "prereview." + sha256(binding-identity)[:24]` over schema version,
  repository domain, base/head revisions and tree digests, diff digest, task ID,
  ledger head, base/result graph digests, and sorted policy IDs.
- `ReportDigestSHA256 = sha256(canonical-json(report))` with **display metadata,
  narrative, and the digest field excluded**.

Consequences (enforced by `Validate` and proven by tests): temporary paths,
render time, and PR number never change identity; logically-equal content
(after normalization) digests equal; repeated rendering is byte-identical; a
report bound to the wrong diff or a tampered body fails validation.

## 8. Non-negotiable laws (enforced in `validate.go`)

1. Bind to an exact repository and diff.
2. A task-backed report binds to the verified ledger head.
3. Base and result bindings stay distinct; an available result requires both
   graph digests.
4. Mutable projections are never authority.
5. Findings keep their source and epistemic status.
6. Scope verification does not imply correctness.
7. Certification displays only from a certification receipt, at `proof_bound`+.
8. Completion displays only from a completion receipt, at `terminal`.
9. Missing evidence cannot become a pass.
10. Model predictions are candidates and never governed findings.
11. Caller booleans and pre-set dispositions cannot establish a verdict.
12. An optional narrative may summarize but never creates findings and is never
    authoritative.
13. Display metadata does not affect semantic identity.
14. Identical inputs produce identical canonical output.
15. A degraded report explicitly names what it could not verify.

## 9. Package shape

`golang/architecture/prereview/` depends on no other Sensei package. Typed
inputs are supplied by adapters in later milestones through narrow source
interfaces (diff, graph, ledger, result, certification, neural). This milestone
ships the model, canonicalization, identity, validation, disposition, ranking,
and the JSON/YAML/Markdown renderers, with fixtures under
`docs/fixtures/architectural-pre-review/v1/` and JSON Schemas under
`docs/schemas/architectural-pre-review/v1/`.

## 10. Out of scope for v1 / PR-0

Result-architecture population (Phase 7), proof/certification adapters
(Phases 4–6 rebased onto the live transaction), the `sensei pre-review` CLI, the
GitHub Action, and neural candidates. The schema reserves those fields; the
report shows them unavailable until a verified receipt exists.
