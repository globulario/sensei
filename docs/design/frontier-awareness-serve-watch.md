# Frontier C — Atomic Live Reload for Awareness Sources

> **Status: opening contract only.** This document authorizes design review, not implementation.
> Watch mode is a development-time projection lifecycle. It must not create a second source of
> architectural truth or allow partially rebuilt knowledge to become queryable.

## 1. Objective

Add an explicit development mode:

```text
sensei serve --watch
```

When canonical awareness source files change, the running Sensei server rebuilds a candidate
graph, validates it completely, and atomically replaces the active in-memory read snapshot.
Agents querying through MCP or other read surfaces see either the previous valid generation or
the next fully valid generation, never a half-built graph.

The desired feedback loop is:

```text
canonical governed source edit
  → debounced candidate build
  → complete validation
  → atomic generation swap
  → new queries observe the new generation
```

## 2. Truth and authority boundary

Canonical governed source files remain the authored source of truth. Watch mode is only a
consumer and projector of those sources.

It must preserve these distinctions:

- a proposal is not governed truth merely because it exists;
- a source-file mutation is not active merely because a file watcher observed it;
- a candidate graph is not active merely because compilation started;
- only a completely validated generation may become the active read snapshot;
- the on-disk generated artifact remains governed by the existing explicit build/rebuild
  workflow unless a later contract authorizes watch-mode persistence.

Therefore v1 watch mode should update the **in-memory active snapshot only** and should not
silently rewrite `awareness.nt`, stamps, seeds, generated indexes, or repository files.

## 3. Activation and defaults

- Watch mode is opt-in through `sensei serve --watch`.
- Normal `sensei serve` behavior remains unchanged.
- Watch mode is intended for local development and agent sessions, not an implicit production
  default.
- Unsupported combinations of serve flags fail loudly.
- The active repository root and source roots are established once through canonical Sensei
  repository identity. They are not changed by watched file content.

## 4. Watched source set

The first implementation must define one canonical source-selection owner rather than embed
ad hoc glob patterns in the watcher.

The intended initial set is the canonical awareness corpus under:

```text
docs/awareness/**/*.yaml
```

The implementation must inspect current build inputs and reuse the exact canonical source
selection used by explicit graph generation. If explicit generation also depends on schemas,
imports, policy files, or other governed inputs, watch mode must either:

1. watch the same complete input set; or
2. declare unsupported/incomplete coverage and refuse to claim equivalence.

No source may be included or excluded only because a filesystem event happened to arrive.

## 5. Generation model

Introduce a closed generation lifecycle:

```text
observed → queued → building → validating → active
                              ↘ rejected
```

Each build attempt has:

- monotonically increasing local attempt number;
- exact canonical input-set digest;
- candidate graph digest;
- start/completion timestamps for observability only;
- terminal outcome (`activated`, `rejected`, `cancelled`, `superseded`, `unchanged`);
- typed reason codes;
- source paths implicated by validation errors;
- previous and resulting active generation identities.

Generation identity must be content-derived and deterministic. Timestamps and attempt numbers
must not affect graph identity.

## 6. Transactional rebuild and atomic swap

The implementation must follow this sequence:

1. watcher observes one or more relevant filesystem events;
2. debounce/coalesce the event burst;
3. rescan the canonical source set from repository identity, not from event payload alone;
4. compute the exact input-set digest;
5. skip as `unchanged` if it equals the active generation's input digest;
6. build a completely isolated candidate graph/store;
7. run the same required validation used by explicit generation;
8. validate the candidate publication/generation metadata;
9. atomically publish one immutable active snapshot reference;
10. retire the previous snapshot only after in-flight readers release it.

The active graph must never be mutated in place.

A failed candidate build leaves the previous active generation fully available. "Last known
good" is a continuity mechanism, not permission to hide failure: status surfaces must clearly
report that sources are newer than the active graph and why activation failed.

## 7. Filesystem event semantics

The watcher must correctly handle editor and platform behavior:

- direct writes;
- create/remove;
- rename-overwrite atomic saves;
- temporary file creation followed by rename;
- directory creation/removal inside the recursive source set;
- chmod/mode events without content change;
- bursty multi-file saves;
- duplicate and reordered events;
- watcher overflow or dropped-event signals;
- repository root deletion/unavailability.

Events are hints to rescan canonical inputs. They are never the authoritative list of changed
sources.

