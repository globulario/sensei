# Serve Runtime Compatibility

Status: additive implementation contract for issue #118  
Base revision: `f7b7b18`  
Target repository: `globulario/sensei`

## 1. Problem

`sensei serve` decides whether to reuse an already-listening Oxigraph or awareness-graph process using nothing but a bare TCP dial: if the port answers, it is reused.

```text
sensei serve: port 127.0.0.1:7878 already in use — using existing Oxigraph
sensei serve: refreshed runtime graph marker <checkout>/.sensei/graph-authority.json from live store
awareness-graph: listen on :10120: bind: address already in use
sensei: child exited
```

Nothing about that sequence checks whether the surviving process belongs to the current checkout, targets the same Oxigraph store, or watches the same graph-marker-file this invocation would use. Worse, the checkout-local marker refresh runs *before* the awareness-graph child even attempts to bind, so a checkout's own `.sensei/graph-authority.json` gets overwritten with data read from whatever store happened to answer — even when that store is a long-lived, shared instance serving a completely different checkout.

The observable failure: a scoped `sensei build --repo <domain>` genuinely succeeds, but `sensei metadata`/`preflight` against the surviving (foreign) awareness-graph service keeps reporting stale — because the surviving service was never told anything changed. It is watching a marker path nobody who actually changed the store just updated.

## 2. Mission

`sensei serve` must prove an occupied listener is compatible with the current invocation's execution habitat before reusing it. An unproven occupant is a hard failure with an actionable diagnostic, never a silent mix of checkout-local and global state.

Port occupancy is observation, not authority — mirroring `docs/design/checkout-repository-domain-binding.md` §3.1's "store contents do not own checkout identity." The parallel law here: an occupied port does not own the right to be reused.

## 3. Core laws

### 3.1 Port occupancy is not authority

That something answers on an address proves only that something is listening. It never, by itself, proves it is safe to treat as this invocation's Oxigraph or awareness-graph service.

### 3.2 The compatibility fingerprint is a durable runtime descriptor, not ambient config

Each process a `sensei serve` invocation actually starts (or that self-describes, for the awareness-graph binary) records a small runtime descriptor: PID, the process kind, the listen address, and the fields specific to that kind (Oxigraph query URL, graph-marker-file path, repo-root, repo-domain, or data directory). This descriptor is the only thing a later invocation may consult to decide reuse — never an environment variable, a config file, or a guess from directory names.

### 3.3 Descriptors are keyed by listen address, not by checkout

A new invocation has no way to know in advance which checkout, if any, started whatever is listening on a given address. The descriptor path is therefore derived from `(process kind, listen address)` alone, stored under a machine-global location, not inside any one checkout's state directory.

### 3.4 Reuse requires exact match, not approximate similarity

An occupied Oxigraph port is compatible only when its descriptor's data directory exactly matches what this invocation would use. An occupied awareness-graph port is compatible only when its descriptor's Oxigraph query URL, graph-marker-file path, repo-root, and repo-domain all exactly match (including both sides leaving repo-root/repo-domain unset). Repository domain is deliberately *not* part of the Oxigraph comparison — one store may legitimately hold multiple repository domains (`docs/design/checkout-repository-domain-binding.md`); it is the awareness-graph service's marker binding that is harmful to share, not multi-domain store sharing.

### 3.5 Unidentifiable occupancy is incompatible occupancy

An occupied port with no descriptor, an unreadable/corrupt descriptor, or a descriptor naming a process that is no longer running is treated identically to a descriptor that names a genuinely incompatible checkout: a hard failure. "The port merely responds" is never sufficient authority to reuse it, regardless of why the descriptor is missing.

### 3.6 Marker refresh never precedes compatibility proof

Any operation that reads live-store state to refresh a checkout-local marker file must run only after this invocation has established that it either started a fresh, uncontended awareness-graph process, or proved an existing one compatible. It must never run merely because a store happened to answer.

### 3.7 Reuse degrades ownership, not correctness

When an occupied listener is proven compatible, `sensei serve` does not start a redundant process for that piece — it logs the reuse and continues in a monitor-only capacity for it: no attempt to signal, wait on, or kill a process this invocation never started. Backend health is still watched. On shutdown, a reused piece is left running untouched.

## 4. Commands and surfaces

- `sensei serve` — both the Oxigraph and awareness-graph reuse decisions.
- `golang/server/main.go`'s `serve()` — the awareness-graph binary self-describes immediately after its own `net.Listen` succeeds, and removes its descriptor on graceful shutdown. It is the only process that can prove what it was actually started with.
- Oxigraph is a third-party binary and cannot self-describe; the `sensei serve` wrapper that starts it writes its descriptor on its behalf.
- Explicitly **not** `sensei build`: its scoped-update path already resolves the default marker path through the same shared helper `sensei serve` uses, so the default-path case is already consistent by construction. The only change here is that `sensei serve` now logs its effective graph-marker-file path clearly, so an operator or script can pass the identical value to `sensei build --graph-marker-file`.

