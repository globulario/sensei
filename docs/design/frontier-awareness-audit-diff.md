# Frontier A — Governed Multi-File Diff Audit

> **Status: Active implementation.** This document governs the `awareness_audit_diff` MCP tool.
> The implementation remains a thin MCP/CLI surface over existing Sensei graph,
> edit-check, admission, and policy owners without creating a second rule evaluator.

## 1. Objective

Add a bounded MCP tool named `awareness_audit_diff` that lets an AI coding agent submit the
actual unified Git diff it intends to present or commit and receive one deterministic,
read-only audit across every touched file.

The tool closes the gap between:

- single-file proposed-content checking;
- repository change admission; and
- the agent's final multi-file change as one architectural subject.

The result is a fast pre-presentation guard. It answers:

> Given exactly this supplied change set, which active invariants, failure modes,
> forbidden fixes, contracts, required tests, or governed boundaries are implicated?

It does **not** answer whether the caller supplied every repository change unless a later,
separately reviewed authoritative observation mode proves that identity.

## 2. Core semantic boundary

`awareness_audit_diff` is a **read-only projection**, never an admission authority and never a
mutation path.

For v1, the diff is caller-supplied evidence. Therefore:

- a clean result means "no blocking finding was found in the supplied diff";
- it must never mean "the working tree is certified clean";
- the result must expose `input_trust: caller_supplied`;
- the tool must never silently infer unsubmitted files from branch names, recent commits,
  task directories, editor buffers, or ambient Git state.

A future authoritative mode may compare a submitted digest with a server-observed Git change,
but that is out of scope for this PR unless separately governed.

## 3. Proposed MCP contract

Tool name:

```text
awareness_audit_diff
```

Input:

```json
{
  "diff": "<unified git diff>",
  "task": "<optional human task text>",
  "expected_head": "<optional exact commit SHA>"
}
```

Rules:

- `diff` is required and non-empty.
- Unknown fields are rejected.
- Input size, file count, hunk count, and line count are bounded by named constants.
- `expected_head`, when supplied, is exact identity evidence only. It is never guessed,
  shortened, normalized, or replaced by the current branch.
- Raw shell commands, arbitrary file reads, URLs, and repository paths supplied outside the
  diff are not accepted.

## 4. Canonical result

Introduce one versioned result shape, suggested media type:

```text
awareness.diff_audit/v1
```

Minimum fields:

- schema/media type;
- deterministic self-excluding digest;
- input diff digest;
- input trust (`caller_supplied` in v1);
- availability (`available`, `cannot_verify`, `unsupported`);
- decision (`pass`, `review`, `block`, `cannot_verify`);
- exact ordered changed-file set;
- per-file change kind (`add`, `modify`, `delete`, `rename`, `mode_change`, `binary`);
- deterministic ordered findings;
- every finding's governed record identity, class, severity/disposition, file/hunk evidence,
  provenance, and explanation;
- implicated required tests and contracts when represented by the graph;
- limitations and typed reason codes.

No consumer may infer a stronger decision from empty arrays or free-form prose. The typed
decision is canonical.

## 5. Diff parsing and identity rules

The parser must treat the supplied diff as hostile input.

It must:

1. parse standard unified Git diff structure without executing hooks, filters, textconv, or
   caller-provided commands;
2. canonicalize repository-relative paths using the existing Sensei path semantics;
3. reject absolute paths, traversal, NUL bytes, malformed quoting, duplicate logical paths,
   case-collision ambiguity, and contradictory file operations;
4. preserve add/delete/rename/mode-change semantics rather than flattening everything into
   "modified";
5. surface binary or unsupported patches as typed `cannot_verify` findings, never silently
   omit them;
6. reject partial parse success: one malformed file section makes the whole change subject
   unavailable unless the canonical parser can prove safe isolation;
7. preserve deterministic ordering independent of input file order where semantics permit.

## 6. Evaluation composition

The MCP surface must delegate to existing owners. The implementation must first map the
current repository call graph and identify:

- the canonical graph query owner;
- the single-file edit-check evaluator;
- change-envelope/admission evaluation;
- policy and finding vocabularies;
- required-test and forbidden-fix projections.

The new composition may orchestrate these owners, but must not copy their rule semantics into
the MCP handler.