On overflow or uncertainty, the system performs a bounded full canonical rescan. It does not
continue from an assumed partial event history.

## 8. Concurrency and reader consistency

Every query must bind to exactly one active generation for its full evaluation.

Required properties:

- readers never observe mixed generations;
- a long-running query may finish against the generation it acquired;
- activation of generation N+1 does not invalidate generation N beneath an active reader;
- rebuild work is single-flight or explicitly superseded;
- a newer input digest may cancel/supersede an older candidate build before activation;
- shutdown cancels watcher and build work and releases resources;
- repeated rebuilds do not leak file descriptors, goroutines, stores, or temporary files.

## 9. Status and observability

Watch mode must expose a typed, read-only status through the existing server/status surface or a
separately reviewed thin MCP/CLI view.

Suggested media type:

```text
awareness.watch_status/v1
```

Minimum fields:

- watch enabled;
- repository/source-root identity;
- active generation identity, input digest, and graph digest;
- newest observed input digest;
- whether active state is current or stale relative to observed sources;
- current attempt state;
- last terminal attempt outcome and typed reasons;
- activation/rejection/supersession counters;
- watched input count;
- watcher health (`healthy`, `rescanning`, `degraded`, `unavailable`);
- deterministic result validation.

Raw source content must not be logged. Logs may include generation IDs, digests, counts, paths,
and bounded validation diagnostics.

## 10. Failure semantics

Closed typed reasons must distinguish at least:

- source parse failure;
- schema/governed validation failure;
- import/reference failure;
- deterministic generation failure;
- candidate store creation failure;
- candidate publication validation failure;
- source root unavailable;
- watcher unavailable/overflow;
- candidate superseded;
- activation failure;
- shutdown cancellation.

A rejected generation must never replace the active generation.

If no valid generation has ever been activated, serve startup follows the existing fail-safe or
fail-closed contract of the canonical server owner. Watch mode may not invent an empty graph as
a fallback.

## 11. Interaction with explicit build artifacts

V1 watch mode must not dirty the working tree.

- No automatic write to `awareness.nt`.
- No automatic seed/stamp regeneration.
- No automatic formatting or source rewrite.
- No automatic acceptance of `sensei propose` output.

An explicit build remains required to materialize persistent generated artifacts. The status
surface should make the difference visible:

- `active_ephemeral_generation`;
- `persisted_artifact_generation`, when known;
- whether they are equal or divergent.

A later `--watch-persist` mode would be a separate mutation contract and is out of scope.

## 12. Required acceptance matrix

Implementation is accepted only with proofs for:

1. valid single-file edit → exactly one new active generation;
2. burst of related multi-file saves → one coalesced generation from the complete source set;
3. invalid YAML/schema/reference → candidate rejected, previous graph remains queryable;
4. fix after rejection → next valid generation activates without server restart;
5. atomic editor rename-save → source remains watched and activates correctly;
6. source deletion → complete rescan and honest validation outcome;
7. mode-only/no-content event → no new semantic generation;
8. same canonical source content with different event order → same generation digest;
9. concurrent queries during activation → each query observes exactly one generation;
10. newer edit during slow build → old candidate cancelled/superseded and never activated after
    the newer generation;
11. watcher overflow → bounded full rescan, no silent continuation;
12. rejected candidate never mutates active graph or persisted artifacts;
13. startup/shutdown/restart leaves no goroutine, descriptor, temporary-store, or lock leak;
14. race detector, repeated activation, platform path, and deterministic rebuild tests;
15. normal `sensei serve` remains byte-for-byte behaviorally unchanged when `--watch` is absent.

## 13. Non-goals

- No automatic promotion or acceptance of proposed knowledge.
- No in-place mutation of the active graph/store.
- No persistent artifact regeneration in v1.
- No production-default watcher.
- No distributed watcher coordination.
- No remote filesystem or network source watching.
- No weakening of explicit build, validation, freshness, or generated-artifact gates.

## 14. Suggested implementation checkpoints

1. **Generation and snapshot owner**: immutable generation model, candidate build/validation,
   atomic acquisition/swap, status projection, concurrency proofs.
2. **Canonical watcher**: exact input selection, recursive event handling, debounce/rescan,
   supersession, lifecycle and resource tests.
3. **`serve --watch` composition + adversarial closure**: opt-in wiring, status rendering,
   invalid-source continuity, no-persistence proof, full race/repetition/platform battery.

Each checkpoint must stop for review before the next expands lifecycle behavior.