## 5. Descriptor lifecycle

- Written atomically (temp file + rename, mirroring `golang/seedmeta.WriteMarkerFile`).
- Read always re-verifies PID liveness; a dead-PID or corrupt descriptor is treated as absent and the stale file is deleted inline. This is the only approach that survives a killed/crashed process or a host reboot without a heavier IPC/registry mechanism.
- Writers additionally remove their own descriptor on graceful shutdown, as a tidy-`ls` courtesy — correctness never depends on that succeeding.
- PID liveness is platform-specific (signal-0 probe on Unix; query-limited handle + exit-code check on Windows), stdlib-only, matching the existing precedent in `cmd/awg/graph_publication_lock_unix.go`/`_windows.go`. Best-effort: a false "alive" from PID reuse after a reboot is an accepted residual risk, the same posture the existing publication lock already accepts for a local dev/CI tool.

## 6. Configuration and observability

No new tracked repository configuration is introduced. `sensei serve` gains one new stable, greppable log line printing the effective graph-marker-file path, so operators and scripts can capture and reuse it with `sensei build --graph-marker-file`.

## 7. Required proof

Add tests proving at least:

### Descriptor package

- write/read round-trips exactly;
- a descriptor naming a dead PID is treated as absent, and the stale file is removed;
- a corrupt/unparseable descriptor is treated as absent, and the stale file is removed;
- the descriptor path is a pure function of (kind, listen address), never of a checkout root;
- distinct kinds at the same address do not collide on the same path.

### Compatibility functions

- an occupied address with no descriptor is incompatible for both Oxigraph and awareness-graph checks;
- an exact match on every relevant field is compatible;
- a mismatch on any single field (data directory for Oxigraph; Oxigraph URL, marker file, repo-root, or repo-domain for awareness-graph) is incompatible, and the diagnostic names that field's running and requested values;
- the diagnostic names the occupied address and the owning PID.

### Two-checkout proof

Using real `oxigraph` and `awareness-graph` subprocesses configured as "checkout A" (its own repo-root, repo-domain, and graph-marker-file), a second invocation configured as "checkout B" against the same addresses must:

- exit non-zero;
- produce a diagnostic naming the occupied address, the owning PID, and both checkouts' marker paths;
- leave both checkouts' marker files byte-for-byte unchanged.

### Freshness-after-reuse

- after a scoped build against a store `sensei serve` legitimately owns, a subsequent metadata/preflight query against that same service reports the graph freshness state as current for the exact marker used.

## 8. Required evidence

The issue #118 implementation handoff must additionally report:

```text
Serve runtime compatibility:
- descriptor package unit tests:
- compatibility-function table tests (per field):
- two-checkout integration proof:
- freshness-after-reuse proof:
- manual dogfood against the live 127.0.0.1:7878/:10120 pair (incompatible reuse now fails visibly, marker untouched):
- manual dogfood with isolated ports/data (`--oxigraph-bind`/`--addr`/`--data`) (clean, isolated startup):
```

## 9. Non-goals

This contract must not:

- weaken freshness or mutation admission;
- treat stale awareness as advisory safety;
- replace repository identity with first-loaded domain or store row order;
- use destructive whole-store rebuild as a repair for one repository slice;
- add a new gRPC/proto field or wire change in this slice (the descriptor is file-based specifically so it covers the Oxigraph process, which can never speak a Sensei RPC);
- add a flag to bypass the compatibility check.

## 10. Stop conditions

Stop and post `ARCHITECT QUESTION` before coding around any of these:

- the machine-global descriptor location proves unwritable in a supported deployment target with no acceptable fallback location;
- PID reuse after reboot proves unacceptably likely in a supported environment, such that the liveness check cannot be trusted even as a best-effort signal;
- a supported platform has no feasible liveness probe at all (not merely an imperfect one).

## 11. Acceptance criteria

This contract is complete only when:

1. existing listeners are never reused solely because their ports respond;
2. reuse requires an explicit compatibility handshake or exact runtime binding match;
3. a checkout-local start can select isolated ports/store/marker paths without editing tracked repository configuration;
4. scoped build and the serving process consume the same explicit marker path;
5. after a successful scoped build, a restarted or dynamically refreshed service reports `Freshness state: current` for the exact marker;
6. a global service for checkout A cannot be silently reused as checkout B's local execution habitat;
7. failure leaves both stores and marker files unchanged except for explicit, already-verified publication;
8. diagnostics name the occupied address, owning process/service when observable, expected marker path, and actual marker path.
