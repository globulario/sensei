# Governed Runner Composition O3

Status: architecture contract

## Purpose

Compose the O1 governed synthesis session (`golang/architecture/synthesis`) with the O2 provider-neutral execution port (`golang/architecture/providerport`) into a real runner for exactly one operation — generation from `PhaseAttempting` — the first piece of this roadmap that actually invokes a provider against real repository content, without granting that provider repository truth or silently overclaiming a guarantee no in-process Go boundary can make.

O1 already reserves the fields this closes. `Attempt.InputCandidateDigestSHA256` and `Attempt.ProposedChangeDigestSHA256` exist today, but O1 is pure data — it never computes them, and O2 is I/O-free — its `Request`/`Result` carry no path, file, or patch field anywhere. Nothing today verifies that either digest corresponds to real content; a concrete `Provider` implementation could currently assert any digest it likes inside a `Result`'s embedded candidate payload, and `MapToCommand` would carry it forward untouched, because O2 has no way to know it is wrong. O3 exists to make those two fields trustworthy: it is the one component that touches real repository content, so it is the one component that must never trust a provider's own account of that content — and it must name and own every piece of machinery that claim depends on, not describe any of it as "somehow attached."

## Architectural position

```text
O1 session (PhaseAttempting) + workspacecontract.Identity (canonical workspace owner)
  -> RepositoryDomain / BaseRevision -- structural, sourced only from these two
     owners; no fallback to HEAD, no fallback to the live working tree

governed runner composition (O3), per attempt:
  1. bounded, read-only snapshot at the session's exact BaseRevision
     -> InputCandidateDigestSHA256 computed HERE, over the snapshot's
        canonical manifest, before any provider runs. Every attempt within
        one plan generation starts from this SAME pinned snapshot -- never
        chained from a prior attempt's sealed candidate.
  2. ephemeral candidate buffer initialized as a full copy of that snapshot
  3. CandidateWorkspace constructed over (snapshot, buffer): a typed,
     closable channel -- never O2's Execute(ctx, Request, Observer)
     signature, never an ambient working directory, global, or env var
  4. GenerationProviderFactory.NewProvider(workspace) constructs a FRESH,
     workspace-bound providerport.Provider for this attempt -- never reused
     across attempts or sessions
  5. one capability-driven O2 Provider.Execute(Request, Observer) call
     (O2 Run, reused verbatim, unchanged) -- the provider mutates the
     buffer copy in place (write, delete, rename, chmod, symlink) through
     the workspace's write handle only, producing one complete final tree
  6. O3 receives an IMMUTABLE Result + O2 Receipt back from Run -- neither
     is ever rewritten by O3, whatever they say
  7. workspace.Close() -- any handle a provider retained past this point
     fails closed
  8. O3 independently verifies repository evidence:
     -> ProposedChangeDigestSHA256: git-diff-shaped digest, snapshot -> final tree
     -> FinalCandidateContentDigestSHA256: the final tree's own canonical
        manifest digest (same scheme as InputCandidateDigestSHA256)
     -> compared against the Result's declared values -- never repaired,
        never rewritten; a divergence is a disposition, not a fix
  9. CandidateArtifact sealed (all three digests + identity + manifest) via
     CandidateArtifactStore.Put -- persists past the ephemeral buffer's
     destruction, addressable by CandidateArtifactDigestSHA256
  10. O3 RunnerReceipt issued: references O2's Request/Result/Receipt
      digests by digest (never alters them), carries a closed Disposition
      (verified | digest-mismatch | snapshot-failure | seal-failure |
      cleanup-failure)

  -> only a Result whose RunnerReceipt.Disposition is `verified` reaches
     O2's existing MapToCommand -> RecordAttemptCommand, unchanged
  -> synthesis.Transition decides acceptance (O1, unchanged)
```

O3 composes with O1 and O2 as fixed, closed contracts. It is the shared runner infrastructure that a future concrete provider adapter (O6) is built on top of, so that no individual adapter has to reinvent bounded repository access, candidate sealing, or digest computation, and no individual adapter can shortcut trustworthy evidence by simply asserting a digest.

