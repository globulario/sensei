# Governed Synthesis Loop

Sensei has a second, complementary capability to the awareness/admission surface
this skill otherwise describes: a bounded, deterministic loop for actually
producing a governed change, not just deciding whether one may proceed.

```text
interpretation -> plan -> generation -> evaluation -> bounded retry/replan
  -> candidate-ready -> admission -> apply -> verification -> completion evidence
```

An external coding agent (Claude, Codex) is driven through a provider-neutral
execution port for interpretation and planning; the resulting candidate is
sealed and evaluated deterministically; only an accepted candidate is ever
submitted to Sensei's existing admission and verification owners. A rejected
or malformed provider output is caught at the schema/evaluation boundary and
stopped, never silently applied.

## Current reality, not the design intent

- It is real, merged Go code (`golang/architecture/synthesisdriver`,
  `golang/architecture/cognitivecommand`, `golang/architecture/agentcommand`),
  not a proposal.
- **There is no CLI.** `golang/architecture/synthesisdriver.Run` has zero
  callers in `cmd/awg`. Driving it requires writing a Go program that
  constructs the session and config directly.
- **It only works on a repository Sensei has already onboarded.** A legal,
  non-placeholder `synthesis.SessionState` needs a real `workspacecontract.Identity`
  (a live graph-authority gRPC Metadata RPC), a real `tasksession.Session` (from
  an actual `sensei prepare-change` run), and a real `closureprotocol` closure
  assessment. None of that can exist for a repository with no served graph and
  no task session. `contract_unknown.sensei.o7_synthesisdriver_run_requires_a_pre_onboarded_repository_u`
  records this as an unresolved contract question.
- Full contract, what is merged, and what remains open: `docs/design/archer-integration-closure.md` in the Sensei source repository (this file is not part of the portable skill bundle, so it is named here rather than linked -- it will not exist in a project that only has Sensei installed, not built from source).

## When to consider it

- The task is bounded code generation (not manual editing) on a repository this
  session has already established real graph authority and a task session for
  (step 4 of the Core Loop) — most concretely, Sensei's own repository, which
  already dogfoods this machinery on itself.
- You are willing to author the driver program yourself; there is no command to
  hand off to.

## When not to

- Any repository without an already-served graph and an active task session —
  attempting this is not a shortcut, it is reconstructing O1-O8's own
  prerequisites first, which is out of scope for an ordinary editing task.
- As a substitute for admission, verification, or completion authority. The
  loop stops at `candidate-ready`; Sensei's existing admission/apply/verification
  owners remain the only acceptance path, unchanged.

Do not present this as available out of the box. `sensei briefing` mentions it
exists whenever a briefing finds substantive content; that mention is
informational, not a claim that this task's repository already satisfies its
precondition.
