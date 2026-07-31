# Governed Runner Composition O3

Status: architecture contract

## Purpose

Compose the O1 governed synthesis session (`golang/architecture/synthesis`) with the O2 provider-neutral execution port (`golang/architecture/providerport`) into a real runner: the first piece of this roadmap that actually invokes a provider and captures what it proposes, without ever granting that provider repository truth.

O1 already reserves the fields this closes. `Attempt.InputCandidateDigestSHA256` and `Attempt.ProposedChangeDigestSHA256` exist today, but O1 is pure data — it never computes them, and O2 is I/O-free — its `Request`/`Result` carry no path, file, or patch field anywhere. Nothing today verifies that either digest corresponds to real content; a concrete `Provider` implementation could currently assert any digest it likes inside a `Result`'s embedded candidate payload, and `MapToCommand` would carry it forward untouched, because O2 has no way to know it is wrong. O3 exists to make those two fields trustworthy: it is the one component that touches real repository content, so it is the one component that must never trust a provider's own account of that content.

## Architectural position

```text
governed synthesis session (O1)
  -> Attempt.InputCandidateDigestSHA256 / ProposedChangeDigestSHA256
     (opaque references O1 already carries, computed by nothing today)
  -> governed runner composition (O3)
       -> bounded, read-only repository snapshot at the session's exact
          BaseRevision -- never the live working tree, never another revision
       -> one capability-driven O2 Provider.Execute(Request, Observer) call
          (O2 Run, reused verbatim, unchanged)
       -> provider proposes content into a candidate buffer -- an ephemeral,
          disposable, non-repository-backed capture surface
       -> O3 independently recomputes InputCandidateDigestSHA256 and
          ProposedChangeDigestSHA256 from the bytes it actually observed in
          the buffer -- never from what the provider's Result payload
          already asserts
  -> O2's existing MapToCommand maps the resulting Result into
     RecordAttemptCommand, unchanged
  -> synthesis.Transition decides acceptance (O1, unchanged)
```

O3 composes with O1 and O2 as fixed, closed contracts. It is the shared runner infrastructure that a future concrete provider adapter (O6) is built on top of, so that no individual adapter has to reinvent bounded repository access or digest computation, and no individual adapter can shortcut trustworthy evidence by simply asserting a digest.

## Hard laws

1. A provider proposes content; O3 captures and digests it independently. A provider-declared digest is never trusted — O3 always recomputes `InputCandidateDigestSHA256` and `ProposedChangeDigestSHA256` from the real bytes it observed, the same "declared must equal recomputed" law O1 and O2 already enforce (`invariant.synthesis.declared_digest_must_equal_computed_content_digest`), applied here to real file content for the first time.
2. Read access is bounded and revision-pinned. A provider may read the target repository only as it exists at the session's exact `BaseRevision` — never a different revision, never live working-tree state, never anything the session has not already committed to as its identity.
3. The candidate buffer is not the real repository. It is an ephemeral, disposable capture surface with no path, index, or ref inside the real checkout. Nothing a provider writes into it is ever applied, staged, committed, merged, or otherwise written back into the real repository by O3.
4. O3 produces evidence, not admission. A real, independently-computed patch digest is stronger evidence than an asserted one, but it remains a candidate under evaluation — O3 grants no correctness, completion, evaluation, or admission authority, exactly as O1 and O2 already state for their own outputs.
5. O3 does not redefine O1 or O2. It does not add fields to `Attempt`, `Request`, or `Result`, does not weaken any existing schema, and does not construct O1 commands itself. `MapToCommand` (O2) remains the only path from a `Result` to a `synthesis.Command`; `Transition` (O1) remains the only session-transition authority.

## O3 scope

### In scope

