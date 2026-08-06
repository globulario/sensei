# Inference v1 — package-level inferred anchors

Supersedes the cited-but-never-written
`docs/awareness/decisions/inference-v0-direct-anchors-only.md`.

## Status

Accepted 2026-08-06.

## The v0 record did not exist

`impact.go` and `main_test.go` both cited `inference-v0-direct-anchors-only.md`
as the authority for leaving `inferred_*` empty, and `TestImpact_InferredFields
EmptyInV0` enforced it. The file was never written. So the constraint was real
and test-enforced, but its rationale was unrecoverable — the code deferred to a
decision nobody could read.

That is itself the failure this repository keeps finding: an assertion pointing
at evidence that does not exist. It is recorded as
`failure.sensei.dangling_tested_by_annotation_asserted_proof_from_a_test_tha`
in its test-reference form. This document exists so the successor decision
cannot be in the same position.

## What was wrong

Anchoring is per-file. A package whose contract is recorded on one file told an
agent nothing when it opened a sibling:

```
golang/architecture/workspacecontract/admission.go  -> failure mode + required test
golang/architecture/workspacecontract/identity.go   -> nothing
```

Both are governed by `high_risk_files.yaml`, so the hook forced a briefing on
both. Measured across 30 sampled governed files in `golang/architecture/`,
`golang/server/` and `cmd/awg/`: **6 substantive, 24 generic**. Four files in
five made an agent pay for a briefing that had nothing to say — which teaches
the agent the ritual is empty, the opposite of what the guardrail is for.

## Decision

A file's briefing MAY include anchors carried by other files in the same
package directory, in the `inferred_*` response fields the proto already
reserved for "reached via package, symbol, or service walks".

Binding constraints:

1. **Inferred never becomes direct.** Separate response fields, separate prose
   section. A neighbour's invariant is evidence this file sits in governed
   territory, not proof the invariant binds this file.
2. **Every inferred anchor names the sibling it came from.** An inferred anchor
   without attribution is indistinguishable from a direct one at the point of
   reading, which is how a neighbour's rule quietly becomes this file's rule.
3. **Exact package only.** IRI path separators are percent-encoded, so a
   directory prefix also matches nested directories; the exact package is
   enforced after the query. A subpackage's rules must not climb.
4. **Same domain scope as every other section.** A foreign-repo rule arriving
   by inference is the same leak the scoping invariant forbids.
5. **Unavailability is stated.** A failed or unsupported walk renders an
   explicit note. "This package has no other governed files" and "the walk
   could not run" must not look identical.
6. **The walk never sinks the direct answer.** Inference is additive; a backend
   failure degrades enrichment, not the architectural result.

## What is NOT decided here

Symbol-level and service-level walks, which the proto comment also mentions,
remain unimplemented. Component-level inference remains blocked on component
granularity: `importgraph.go` rolls every path under a source root up to two
segments, so all 86 packages under `golang/architecture/` share one component
node. Splitting that re-identifies generated entities graph-wide and is a
migration, not a follow-on to this change.

## Implementation

- `store.PackageAnchorStore` — an optional capability, deliberately outside
  `Store` so the oxigraph client, embedded seed, repograph adapter and ~8 test
  fakes are not forced to grow a method they have no use for. Callers
  type-assert and degrade explicitly.
- `golang/server/package_inference.go` — the walk, exclusion, scoping and
  attribution.
- `golang/server/package_inference_test.go` — one test per constraint above.
- `TestImpact_InferredFieldsEmptyWithoutPackageCapability` retains the v0
  guarantee where it still applies: a backend without the capability produces
  no inferred anchors.
