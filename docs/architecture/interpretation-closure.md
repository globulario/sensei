# Interpretation Authority Closure

## Status

Proposal motivated by the frozen Mode B vs Mode E pilot on R1 (`f8a4e7639071`).

This document does not change the frozen benchmark revision, its corpus corrections, or the separately proposed live-unverified R2 transport.

## Summary

Sensei already refuses to say **"repair proven"** without proof. The benchmark exposed the upstream equivalent that Sensei does not yet enforce:

> **"architectural rule authoritative" without proof.**

Today an authored invariant, repair interpretation, or declared scope can become governing authority because it was written down and accepted by the pipeline. The pilot showed that Sensei's downstream rigor can then faithfully amplify an incorrect or incomplete premise.

The next architectural step is therefore to make interpretations earn authority before they can govern repair.

The target flow is:

```text
candidate interpretation
        ↓
static contradiction checks
        ↓
scope/completeness closure
        ↓
minimal-realization analysis
        ↓
proof obligations
        ↓
certified interpretation
        ↓
governed repair
        ↓
independent repair verification
```

Interpretations that do not close remain useful candidate or advisory knowledge, but must not silently become hard governing authority.

## Benchmark evidence

The pilot produced four distinct observations that define the required gates.

### Gate 1: truth — go-zero #2537

The authored invariant contradicted facts already present in the repository.

The contradiction was mechanically discoverable before synthesis:

- declared type;
- underlying type;
- implemented interfaces;
- reachable conversion behavior.

For the concrete failure, `uuid.UUID` had an underlying `[16]byte` representation and implemented `encoding.TextUnmarshaler`. The interpretation instead asserted a theory of assignability that conflicted with those facts.

Sensei then followed the governing premise coherently and lost a task that the unrestricted baseline solved by inspecting the repository.

This is not primarily an O3 intelligence problem. It is an authority-certification problem.

A governing claim must therefore be challenged against deterministic repository facts before it can become authority.

### Gate 2: completeness — grpc-go #3119

The authored interpretation did not disclose a file that the repair required.

Sensei correctly enforced the declared scope. Because the necessary repair surface was absent, the valid repair was unreachable.

The failure chain was:

```text
incomplete governed knowledge
        ↓
correct scope enforcement
        ↓
repair impossible
```

A plausible file set is not sufficient evidence that the governed surface is complete enough for the repair.

Sensei must represent scope closure explicitly as `complete`, `incomplete`, or `unknown` and must not silently upgrade an unresolved scope to hard governing authority.

### Gate 3: minimal realization — go-zero #1969

The governing invariant was broader than the upstream repair. Sensei followed it more completely across Int, Uint, and Float branches rather than only the narrower upstream path.

That broader realization was principled, but it expanded into another production file and introduced a precision defect through `float64(int64)` comparison above `2^53`.

The lesson is not that broad invariants are wrong. The lesson is that:

> **A true invariant still needs a minimal sufficient realization.**

Sensei must distinguish:

```text
what must be true
```

from:

```text
how broadly code must change to make it true
```

Invariant completeness must not automatically justify implementation breadth.

### Gate 4: independent verification — grpc-go #2630

Sensei produced a sealed candidate-ready artifact that did not compile.

This proves that interpretation closure must never become another oracle.

```text
certified interpretation ≠ correct implementation
```

Even after interpretation certification, candidate build/test verification remains an independent authority chain.

## Design law

This proposal establishes a new Sensei law:

> **Architectural knowledge must earn authority before it can govern an AI.**

More generally:

> **No claim becomes authoritative merely because a trusted mechanism successfully produced, parsed, stored, or scored it. The claim itself must carry evidence of validity.**

This law applies directly to authored architectural interpretations and mirrors Sensei's existing repair-proof discipline.

## Goals

1. Represent interpretations as candidate knowledge before certification.
2. Mechanically challenge candidate claims against repository facts.
3. Detect contradictions before synthesis.
4. Determine whether the declared repair surface is sufficiently closed.
5. Preserve unresolved scope rather than silently assuming completeness.
6. Distinguish invariant completeness from implementation breadth.
7. Attach explicit proof obligations to governing interpretations.
8. Prevent uncertified interpretations from silently becoming hard governing authority.
9. Preserve uncertainty and contradiction as first-class graph state.
10. Keep independent candidate verification mandatory after generation.

## Non-goals