```go
// Illustrative shapes, not an implementation commitment.

// CandidateWorkspace is the typed, closable channel a concrete Provider is
// constructed with, so it never needs ambient filesystem access to
// participate. Any call after Close returns an error.
type CandidateWorkspace interface {
    ReadSnapshot(path string) ([]byte, error)
    WriteCandidate(path string, content []byte) error
    Delete(path string) error
    Rename(oldPath, newPath string) error
    SetMode(path string, mode CandidateFileMode) error
    Symlink(path, target string) error
    Close() error
}

// GenerationProviderFactory produces a fresh, workspace-bound Provider for
// exactly one attempt. Never reused across attempts or sessions.
type GenerationProviderFactory interface {
    NewProvider(workspace CandidateWorkspace) (providerport.Provider, error)
}
```

## Hard laws

1. **The workspace channel is typed and constructor-injected, never ambient.** A provider proposes content only by reading and writing through a `CandidateWorkspace` it was constructed with. O3 never relies on a working-directory convention, a global, or an environment variable to communicate the snapshot or the candidate buffer's location.
2. **This is a cooperative-provider contract, not a hard sandbox, and the contract must say so.** An in-process Go provider can reach the host filesystem independently of whatever O3 hands it — no typed channel changes that fact. O3 guarantees that a well-behaved provider has no sanctioned path to real repository state other than `CandidateWorkspace`. It does not, and cannot, guarantee that an adversarial or buggy in-process provider is physically prevented from touching the real repository. No implementation, test name, or doc line may claim "structurally impossible" or equivalent language beyond this cooperative-provider scope.
3. **Repository and revision identity are structural, never re-derived.** `RepositoryDomain` and `BaseRevision` come solely from the O1 session (`Session.RepositoryDomain`, `Session.BaseRevision`) and the canonical workspace owner that session identity already traces to (`workspacecontract.Identity`, via `Session.WorkspaceIdentityDigestSHA256`). There is no fallback to `HEAD`, no fallback to the live working tree, and no independent re-derivation from any other source.
4. **This runner handles exactly one operation.** O3 accepts only `OperationGeneration` requests from a session in `PhaseAttempting`. Interpretation, planning, and evaluation-observation are out of scope for this runner; a request for any other operation or phase is rejected before a snapshot is ever taken.
5. **Provider construction is factory-mediated.** A concrete `Provider` is never constructed ambient to O3. `GenerationProviderFactory.NewProvider(CandidateWorkspace)` produces a fresh, workspace-bound `Provider` for every attempt; no `Provider` instance is reused across attempts or sessions.
6. **The workspace closes; retained handles fail closed.** After a run completes — success, failure, or any disposition — O3 closes the `CandidateWorkspace`. Any further call through a handle a provider retained past `Close` returns an error; it never silently succeeds and never falls back to ambient access.
7. **Every attempt starts from the same pinned base.** A retry (a new `AttemptNumber` under the same `PlanGeneration`) starts from the same pinned `BaseRevision` snapshot as every other attempt in that generation — never chained from a prior attempt's sealed candidate. `InputCandidateDigestSHA256` is therefore stable across every attempt within one plan generation; only `ProposedChangeDigestSHA256`, `FinalCandidateContentDigestSHA256`, and the sealed candidate's own manifest vary per attempt.
8. **The candidate is a full tree, not a partial overlay.** The ephemeral buffer is initialized as a complete copy of the pinned snapshot; a provider mutates that copy in place (write, delete, rename, change mode, create a symlink) to produce one complete, self-consistent final tree. O3 does not support a partial change-operation model — every attempt's candidate is diffable, in full, against the snapshot using real `git diff` semantics (which already understands renames, deletions, mode changes, and symlinks natively).
9. **Canonical tree encoding is closed and specific.** Paths are POSIX-relative, `/`-separated, with no `.`/`..` segment and no leading `/`; any path that would traverse outside the tree root is rejected before it reaches the buffer. Symlink targets are recorded as opaque strings and never resolved or followed by O3 — a symlink cannot be used to escape the tree, because O3 never dereferences one. Files are enumerated in sorted lexicographic path order for every canonical digest. Content is digested as raw bytes, with no encoding or line-ending transform. Mode is a closed three-value vocabulary: regular, executable, symlink. A path present in the snapshot and absent from the final tree is a deletion.
10. **Three digests, not two, describe a candidate.** `InputCandidateDigestSHA256` (the pinned snapshot's canonical manifest digest, computed before the provider runs), `ProposedChangeDigestSHA256` (the `git diff`-shaped digest from that snapshot to the final tree, computed after), and `FinalCandidateContentDigestSHA256` (the final tree's own canonical manifest digest, computed the same way as `InputCandidateDigestSHA256` but over the ending state) are three separate identities. None substitutes for another.
11. **A provider-declared digest is never trusted.** O3 always independently computes all evidence from what it directly observed through `CandidateWorkspace` — never from what a provider's `Result` payload already asserts — the same "declared must equal recomputed" law O1 and O2 already enforce (`invariant.synthesis.declared_digest_must_equal_computed_content_digest`), applied here to real file content for the first time.
12. **O3 never mutates a provider's `Result` or `Receipt`.** It compares its own independently computed evidence against the unchanged `Result`. A match passes the `Result` through byte-for-byte to O2's existing `Run`/`MapToCommand`. A mismatch is rejected before `MapToCommand` ever sees it. O3 never repairs, rewrites, or injects corrected digests into a candidate `Attempt`, its `Result`, or O2's `Receipt` — doing so would break O2's own declared-equals-recomputed vacuum seal over the outer `Result` envelope (outer digest, payload digest, and embedded `Attempt` digest all already revalidated by O2).
13. **O2's completion and O3's verification are two different layers of truth, and both are recorded, never merged.** O2's `Run` can truthfully report a completed `Result` and `Receipt` — meaning the provider's execution itself finished as claimed — while O3 separately finds the claimed patch digest does not match observed reality. These are not a contradiction to paper over; they are two distinct questions with two distinct, separately-recorded answers. O3 issues its own closed `RunnerReceipt` referencing O2's `RequestDigestSHA256`/`ResultDigestSHA256`/`ReceiptDigestSHA256` by digest — it never alters them.
14. **The sealed candidate is a closed, addressable artifact, not a digest pointing at nothing.** The ephemeral capture surface is destroyed after every run, but before that happens its content is sealed into an immutable `CandidateArtifact` via `CandidateArtifactStore.Put`, which verifies the artifact's own digest before storing; `Get` reverifies it before returning, so storage corruption cannot silently launder a tampered artifact. O4, O5, retry, and resume address a candidate by `CandidateArtifactDigestSHA256` against this store — never against a temporary directory or any other filesystem location.
15. **O3 produces evidence, not admission.** A real, independently computed digest is stronger evidence than an asserted one, but the candidate it describes remains under evaluation — O3 grants no correctness, completion, evaluation, or admission authority, exactly as O1 and O2 already state for their own outputs.
16. **O3 does not redefine O1 or O2.** It does not add fields to `Attempt`, `Request`, or `Result`, does not weaken any existing schema, and does not construct O1 commands itself. `MapToCommand` (O2) remains the only path from a `Result` to a `synthesis.Command`; `Transition` (O1) remains the only session-transition authority.

