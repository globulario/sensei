# Governed Runner Composition O3

Status: architecture contract

## Purpose

Compose the O1 governed synthesis session (`golang/architecture/synthesis`) with the O2 provider-neutral execution port (`golang/architecture/providerport`) into a real runner for exactly one operation — generation from `PhaseAttempting` — the first piece of this roadmap that actually invokes a provider against real repository content, without granting that provider repository truth or silently overclaiming a guarantee no in-process Go boundary can make.

O1 already reserves the fields this closes. `Attempt.InputCandidateDigestSHA256` and `Attempt.ProposedChangeDigestSHA256` exist today, but O1 is pure data — it never computes them, and O2 is I/O-free — its `Request`/`Result` carry no path, file, or patch field anywhere. Nothing today verifies that either digest corresponds to real content; a concrete `Provider` implementation could currently assert any digest it likes inside a `Result`'s embedded candidate payload, and `MapToCommand` would carry it forward untouched, because O2 has no way to know it is wrong. O3 exists to make those two fields trustworthy: it is the one component that touches real repository content, so it is the one component that must never trust a provider's own account of that content — and it must be honest about exactly how strong a guarantee it can offer a cooperative in-process provider versus an adversarial or buggy one.

## Architectural position

```text
O1 session (PhaseAttempting) + workspacecontract.Identity (canonical workspace owner)
  -> RepositoryDomain / BaseRevision -- structural, sourced only from these two
     owners; no fallback to HEAD, no fallback to the live working tree

governed runner composition (O3)
  -> bounded, read-only snapshot at the session's exact BaseRevision
     -> InputCandidateDigestSHA256 computed HERE, over the snapshot itself,
        before any provider runs
  -> CandidateWorkspace: a typed channel (read handle to the snapshot, write
     handle to an ephemeral candidate buffer) -- constructed and handed to a
     concrete Provider by its own constructor, never through O2's
     Execute(ctx, Request, Observer) signature, never through an ambient
     working directory, global, or environment variable
  -> one capability-driven O2 Provider.Execute(Request, Observer) call
     (O2 Run, reused verbatim, unchanged) -- the provider proposes content
     by writing through the CandidateWorkspace's write handle only
  -> ProposedChangeDigestSHA256 computed HERE, over the canonical change
     from the snapshot to the final buffer content, after the provider
     finishes -- a distinct digest over distinct content from
     InputCandidateDigestSHA256, never a proxy for it
  -> buffer content sealed into an immutable, content-addressed candidate
     artifact BEFORE the ephemeral capture surface is destroyed
  -> O3's independently computed evidence is compared against the
     provider's OWN Result -- unchanged, never rewritten:
       match    -> the Result passes through byte-for-byte to O2's
                   existing Run/MapToCommand
       mismatch -> rejected before MapToCommand ever sees it

  -> O2's existing MapToCommand maps the (unmodified) Result into
     RecordAttemptCommand, unchanged
  -> synthesis.Transition decides acceptance (O1, unchanged)
```

O3 composes with O1 and O2 as fixed, closed contracts. It is the shared runner infrastructure that a future concrete provider adapter (O6) is built on top of, so that no individual adapter has to reinvent bounded repository access or digest computation, and no individual adapter can shortcut trustworthy evidence by simply asserting a digest.

```go
// Illustrative shape, not a implementation commitment: the typed channel a
// concrete Provider is constructed with, so it never needs ambient
// filesystem access to participate.
type CandidateWorkspace interface {
    ReadSnapshot(path string) ([]byte, error)
    WriteCandidate(path string, content []byte) error
}
```

## Hard laws

