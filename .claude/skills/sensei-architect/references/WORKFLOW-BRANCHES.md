# Workflow Branches

Use the smallest branch that fits the work. Re-run preflight when the edit set or behavioral scope expands.

## New Architecture or Subsystem Design

Steps:

1. Establish metadata, scope, and current coverage.
2. Query current components, boundaries, contracts, decisions, patterns, and related invariants.
3. Build the current architecture view before proposing a future one.
4. Name authority, truth layers, lifecycle, recovery, observability, and proof before module shapes.
5. Compare at least two materially different designs when the decision is consequential.
6. Choose the smallest contract-respecting move.
7. Propose durable decisions or contract unknowns when the design reveals them.

Completion criteria:

- owner and mutation path are explicit
- lifecycle and recovery are complete enough for the blast radius
- required proof is named
- irreversible decisions are recorded or proposed

Stop conditions:

- no verified authority for a load-bearing truth
- security/data-loss transition without approval
- design depends on candidate knowledge as active authority

## Architecture Audit

Steps:

1. Define scope.
2. Run metadata, repo-eval, audit, and relevant typed queries.
3. Inspect source and tests where the graph is thin.
4. Report evidenced findings only.
5. Rank repairs by root authority/contract impact.

Completion criteria:

- findings have evidence, consequence, contract, recommended move, and proof
- awareness gaps are separated from code defects

Stop conditions:

- coverage is too degraded to support a conclusion and no fallback evidence exists

## Implementation Planning

Steps:

1. Preflight task and likely files.
2. Brief likely target files.
3. Resolve high and critical nodes.
4. Produce an Architecture Brief when risk is not low.
5. Plan tests and Sensei checks.

Completion criteria:

- governing contract and proof are named before edits
- forbidden alternatives are known

Stop conditions:

- active contract conflict unresolved
- unknown impact in high-risk path without mitigation or approval

## Normal Architecture-Sensitive Implementation

Steps:

1. Keep edits scoped to the recommended move.
2. Brief every newly touched file before editing.
3. Run edit-check on proposed architecture-sensitive content.
4. Re-run preflight if files or behavior expand.
5. Run required tests and normal repo tests.
6. Run final gate where configured.

Completion criteria:

- all changed files were checked
- required proof ran or the blocker is explicit
- no forbidden fix was introduced

Stop conditions:

- edit-check finds a known bad shape that cannot be removed
- required proof cannot run for a load-bearing contract

## Incident Investigation and Debugging

Steps:

1. Establish the violated contract before patching.
2. Trace authority, truth layers, transitions, lifecycle, and recovery.
3. Search Sensei for prior incidents, forbidden fixes, required tests, and patterns.
4. Reproduce at the contract seam when possible.
5. Fix the earliest broken invariant, not the latest symptom.
6. Propose the durable lesson after proof passes.

Completion criteria:

- root contract break is named
- regression test or observation proves the fix

Stop conditions:

- proposed recovery depends on the failed subsystem
- fix hides or downgrades the signal instead of repairing the contract

## Code Review and Pull-Request Review

Review separate axes:

- requested behavior
- architecture contracts
- authority and mutation paths
- lifecycle and recovery
- pattern validity
- proof sufficiency
- awareness feedback

Completion criteria:

- findings are ordered by consequence
- implementation-level comments do not hide contract findings

Stop conditions:

- active contract, forbidden fix, security risk, or data-loss risk is present

## Recovery Design

Steps:

1. Identify recovery authority.
2. Ensure recovery does not require the failed subsystem.
3. Name evidence of completion.
4. Define idempotency, retry, rollback, cleanup, and degraded signal shape.
5. Require runtime proof when the contract depends on live state.

Completion criteria:

- recovery can run under the failure it is meant to repair
- success signal binds to owner evidence

Stop conditions:

- break-glass path is missing for a circular dependency
- fallback returns the same shape as truth

## Migration and Destructive Changes

Steps:

1. Treat as `DATA_LOSS_RISK` unless preflight proves otherwise.
2. Identify canonical owner, backup/restore path, rollback, and compatibility window.
3. Verify idempotency and partial-failure cleanup.
4. Require explicit approval when irreversible.

Completion criteria:

- migration has precondition, postcondition, rollback or forward recovery, and proof

Stop conditions:

- no verified owner
- no recovery story for partial application

## Security-Sensitive Work

Steps:

1. Treat as `SECURITY_RISK`.
2. Resolve auth/RBAC/PKI/token/secret invariants.
3. Check boundary ownership and caller identity.
4. Require proof at the security boundary, not a helper-only test.

Completion criteria:

- authorization source and scope are explicit
- proof exercises the real boundary

Stop conditions:

- credential, token, or authorization path changed without verified owner and proof

## Post-Fix Learning

Steps:

1. Decide whether the session taught a durable architectural lesson.
2. Use `awareness_propose` or `sensei propose`.
3. Keep candidate status and human review.
4. Do not promote automatically.

Completion criteria:

- proposed entry has contract, evidence, affected files, related nodes, and required proof where applicable

## Sparse or Degraded Coverage

Steps:

1. State the degraded awareness condition.
2. Read source, tests, local awareness YAML, ADRs, history, and runtime evidence.
3. Treat high-risk unknowns as high-risk.
4. Propose a coverage gap or contract unknown when the gap is load-bearing.

Completion criteria:

- the user can tell what Sensei knew, what source proved, and what remains unknown