This work must not:

- make O3 more capable;
- tune prompts or scope selection to benchmark outcomes;
- expose gold patches to agents or interpretation generation;
- automatically widen scope until tests pass;
- turn closure into unrestricted repository exploration during synthesis;
- treat an LLM opinion as sufficient proof of an invariant;
- silently rewrite contradicted interpretations into convenient replacements;
- weaken sealed-candidate, digest-binding, scope, or proof-integrity laws;
- claim regression safety without p2p evidence.

## Interpretation lifecycle

Add a first-class interpretation lifecycle. Exact names should reuse existing Sensei vocabulary where possible rather than creating parallel concepts unnecessarily.

Suggested states:

```text
candidate
checking
contradicted
evidence_insufficient
closure_incomplete
minimal_realization_review
certifiable
certified
superseded
```

An interpretation may exist in the graph while uncertified.

Only a certified interpretation may act as hard governing authority for a repair. Uncertified interpretations can remain advisory and useful for briefing, exploration, or review.

## Canonical shape

A possible representation is:

```yaml
interpretation:
  id: interpretation.go_zero.2537.named_type_assignability
  status: candidate

  claims:
    - id: claim.named_type_assignability
      statement: >
        Defined types whose underlying kind is supported by the unmarshalling
        path are assignable through that path.

  source_references:
    - path: internal/encoding/unmarshaler.go

  governing_invariants:
    - invariant.underlying_kind_governs_assignability

  evidence_requirements:
    - declared_type
    - underlying_type
    - implemented_interfaces
    - reachable_conversion_paths

  contradiction_checks: []

  closure:
    scope_status: unknown
    unresolved_paths: []

  realization:
    status: unknown

  certification:
    status: pending
    certifiable: false
```

Prefer to reuse existing concepts such as:

- `contract_unknown`;
- closure;
- proof obligations;
- evidence mappings;
- authority surfaces;
- certification requirements.

## Gate 1 — Truth

Before an interpretation may govern, Sensei must ask:

> **Does deterministic repository evidence contradict this claim?**

The first implementation should prioritize static facts that can be established mechanically:

- declared and underlying types;
- implemented interfaces and method sets;
- function signatures;
- call relationships;
- explicit ownership declarations;
- generated contracts;
- configuration schema;
- constants and enums;
- existing authoritative graph facts.

A contradiction fails closed.

Example:

```yaml
truth_check:
  status: contradicted
  claim_id: claim.named_type_assignability
  evidence:
    - kind: go_underlying_type
      subject: uuid.UUID
      value: "[16]byte"
    - kind: implemented_interface
      subject: uuid.UUID
      value: encoding.TextUnmarshaler
  reason: >
    The governing claim assumes a different dispatch model than the repository
    evidence establishes.
```

Sensei must not silently correct the claim. The contradiction should remain visible with provenance:

```text
claim
→ contradictedBy
→ repository evidence
```

A corrected interpretation is a new candidate or an explicitly revision-linked successor.

## Gate 2 — Completeness

A truthful interpretation may still be insufficient to govern a repair.

Sensei should establish whether the declared surface is complete enough using existing graph relations where possible:

- authority edges;
- realizes-contract edges;
- constrained-by-invariant edges;
- requires-test edges;
- call/dependency relationships;
- shared-helper relationships;
- owner surfaces;
- explicit source references.

The goal is not to discover the final patch. The goal is to justify or refuse the claim:

```text
this governed surface is sufficient
```

Possible outcomes:

```yaml
closure:
  status: complete
```

```yaml
closure:
  status: incomplete
  unresolved:
    - reason: related_authority_surface_not_disclosed
      candidate_path: grpclb_remote_balancer.go
```

```yaml
closure:
  status: unknown
  reason: insufficient_static_evidence
```

When closure is incomplete or unknown, policy may request revision, request evidence, downgrade to advisory, or stop governed synthesis. It must not silently widen the scope itself.

## Gate 3 — Minimal realization

A valid invariant can still be implemented too broadly.

Sensei therefore needs a separate minimal-realization assessment.

Questions include:

- Is there a common choke point that realizes the invariant once?
- Can one authoritative mutation satisfy the family-wide property?
- Are multiple proposed edits semantically redundant?
- Does broader realization introduce unnecessary conversions, state, duplication, or branching?
- Can the invariant be satisfied at a higher shared abstraction?

