# Pilot candidates: `github.com/etcd-io/etcd` (persisted analysis, not promoted)

This directory persists the **etcd cold-source candidates** as committed
artifacts. Until now they existed only as an ai-memory analysis of the prior
etcd dry-run transcript; Phase E writes them down so they survive outside a
conversation.

## What this is — and is NOT

- **Is:** 4 reviewed candidate rules under `candidates/etcd_candidates.yaml`,
  `status: candidate`, domain-scoped to `github.com/etcd-io/etcd`.
- **Is NOT:** promoted. Nothing here is in any graph. The importer skips any
  `candidates/` directory, and `pilot/` lives outside the seed-scanned paths, so
  these never compile into `awareness.nt`.

## Provenance honesty (important)

Unlike `pilot/caddy` — whose citations are **real PR-comment ids** from a
committed fixture, cage-verified — the etcd candidates here are
**transcript-derived**:

- They come from the prior etcd cold-source **live-LLM dry-run** and its scoring
  transcript (the etcd run was `--gh etcd-io/etcd`, which never persisted
  candidates).
- The evidence is **descriptive** (file/symbol/bug as analysed). The specific
  commit SHAs / PR-comment ids were **not independently re-verified** in this
  pass, so `provenance.citations` are marked unverified and `commit_range` is
  "unknown".

**Consequence:** these must NOT be promoted as-is. Promotion requires
re-deriving and verifying real citations against the etcd repo first — the
citation cage rejects uncited/fabricated drafts by design.

## The candidates

| id | class | maps to | label |
|----|-------|---------|-------|
| `…proto_struct_with_mutex_must_not_be_copied` | forbidden_fix | `meta.code.identity_bound_state_must_not_be_copied` | load-bearing |
| `…raft_message_must_be_copied_before_shared_mutation` | failure_mode | `meta.code.identity_bound_state_must_not_be_copied` | load-bearing |
| `…client.v3.dial_auth_timeout_contract_is_fragile` | failure_mode | connection_errors / authz-snapshot / timeout | borderline (compound — split on promotion) |
| `…tests.integration.v3_grpc…assert_real_request_path` | invariant | end_to_end_check / half_done_must_not_look_done | borderline |

The first two are the etcd half of the cross-language corroboration behind the
now-committed `meta.code.identity_bound_state_must_not_be_copied` (the vite half
is in the live-validation analysis).

## Non-goals

No promotion · no graph mutation · no detect rules · no enforcement · no
re-run/LLM/key in this pass (authored from the existing analysis). See
`pilot/caddy/` for the verified-citation reference shape.