## O3 scope

### In scope

- **`CandidateWorkspace`**: a typed, closable channel — read access to the pinned snapshot, and write/delete/rename/mode/symlink access to the ephemeral candidate buffer — constructed and handed to a concrete `Provider` by its own constructor (dependency injection), never through O2's `Execute(ctx, Request, Observer)` signature and never through ambient state. Calls after `Close` fail closed (hard law 6).
- **`GenerationProviderFactory`**: constructs a fresh, workspace-bound `Provider` for exactly one attempt (hard law 5). A concrete factory implementation (wiring to a real model/SDK) is O6, out of scope here.
- **Bounded read-only repository snapshot**: exposes the target repository's content exactly as it exists at the session's `BaseRevision`, with that identity sourced solely from `Session.RepositoryDomain`/`Session.BaseRevision`/`workspacecontract.Identity` (hard law 3) — never the live working tree, never a different revision, never uncommitted local state, and stable across every attempt within one plan generation (hard law 7). `InputCandidateDigestSHA256` is computed over this snapshot's canonical manifest at creation time.
- **Ephemeral candidate buffer**: a disposable, non-repository-backed capture location (e.g. an isolated temporary directory, never inside the real checkout and never sharing its git index or refs), initialized as a full copy of the snapshot (hard law 8) that a provider mutates in place through `CandidateWorkspace`. Destroyed after each run, once its content has been sealed.
- **Canonical tree encoding**: the exact path, symlink, ordering, content, and mode rules in hard law 9, applied identically to compute `InputCandidateDigestSHA256` and `FinalCandidateContentDigestSHA256`.
- **`CandidateArtifact`**: an immutable, closed document binding repository/base/workspace identity, session/plan/generation/attempt identity, `InputCandidateDigestSHA256`, `ProposedChangeDigestSHA256`, `FinalCandidateContentDigestSHA256`, a canonical manifest of the final tree, and its own self-referential `CandidateArtifactDigestSHA256`.
- **`CandidateArtifactStore`**: the sole typed owner of `CandidateArtifact` persistence, with a verified `Put` (rejects a self-inconsistent artifact before storing) and a verified `Get` (rejects a corrupted artifact before returning it). The exact storage substrate (filesystem, embedded store, or otherwise) is an implementation decision; the verification contract is not.
- **`RunnerReceipt`**: O3's own closed evidence document, referencing O2's `Request`/`Result`/`Receipt` digests and the `CandidateArtifactDigestSHA256` by digest, carrying a closed `Disposition` (`verified`, `digest-mismatch`, `snapshot-failure`, `seal-failure`, `cleanup-failure`) and its own self-referential `RunnerReceiptDigestSHA256`.
- **Patch-digest computation** for `ProposedChangeDigestSHA256`, reusing the existing convention already present twice in this codebase — `sha256` over `git diff --no-ext-diff --binary`-shaped bytes, as used by `golang/architecture/admission.CaptureChanges` and `golang/architecture/resulttransition`'s private `patchDigest` — as a *convention* to match, not by importing either (both are private and/or coupled to Sensei's own closure-protocol v2 ledger, a different domain from an O1 session against an arbitrary target repository).
- Deterministic digest computation: the same snapshot always produces the same `InputCandidateDigestSHA256`; the same snapshot-plus-final-tree pair always produces the same `ProposedChangeDigestSHA256` and `FinalCandidateContentDigestSHA256`, byte-for-byte.
- Test doubles and adversarial contract tests, including the required proof additions below.

