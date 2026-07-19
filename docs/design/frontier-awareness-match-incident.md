# Frontier B — Governed Incident and Stack-Trace Matching

> **Status: opening contract only.** This document authorizes design review, not implementation.
> The feature must return governed diagnostic candidates with explicit provenance. It must
> never convert resemblance into root-cause authority.

## 1. Objective

Add a bounded MCP tool named `awareness_match_incident` that accepts an error message,
test failure, panic, log excerpt, or stack trace and searches Sensei's governed incident and
failure knowledge for structurally relevant matches.

The tool turns open-ended debugging into a governed first diagnostic pass:

```text
Observed failure
  → normalized diagnostic signature
  → governed incident/failure candidates
  → associated invariants, forbidden fixes, contracts, and verification steps
```

A useful response may say:

> This signature exactly matches governed incident `incident.stale_seed_reload` and is linked
> to failure mode `failure.seed_reload_uses_stale_generation`. The governed record forbids
> bypass `forbidden_fix.disable_seed_freshness_check`.

It may not say that a root cause is proven unless the governed record itself defines an exact,
verified signature and the observation satisfies that full contract.

## 2. Authority boundary

The matcher is a **read-only diagnostic projection**.

It does not:

- mutate incident knowledge;
- promote a candidate into architectural truth;
- choose or apply a fix;
- mark an incident resolved;
- replace test, runtime, or owner verification;
- infer missing evidence with an LLM;
- create a new failure mode from one stack trace.

The canonical distinction is:

- **verified match**: all fields of a governed exact-signature contract match;
- **candidate match**: structural evidence suggests relevance but does not prove identity;
- **no governed match**: the corpus provides no supported candidate;
- **cannot verify**: the corpus or matcher is unavailable/invalid.

A high score alone is never a verified match.

## 3. Proposed MCP contract

Tool name:

```text
awareness_match_incident
```

Input:

```json
{
  "error_or_stacktrace": "<diagnostic text>",
  "task": "<optional task context>",
  "files": ["<optional repository-relative paths>"],
  "runtime": "<optional closed runtime identifier>"
}
```

Rules:

- `error_or_stacktrace` is required and non-empty.
- Unknown fields are rejected.
- Input bytes, lines, frame count, and candidate count are bounded.
- Optional file paths use canonical repository-relative identity and are filtering evidence,
  never proof of incident identity.
- Runtime values, if supported, come from a closed vocabulary. Unknown values are rejected or
  represented as typed unsupported input, never silently normalized.

## 4. Governed source model

The implementation must inspect and reuse the current canonical owners for:

- `incident_patterns.yaml`;
- `failure_modes.yaml`;
- invariants;
- forbidden fixes;
- contracts;
- required tests and verification guidance;
- graph relationships among those records.

The matcher must consume the compiled governed graph or its canonical owner APIs. It must not
maintain a private parallel YAML index whose semantics drift from the graph.

Associations must come from explicit graph edges or canonical source fields. Matching similar
names, shared prefixes, or prose fragments is not sufficient to attach a forbidden fix or
invariant.

## 5. Versioned result

Introduce one canonical result shape, suggested media type:

```text
awareness.incident_match/v1
```

Minimum fields:

- schema/media type;
- deterministic self-excluding digest;
- input digest, never the raw diagnostic text;
- availability (`available`, `cannot_verify`, `unsupported`);
- overall disposition (`verified_match`, `candidate_matches`, `no_match`, `cannot_verify`);
- normalization summary and redaction indicators;
- deterministic ordered candidates;
- for every candidate:
  - governed incident/failure identity;
  - match class;
  - matched signature clauses;
  - unmatched required clauses;
  - exact evidence spans or normalized tokens;
  - provenance;
  - linked invariants, forbidden fixes, contracts, required tests, and verification steps;
  - typed limitations;
- corpus generation/digest used for the query.

Consumers must use the typed disposition and match class. They must not derive authority from
rank, score, or prose.

## 6. Closed match classes

The initial implementation should use a small, reviewable vocabulary:

1. `exact_signature` — every required governed literal/regex/frame/code clause matches;
2. `normalized_signature` — exact match after only governed deterministic normalization of
   volatile values;