The first implementation does not need to solve general program minimization. It only needs to preserve uncertainty rather than assuming that more invariant coverage means a better repair.

Suggested statuses:

```text
minimal
candidate_minimal
broader_than_proven
review_required
unknown
```

A broader-than-proven realization does not have to fail automatically, but it must not receive a clean minimality claim.

## Gate 4 — Independent verification

Interpretation certification and repair verification remain separate authority chains.

Interpretation certification answers:

> Was the architectural premise sufficiently trustworthy to govern?

Repair verification answers:

> Did the resulting implementation actually work?

The existing sealed-candidate and disposable-verification path remains authoritative for repair execution.

A certified interpretation followed by a non-compiling candidate remains a failed repair.

## Interpretation proof obligations

Interpretations should carry proof obligations in the same spirit as repairs.

Example:

```yaml
proof_obligations:
  - id: proof.interpretation.named_type_assignability
    claim_id: claim.named_type_assignability
    required_slots:
      - declared_type
      - underlying_type
      - implemented_interfaces
      - reachable_conversion_path
```

Evidence satisfaction should preserve Sensei's existing priority model:

1. explicit evidence mapping;
2. deterministic repository-derived mapping;
3. available but unmapped;
4. missing.

A statement's presence in an interpretation document is never evidence for the statement itself.

## Authority promotion

Create an explicit promotion boundary:

```text
candidate interpretation
        ↓
certification
        ↓
governing interpretation
```

Promotion requires at minimum:

```yaml
certification:
  truth: pass
  completeness: pass
  realization: pass_or_non_blocking
  evidence: sufficient
  contradicted: false
  unresolved_critical_claims: false
  certifiable: true
```

Otherwise:

```yaml
promotion_allowed: false
```

The interpretation remains available as candidate or advisory knowledge.

## Advisory versus governing knowledge

Not every useful architectural hypothesis needs full certification.

Sensei should distinguish:

```text
advisory candidate
```

from:

```text
governing authority
```

An uncertified interpretation may support exploration, briefing, candidate discovery, or human review. It must not constrain repair correctness as though it were established fact.

## CLI/service surface

Add repository-native commands under the existing Sensei/AWG command tree, for example:

```bash
sensei interpretation check <interpretation>
```

with machine-readable output:

```json
{
  "interpretation_id": "...",
  "truth": "pass",
  "closure": "incomplete",
  "minimal_realization": "unknown",
  "certification": "blocked",
  "promotion_allowed": false,
  "missing_evidence": [],
  "contradictions": [],
  "unresolved_scope": []
}
```

and, where appropriate:

```bash
sensei interpretation certify <interpretation>
```

Certification must be deterministic over the same repository/graph state and produce a durable receipt. Checking and repair generation should remain separate operations.

## Freshness

Interpretation certification is evidence-bound and therefore can become stale.

Receipts should bind at least:

```yaml
repository_revision:
graph_revision:
evidence_digest:
certified_at:
```

Relevant repository or graph changes invalidate the authority claim until certification is refreshed.

The law is the same as elsewhere in Sensei:

> stale evidence is not present evidence.

## Learning and provenance

A contradicted interpretation should produce a learning event, but contradiction must not automatically create a replacement rule.

Example:

```yaml
learning_event:
  kind: interpretation_contradiction
  interpretation_id: ...
  contradicted_claim: ...
  evidence: ...
  correction_status: human_or_certified_revision_required
```

Preserve the historical chain:

```text
original claim
→ evidence contradicted it
→ corrected interpretation proposed
→ corrected interpretation certified
```

Do not launder a disproven causal or architectural claim by silently rewriting history.

## Regression fixtures from the pilot

Where practical, preserve the benchmark findings as deterministic regression fixtures.

### Fixture A — contradicted invariant (#2537)

Expected:

```text
candidate interpretation
→ static contradiction
→ certification blocked
→ false invariant never governs synthesis
```

### Fixture B — incomplete repair surface (#3119)

Expected:

```text
interpretation truthful
→ required-surface closure unresolved
→ certification blocked or downgraded
→ no false claim that governed scope is complete
```

### Fixture C — broader-than-proven realization (#1969)

Expected:

```text
invariant valid
→ proposed realization broader than proven necessary
→ minimal_realization = review_required
```

The system must not infer that more invariant coverage automatically means a better repair.

### Fixture D — certified interpretation, bad candidate (#2630)