1. **The workspace channel is typed and constructor-injected, never ambient.** A provider proposes content only by reading and writing through a `CandidateWorkspace` it was constructed with. O3 never relies on a working-directory convention, a global, or an environment variable to communicate the snapshot or the candidate buffer's location.
2. **This is a cooperative-provider contract, not a hard sandbox, and the contract must say so.** An in-process Go provider can reach the host filesystem independently of whatever O3 hands it — no typed channel changes that fact. O3 guarantees that a well-behaved provider has no sanctioned path to real repository state other than `CandidateWorkspace`. It does not, and cannot, guarantee that an adversarial or buggy in-process provider is physically prevented from touching the real repository. No implementation, test name, or doc line may claim "structurally impossible" or equivalent language beyond this cooperative-provider scope.
3. **Repository and revision identity are structural, never re-derived.** `RepositoryDomain` and `BaseRevision` come solely from the O1 session (`Session.RepositoryDomain`, `Session.BaseRevision`) and the canonical workspace owner that session identity already traces to (`workspacecontract.Identity`, via `Session.WorkspaceIdentityDigestSHA256`). There is no fallback to `HEAD`, no fallback to the live working tree, and no independent re-derivation from any other source.
4. **This runner handles exactly one operation.** O3 accepts only `OperationGeneration` requests from a session in `PhaseAttempting`. Interpretation, planning, and evaluation-observation are out of scope for this runner; a request for any other operation or phase is rejected before a snapshot is ever taken.
5. **`InputCandidateDigestSHA256` and `ProposedChangeDigestSHA256` are distinct identities.** `InputCandidateDigestSHA256` identifies the exact initial snapshot the provider was given, computed once, before the provider runs, and independent of anything the provider does. `ProposedChangeDigestSHA256` identifies the canonical change from that snapshot to the final candidate content, computed once, after the provider finishes. Neither is ever substituted for the other.
6. **A provider-declared digest is never trusted.** O3 always independently computes both digests from what it directly observed through `CandidateWorkspace` — never from what a provider's `Result` payload already asserts — the same "declared must equal recomputed" law O1 and O2 already enforce (`invariant.synthesis.declared_digest_must_equal_computed_content_digest`), applied here to real file content for the first time.
7. **O3 never mutates a provider's `Result`.** It compares its own independently computed evidence against the unchanged `Result`'s declared values. A match passes the `Result` through byte-for-byte to O2's existing `Run`/`MapToCommand`. A mismatch is rejected before `MapToCommand` ever sees it. O3 never repairs, rewrites, or injects corrected digests into a candidate `Attempt` — doing so would break O2's own declared-equals-recomputed vacuum seal over the outer `Result` envelope (outer digest, payload digest, and embedded `Attempt` digest all already revalidated by O2).
8. **The ephemeral capture surface is disposable; the candidate itself is not.** The candidate buffer's ephemeral working surface (e.g. a temporary directory) is destroyed after every run, but its content is sealed into an immutable, content-addressed candidate artifact before that cleanup happens. O4, O5, retry, and resume address a candidate by digest against this sealed artifact — never against the destroyed ephemeral surface, which would leave them with nothing to evaluate or submit.
9. **O3 produces evidence, not admission.** A real, independently computed digest is stronger evidence than an asserted one, but the candidate it describes remains under evaluation — O3 grants no correctness, completion, evaluation, or admission authority, exactly as O1 and O2 already state for their own outputs.
10. **O3 does not redefine O1 or O2.** It does not add fields to `Attempt`, `Request`, or `Result`, does not weaken any existing schema, and does not construct O1 commands itself. `MapToCommand` (O2) remains the only path from a `Result` to a `synthesis.Command`; `Transition` (O1) remains the only session-transition authority.

## O3 scope

### In scope

- **`CandidateWorkspace`**: a typed channel — read access to the pinned snapshot, write access to the ephemeral candidate buffer — constructed and handed to a concrete `Provider` by its own constructor (dependency injection), never through O2's `Execute(ctx, Request, Observer)` signature and never through ambient state.
- **Bounded read-only repository snapshot**: exposes the target repository's content exactly as it exists at the session's `BaseRevision`, with that identity sourced solely from `Session.RepositoryDomain`/`Session.BaseRevision`/`workspacecontract.Identity` (hard law 3) — never the live working tree, never a different revision, never uncommitted local state. `InputCandidateDigestSHA256` is computed over this snapshot at creation time.
- **Ephemeral candidate buffer**: a disposable, non-repository-backed capture location (e.g. an isolated temporary directory, never inside the real checkout and never sharing its git index or refs) that a provider writes proposed content into through `CandidateWorkspace`. Destroyed after each run, once its content has been sealed (see below).
- **Sealed candidate artifact**: an immutable, content-addressed artifact holding the final candidate buffer's content, created before the ephemeral capture surface is destroyed. Retained past the run, retrievable by digest, so O4/O5/retry/resume have real bytes to work with, not just a digest pointing at nothing.
- **The runner**: the harness that derives structural repository/revision identity (hard law 3), constructs the snapshot and `CandidateWorkspace`, injects it into a concrete `Provider` (O2 interface; a concrete adapter is O6, out of scope here), invokes O2's `Run` unchanged, computes both digests independently, seals the candidate artifact, and compares its evidence against the unmodified `Result` — passing it through unchanged or rejecting it (hard law 7).
- **Patch-digest computation** for `ProposedChangeDigestSHA256`, reusing the existing convention already present twice in this codebase — `sha256` over `git diff --no-ext-diff --binary`-shaped bytes, as used by `golang/architecture/admission.CaptureChanges` and `golang/architecture/resulttransition`'s private `patchDigest` — as a *convention* to match, not by importing either (both are private and/or coupled to Sensei's own closure-protocol v2 ledger, a different domain from an O1 session against an arbitrary target repository).
- Deterministic digest computation: the same snapshot always produces the same `InputCandidateDigestSHA256`; the same snapshot-plus-final-buffer pair always produces the same `ProposedChangeDigestSHA256`, byte-for-byte.
- Test doubles and adversarial contract tests, including the required proof additions below.

### Explicitly out of scope

