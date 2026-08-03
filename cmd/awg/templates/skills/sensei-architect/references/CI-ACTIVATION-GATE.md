# CI Activation Gate (the PR #148 pattern)

Sensei's own repository configures a concrete example of taking the
awareness/admission surfaces this skill describes and exercising them
automatically, enforceably, on every pull request, rather than only when an
agent chooses to call them. This is repository-specific CI wiring, not a
built-in Sensei behavior — OPERATING-MODEL.md already says results are
"enforceable where hooks, gate policy, CI... are configured"; this file
documents one concrete configuration of that, as implemented for
`github.com/globulario/sensei`.

## What runs, on every PR

Two workflows, in sequence:

1. `Sensei architect activation`
   (`.github/workflows/sensei-architect-activation.yml` +
   `scripts/sensei-architect-activation.sh`) — `pull_request`-triggered, so
   it runs the PR's own copy of itself. That is safe here because it never
   touches a secret: it builds a private, ephemeral, self-only governed
   graph for the exact PR head, then runs the activation sequence — metadata
   → exact-diff preflight → task-state inspection → bounded Phase 10 surface
   proof → `sensei gate --enforce`. In `--mode enforce`, any degraded
   preflight, gate failure, metadata failure, or Phase 10 fixture failure
   fails the check (exit 1).

2. `Sensei Codex architect review`
   (`.github/workflows/sensei-codex-architect-review.yml`) —
   `workflow_run`-triggered on the first workflow's completion, which means
   it always executes the *default branch's* copy of itself, never a PR's
   own edited copy. That is the load-bearing reason the secret-bearing step
   lives in a separate file from the `pull_request`-triggered one: a PR
   cannot alter the workflow that is about to run a credential-bearing
   review of itself. It downloads the first workflow's evidence artifact,
   overlays the review prompt, schema, and this whole skill directory from
   the default branch (so a PR also cannot rewrite its own review's
   governing instructions through any file the prompt tells the model to
   read), then runs a read-only Codex review scoped to that evidence and the
   exact diff.

## Current reality on this repository, not the design intent

- The activation check is honestly **red** on this repository right now: the
  private per-PR graph is self-only, and this repository's own architectural
  corpus is thin relative to its size, so preflight legitimately reports
  `DEGRADED` coverage for most changed files, and enforce mode fails on that
  by design. This was a deliberate, accepted decision, not an unnoticed
  defect — a gate that reported false confidence over thin coverage would be
  worse than one that honestly fails.
- The Codex review workflow structurally cannot be exercised by a PR's own
  CI run before merge (a `workflow_run` job always reflects the default
  branch), so a PR that edits it can only be checked by static reasoning
  pre-merge, and observed for real after merge.
- Full authority sequence and rationale:
  `docs/design/sensei-full-activation.md` in the Sensei source repository
  (this file is not part of the portable skill bundle, so it is named here
  rather than linked — it will not exist in a project that only has Sensei
  installed, not built from source).

## Authority boundaries this gate respects

Same authority discipline as the rest of this skill, made concrete:

- The private per-PR graph is architectural memory for that PR's diff, not
  mutation authority.
- `sensei gate --enforce` can block a change; it cannot approve, apply,
  commit, push, or merge one.
- The Codex review runs sandboxed and read-only. Its JSON verdict
  (`pass` / `warn` / `block` / `cannot_verify`), posted as a PR comment, is
  advisory evidence — it cannot grant itself admission, completion,
  approval, or merge authority. Branch protection and the repository's
  human/owner policy remain the only merge authority.
- The credential the Codex step needs is never placed in job- or step-level
  `env:` (which every sibling step, and any subprocess a composite action
  spawns, would inherit). Presence is checked as a boolean via a
  step-scoped `env:`; the real key is passed directly to the pinned
  action's own input at the point of use.

## When this file is relevant

- Reviewing or modifying `.github/workflows/sensei-architect-activation.yml`,
  `.github/workflows/sensei-codex-architect-review.yml`,
  `scripts/sensei-architect-activation.sh`, or the Codex review
  prompt/schema under `.github/codex/` — treat these as `SECURITY_RISK` and
  `ARCHITECTURE_SENSITIVE` per the usual lenses. A change here can reopen
  the credential or instruction-authority boundary this gate depends on.
- Reviewing a PR to this repository — expect the activation check to be
  present and (currently) honestly degraded. Do not read a passing Codex
  review as merge authority, and do not read a failing activation check as
  proof the PR itself is wrong — confirm whether the failure is the known
  thin-coverage condition or a genuine regression.
- Deciding whether to adopt this pattern in another repository — it is a
  concrete reference implementation, not something `sensei init` wires up
  automatically. Adopting it means genuinely reproducing (1) the
  `workflow_run` split that keeps the secret-bearing step off the
  PR-controlled code path, (2) the trusted-branch overlay covering the
  prompt, schema, and every file the prompt tells the model to read, and
  (3) the anti-fail-open preflight/gate wiring. Copying only the YAML
  without those three properties recreates exactly the defects PR #148
  found and fixed on this repository.
