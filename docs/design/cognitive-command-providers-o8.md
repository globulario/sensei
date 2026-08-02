# Cognitive Command Planning Providers O8

Status: implementation contract

## Purpose

O8 supplies concrete, bounded Codex and Claude command-backed providers for O7's planning slot without granting either command repository, candidate-workspace, admission, completion, or GitHub authority.

O8 consumes only an already accepted, evidence-grounded O1 Interpretation embedded in the exact closed O2 planning Request. Interpretation remains outside O8 until a separately governed owner can resolve the Session's graph and closure digests into exact source content and source references.

This planning-only boundary is deliberate: digest references without resolved content are not architectural knowledge. O8 must not convert missing closure evidence into invented intent, invariants, contracts, failure modes, forbidden fixes, or proof obligations.

## Structured command boundary

O8 extends the existing O6C command profile with a reusable `StructuredAgent` capability:

1. direct argv, process-tree cancellation, explicit environment allowlist, output limits, and an empty dedicated working directory remain owned by O6/O6C;
2. the command receives the exact O2 planning Request JSON and the exact embedded canonical planning-proposal JSON Schema;
3. the command returns one JSON proposal and nothing else;
4. Go rejects unknown fields, duplicate object keys, multiple documents, trailing text, malformed content, and oversized output;
5. Go assigns all identity and binding fields, computes O1 payload digests, and composes the O2 Result;
6. `providerport.Run`, `MapToCommand`, and O1 remain the only acceptance path.

## Interpretation boundary

O8 does not advertise or execute `providerport.OperationInterpretation`.

A planning Request is eligible only after another governed provider has produced an accepted O1 Interpretation with exact `SourceReferences` bound to source digests. A future interpretation-capable command provider requires a separately governed resolver that supplies those resolved facts. Until that resolver exists, unsupported interpretation configuration fails closed during provider construction.

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

`intended_files` are repository-relative logical references justified by the accepted Interpretation. Absolute paths, repository roots, worktree paths, ambient discovery, and commands are forbidden.

## Hard laws

1. Only the planning operation is supported.
2. The exact O2 planning Request is the complete semantic input.
3. The exact canonical planning-proposal schema is included in the prompt.
4. The planning Request must embed an already accepted O1 Interpretation.
5. Unknown fields, duplicate keys, multiple documents, trailing text, malformed content, and oversized output fail closed as O2 `invalid-output`.
6. The command cannot choose session, repository, base revision, interpretation digest, plan generation, provider identity, or any semantic digest.
7. Planning steps remain proposals. O3 and O4 still verify generated candidate evidence and behavior.
8. Direct argv, environment isolation, empty cwd, and process-tree cleanup remain inherited from O6C.
9. No generation mutation, admission, application, verification, commit, push, pull request, approval, merge, or promotion authority is added.

## Required proof

- Codex and Claude structured profiles reuse O6C confinement;
- O8 refuses interpretation capability at construction;
- planning proposals are schema-closed;
- the exact embedded schema appears in the command prompt and matches the canonical source;
- duplicate JSON keys fail closed rather than using last-value-wins;
- Go-owned identity fields cannot be overridden by command output;
- interpretation digest, plan generation, and provider observation are bound exactly;
- `providerport.Run` and `MapToCommand` accept valid planning results;
- O7 completes using a separate grounded interpretation provider, O8 planning, O6C generation, O3 candidate sealing, and O4 evaluation;
- full repository CI, dogfood, generated import graph, and smokes pass on the exact accepted head.