- Claude, Codex, OpenAI, Gemini, or local-model provider adapters (O6).
- Evaluation composition (O4).
- Admission or completion bridging (O5).
- Runner support for interpretation, planning, or evaluation-observation operations (hard law 4) — a separate future scope decision, not assumed here.
- Any repository mutation: no `git add`/`commit`/`push`, no file write outside the candidate buffer, no PR creation, no GitHub API call.
- Real `git worktree add`-based physical isolation, or any concurrent multi-worktree pool. The candidate buffer is not a git worktree and has no relationship to the real repository's index, refs, or working tree.
- OS-level sandboxing, containers, process isolation, or any guarantee against an adversarial or buggy in-process provider (hard law 2) — O3 is a cooperative-provider contract.
- Provider selection/routing policy.
- Changes to O1's or O2's existing schemas, types, or commands.
- Autonomous session-driving loops.

## Acceptance criteria

The implementation is complete only when:

- repository read access is bounded to exactly the session's `BaseRevision`, sourced only from `Session`/`workspacecontract.Identity`, proven by a test that any other revision — including `HEAD` and the live working tree — is rejected before a read happens;
- a request for any operation other than `OperationGeneration`, or from any phase other than `PhaseAttempting`, is rejected before a snapshot is taken;
- the candidate buffer's ephemeral surface is never backed by a real repository path, index, or ref, proven by a test that a run leaves the real checkout's `git status`/`git diff` untouched;
- `InputCandidateDigestSHA256` and `ProposedChangeDigestSHA256` are computed independently, over distinct content, at distinct moments, and are never trusted from a provider-declared value;
- a provider `Result` whose declared digests match O3's independently computed evidence passes through byte-for-byte, unmodified; a mismatch is rejected without O3 rewriting, repairing, or injecting any digest into the `Result` or its embedded `Attempt`;
- the sealed candidate artifact persists past the ephemeral buffer's cleanup and is retrievable by digest;
- no repository mutation of any kind appears anywhere in O3;
- O3 does not construct `synthesis.Command` itself — it produces a `Result` for O2's existing, unchanged `MapToCommand`;
- the design and implementation are explicit that O3's guarantee is scoped to cooperative providers using `CandidateWorkspace`, not a claim of physical isolation against an adversarial in-process provider;
- full repository build, vet, tests, freshness, generated-artifact, and Sensei dogfood checks pass;
- no forbidden scope (adapters, evaluation composition, admission bridging, repository mutation, real worktree pooling, OS-level sandboxing, routing policy, autonomous loops) appears;
- the exact implementation head receives an architect-review handoff with fixture digests, matching O1 and O2.

### Required proof additions

Adversarial contract tests must additionally demonstrate:

- **revision pinning**: a runner given a different or wrong `BaseRevision`, or one derived from `HEAD`/the live tree instead of the session/workspace owner, is rejected before any repository read happens;
- **operation/phase scope**: a request for interpretation, planning, or evaluation-observation, or from any phase other than `PhaseAttempting`, is rejected before a snapshot is taken;
- **no ambient channel**: a provider given no `CandidateWorkspace` has no other sanctioned path in the runner to read the snapshot or write a candidate;
- **buffer non-leakage**: nothing written into the ephemeral candidate buffer during a run is observable in the real repository checkout afterward — no new files, no modified index, no dangling ref;
- **digest independence**: `InputCandidateDigestSHA256` changes only when the snapshot changes; `ProposedChangeDigestSHA256` changes only when the final candidate content changes relative to a fixed snapshot; changing one without the other proves they are not proxies for each other;
- **declared-vs-computed comparison, not repair**: a provider `Result` whose declared digests diverge from O3's independently computed evidence is rejected byte-for-byte unmodified — no test may assert that O3 "fixes" a divergent `Result`, only that it rejects it;
- **result pass-through**: a matching `Result` reaches O2's `MapToCommand` identical, field-for-field, to what the provider originally returned;
- **sealed-artifact survival**: the candidate artifact is retrievable by digest after the run completes and the ephemeral buffer has been destroyed;
- **determinism**: identical snapshot and candidate content always produce identical `InputCandidateDigestSHA256`/`ProposedChangeDigestSHA256`, byte-for-byte.

## Implementer protocol

This document is the contract. Implementation must not begin until it is committed to this PR and reviewed by the architect.

Claude must use the repository's canonical Sensei briefing, preflight, task, admission, and verification workflow before editing governed files, exactly as during O1 and O2.

Claude must first map the existing typed owners this runner composes with — `synthesis.Session`'s identity fields, `synthesis.Attempt`'s existing digest fields, `providerport.Run`/`Provider`/`Observer`, `workspacecontract.Identity`, and the existing `sha256(git diff)`-shaped patch-digest convention already used elsewhere in this codebase — rather than approximating or duplicating them. When an interface boundary or mapping is unclear, post `ARCHITECT QUESTION` and stop that affected portion. Do not invent real git-worktree isolation, OS-level sandboxing, a parallel session owner, or weaken closedness to make progress.

Finish each bounded step by posting `IMPLEMENTATION READY FOR ARCHITECT REVIEW` bound to the exact head SHA, including all schema digests, representative fixture digests, contract-test evidence, full verification commands, and explicit confirmation that no provider adapter, real worktree, OS-level sandbox, or repository mutation was added.
