# Sensei Full Activation

Status: implementation contract

## Purpose

Turn Sensei's existing capabilities into a workflow that is actually exercised,
observable, and enforceable without collapsing evidence, candidate generation,
admission, application, completion, approval, and merge into one authority.

The activation sequence is:

```text
graph metadata and domain scope
  -> exact diff preflight
  -> tracked task-state inspection
  -> bounded Phase 10 surface proof
  -> enforce-mode Sensei gate
  -> read-only Codex architectural challenge when configured
  -> human or repository-owned merge authority
```

This sequence activates the already-merged awareness, task-session, Phase 10,
and gate surfaces on pull requests. It does not claim that the separate O1-O8
governed synthesis loop is closed, that an ephemeral CI runner can see a
developer workstation's untracked task directories, or that a whole-repository
HOW extraction is cheap enough to be a blocking per-PR operation.

## Authority boundaries

1. The Sensei graph is architectural memory, not mutation authority.
2. Task-session state scopes one bounded change; stale local task directories
   remain debt until explicitly inspected or superseded by their existing owner.
3. Phase 10 HOW/WHY/architecture outputs are derived evidence and review
   candidates, never canonical truth.
4. `sensei gate --enforce` may block a change but cannot approve, apply, commit,
   push, or merge it.
5. Codex Action runs read-only. Its JSON review is advisory evidence and cannot
   grant itself admission, completion, approval, or merge authority.
6. GitHub branch protection and the repository's human/owner policy remain the
   merge authority.

## Implemented checkpoint

The first activation checkpoint adds:

- `scripts/sensei-architect-activation.sh`, which binds metadata, preflight,
  tracked task-state audit, bounded Phase 10 proof, and the final gate to one
  exact diff;
- `scripts/sensei-task-audit.sh`, a read-only audit of each task directory
  visible in the selected checkout through the canonical
  `task-status --verify` reader;
- `.github/workflows/sensei-architect-activation.yml`, which runs the activation
  path in enforce mode for pull requests and preserves all evidence as an
  artifact;
- a read-only `openai/codex-action` review that consumes the repository-owned
  `sensei-architect` skill and the activation evidence, returns closed-schema
  JSON, and updates one bounded PR comment when the required secret is present;
- top-level dispatch for the already-implemented `investigate` and `candidates`
  command families, with regression tests proving both surfaces are reachable.

## Phase 10 activation levels

Phase 10 is split deliberately rather than represented as one binary switch.

### Blocking surface proof

Every activation run executes and validates deterministic HOW and WHY artifacts
against the repository-owned small fixture. WHY uses an explicit target,
explicit Git range, and the declared `git_history_provider`. This proves that
the shipped dispatcher, artifact writer, validators, provider registry, and
history path remain operational.

### Bounded full-repository probe

A full-repository HOW extraction is attempted under an explicit wall-clock
budget. A valid completed artifact is recorded when it finishes. Timeout is
recorded as `bounded_timeout`, not rewritten as success and not allowed to hang
the PR indefinitely.

The current HOW implementation records `resource-limit` values in the artifact
but does not use them to bound semantic or AST extraction. Therefore the
full-repository probe is advisory until extraction gains a real incremental or
budget-enforcing contract.

### Not yet ready

Architecture composition, candidate listing, blast radius, and challenge remain
unavailable in the generic PR workflow until canonical owners can provide the
exact graph, claims, closure-state, existing-question, and review-history
digests required by the Phase 10 composition contract. Their absence is
reported explicitly instead of manufacturing placeholder digests.

## Task-state coverage

CI audits only task directories present in the repository checkout. It cannot
see the additional local task directories on a developer machine. The same
read-only script must be run locally to audit that debt. Invalid or unreadable
tasks are reported and preserved; the audit never deletes, clears, supersedes,
or repairs state.

## Degraded worlds

Activation must report, not blur, the following worlds:

- no `.sensei/tasks` directory in the selected checkout;
- tracked task debt versus additional local-only task debt;
- missing MCP capability in GitHub Actions, requiring an explicit CLI fallback;
- Phase 10 command compiled but unreachable from the top-level dispatcher;
- fixture HOW or WHY failure;
- full-repository HOW bounded timeout;
- canonical architecture-composition digests unavailable;
- graph metadata unavailable, stale, empty, degraded, or scoped differently
  from the canonical host/path repository identity;
- Codex Action secret unavailable, in which case the deterministic Sensei gate
  still runs and the missing review is visible.

In enforce mode, unavailable graph metadata, failed exact-diff preflight, a
failing Sensei gate, or failed fixture HOW/WHY proof blocks the activation job.
Tracked task debt, full-repository HOW timeout, unavailable composition digests,
and unavailable Codex credentials remain visible but do not masquerade as a
clean architectural result.

## MCP and CLI

Interactive agents must use the MCP surface first when it is available and must
record when they fall back to CLI. GitHub Actions has no ambient MCP session,
so its CLI use is an explicit execution-environment constraint, not a claim
that CLI and MCP availability are equivalent. The current Phase 10 MCP tools
consume and inspect existing artifacts; artifact creation remains a CLI/library
surface.

The architect skill remains responsible for routing interactive work through:

- `awareness_metadata`;
- `awareness_preflight`;
- `prepare-change`, task briefing, and bounded task advancement;
- `awareness_investigate`, evidence coverage, candidate review, and challenge;
- `awareness_edit_check` before architecture-sensitive edits;
- `awareness_propose` for durable review candidates.

`prepare-change` cannot be applied retroactively to an already-completed edit.
Activation therefore establishes the forward contract and reports existing task
debt rather than fabricating prior authorization.

## Separate governed-synthesis closure

The merged O1-O8 libraries are not made fully operational by this workflow.
`docs/design/archer-integration-closure.md` still requires a production
`sensei synthesis-run` surface and a real end-to-end proof through O5 admission,
O5B application, admission verification, and terminal evidence.

That closure must remain a separate bounded checkpoint because it introduces
provider execution and candidate materialization. It must prove the existing
completion matrix and preserve these laws:

- no application by default;
- `--apply` targets only a dedicated, clean, base-bound worktree;
- only the exact admitted sealed artifact may be materialized;
- no automatic commit, push, PR, approval, merge, or knowledge promotion;
- every unavailable prerequisite remains a typed stop rather than a fabricated
  session identity.

## Required proof

This activation checkpoint is accepted only when an exact PR head proves:

- the workflow parses and runs on a real pull request;
- metadata and the exact diff are bound into the activation artifact;
- canonical repository identity and selectable graph scope remain distinct;
- tracked task audit is read-only and reports valid, superseded, active, and
  unreadable worlds without deleting anything;
- fixture HOW and WHY artifacts validate under explicit bounds;
- the full-repository HOW probe terminates within its declared budget;
- unavailable Phase 10 composition digests remain explicit;
- `sensei gate --enforce` is the blocking PR gate;
- Codex Action has read-only repository access and closed-schema output;
- absence of `OPENAI_API_KEY` is visible and does not silently pretend a Codex
  review occurred;
- no production workflow step commits, pushes, approves, or merges;
- existing repository tests, seed freshness, generated graphs, and smokes stay
  green.
