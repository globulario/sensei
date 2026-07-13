# Architectural Finding Rubric

A finding connects evidence to architectural consequence. Avoid aesthetic criticism dressed as governance.

## Evidence Strength

`Proven`:

- active invariant or contract is directly violated
- known forbidden fix matches the proposed change
- two authoritative writers are visible
- regression test reproduces the contract failure
- runtime evidence and source ownership establish the failure path

`Strong signal`:

- multiple independent clues agree
- structural evidence plus history
- code path plus missing lifecycle inverse
- repeated incident shape plus current implementation
- pattern mismatch plus failing or absent proof

`Hypothesis`:

- plausible concern without enough evidence to block
- useful for investigation, not sufficient for governance

## Severity

`Blocker`:

- active contract or critical invariant violation
- authority conflict over canonical truth
- known forbidden fix
- uncontrolled security or data-loss path
- irreversible state change without verified owner and recovery
- recovery path that cannot function under the target failure

Action: stop mutation, explain the contract, and resolve the conflict or obtain an explicit architectural decision.

`High`:

- lifecycle gap likely to create stale or stuck state
- partial state presented as success
- missing idempotency on retried effects
- ambiguous truth layer
- unsupported pattern on a load-bearing path
- proof that bypasses the real authority path
- degraded Sensei coverage in a sensitive area

Action: propose mitigation and proof before continuing.

`Advisory`:

- localized contract ambiguity
- weak observability
- stale but non-critical awareness
- recoverable pattern misuse
- missing non-load-bearing test coverage

Action: keep moving and include the risk in the summary.

`Opportunity`:

- clearer seam
- deeper module
- reduced coupling
- better naming
- additional automation

Action: keep separate from the requested work unless it materially reduces current risk.

## Finding Classes

- Broken contract
- Authority conflict
- Semantic identity split
- Truth-layer ambiguity
- Lifecycle gap
- Invalid intermediate completion
- Recovery inversion
- Signal corruption
- Fallback-as-truth
- Dependency inversion
- Control-plane/data-plane coupling
- Perception or health-reporting lie
- Composition or boundary leakage
- Evolution or delivery hazard
- Pattern misuse
- Proof gap
- Awareness gap

## Required Finding Shape

```text
[Severity] <Finding class>: <short title>

Evidence:
- <Sensei node, source path, test, history, or runtime observation>

Architectural consequence:
- <what can become false, ambiguous, unrecoverable, or unsafe>

Governing contract or principle:
- <active id or explicit statement>
- <state "unknown" when not established>

Recommended move:
- <smallest change that restores the contract>

Proof:
- <test, check, or observation that must pass>

Confidence:
- Proven | Strong signal | Hypothesis
```

## Reporting Discipline

- Rank by consequence, not code ugliness.
- Separate current violations from future risks.
- Separate architecture defects from awareness/documentation gaps.
- Preserve uncertainty.
- Do not repeat the same root cause as several findings.
- Tie each recommendation to proof.
- Do not enlarge scope for cosmetic or speculative improvements.
