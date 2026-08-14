# Closure authority must follow disposition, not source membership

## Decision status

Proposed for implementation. This document defines the product contract and acceptance criteria before code changes.

## Problem

`sensei import --refresh` creates machine-generated candidate knowledge. Those records are intentionally unpromoted and the publication owner correctly refuses to publish them as governing knowledge.

The closure path currently treats identities discovered under the certified source surface as identities that must appear in the authoritative projection. That composes badly with candidate publication semantics: ordinary onboarding can generate candidate identities that are then required for semantic closure even though they have not earned authority.

The defect is general, not benchmark-specific:

> Discovery membership is being conflated with authority-required projection membership.

## Required semantic model

Source/discovery membership only means Sensei knows about a record. It does not grant that record governing authority.

The required flow is:

```text
source/discovery
        |
        v
knowledge visible to Sensei
        |
        v
canonical disposition / owner-governed admission
        |-- candidate / non-governing
        |       -> discoverable
        |       -> NOT required in authoritative projection
        |
        `-- owner-adopted / governing
                -> required in authoritative projection
                -> absence may block semantic closure
```

## Authority rule

An identity becomes closure-required only through the canonical owner-governed admission/adoption mechanism.

Neither of these may independently confer authority:

- filesystem pathname or directory membership;
- a caller-editable `status:` field in source text.

Location tells Sensei where knowledge was found. It does not tell Sensei whether that knowledge governs.

## Owner boundary

`closure.ComputeClosureRoots` reconciles the required identity set it receives against the published projection. It should remain a reconciliation mechanism unless implementation evidence proves a change there is necessary.

The primary repair belongs at the producer of the required/expected identity set: separate discovered identities from authority-required identities before closure reconciliation.

Reuse the existing canonical adoption/governance mechanism. Do not invent a second admission ontology or a convenience boolean that callers can self-assert.

## Explicit non-goals / forbidden fixes

This change must NOT:

- exclude `docs/awareness/candidates/**` merely by pathname;
- promote the generated candidate set to make closure green;
- add benchmark-specific exceptions;
- weaken candidate publication filtering;
- weaken `sensei import --refresh` candidate generation;
- fabricate or require a graph-authority transaction stamp for this authority decision;
- treat raw source `status: adopted` as sufficient evidence of owner-governed admission.

A path-based exclusion only inverts the defect from `path => authority` to `path => non-authority`. A candidate moved elsewhere would then accidentally become governing.

## Acceptance tests

The product revision is acceptable only if all of the following are proven by tests:

1. Candidate knowledge remains discoverable but is not authoritative-projection-required.
2. Generating new candidates does not by itself degrade an otherwise authoritative repository.
3. Owner-governed adoption/promotion makes that knowledge authoritative-projection-required.
4. Removing an admitted required identity from the published projection still makes semantic closure fail.
5. Moving candidate content between directories does not alter its authority disposition.
6. **Load-bearing:** editing a caller-controlled `status:` field cannot manufacture governing authority.
7. Rejected, stale, superseded, and other non-governing dispositions remain outside the required authority set according to canonical semantics.

## R3 boundary

The benchmark does not repair this defect. It does not promote candidates and does not hide them from closure.

R3 may consume this change only after a product revision is implemented, accepted, and frozen. The executed Sensei binary must then be preregistered by resolved-path SHA-256 before the run begins.

## Implementation guidance

Find the caller that constructs the `expected`/required identity set passed to closure reconciliation. Split discovery from authority-required membership there, using the existing owner-governed admission/adoption evidence.

Do not make directory names, raw YAML disposition strings, or benchmark configuration into authority sources.
