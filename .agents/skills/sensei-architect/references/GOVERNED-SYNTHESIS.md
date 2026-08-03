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
- **A CLI exists: `sensei synthesis-run`.** Run `sensei synthesis-run --help`
  for the authoritative, current flag surface -- do not treat this document as
  a substitute for it. It stops at candidate-ready-for-admission or a governed
  terminal/stopped/step-limit disposition; it never admits, applies, commits,
  pushes, or merges anything. Sensei's existing `sensei admit-change` /
  `sensei verify-admission` remain the only acceptance path, unchanged.
- **It only works on a repository Sensei has already onboarded.** A legal,
  non-placeholder `synthesis.SessionState` needs a real `workspacecontract.Identity`
  (a live graph-authority gRPC Metadata RPC), a real `tasksession.Session` (from
  an actual `sensei prepare-change` run), and a real `closureprotocol` closure
  assessment. None of that can exist for a repository with no served graph and
  no task session.
- **Interpretation is caller-supplied, not automated.** `--interpretation`
  takes a hand-authored JSON file describing objective, invariants, contracts,
  and known failure modes -- no governed source resolver exists yet to derive
  this automatically from the graph, so an honest interpretation file should
  say plainly that it is hand-authored. Its `objective` must exactly match
  the resolved session objective (`--objective`, or the task's own recorded
  description) -- a mismatch refuses the run before planning or generation,
  rather than letting two silently divergent objectives drive generation and
  the receipt. Its `required_proof_obligations` must be empty -- no
  production `EvidenceResolver` exists yet to bind a declared obligation to
  a verified discharge digest, so a non-empty declaration refuses the run
  rather than being silently dropped in favor of an empty
  `ProofObligationDigests`.
- **The O4 evaluator needs `.sensei/gate-policy.yaml` to exist.** This is a
  deliberate security check (the policy path must resolve to a real file
  outside the candidate surface, so a malicious candidate cannot supply its
  own weakened policy) -- not a bug. A repository without this file will fail
  with `required-evaluator-unavailable` before the gate ever runs. Create a
  minimal one (`default: inherit`) if the repository doesn't already have one.
- **Real vendor CLI generation is genuinely slow and occasionally flaky.**
  O3's mutation-plan format regenerates a whole file's content (base64-encoded)
  per write operation rather than a diff, so even a one-line change to a
  large file means real minutes of LLM output. Extended-thinking models add
  further latency. A real run can also hit transient vendor-CLI failures
  (crashes, streaming errors, credit/auth issues) that surface as a governed
  `runner-stopped`/`provider-stopped` disposition, not a content rejection --
  retrying the whole command is the correct response, not a code fix.
- Full contract, what is merged, and what remains open: `docs/design/archer-integration-closure.md` in the Sensei source repository (this file is not part of the portable skill bundle, so it is named here rather than linked -- it will not exist in a project that only has Sensei installed, not built from source).

## When to consider it

- The task is bounded code generation (not manual editing) on a repository this
  session has already established real graph authority and a task session for
  (step 4 of the Core Loop).
- You can supply a real, absolute path to an installed `claude` or `codex`
  binary, and an honest hand-authored `--interpretation` file.

## When not to

- Any repository without an already-served graph and an active task session —
  attempting this is not a shortcut, it is reconstructing O1-O8's own
  prerequisites first, which is out of scope for an ordinary editing task.
- As a substitute for admission, verification, or completion authority. The
  loop stops at `candidate-ready`; Sensei's existing admission/apply/verification
  owners remain the only acceptance path, unchanged.
- As a substitute for ordinary editing on a small, well-understood, low-risk
  change where a human or agent can just edit the file directly and run the
  repository's normal tests/gate -- this loop's real cost (vendor CLI latency,
  possible retries) is only worth paying when the governed evidence chain
  itself (interpretation/plan/generation/evaluation receipts) is the point.

Do not present this as available out of the box. `sensei briefing` mentions it
exists whenever a briefing finds substantive content; that mention is
informational, not a claim that this task's repository already satisfies its
precondition.
