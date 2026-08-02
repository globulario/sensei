# Governed Agent Command Bridge O6C

Status: implementation contract

## Purpose

Close the remaining seam between O6's generic command-backed `providerport.Provider` and real coding-agent CLIs without granting a CLI ambient repository mutation, architectural, admission, completion, or GitHub authority.

A raw Codex or Claude process does not natively speak Sensei's closed O2 request/result protocol and cannot be handed O3's in-process `CandidateWorkspace`. O6C therefore introduces a bounded agent-command bridge:

1. Sensei supplies the exact O2 generation request and a read-only bounded snapshot view.
2. The external agent returns one closed mutation-plan document, not a shell script, patch application command, or repository path.
3. Go validates the mutation plan and applies every operation through O3's existing `CandidateWorkspace` methods.
4. O3 supplies a canonical preview of input, proposed-change, and final-candidate evidence.
5. The bridge composes one closed O2 `Result` carrying the corresponding `synthesis.Attempt`.
6. O3 closes the workspace and independently recomputes the same evidence before sealing the candidate. Any divergence remains `digest-mismatch`.

## New bounded contracts

### Candidate evidence preview

O3 remains the sole evidence owner. A concrete O3 workspace may additionally implement a read-only `CandidateEvidencePreviewer` interface. The preview returns:

- `InputCandidateDigestSHA256` from the exact pinned snapshot already extracted by O3;
- `ProposedChangeDigestSHA256` from O3's canonical `GitChangeDigest` implementation;
- `FinalCandidateContentDigestSHA256` from O3's canonical manifest digest.

The preview grants no write capability and does not replace O3's post-close recomputation. It exists only so a workspace-bound provider can populate the immutable Attempt that O2 must return before O3's final verification step.

### Agent mutation plan

The agent returns one closed JSON document containing an ordered list of operations from this vocabulary:

- `write`
- `delete`
- `rename`
- `set-mode`
- `symlink`

Each operation maps one-to-one to an existing `CandidateWorkspace` method. Paths are validated by O3. File content is base64-encoded bytes. Mode uses O3's existing closed mode vocabulary. Unknown fields, unknown operations, invalid paths, duplicate operation IDs, oversized documents, or malformed content fail closed as invalid provider output.

The mutation plan contains no repository root, command, shell, environment, admission decision, completion claim, branch, commit, pull-request, or merge field.

## Provider profiles

Codex and Claude profiles are configuration constructors over a common `AgentCommand` interface. They define only:

- provider identity and model observation;
- executable and direct argv;
- explicit environment allowlist;
- request and response byte limits;
- supported operation set;
- a parser for the vendor's noninteractive response envelope.

Profiles do not choose providers dynamically and do not own authentication. Credentials are inherited only when their variable names are explicitly allowlisted by the caller.

The command process receives a prompt containing the closed task, plan, bounded snapshot files selected by the accepted plan, and the mutation-plan JSON schema. It receives no candidate worktree path. All built-in filesystem, shell, Git, network, commit, and GitHub tools must be disabled by profile configuration. The bridge accepts only the mutation-plan JSON returned in the command's textual result.

## Hard laws

1. The external command never receives a repository or candidate-buffer filesystem path.
2. The external command cannot apply its own patch. Go applies only schema-validated operations through `CandidateWorkspace`.
3. O3 owns evidence preview and final recomputation. Provider-declared evidence is never accepted without O3's post-close verification.
4. Snapshot disclosure is bounded to files named by the accepted plan and successfully read through `CandidateWorkspace.ReadSnapshot`.
5. The agent cannot request additional files during this checkpoint. Missing context becomes a visible provider limitation or non-completed result, never ambient discovery.
6. Direct argv only. No shell interpolation.
7. Environment inheritance is deny-by-default and name-allowlisted.
8. Unknown JSON fields, trailing documents, oversized output, and vendor-envelope ambiguity fail closed.
9. Provider output remains untrusted until O2 validates it, O3 verifies candidate evidence, and O1 accepts the mapped Attempt.
10. No admission, application, commit, push, pull-request, review, merge, promotion, or completion authority is added.

## Deliberately deferred

- interactive agent tool loops and MCP workspace tools;
- arbitrary agent-requested file discovery;
- provider routing or fallback policy;
- durable remote sessions;
- direct GitHub writes;
- OS sandbox claims beyond O6's process and environment controls;
- O7 orchestration.

## Required proof

- exact snapshot-file disclosure and no ambient path exposure;
- all five mutation operations through a recording workspace;
- path traversal, duplicate IDs, unknown fields, invalid base64, invalid modes, and oversized output refused;
- provider cannot mutate after workspace close;
- evidence preview is O3-computed and post-close recomputation catches divergence;
- Codex and Claude profiles produce deterministic direct argv and deny ambient credentials unless explicitly allowlisted;
- deterministic helper-command tests require no external credentials;
- full repository CI, dogfood, generated import graph, and cold-start smokes pass on the exact accepted head.
