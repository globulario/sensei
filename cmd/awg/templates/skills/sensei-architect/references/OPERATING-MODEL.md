# Sensei Operating Model

Use this file to interpret Sensei results correctly before relying on them.

## What Sensei Is

Sensei is repository-owned architectural memory:

- typed knowledge about invariants, intent, contracts, failure modes, forbidden fixes, required tests, patterns, proof obligations, components, boundaries, decisions, evidence, source files, and symbols
- provenance-carrying: nodes report where they came from, such as authored YAML or code annotations
- scoped by repository/domain, with shared knowledge allowed only through explicit shared scope
- queried before consequential change
- advisory in briefing, preflight, impact, edit-check, repo-eval, audit, and proposal surfaces
- enforceable where hooks, gate policy, CI, frozen contracts, or source/pattern checks are configured
- designed to accumulate reviewed architectural lessons over time

## What Sensei Is Not

Sensei is not:

- runtime authority
- desired-state authority
- installed-state authority
- a replacement for source inspection
- a replacement for tests, builds, runtime observation, review, or user decisions
- permission to invent missing contracts
- a generic RAG search box
- a raw query interface for agents
- proof that graph silence means safety

The graph is compiled context. Authored corpus files, source code, tests, runtime evidence, and user decisions remain separate forms of evidence.

## Authority and Freshness

Every graph-backed answer carries authority or metadata. Read it.

- `authoritative=true`, current freshness, stamped build provenance, and current seed state mean the answer came from the expected validated graph artifact.
- Stale, unknown, empty, check-error, dev, incomplete, or missing provenance means the answer may be useful but must not be treated as complete.
- A live graph with many graph-wide nodes can still be incorrectly scoped for the current repository.

When authority is not current, state the degradation and fall back to local YAML, source annotations, tests, history, runtime evidence, and user approval for risky changes.

## Status Semantics

Briefing and Preflight use explicit status.

- `OK`: anchors or patterns were found. Treat returned nodes as active context, then resolve high or critical ones before relying on summaries.
- `EMPTY`: no direct anchors or patterns for the requested scope under the server's coverage rules. This is not safety. Check metadata, scope, source, and tests.
- `DEGRADED`: backend, coverage, freshness, or high-risk no-anchor conditions prevented reliable classification. Unknown impact stays unknown.

Read status with risk class. `EMPTY` plus `LOW_RISK` in a well-covered clean path differs from `DEGRADED` or `UNKNOWN_IMPACT` in a high-risk or thin path.

## Risk Classes

Current Preflight risk classes:

- `LOW_RISK`: proceed normally while honoring returned actions.
- `ARCHITECTURE_SENSITIVE`: brief target files, resolve governing nodes, and build the architecture view.
- `CONVERGENCE_RISK`: desired, installed, runtime, observed, repository, cached, or generated state may be confused. Walk the truth layers before editing.
- `SECURITY_RISK`: auth, RBAC, PKI, credentials, tokens, or boundaries may be affected. Get explicit user approval before mutation unless the user already authorized the risk.
- `DATA_LOSS_RISK`: deletion, migration, wipe, rollback, package/artifact lifecycle, or irreversible state is involved. Verify owner, backup/recovery, and proof before mutation.
- `UNKNOWN_IMPACT`: Sensei cannot classify the impact. Treat as high risk until source, tests, contracts, and user approval narrow it.

Current confidence values:

- `CONFIDENCE_HIGH`: at least three direct anchors and sufficient coverage.
- `CONFIDENCE_MEDIUM`: one or two direct anchors or a strong pattern match.
- `CONFIDENCE_LOW`: sparse, degraded, or otherwise weak evidence.

## Repository and Domain Scoping

Sensei scopes results to a repository/domain. A multi-domain graph with empty scope fails closed instead of mixing repositories.

Use a repo domain such as `github.com/org/repo` when known. Shared meta-principles can participate across scopes, but repo-specific facts from another project must not guide the current change unless explicitly shared.

Report a likely domain mismatch when:

- metadata graph-wide counts are high but scoped results are empty
- a node resolves without domain but not with the current domain
- preflight blind spots mention scope or zero anchors for a high-risk path
- repository paths in returned facts do not belong to the current repo

Graph-wide counts are health signals, not proof that the current task is covered.

## Authored Knowledge vs Runtime Observation

Authored architecture says what should govern the system. Runtime observation says what happened. Desired, installed, runtime, observed, repository, generated, cached, and projected state are different layers.

Do not rewrite an invariant because one runtime observation contradicted it. First determine whether the observation proves:

- the invariant is violated
- the invariant is stale
- the runtime observation is stale or scoped differently
- the query was pointed at the wrong domain
- the evidence is incomplete

## Architectural Lenses

Sensei's meta-principle corpus is the source for lenses. Query or resolve relevant active principles instead of copying a static list.

Useful lens groups:

- Authority: owner, writer, semantic identity, allowed mutation path
- Signal: fallback, uncertainty, absence scope, absorbed errors, scoped assertions
- Lifecycle: write completion, partial state, retry, cleanup, rollback, idempotency
- Dependency: critical path, circular dependency, break-glass, recovery direction
- Perception: health/reporting truth, misleading green signals, UI claims
- Composition: visual or structural composition that hides truth
- Structure: boundary honesty, abstraction depth, projection, leakage
- Evolution: reviewable slices, deterministic builds, intent before drift, release safety

Resolve active project or shared principles when they govern the task.