**Admission is not composed into v1.** The canonical admission owner
(`admission.Verify`) observes *ambient* repository/working-tree state against an
admitted scope. That is a different subject than the caller-supplied diff, and
invoking it here would both make this read-only projection an admission
authority and cause it to read ambient Git state — each forbidden by §2.
Working-tree admission verification is the separately governed `verify_admission`
tool. This projection lists admission among the owners it is *aware of*, not
among the owners it *calls*.

**Companion and test obligations require typed file roles.** The graph query
owner returns governed records whose `related_ids` are IRI-shaped node
references and whose `anchor` records the authoring location, never necessarily
the implementation or test file that must change. The handler must not infer
obligation file paths from these by string shape; doing so fabricates
obligations. A required_test's own canonical ID (`<test/file>:<TestName>`) is a
grounded exception — it *is* the test file's identity. The cross-file companion
checks in §7 (a contract changed without its implementation/test; a required
companion omitted) therefore stay dormant until the graph owner returns typed
companion/test file roles (a later, separately reviewed Impact-contract
extension). A dormant grounded check is honest; a firing guessed one is not.

Required high-level flow:

1. validate and parse the complete diff;
2. establish the exact changed-file subject;
3. derive bounded per-file before/after evidence only through canonical repository context;
4. gather governed records relevant to every path and change relation;
5. evaluate the change as one multi-file subject so cross-file invariants are not reduced to
   unrelated single-file checks;
6. merge findings monotonically and deterministically;
7. publish one canonical result.

A file-level pass cannot erase a cross-file block. Duplicate findings must merge by canonical
finding identity, not by matching prose.

## 7. Required behavior

The tool must detect and report, when represented by active governed knowledge:

- a forbidden fix introduced in any touched file;
- a required companion change omitted from the supplied change set;
- a contract changed without its required implementation or test;
- a high-risk file changed without required proof;
- mutually inconsistent edits across files;
- deleted or renamed governed targets;
- active invariant violations caused only by the composition of otherwise locally valid edits.

Zero relevant records is an honest result, not permission to invent advice.

## 8. Failure semantics

Closed typed reasons must distinguish at least:

- malformed diff;
- unsupported diff feature;
- path identity invalid;
- repository context unavailable;
- graph unavailable;
- evaluator unavailable;
- governed corpus invalid;
- bounded-input limit exceeded;
- result validation failure.

Availability failures must never be rendered as `pass`.

## 9. Security and privacy

- Do not persist the raw diff by default.
- Logs may contain the diff digest, counts, and typed reasons, never source content or secrets.
- Error messages must not echo arbitrary diff payloads.
- Evaluation must be cancellable and bounded.
- Symlink and repository-boundary behavior must reuse canonical Sensei path ownership rather
  than ad hoc filesystem checks in the MCP handler.

## 10. Acceptance matrix

Implementation is accepted only with proofs for:

1. valid multi-file diff with no findings → deterministic `pass`;
2. one blocking invariant in one file → `block` with exact provenance;
3. cross-file invariant violated only by composition → `block`;
4. required companion file omitted → finding identifies the missing obligation;
5. add/delete/rename/mode-only changes preserve their exact classes;
6. binary change → typed `cannot_verify`, never omission;
7. traversal, absolute path, duplicate path, and case collision → rejection;
8. malformed second file after a valid first file → no partial success;
9. same semantic diff with reordered file sections → same canonical digest and findings;
10. graph/evaluator unavailable → `cannot_verify`, never `pass`;
11. zero mutation of governed files, Git state, ledgers, receipts, or generated artifacts;
12. race, cancellation, size-limit, and repeated-run determinism tests.

## 11. Non-goals

- No automatic commit, staging, patch application, or file mutation.
- No new architectural rule language.
- No replacement for admission or completion gates.
- No claim that a caller-supplied diff is the repository's complete change.
- No fuzzy or generative finding creation unsupported by governed records.
- No CI enforcement in this PR.

## 12. Suggested implementation checkpoints

1. **Canonical parser + versioned result**: hostile-input parsing, identity, validation,
   deterministic digest, tests.
2. **Owner composition**: reuse current edit-check/graph/admission owners; multi-file merge and
   cross-file obligations.
3. **MCP/CLI surface + adversarial closure**: strict schema, bounded execution, rendering,
   no-mutation and determinism battery.

Each checkpoint must stop for review before the next expands the authority surface.