3. `structural_candidate` — partial frame/error-code/component structure matches;
4. `context_candidate` — task/file/runtime context narrows an otherwise weak match;
5. `no_match` — no candidate crosses the governed minimum evidence threshold.

Only `exact_signature` and a separately authorized subset of `normalized_signature` may be
reported as `verified_match`. All other classes remain candidates.

No embeddings, opaque model scores, or generative classification are authorized in v1.

## 7. Deterministic normalization

Normalization exists to remove volatility, not meaning.

The normalizer may recognize governed forms for:

- absolute repository paths → repository-relative paths;
- line and column numbers;
- hexadecimal addresses;
- UUIDs and request IDs;
- timestamps and durations;
- goroutine/thread identifiers;
- platform path separators;
- stack-frame argument values where the symbol identity is preserved.

It must not erase:

- error codes;
- function/method/package symbols;
- causal ordering;
- exception/panic class;
- component identity;
- state or verdict vocabulary;
- quoted values that a governed signature declares semantically relevant.

Every normalization rule must be named, versioned, deterministic, and tested. Unknown text is
preserved rather than creatively summarized.

## 8. Candidate ranking

Ranking must be deterministic and explainable.

A candidate may be ordered by a tuple such as:

1. verified before candidate;
2. match class precedence;
3. number/weight of satisfied governed clauses;
4. number of unmatched required clauses;
5. explicit file/runtime relevance;
6. canonical governed record identity.

Weights, if used, are governed constants and part of the result version. Free-form confidence
percentages are forbidden unless their semantics are formally defined and calibrated.

Contradictory exact matches must remain separate candidates and produce a typed ambiguity. The
matcher must not silently pick one.

## 9. Privacy and secret handling

Diagnostic text may contain credentials, tokens, user data, paths, or payloads.

Required protections:

- raw input is not persisted by default;
- logs contain input digest, sizes, and typed outcomes only;
- errors never echo the complete stack trace;
- known secret forms are redacted before evidence snippets are returned;
- result evidence is minimal and bounded;
- cancellation and time limits are enforced;
- no network call or external model receives diagnostic content in v1.

## 10. Failure semantics

Closed typed reasons must distinguish at least:

- empty or oversized input;
- malformed optional path/runtime context;
- graph unavailable;
- governed corpus invalid;
- incident-pattern owner unavailable;
- unsupported pattern version;
- normalization failure;
- contradictory verified signatures;
- result validation failure.

An unavailable corpus is `cannot_verify`, never `no_match`.

## 11. Required acceptance matrix

Implementation is accepted only with proofs for:

1. exact governed panic signature → one verified match with full provenance;
2. same incident with changed line numbers, addresses, timestamps, and request IDs → same
   normalized candidate identity;
3. semantically relevant error code changed → exact match is lost;
4. partial frame overlap → candidate, never verified root cause;
5. two contradictory exact signatures → explicit ambiguity, neither silently selected;
6. explicit graph-linked forbidden fix is returned; a similarly named unlinked record is not;
7. no corpus match → honest `no_match` with zero invented advice;
8. malformed/unsupported governed pattern → typed `cannot_verify` or safely isolated finding,
   according to owner semantics;
9. secrets in input are absent from logs and bounded result evidence;
10. candidate ordering and result digest are stable across repeated runs;
11. zero mutation of governed sources, graph artifacts, tasks, ledgers, or receipts;
12. race, cancellation, size-limit, Unicode, platform-path, and adversarial-regex tests.

## 12. Non-goals

- No automated root-cause declaration from similarity.
- No fix generation or fix application.
- No incident promotion from runtime input.
- No external telemetry upload.
- No embedding/GNN/LLM matcher in v1.
- No replacement for `awareness_briefing`, tests, runtime observation, or owner verification.

## 13. Suggested implementation checkpoints

1. **Canonical diagnostic model + normalizer**: versioned input/result, closed match classes,
   secret-safe normalization, validation, deterministic digest.
2. **Governed pattern compiler + graph composition**: consume canonical sources/owners,
   explicit relationships, exact and structural matching, deterministic ranking.
3. **MCP surface + adversarial closure**: strict schema, bounded execution, rendering, privacy,
   ambiguity/no-match/unavailability proofs.

Each checkpoint must stop for review before expanding matching power.
