# Contract Synthesis Gate

## Purpose

Define the minimum deterministic gate AWG must apply before an AI agent may
create or propose a contract when no explicit contract already governs the
change.

This first pass is intentionally narrow. It defines outcome classes, evidence
dimensions, blockers, thresholds, and a stable output shape. It does not query
the graph, call Preflight, generate contracts, or change agent runtime
behavior.

## Outcomes

The gate must classify a contract assessment into exactly one outcome:

### `contract-found`

An explicit existing contract governs the change. The agent may proceed only if
tests and evidence support the change under that contract.

### `contract-synthesis-safe`

No explicit contract exists, but evidence is strong enough to allow the agent
to synthesize a contract from evidence. The agent should attach or create a
governing test if one does not already exist.

### `contract-proposal-only`

Evidence suggests a likely rule, but confidence is not high enough for
automatic enforcement. The agent may draft a contract with citations, but it
must be reviewed by a human before enforcement.

### `contract-unknown`

Evidence is weak, conflicting, or ambiguous. The agent must stop and make the
missing contract visible instead of pretending the issue is resolved.

## Core Invariant

An agent may synthesize a contract only from evidence, never from preference,
style, or convenience.

If the gate cannot show enough evidence, the correct result is
`contract-unknown` or `contract-proposal-only`, not silent enforcement.

## Evidence Dimensions

Each dimension is scored explicitly and deterministically.

| Dimension | Range | Meaning |
| --- | --- | --- |
| `direct_source_annotation` | `0-3` | Exact code or graph annotation binds the rule to the symbol, file, or path. |
| `existing_tests_proving_behavior` | `0-4` | Tests explicitly prove the expected behavior for the assessed scope. |
| `repeated_implementation_pattern` | `0-2` | The same rule appears repeatedly in owned code or paired repos. |
| `ownership_authority_path` | `0-3` | Ownership and the authoritative source path are clear. |
| `failure_mode_or_incident_history` | `0-2` | Failure history, repair history, or regressions support the rule. |
| `nearby_human_intent` | `0-3` | Human-authored intent, invariants, or failure modes nearby constrain the behavior. |
| `cross_repo_consistency` | `0-2` | Paired repositories or sibling implementations agree on the same rule. |
| `absence_of_conflicting_contracts` | `0-3` | No nearby explicit contract, test, or authority contradicts the inference. |

Maximum total score: `22`

## Hard Blockers

Any hard blocker prevents `contract-synthesis-safe` even if the raw score is
high.

- conflicting explicit contract
- conflicting test
- missing ownership or authority path
- product ambiguity or policy ambiguity
- weak pattern similarity without strong local anchors
- evidence comes only from shared or generic corpus with no local anchor

## Thresholds

### `contract-found`

Selected when an explicit contract already exists for the assessed scope.
Scoring may still be recorded, but the outcome is governed by explicit
authority, not by threshold math.

### `contract-synthesis-safe`

Requires:

- total score `>= 16`
- `existing_tests_proving_behavior >= 3`
- `ownership_authority_path >= 2`
- at least one of:
  - `direct_source_annotation >= 2`
  - `nearby_human_intent >= 2`
- no hard blockers

Required follow-up:

- include `attach-governing-test` when no governing test is already attached
- include `draft-contract-with-citations`

### `contract-proposal-only`

Selected when:

- total score is `10-15`, or
- score is high but a required synthesis-safe anchor is missing, or
- the evidence is useful but not strong enough for automatic enforcement

Required follow-up:

- include `draft-contract-with-citations`
- include `review-required`

### `contract-unknown`

Selected when:

- total score `< 10`, or
- any hard blocker makes the rule unsafe or ambiguous, or
- ownership and authority are not clear enough to support synthesis

Required follow-up:

- include `escalate-to-human`

## Required Output Shape

The gate should return a structured assessment:

```json
{
  "outcome": "contract-synthesis-safe",
  "score": 17,
  "scores": {
    "direct_source_annotation": 2,
    "existing_tests_proving_behavior": 4,
    "repeated_implementation_pattern": 1,
    "ownership_authority_path": 3,
    "failure_mode_or_incident_history": 1,
    "nearby_human_intent": 3,
    "cross_repo_consistency": 1,
    "absence_of_conflicting_contracts": 2
  },
  "blockers": [],
  "required_actions": [
    "draft-contract-with-citations"
  ]
}
```

The output must make missing evidence visible instead of collapsing uncertainty
into a false positive.

## Minimum Test Plan

This first pass needs deterministic table-driven tests for:

1. explicit contract present -> `contract-found`
2. score `>= 16` with required anchors and no blockers ->
   `contract-synthesis-safe`
3. score `10-15` -> `contract-proposal-only`
4. score `< 10` -> `contract-unknown`
5. high score with conflicting explicit contract -> not safe
6. generic or shared evidence only with no local anchor ->
   at most `contract-proposal-only`
7. synthesis-safe without governing test -> include `attach-governing-test`
8. high total score but missing required test anchor ->
   `contract-proposal-only`
9. high total score but missing ownership authority path ->
   `contract-unknown`

## Non-Goals For This First Pass

- graph traversal
- Preflight integration
- runtime agent behavior changes
- LLM scoring
- heuristic confidence prompts
- automatic contract generation
- hidden policy in prompts
- corpus reorganization

The deliverable is a stable deterministic gate that later systems can call.