### Explicitly out of scope

- Claude, Codex, OpenAI, Gemini, or local-model provider adapters, and any concrete `GenerationProviderFactory` wiring to a real model or SDK (O6).
- Evaluation composition (O4).
- Admission or completion bridging (O5).
- Runner support for interpretation, planning, or evaluation-observation operations (hard law 4) — a separate future scope decision, not assumed here.
- Any repository mutation: no `git add`/`commit`/`push`, no file write outside the candidate buffer, no PR creation, no GitHub API call.
- Real `git worktree add`-based physical isolation, or any concurrent multi-worktree pool. The candidate buffer is not a git worktree and has no relationship to the real repository's index, refs, or working tree.
- OS-level sandboxing, containers, process isolation, or any guarantee against an adversarial or buggy in-process provider (hard law 2) — O3 is a cooperative-provider contract.
- Candidate lineage across attempts (hard law 7) — no attempt starts from a prior attempt's sealed candidate.
- Provider selection/routing policy.
- Changes to O1's or O2's existing schemas, types, or commands.
- Autonomous session-driving loops.

## Acceptance criteria

The implementation is complete only when:

- repository read access is bounded to exactly the session's `BaseRevision`, sourced only from `Session`/`workspacecontract.Identity`, proven by a test that any other revision — including `HEAD` and the live working tree — is rejected before a read happens;
- a request for any operation other than `OperationGeneration`, or from any phase other than `PhaseAttempting`, is rejected before a snapshot is taken;
- every attempt gets a freshly constructed, workspace-bound `Provider` from `GenerationProviderFactory` — never a reused instance — and the same pinned snapshot as every other attempt in its plan generation;
- a `CandidateWorkspace` call made after `Close` returns an error rather than succeeding or panicking;
- the candidate buffer's ephemeral surface is never backed by a real repository path, index, or ref, proven by a test that a run leaves the real checkout's `git status`/`git diff` untouched;
- `InputCandidateDigestSHA256`, `ProposedChangeDigestSHA256`, and `FinalCandidateContentDigestSHA256` are computed independently, over distinct content, at distinct moments, using the canonical tree encoding in hard law 9, and are never trusted from a provider-declared value;
- a provider `Result` whose declared digests match O3's independently computed evidence passes through byte-for-byte, unmodified; a mismatch is rejected without O3 rewriting, repairing, or injecting any digest into the `Result`, its embedded `Attempt`, or O2's `Receipt`;
- a `CandidateArtifact` is sealed via `CandidateArtifactStore.Put` before the ephemeral buffer is destroyed, persists past that destruction, and is retrievable and re-verified by `Get`;
- a `RunnerReceipt` is issued for every run with a closed `Disposition`, referencing O2's `Request`/`Result`/`Receipt` digests without altering them;
- only a `Result` whose `RunnerReceipt.Disposition` is `verified` reaches O2's existing, unchanged `MapToCommand`;
- no repository mutation of any kind appears anywhere in O3;
- O3 does not construct `synthesis.Command` itself;
- the design and implementation are explicit that O3's guarantee is scoped to cooperative providers using `CandidateWorkspace`, not a claim of physical isolation against an adversarial in-process provider;
- full repository build, vet, tests, freshness, generated-artifact, and Sensei dogfood checks pass;
- no forbidden scope (adapters, evaluation composition, admission bridging, repository mutation, real worktree pooling, OS-level sandboxing, candidate lineage, routing policy, autonomous loops) appears;
- the exact implementation head receives an architect-review handoff with fixture digests, matching O1 and O2.

