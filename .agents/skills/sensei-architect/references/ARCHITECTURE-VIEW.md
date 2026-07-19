# Architecture View

The architecture view is the agent's grounded working model of the system slice involved in the task.

It is internal by default. Materialize it only when the user asks, when the work spans sessions, or when the view itself should become a durable artifact.

The view is not a folder diagram. It models behavior, authority, contracts, time, failure, recovery, and proof.

## Compact Form

Use this form during normal work:

```text
Intent:
Topology:
Boundaries:
Contracts:
Authority and truth layers:
State transitions:
Lifecycle:
Dependencies:
Failure and recovery:
Signals and observability:
Patterns and pattern conditions:
Proof:
Blind spots:
```

## Intent

Establish:

- user or system outcome
- actors and responsibilities
- non-negotiable behavior
- out-of-scope behavior
- relevant intent, ADR, decision, or contract nodes

Ask:

- What product or system truth is changing?
- Which behavior must remain invariant?
- Which decisions are reversible?

## Topology and Boundaries

Establish:

- components/modules
- interfaces and boundaries
- dependency direction
- control and data flow
- external systems
- important files and symbols

Ask:

- Where does behavior enter and leave?
- Which boundary is crossed?
- Is the dependency direction compatible with recovery?
- Is control-plane code coupled to the data plane it must control?

## Contracts

For each important interaction, establish:

- preconditions
- allowed inputs
- success meaning
- failure meaning
- ordering constraints
- idempotency
- compatibility requirements
- evidence that proves respect

Ask:

- What does "success" certify?
- Can a partial result be mistaken for completion?
- Is the contract explicit, inferred, contradicted, or absent?

## Authority and Truth Layers

For every load-bearing truth, establish:

```text
Semantic meaning:
Canonical owner:
Allowed writer:
Allowed mutation path:
Readers:
Replicas, caches, and projections:
Generation or version semantics:
Evidence:
```

Separate rows when one name carries multiple meanings.

Ask:

- Who may decide this value?
- Who may persist it?
- Which copy is canonical, and which copies are derived?
- Can two actors race to become authority?
- Can a fallback masquerade as truth?

## State Transitions

For every important transition, establish:

```text
Actor:
Trigger:
Precondition:
Intermediate state:
Completion evidence:
Retry behavior:
Idempotency:
Rollback:
Cleanup:
Recovery:
```

Separate desired, admitted, installed, runtime, observed, repository, generated, cached, and projected state.

Ask:

- Is an intermediate state visible as success?
- Can stale observation overwrite newer intent?
- Is generation advanced by the correct owner?
- Does recovery depend on the failed subsystem?

## Lifecycle

Map creation, activation, update, failure, recovery, deactivation, deletion, and garbage collection.

Ask:

- Does every acquisition have release?
- Does every write have reconciliation or cleanup?
- What happens after process death between steps?
- Can work be retried without duplicating effects?

## Failure, Recovery, and Signals

Establish:

- known incidents and failure modes
- forbidden fixes
- degraded modes
- blast radius
- recovery authority
- dependencies required for recovery
- evidence recovery completed
- observability signals and their scope

Ask:

- Is degraded operation explicit?
- Can a green health signal be stale or scoped incorrectly?
- Which failure is the earliest contract break?

## Patterns

For each relevant pattern, establish:

- pattern name
- problem it solves
- valid conditions
- required calls or structure
- forbidden shortcuts
- known misuse
- evidence in current code

Ask:

- Is this pattern solving the same problem here?
- Are its preconditions present?
- Does a local pattern violate a higher contract?

## Proof

Establish:

- required tests
- contract-level tests
- runtime observations
- static checks
- CI gates
- missing proof
- contradictory proof

Ask:

- Which test goes red when the contract breaks?
- Does the test exercise the authority path?
- Is the proof behavioral, or only implementation-specific?

## Blind Spots

Record:

- `EMPTY`, `DEGRADED`, stale, or unavailable Sensei surfaces
- unindexed files
- missing contracts
- ambiguous owners
- stale ADRs or knowledge
- absent tests
- runtime state not observed
- hypotheses awaiting evidence

A blind spot remains part of the view after implementation proceeds.