- **CandidateBuffer**: an ephemeral, disposable, non-repository-backed capture location (e.g. an isolated temporary directory, never inside the real checkout and never sharing its git index or refs) that a provider running under O3's runner may write proposed file content into. Destroyed after each run; nothing about its lifetime or location is observable to the real repository.
- **Bounded read-only repository snapshot**: exposes the target repository's content exactly as it exists at the session's `BaseRevision` (e.g. a `git show`/`git archive` extraction of that pinned revision into a read-only location) — never the live working tree, never a different revision, never uncommitted local state.
- **The runner**: the harness that wires a concrete `Provider` (O2 interface, O6 out of scope here) to the bounded read snapshot and the candidate buffer, invokes O2's `Run` unchanged, and independently recomputes `InputCandidateDigestSHA256`/`ProposedChangeDigestSHA256` from what it actually observed — never from whatever the provider's `Result` payload already asserts.
- **Patch-digest computation**, reusing the existing convention already present twice in this codebase — `sha256` over `git diff --no-ext-diff --binary`-shaped bytes, as used by `golang/architecture/admission.CaptureChanges` and `golang/architecture/resulttransition`'s private `patchDigest` — as a *convention* to match, not by importing either (both are private and/or coupled to Sensei's own closure-protocol v2 ledger, a different domain from an O1 session against an arbitrary target repository).
- Deterministic digest computation: the same candidate buffer content always produces the same digest, byte-for-byte.
- Test doubles and adversarial contract tests, including the required proof additions below.

### Explicitly out of scope

- Claude, Codex, OpenAI, Gemini, or local-model provider adapters (O6).
- Evaluation composition (O4).
- Admission or completion bridging (O5).
- Any repository mutation: no `git add`/`commit`/`push`, no file write outside the candidate buffer, no PR creation, no GitHub API call.
- Real `git worktree add`-based physical isolation, or any concurrent multi-worktree pool. The candidate buffer is not a git worktree and has no relationship to the real repository's index, refs, or working tree.
- Provider selection/routing policy.
- Changes to O1's or O2's existing schemas, types, or commands.
- Autonomous session-driving loops.

## Acceptance criteria

The implementation is complete only when:

- repository read access is bounded to exactly the session's `BaseRevision`, proven by a test that any other revision is rejected before a read happens;
- the candidate buffer is never backed by a real repository path, index, or ref, proven by a test that a run leaves the real checkout's `git status`/`git diff` untouched;
- `InputCandidateDigestSHA256` and `ProposedChangeDigestSHA256` are always independently recomputed by O3 from observed buffer content, never trusted from a provider-declared value — a mismatched or provider-asserted-only digest is rejected before an `Attempt` candidate is produced;
- no repository mutation of any kind appears anywhere in O3;
- O3 does not construct `synthesis.Command` itself — it produces a `Result` for O2's existing, unchanged `MapToCommand`;
- full repository build, vet, tests, freshness, generated-artifact, and Sensei dogfood checks pass;
- no forbidden scope (adapters, evaluation composition, admission bridging, repository mutation, real worktree pooling, routing policy, autonomous loops) appears;
- the exact implementation head receives an architect-review handoff with fixture digests, matching O1 and O2.

### Required proof additions

Adversarial contract tests must additionally demonstrate:

- **revision pinning**: a runner given a different or wrong `BaseRevision` is rejected before any repository read happens;
- **buffer non-leakage**: nothing written into the candidate buffer during a run is observable in the real repository checkout afterward — no new files, no modified index, no dangling ref;
- **digest integrity**: a provider that asserts a false or mismatched digest inside its `Result` payload does not survive — the recomputed digest from actually-observed buffer content is what O3 carries forward, and a divergence is rejected;
- **determinism**: identical candidate buffer content always produces identical `InputCandidateDigestSHA256`/`ProposedChangeDigestSHA256`, byte-for-byte;
- **cleanup**: a candidate buffer does not survive past the run that created it — no leftover ephemeral state is carried into a subsequent attempt or session.

## Implementer protocol

This document is the contract. Implementation must not begin until it is committed to this PR and reviewed by the architect.

Claude must use the repository's canonical Sensei briefing, preflight, task, admission, and verification workflow before editing governed files, exactly as during O1 and O2.

Claude must first map the existing typed owners this runner composes with — `synthesis.Attempt`'s existing digest fields, `providerport.Run`/`Provider`/`Observer`, and the existing `sha256(git diff)`-shaped patch-digest convention already used elsewhere in this codebase — rather than approximating or duplicating them. When an interface boundary or mapping is unclear, post `ARCHITECT QUESTION` and stop that affected portion. Do not invent real git-worktree isolation, a parallel session owner, or weaken closedness to make progress.

Finish each bounded step by posting `IMPLEMENTATION READY FOR ARCHITECT REVIEW` bound to the exact head SHA, including all schema digests, representative fixture digests, contract-test evidence, full verification commands, and explicit confirmation that no provider adapter, real worktree, or repository mutation was added.
