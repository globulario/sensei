# Diff audit identity separation

## Status

Implementation contract for `fix/audit-base-vs-graph-identity`.

Do not merge this branch until the red regression test in
`cmd/awareness-mcp/audit_identity_separation_test.go` is green and the full
Sensei suite is green.

## Defect

`awareness_audit_diff` currently overloads `expected_head` with two unrelated
identities.

`mcpSingleFileChecker.ReadBaseFile` correctly treats `expected_head` as the
caller-pinned base commit of the repository whose candidate diff is being
audited. It uses that commit with `git show <expected_head>:<path>` to recover
the pre-change bytes of modified files. If it is omitted, modify hunks must
remain `cannot_verify`; falling back to the working tree would bind the audit
to ambient mutable state.

`mcpSingleFileChecker.GetFileImpact` also compares that same value to the graph
authority's `SourceRepoCommit` / `GraphBuildCommit`. That second identity is the
snapshot that produced the rules being consulted. In a multi-domain or
separately-built awareness deployment it is not necessarily a commit in the
repository being audited.

The result is a closed loop for every modification:

```text
expected_head omitted
  -> no canonical base bytes
  -> contentLoaded=false
  -> cannot_verify

expected_head = candidate repository base
  -> graph authority commit != expected_head
  -> cannot_verify
```

Added files escape because their new content can be reconstructed from the diff
without base bytes. That explains why add-only changes can pass while modified
files cannot.

## Required semantic repair

Keep the identities separate.

### Audit target identity

`expected_head` remains, for compatibility, the exact commit in the repository
being audited. Its only authority is over reconstruction of candidate base
content and the existing `AuditResult.ExpectedHead` binding.

It MUST continue to be:

- optional at the API boundary;
- validated as a full hex SHA when supplied;
- verified to resolve to a commit in the audited repository;
- required in practice for any modify hunk whose old bytes cannot otherwise be
  canonically reconstructed;
- never guessed from ambient `HEAD`.

A later compatibility cleanup may rename it to `base_head` or
`audit_base_commit`, but that rename is not required to close this defect.

### Graph rule-snapshot identity

`SourceRepoCommit`, falling back to `GraphBuildCommit`, remains the identity of
the authoritative graph/rule snapshot used by `GetFileImpact`.

It MUST continue to be:

- required for an authoritative graph;
- returned by `GetFileImpact`;
- included in the diff-audit digest/provenance through the existing
  `diffaudit` path;
- rejected when absent;
- never replaced with the candidate repository base merely to make two strings
  equal.

### The removed rule

Delete the rule that requires:

```text
graphCommit == expected_head
```

There is no valid cross-repository meaning to that equality. If Sensei later
needs to prove that a domain-specific graph was compiled from the exact target
repository revision, it needs a separately named, typed provenance field that
states that relationship. It must not infer the relationship from a generic
service/rule-snapshot commit.

## Tests

The branch starts with one red property test:

- `TestAuditTargetBaseIsIndependentFromGraphSnapshotCommit`
  - supplies a real candidate repository `expected_head`;
  - supplies a different authoritative graph commit;
  - requires `GetFileImpact` to succeed while returning the graph commit
    unchanged.

Update the existing test that currently encodes the defect:

- `TestAwarenessAuditDiffTool_GraphCommitMismatch`

It must no longer expect `cannot_verify` merely because the graph snapshot
commit differs from the audit target commit. Prefer turning it into a full
modify-file regression using a caller-pinned base, so the acceptance property
covers the exact catch-22 seen by sensei-code.

The following existing properties must remain green and unchanged in meaning:

- modify without `expected_head` -> `cannot_verify`;
- graph authority with no source/build commit -> fail closed;
- malformed/nonexistent `expected_head` -> refused;
- stale/non-authoritative graph -> refused;
- graph snapshot identity remains present in result provenance/digest.

## Downstream handoff

Once this lands, sensei-code can send its immutable
`candidate.Identity.BaseSHA` as `expected_head` on every `awareness_audit_diff`
call. That is the missing base-content binding for modified files.

This upstream PR is therefore a prerequisite for the sensei-code governed
self-evolution closure PR. Do not work around it by stamping the graph service
with the candidate repository SHA unless the graph producer actually defines
that SHA as its source snapshot. A provenance field must say what it really is,
not what a downstream equality check happens to want.

## The third identity: which generation answered (law 5)

Added 2026-09-13 by the graph-identity front
(`docs/architecture/oxygraph_usage.md`, law 5: *every graph query used by that run
must prove it belongs to the pinned identity; a silent generation switch is
forbidden*).

This document separated two identities that had been conflated. Both were needed,
and neither is the one law 5 asks for. A third exists:

| identity | what it names | field |
|---|---|---|
| audit target | the repository base the diff applies to | `expected_head` |
| rule snapshot | the commit that produced the rules consulted | `graph_commit` |
| **generation** | **the bytes that answered the queries** | **`graph_generation_sha256`** |

The closing paragraph above states the rule this obeys: *a provenance field must
say what it really is, not what a downstream equality check happens to want.* The
generation cannot be carried by `graph_commit` for exactly that reason. On this
installation the graph server runs with
`-home-domain github.com/globulario/services`, so the rule snapshot commit belongs
to the **services** repository — two different Sensei graph generations built from
one snapshot share it, and the field cannot distinguish them. Three live stores on
the development machine answer healthily with 237,049 / 142,739 and 35,268 triples.

### The mechanism

The evaluator already enforced *one* identity across an audit, and its comment
already said why: "a divergence means the snapshot shifted mid-audit and the result
cannot be trusted." The generation extends that from the snapshot to the bytes.

`SingleFileChecker` gains an optional companion, beside `BaseFileReader`:

```go
type GraphGenerationReporter interface {
        GraphGeneration(ctx context.Context) (string, error)
}
```

It is sampled **before the first graph query and after the last**, so an equal pair
brackets every query the audit made. Three outcomes, kept distinct:

- equal and non-empty → recorded in `graph_generation_sha256`, which participates
  in the result digest, so two audits answered by different generations are not the
  same record;
- different → `cannot_verify` with reason `graph_generation_switched`, naming both
  values. This is deliberately **not** `graph_unavailable`: "a different graph
  answered the second half" and "the graph did not answer" are opposite diagnoses,
  and collapsing them sends a reader to look for an outage that never happened;
- absent or unreportable → `cannot_verify` with `graph_unavailable`. An identity
  that cannot be checked is not a matching one, which is law 13's rule in another
  currency.

`Validate` gains the same structural lock `graph_commit` already had: an
`available` result must be bound to the generation that answered it, so no future
caller can construct an available verdict anchored to nothing.

### Fixtures this exposed

Optional in the Go sense only: a checker that cannot report a generation produces
results that are *not available*, and six existing fixtures were relying on that
silence — one hand-built `AuditResult`, the evaluator's `fakeChecker`, and five
bridge tests whose fake client had no metadata stub. All were completed rather than
the guard relaxed. A guard moved into a path will often expose invalid fixtures;
correcting the fixture is the work, and relaxing the guard to accommodate them
would leave both defects concealing each other.
