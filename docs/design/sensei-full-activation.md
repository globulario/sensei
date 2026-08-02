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
  -> active task and local task-debt inspection
  -> Phase 10 evidence extraction and validation
  -> enforce-mode Sensei gate
  -> read-only Codex architectural challenge
  -> human or repository-owned merge authority
```

This sequence activates the already-merged awareness, task-session, Phase 10,
and gate surfaces on every pull request. It does not claim that the separate
O1-O8 governed synthesis loop is closed.

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
  task audit, Phase 10 HOW/WHY validation, and the final gate to one exact diff;
- `scripts/sensei-task-audit.sh`, a read-only audit of every local task directory
  through the canonical `task-status --verify` reader;
- `.github/workflows/sensei-architect-activation.yml`, which runs the activation
  path in enforce mode for pull requests and preserves all evidence as an
  artifact;
- a read-only `openai/codex-action` review that consumes the repository-owned
  `sensei-architect` skill and the activation evidence, returns closed-schema
  JSON, and updates one bounded PR comment when the required secret is present.

## Degraded worlds

Activation must report, not blur, the following worlds:

- no local `.sensei/tasks` directory on the ephemeral runner;
- local task directories unavailable to CI but present on a developer machine;
- missing MCP capability in GitHub Actions, requiring an explicit CLI fallback;
- Phase 10 command compiled but unreachable from the top-level dispatcher;
- WHY provider unavailable or not configured;
- graph metadata unavailable, stale, empty, degraded, or scoped to another
  repository domain;
- Codex Action secret unavailable, in which case the deterministic Sensei gate
  still runs and the missing review is visible.

In enforce mode, unavailable graph metadata, an unreachable Phase 10 surface,
or a failing Sensei gate blocks the activation job. Candidate absence or
provider unavailability remains explicit evidence and is not rewritten as a
clean architectural result.

## MCP and CLI

Interactive agents must use the MCP surface first when it is available and must
record when they fall back to CLI. GitHub Actions has no ambient MCP session, so
its CLI use is an explicit execution environment constraint, not a claim that
CLI and MCP availability are equivalent.

The architect skill remains responsible for routing interactive work through:

- `awareness_metadata`;
- `awareness_preflight`;
- `prepare-change`, task briefing, and bounded task advancement;
- `awareness_investigate`, evidence coverage, candidate review, and challenge;
- `awareness_edit_check` before architecture-sensitive edits;
- `awareness_propose` for durable review candidates.

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

- the new workflow parses and runs on a real pull request;
- metadata and the exact diff are bound into the activation artifact;
- task audit is read-only and reports zero, valid, superseded, active, and
  unreadable worlds without deleting anything;
- Phase 10 HOW and WHY artifacts validate, or their typed unavailable state is
  visible and enforce mode blocks where required;
- `sensei gate --enforce` is the blocking PR gate;
- Codex Action has read-only repository access and closed-schema output;
- absence of `OPENAI_API_KEY` is visible and does not silently pretend a Codex
  review occurred;
- no workflow step commits, pushes, approves, or merges;
- existing repository tests, seed freshness, generated graphs, and smokes stay
  green.