### Required proof additions

Adversarial contract tests must additionally demonstrate:

- **revision pinning**: a runner given a different or wrong `BaseRevision`, or one derived from `HEAD`/the live tree instead of the session/workspace owner, is rejected before any repository read happens;
- **operation/phase scope**: a request for interpretation, planning, or evaluation-observation, or from any phase other than `PhaseAttempting`, is rejected before a snapshot is taken;
- **no ambient channel**: a provider given no `CandidateWorkspace` has no other sanctioned path in the runner to read the snapshot or write a candidate;
- **fresh provider per attempt**: two attempts in the same plan generation receive two distinct `Provider` instances from `GenerationProviderFactory`, never the same one;
- **fail-closed after `Close`**: any `CandidateWorkspace` method called after `Close` returns an error;
- **stable base across retries**: two attempts in the same plan generation produce the same `InputCandidateDigestSHA256`;
- **full-tree diffability**: a candidate that deletes a file, renames a file, changes a file's mode, or replaces a file with a symlink is correctly reflected in `ProposedChangeDigestSHA256` via real `git diff` semantics;
- **traversal and symlink containment**: a candidate path containing `..`, an absolute path, or a symlink target is never resolved outside the tree root — recorded as opaque data, never followed;
- **buffer non-leakage**: nothing written into the ephemeral candidate buffer during a run is observable in the real repository checkout afterward — no new files, no modified index, no dangling ref;
- **digest independence**: `InputCandidateDigestSHA256` changes only when the snapshot changes; `ProposedChangeDigestSHA256` and `FinalCandidateContentDigestSHA256` change only when the final tree changes relative to a fixed snapshot; changing one without the others proves none is a proxy for another;
- **declared-vs-computed comparison, not repair**: a provider `Result` whose declared digests diverge from O3's independently computed evidence is rejected byte-for-byte unmodified — no test may assert that O3 "fixes" a divergent `Result`, only that it rejects it;
- **result and receipt pass-through**: a matching `Result` reaches O2's `MapToCommand` identical, field-for-field, to what the provider originally returned, and O2's `Receipt` is never altered by O3 regardless of `RunnerReceipt.Disposition`;
- **sealed-artifact survival and verification**: a `CandidateArtifact` is retrievable by digest after the run completes and the ephemeral buffer has been destroyed; a corrupted stored artifact is rejected by `Get`, not silently returned;
- **disposition coverage**: each of `verified`, `digest-mismatch`, `snapshot-failure`, `seal-failure`, and `cleanup-failure` is independently reachable and asserted by a dedicated test;
- **determinism**: identical snapshot and final-tree content always produce identical `InputCandidateDigestSHA256`/`ProposedChangeDigestSHA256`/`FinalCandidateContentDigestSHA256`, byte-for-byte.

## Implementer protocol

This document is the contract. Implementation must not begin until it is committed to this PR and reviewed by the architect.

Claude must use the repository's canonical Sensei briefing, preflight, task, admission, and verification workflow before editing governed files, exactly as during O1 and O2.

Claude must first map the existing typed owners this runner composes with — `synthesis.Session`'s identity fields, `synthesis.Attempt`'s existing digest fields, `providerport.Run`/`Provider`/`Observer`/`Receipt`, `workspacecontract.Identity`, and the existing `sha256(git diff)`-shaped patch-digest convention already used elsewhere in this codebase — rather than approximating or duplicating them. When an interface boundary or mapping is unclear, post `ARCHITECT QUESTION` and stop that affected portion. Do not invent real git-worktree isolation, OS-level sandboxing, candidate lineage across attempts, a parallel session owner, or weaken closedness to make progress.

Finish each bounded step by posting `IMPLEMENTATION READY FOR ARCHITECT REVIEW` bound to the exact head SHA, including all schema digests, representative fixture digests, contract-test evidence, full verification commands, and explicit confirmation that no provider adapter, real worktree, OS-level sandbox, or repository mutation was added.
