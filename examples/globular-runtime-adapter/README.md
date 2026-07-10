# Globular runtime-evidence adapter (example)

This directory is the **first runtime adapter** for Sensei's runtime proof lane — and
a proof that the boundary holds: **Sensei validates and consumes a Globular adapter
without any Globular code, RPC, or protobuf in Sensei core.**

```
Globular services  →  this adapter  →  runtime-evidence/v1 snapshot  →  Sensei
(cluster_controller,   (maps services      (normalized lanes +            (validate now;
 node_agent,            to Sensei lanes)     freshness + authority)         diagnose/gate
 cluster_doctor,                                                            in later phases)
 workflow, metrics)
```

## Files

- **`globular-runtime-adapter.yaml`** — the `runtime-adapter/v1` manifest. Maps each
  generic Sensei lane to a real Globular surface (`source:` values are real MCP tools /
  their backing RPCs) with an authority level and freshness requirement. Sensei core
  validates this shape but never embeds these names.
- **`sidekick-quorum-snapshot.example.yaml`** — a worked `runtime-evidence/v1`
  snapshot for the canonical "sidekick is held by object-store quorum" scenario.
  Normalized evidence only — the shape Sensei consumes.

## Validate (Phase 1 validators, available now)

```bash
awg runtime-adapter validate  --manifest examples/globular-runtime-adapter/globular-runtime-adapter.yaml
awg runtime-snapshot validate --in       examples/globular-runtime-adapter/sidekick-quorum-snapshot.example.yaml
```

Both are kept valid in CI by `TestGlobularExampleAdapterValidates`.

## What is and isn't here

**Phase 2a (this change):** the *declarative* adapter — the lane→service mapping and a
worked, Sensei-validated snapshot. This is the contract and the proof of boundary.

**Phase 2b (not built):** the *live collector* — a program that calls the Globular
surfaces above (via MCP/gRPC), normalizes the responses into a snapshot, and stamps
real freshness/authority. It belongs **outside Sensei core** (in the Globular/services
side, where the clients and auth live), because importing Globular protobufs or RPCs
into Sensei core is a hard non-negotiable. It needs a live cluster + auth, so it is a
separate lane.

**Phases 3–6 (not built):** `awg cluster-diagnose` (typed verdicts like
`blocked_by_quorum`), `awg repair-report` (before/action/after), `awg gate`
(fail-closed on missing/stale/unauthorized evidence), and memory promotion. The
verdict *rules* (stale-can-diagnose-not-validate-repair, unknown-must-not-green,
missing-owner-blocks) live there — deliberately not in a schema validator.

## North star

Sensei should be able to say *"the cluster is blocked because quorum is missing; the
evidence is fresh; the owners are valid; a force-install is forbidden; restore quorum
and re-check"* — **without knowing the platform is Globular.** This adapter is how
Globular plugs into that, as data, not as a dependency.
