# Cognitive Command Providers O8

Status: implementation contract

## Purpose

O8 supplies concrete Codex and Claude command-backed providers for O7's interpretation and planning slots without granting either command repository, candidate-workspace, admission, completion, or GitHub authority.

O8 consumes only the exact closed O2 request payload already accepted by O7:

- interpretation receives the O1 Session embedded in the O2 Request;
- planning receives the accepted O1 Interpretation embedded in the O2 Request.

No repository path, closure bundle path, graph path, candidate-buffer path, Git worktree, shell, or ambient project directory is disclosed. Because O2 v1 carries digest references rather than resolved closure content, the concrete interpretation records that limitation explicitly. A richer future provider may resolve those digests through a separately governed owner, but O8 does not invent such a resolver.

## Structured command boundary

O8 extends the existing O6C command profile with a reusable `StructuredAgent` capability:

1. direct argv, process-tree cancellation, explicit environment allowlist, output limits, and empty dedicated working directory remain owned by O6/O6C;
2. the command receives one textual prompt containing the exact O2 request JSON and one operation-specific proposal schema;
3. the command returns one JSON proposal and nothing else;
4. Go validates the proposal, assigns all identity and binding fields, computes O1 payload digests, and composes the O2 Result;
5. `providerport.Run`, `MapToCommand`, and O1 remain the only acceptance path.

## Interpretation proposal

The external command may propose only:

- applicable intent;
- binding invariants;
- relevant contracts;
- authority boundaries;
- known failure modes;
- forbidden fixes;
- required proof obligations;
- assumptions;
- unresolved questions;
- additional limitations.

Go fixes:

- schema version;
- interpretation ID;
- session digest;
- generated-by identity;
- objective copied exactly from the Session;
- source references, which remain empty in this session-only provider;
- the explicit session-only context limitation;
- interpretation digest.

## Planning proposal

The external command may propose only:

- ordered plan steps;
- assumptions;
- risks;
- stop conditions.

Go fixes:

- schema version;
- plan ID;
- interpretation digest;
- expected plan generation;
- provider observation from the frozen capability snapshot;
- plan digest.

## Hard laws

1. Only interpretation and planning operations are supported.
2. The exact O2 Request is the complete semantic input. No ambient repository discovery occurs.
3. Unknown JSON fields, multiple documents, trailing text, malformed content, and oversized output fail closed as O2 `invalid-output`.
4. The command cannot choose session, repository, base revision, parent digest, plan generation, provider identity, or any semantic digest.
5. Interpretation objective is copied from the Session and cannot be rewritten.
6. Interpretation source references remain empty because no resolved source content crosses this boundary.
7. The session-only context limitation is always present and cannot be removed by the command.
8. Planning steps remain proposals. O3 and O4 still verify generated candidate evidence and behavior.
9. Direct argv, environment isolation, empty cwd, and process-tree cleanup remain inherited from O6C.
10. No generation mutation, admission, application, verification, commit, push, pull request, approval, merge, or promotion authority is added.

## Required proof

- Codex and Claude structured profiles reuse O6C confinement;
- interpretation and planning proposals are schema-closed;
- Go-owned identity fields cannot be overridden by command output;
- objective, session digest, interpretation digest, plan generation, and provider observation are bound exactly;
- unknown fields and extra documents become typed invalid output;
- `providerport.Run` and `MapToCommand` accept valid results;
- O7 completes with O8 providers and O6C generation using deterministic command doubles and no external credentials;
- full repository CI, dogfood, generated import graph, and smokes pass on the exact accepted head.