Expected:

```text
interpretation certified
→ candidate synthesized
→ independent build/test fails
→ repair remains failed
```

This protects against interpretation certification becoming another oracle.

## Meta-rule: certify the evaluator too

The benchmark exposed the same authority defect in its own harness:

- `0 passed == 0 total` could become full `40/40` despite verifying nothing;
- contaminated executions could be recorded as ordinary repair failures;
- unmeasured p2p could be confused with absence of regression if result classes were collapsed.

The same law therefore applies to Sensei-generated measurements:

> **A computed result does not gain evidentiary authority merely because trusted code computed it.**

Where appropriate, evidence-bearing measurements should carry the prerequisites that make the measurement meaningful.

For example:

```yaml
test_score:
  score: 40
  evidence_valid: true
  tests_executed: 4
  denominator_nonzero: true
  execution_contaminated: false
```

This proposal does not redesign the benchmark harness. It requires the interpretation-certification layer to follow this law from the beginning.

## Acceptance criteria

- [ ] Authored interpretations enter Sensei as candidate knowledge rather than automatic authority.
- [ ] Candidate claims can be deterministically contradicted by repository facts.
- [ ] Contradiction blocks governing promotion.
- [ ] Contradiction evidence is preserved with provenance.
- [ ] Scope completeness is explicitly classified as complete, incomplete, or unknown.
- [ ] Incomplete scope cannot silently become hard governing scope.
- [ ] Minimal realization is represented independently from invariant completeness.
- [ ] A true invariant does not automatically justify a broader implementation.
- [ ] Interpretation certification has explicit proof/evidence requirements.
- [ ] Certification is revision/evidence bound and can become stale.
- [ ] Uncertified interpretations may remain advisory.
- [ ] Only certified interpretations may act as hard governing authority.
- [ ] Repair verification remains independent from interpretation certification.
- [ ] A certified interpretation can still produce a failed repair.
- [ ] No gold benchmark patch can enter interpretation certification.
- [ ] Regression fixtures cover the #2537, #3119, #1969, and #2630 failure classes.
- [ ] Existing proof-integrity, sealed-candidate, digest-binding, and result-class laws remain unchanged.
- [ ] `sensei rebuild --check` or equivalent graph-integrity validation passes when corpus/schema integration is added.

## Rollout

### Phase 1 — Truth

Start with deterministic contradiction checks. This alone would have prevented the #2537 failure.

Do not begin with LLM-based interpretation judgement.

### Phase 2 — Completeness

Use repository graph relationships to determine whether a governing interpretation has unresolved required surfaces. This targets #3119.

### Phase 3 — Minimal realization

Model invariant completeness separately from implementation breadth. Initially prefer `review_required` to pretending to solve general minimality. This targets the lesson from #1969.

### Phase 4 — Certification integration

Require interpretation certification before governed synthesis may treat authored interpretations as hard authority. Preserve an advisory path for uncertified hypotheses.

## Post-implementation evaluation

Do not simply rerun the same benchmark and declare improvement. Run the already identified experiments.

### Bounded-B

Give the baseline the same bounded information as the governed lane.

Purpose: separate governance effect from exploration advantage.

### Invariant ablation

Hold model, task, and files fixed and compare:

```text
bounded context without invariant
vs.
bounded context with invariant
vs.
full governed Sensei
```

Use #1969 as an important fixture.

Purpose: measure how much the governing invariant itself changes repair behavior.

### Interpretation robustness

Create multiple independently authored candidate interpretations for the same tasks.

Purpose: measure sensitivity of governed repair behavior to reasonable interpretation variation and calibrate interpretation authorship.

### p2p verification

Do not make regression-safety claims until the pinned verifier actually measures p2p.

## Expected outcome

This work should not make Sensei more aggressive or optimize it for benchmark scores.

It should make Sensei **less willing to govern from unjustified premises**.

The behavior changes from:

```text
architect wrote invariant
→ invariant governs
```

to:

```text
architect proposed invariant
→ Sensei checked mechanically checkable facts
→ unresolved claims stayed explicit
→ contradictions blocked authority
→ sufficient evidence earned certification
→ only then could the interpretation govern
```

The pilot showed that downstream governance is already strong enough to faithfully amplify whatever premise it receives.

The next responsibility is therefore upstream:

> **Make architectural knowledge earn the right to be amplified.**
