# Phase 9.5 Plan: Ontology-Aligned Architectural Control Panel

> **Status: saved plan only.** Phase 9.5 remains locked until Phase 9.6 is closed and merged.
> This document records the reviewed product direction. It authorizes no implementation,
> server change, protobuf change, editor mutation, or new architectural authority.

## 1. Product objective

Phase 9.5 expands from the originally narrow completion cockpit into Sensei's primary
architectural control panel.

The extension should provide a simple, intuitive, single-screen view that lets an architect
understand:

- whether the currently served architectural knowledge is trustworthy;
- the present architecture state of the repository or selected domain;
- which architectural objects are closed, open, degraded, unknown, or not applicable;
- why an object is open;
- which closure dimensions remain unsatisfied;
- which warnings, problems, and critical issues need attention;
- which architectural questions await an architect;
- what action is permitted next;
- how task closure, completion, promoted knowledge, and certification differ.

The control panel must answer two questions immediately:

```text
What is the state of this architecture?
```

and:

```text
What is the state of this particular architectural object?
```

The interface must remain useful to someone who does not yet understand the complete Sensei
ontology.

## 2. Core product law

The extension is a client of architectural truth, never its author.

```text
Sensei owners classify
VS Code explains
Guarded owners mutate
VS Code delegates
```

The extension must never:

- infer closure from edge counts;
- decide which dimensions apply to an object;
- manufacture severity;
- derive warnings from labels, colors, or graph shape;
- treat graph adjacency as authority;
- reinterpret task completion;
- promote answers directly;
- certify correctness;
- write governed YAML;
- hide unknown or degraded state behind a green display.

Every visible architectural state must come from a typed Sensei projection or a typed owner
result.

## 3. State distinctions that must remain visible

### 3.1 Graph authority

Whether the served graph is fresh, provenance-bound, and authoritative.

### 3.2 Repository architecture posture

A read-only summary of known architectural state across the current repository or domain.

This is not repository-wide correctness certification.

### 3.3 Architectural object lifecycle

Examples:

- active;
- proposed;
- deprecated;
- superseded;
- revoked.

Lifecycle is not closure.

### 3.4 Architectural object closure

Whether one graph object satisfies all applicable architectural dimensions.

Examples:

- closed;
- open;
- degraded;
- unknown;
- not applicable.

### 3.5 Task closure

Whether the current bounded task has closed its required task-local architecture.

Task closure does not imply that every repository object is closed.

### 3.6 Task completion

Whether the exact task reached authoritative terminal completion.

### 3.7 Correctness certification

Owned by Phase 6 only.

The editor must not suggest that artifact closure, task closure, or completion means
`CorrectnessCertified`.

## 4. No misleading single percentage

Do not begin with a synthetic architecture score.

A percentage can hide unknown coverage and make a partially observed repository appear healthy.
The first version should display typed counts and conditions, for example:

```text
Graph authority          Current
Architecture objects     112 closed, 17 open, 3 degraded, 8 unknown
Critical issues          2
Architect questions      4 waiting
Active task              Closure open, mutation admitted
Completion gate          Not eligible
Coverage                 Sufficient, 6 blind spots
```

A future score may be added only through a separately reviewed, owner-defined projection.

## 5. Canonical backend projections

Phase 9.5 must not begin with visual refactoring. It first requires stable backend-owned read
models.

### 5.1 Repository control snapshot

Introduce a projection approximately equivalent to:

`architecture.control_snapshot/v1`

It should contain:

- repository identity;
- domain identity;
- graph authority and freshness;
- projection digest and provenance;
- architecture object counts by class;
- object closure distribution;
- critical, warning, degraded, and unknown attention counts;
- open architect-question count;
- unresolved contradiction count;
- missing-evidence count;
- coverage and blind-spot state;
- active task summary;
- task closure summary;
- completion summary;
- briefing-feedback summary from Phase 9.6;
- top bounded attention items;
- explicit limitations;
- `non_authoritative_projection: true`.

This projection powers the one-screen overview.

It must not declare repository correctness or repository completion.

### 5.2 Architectural object state

Introduce a projection approximately equivalent to:

`architecture.object_state/v1`

It should contain:

- exact object identity;
- object class;
- label and governed description;
- authority state;
- lifecycle state;
- closure applicability;
- aggregate object-closure state;
- applicable closure dimensions;
- blockers;
- warnings;
- supporting evidence;
- associated tests;
- affected source files;
- open questions;
- promoted-knowledge lineage;
- contradictions;
- owner identity;
- next permitted action;
- projection limitations;
- deterministic digest.

The editor must consume this projection instead of computing object state from relationships.

### 5.3 Attention item

Define one shared typed attention shape approximately equivalent to:

`architecture.attention_item/v1`

Each item should include:

- stable identity;
- severity;
- attention class;
- affected repository or domain;
- affected architectural objects;
- related task where applicable;
- reason code;
- concise explanation;
- blocking state;
- evidence references;
- owner;
- next permitted action;
- whether architect input is required.

The same attention type should drive:

- the repository attention queue;
- warnings in an object inspector;
- task-level warnings;
- persistent critical notification badges.

## 6. Per-object closure dimensions

An architectural object may be open because one or more applicable dimensions remain
unsatisfied.

The backend owner defines applicability. The extension must never assume that every class uses
the same dimensions.

A dimension projection should include:

```text
dimension identity
label
applicable
state
reason code
blockers
evidence
questions
owner
next permitted action
```

Dimension states:

- `satisfied`;
- `open`;
- `degraded`;
- `unknown`;
- `not_applicable`.

Possible dimensions may include, where applicable:

- definition;
- identity;
- ownership;
- scope;
- authority;
- contract completeness;
- realization;
- enforcement;
- verification;
- evidence;
- failure handling;
- operational observability;
- provenance;
- consumer correspondence.

These examples do not authorize a universal checklist. Applicability must come from the
ontology and closure owners.

## 7. Contract example

When the architect selects a contract, the panel should be able to display:

```text
Candidate YAML corpus schema

Contract
Lifecycle: active
Authority: current
Object closure: OPEN
Satisfied dimensions: 5 of 7 applicable

Satisfied
✓ Canonical identity
✓ Definition
✓ Owner
✓ Scope
✓ Consumers

Open
○ Enforcement
  No enforcing implementation is bound.

○ Verification
  No required test proves the contract.

Warnings
! Source implementation exists but is not linked as realization evidence.

Architect question
? Which component owns enforcement of this contract?

Next permitted action
Answer the ownership question or bind verified enforcement evidence.
```

The object may be open while the currently selected task is closed. The interface must explain
that distinction instead of presenting unexplained conflicting badges.

## 8. One-screen information architecture

Preserve the current dashboard concept, but simplify its hierarchy.

### 8.1 Top status strip

Display only the most important repository-level state:

- repository or domain;
- graph authority;
- architecture posture;
- critical issue count;
- open question count;
- active task state;
- completion state.

Technical digests and detailed provenance remain available through an expandable section.

### 8.2 Left ontology rail

Replace the long horizontal taxonomy with compact grouped families.

#### Knowledge

- Invariants
- Intents
- Failure modes
- Incident patterns
- Forbidden fixes

#### Architecture

- Components
- Boundaries
- Contracts
- Decisions

#### Realization

- Source files
- Implementations
- Tests
- Evidence

#### Patterns

- Meta-principles
- Design patterns
- Implementation patterns
- Pattern misuses

#### Dialogue and closure

- Claims
- Questions
- Answers
- Probes
- Promoted knowledge

The ontology descriptor should preferably come from Sensei rather than growing another permanent
client-side class table.

### 8.3 Main center view

The default view should be the attention queue, not the invariant list.

It should answer:

```text
What needs architectural attention now?
```

Provide quick filters:

- Critical
- Warning
- Open objects
- Questions
- Contradictions
- Missing evidence
- Degraded
- Unknown
- All objects

### 8.4 Architectural object list

Each row should show only:

- object label;
- class;
- closure badge;
- number of open dimensions;
- highest attention severity;
- optional owner.

Examples:

```text
CLOSED
OPEN · 2
DEGRADED
UNKNOWN
N/A
```

### 8.5 Right object inspector

Display sections in this order:

1. Identity and purpose
2. Authority
3. Object closure
4. Open dimensions
5. Warnings and blockers
6. Architect questions
7. Next permitted action
8. Evidence and tests
9. Promotion and feedback provenance
10. Relationships
11. Focus graph

The graph remains available, but it should not dominate the initial view. It is supporting
evidence, not the primary explanation.

## 9. Attention and alarm model

The architect must see warnings, problems, and critical conditions without studying the whole
graph.

Use backend-defined severities such as:

- informational;
- attention;
- warning;
- critical.

Do not derive severity in JavaScript.

Possible attention classes include:

- graph authority stale;
- object closure open;
- contradiction present;
- enforcement missing;
- verification missing;
- required evidence missing;
- required test missing;
- ownership unresolved;
- scope ambiguous;
- open architect question;
- task admission blocked;
- completion blocked;
- briefing feedback degraded;
- repository coverage blind spot;
- integrity or provenance failure;
- dangerous forbidden move detected.

Critical issues should remain visible while browsing unrelated ontology classes.

Do not use large amounts of red decoration. Red must retain meaning.

## 10. Architect-question workspace

The panel must allow the architect to respond to questions generated by Sensei.

This is the only planned interactive mutation family in the cockpit. It must remain a guarded
delegation to existing dialogue owners.

### 10.1 Question display

Show:

- exact question identity;
- affected architectural objects;
- blocked closure dimension;
- why architect judgment is required;
- relevant evidence;
- current hypothesis;
- answer status;
- governance status;
- scope implications;
- whether the answer is task-local or potentially reusable.

### 10.2 Answer interaction

The architect may:

- enter an answer;
- choose or confirm answer classification;
- declare intended scope;
- attach evidence references;
- record the answer through the authorized answer owner;
- invoke adjudication through the authorized adjudication owner where permitted.

The extension must not:

- write dialogue files directly;
- silently adjudicate an answer;
- treat a recorded answer as accepted;
- treat an accepted answer as governed knowledge;
- promote an answer automatically;
- mark a question resolved before the canonical owner reports it;
- complete the task automatically.

The interface must preserve these stages visibly:

```text
Question open
→ Answer recorded
→ Awaiting evidence or governance
→ Accepted for question
→ Optional governed promotion
→ Future briefing feedback
```

A recorded answer is not architectural truth.

A promoted governed record is reusable architectural truth.

Phase 9.6 supplies the feedback lineage shown after promotion.

## 11. Guarded editor invocation

The original Phase 9.5 roadmap described the editor as read-only.

This revised plan permits narrowly bounded delegation for architect dialogue only.

The rule becomes:

> The control panel is read-only for architectural state and delegates architect-answer
> operations exclusively to existing authorized owners.

No generic mutation RPC is allowed.

No arbitrary command execution is allowed.

Each editor action must map to a specific typed owner operation with:

- explicit request schema;
- actor identity;
- repository, task, and session binding;
- stable outcome vocabulary;
- replay behavior;
- refusal behavior;
- audit result.

All refusals remain visible.

## 12. Phase 9.6 integration

Phase 9.5 consumes the canonical Phase 9.6 feedback projection.

It should show:

- which governed object was promoted;
- originating question ID;
- answer ID;
- disposition receipt;
- promotion receipt;
- task and session;
- whether feedback is available, degraded, unavailable, or invalid;
- which object closure dimension was affected.

The editor must not independently discover promotion artifacts or verify promotion receipts.

## 13. Completion integration

The control panel consumes existing completion projections and Phase 9.4 gate state.

Display separately:

- task closure;
- terminal completion;
- GitHub change binding;
- gate decision;
- controlled degraded state;
- certification state.

Do not collapse these into a single `done` badge.

Example:

```text
Task closure        Closed
Completion          Completed
GitHub binding      Authoritative
CI gate             Passed
Correctness         Not certified
```

## 14. Revised Phase 9.5 checkpoints

### Opening: design-first

Freeze:

- product information architecture;
- owner boundaries;
- projection contracts;
- closure-dimension semantics;
- attention vocabulary;
- dialogue delegation boundary;
- visual simplicity rules;
- accessibility;
- proof matrix.

No implementation.

### Checkpoint 1: canonical control projections

Implement:

- repository control snapshot;
- architectural object state;
- closure-dimension model;
- attention-item model;
- deterministic validation and digests;
- server read APIs;
- no VS Code redesign yet.

### Checkpoint 2: dashboard information architecture

Refactor:

- grouped ontology navigation;
- compact top status strip;
- default attention queue;
- closure badges;
- responsive layout;
- simplified authority presentation;
- no dialogue mutation yet.

### Checkpoint 3: architectural object inspector

Implement:

- object closure summary;
- applicable-dimension matrix;
- blockers;
- warnings;
- evidence;
- tests;
- questions;
- next action;
- collapsible relationships and focus graph.

### Checkpoint 4: architect dialogue workflow

Implement:

- question workspace;
- answer recording delegation;
- adjudication delegation where authorized;
- typed outcomes;
- visible answer lifecycle;
- no automatic promotion;
- no automatic completion.

### Checkpoint 5: completion, feedback, and adversarial closure

Integrate:

- Phase 9.6 feedback;
- Phase 9.4 gate state;
- completion state;
- certification distinction;
- final accessibility and usability proof;
- full adversarial matrix;
- Ubuntu, Windows, and packaged-extension validation.

## 15. Simplicity rules

The panel must feel calm rather than overloaded.

Freeze these rules:

- one dominant question per screen;
- default view shows attention, not ontology trivia;
- important state visible without scrolling;
- advanced provenance collapsible;
- no unexplained acronyms;
- no color-only meaning;
- every warning or critical state has a reason and next action;
- no more than one primary action per panel;
- no raw digests unless expanded;
- no giant wall of badges;
- no graph animation;
- no automatic rearrangement while the architect reads;
- keyboard navigation throughout;
- accessible contrast;
- usable at common laptop width with VS Code sidebars open.

## 16. Required design proofs

The opening contract must forward-declare tests proving:

1. graph authority and object closure are never conflated;
2. lifecycle and closure are never conflated;
3. task closure and object closure are never conflated;
4. completion and correctness certification remain distinct;
5. an object shows only applicable dimensions;
6. missing non-applicable dimensions do not open an object;
7. the editor cannot calculate closure locally;
8. the editor cannot manufacture severity;
9. critical attention remains visible across navigation;
10. open questions link to affected objects and dimensions;
11. recorded answer is not shown as accepted;
12. accepted answer is not shown as promoted;
13. promoted knowledge shows Phase 9.6 lineage;
14. dialogue actions delegate only to authorized owners;
15. refusal writes nothing;
16. raw answer text never becomes governed-object prose;
17. stale graph authority disables authoritative claims;
18. degraded feedback does not erase base architecture state;
19. unknown state remains unknown;
20. no repository-wide correctness claim is produced;
21. keyboard-only operation works;
22. color is never the only state carrier;
23. the panel remains usable at constrained VS Code widths;
24. existing extension consumers remain compatible;
25. Phase 9.4 behavior remains unchanged;
26. `CorrectnessCertified` remains owner-controlled and unchanged.

## 17. Explicit exclusions

Phase 9.5 must not implement:

- automatic architectural decisions;
- automatic question answering;
- automatic answer adjudication;
- automatic governed promotion;
- automatic task completion;
- correctness certification;
- client-side closure algorithms;
- client-side severity calculation;
- a generic graph mutation console;
- a visual RDF editor;
- GitHub issue or pull-request mutation;
- GNN training;
- model ranking;
- repository-wide architecture scoring without a separate governed owner.

## 18. Planned guarantee

At closure, Phase 9.5 should support this statement:

> Sensei's VS Code extension presents a simple, typed, owner-derived architectural control
> panel. It shows the authority and architecture posture of the current repository, the
> applicable closure state of each architectural object, the exact warnings and questions
> requiring attention, and the next permitted action. It allows architects to answer Sensei
> questions only by delegating to guarded owners, while preserving the distinctions between
> answer, governance, promotion, task closure, completion, and correctness certification.

## 19. Stop boundary

This file records the reviewed direction only.

Do not open or implement Phase 9.5 until:

1. Phase 9.6 is closed and merged;
2. this plan is reconciled against the then-current repository;
3. a new design-first Phase 9.5 opening PR is reviewed;
4. only the explicitly unlocked checkpoint begins.
