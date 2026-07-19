# Sensei API Reference — gRPC service + MCP bridge

This is the authoritative reference for the **wire surface** of Sensei: the
`AwarenessGraph` gRPC service defined in
[`proto/awareness_graph.proto`](../proto/awareness_graph.proto), and the
`awareness-mcp` stdio bridge that re-exposes it as MCP tools.

For the command-line surface, see [cli-reference.md](./cli-reference.md). For
how an agent should *use* these calls in practice, see
[agent-usage.md](./agent-usage.md).

---

## How the pieces connect

```
docs/awareness/*.yaml ──build──▶ Oxigraph (SPARQL store) ──serve──▶ AwarenessGraph gRPC (:10120)
                                                                          │
                                            ┌─────────────────────────────┤
                                            │                             │
                                      sensei CLI (gRPC client)        awareness-mcp (stdio↔gRPC)
                                                                          │
                                                                    MCP-capable agent
```

- The **gRPC service** is the single source of truth. Every reader (the CLI,
  the MCP bridge, a CI step, the editor extension) is a thin client of it.
- **Oxigraph** holds the compiled graph. The service queries it with bounded,
  templated SPARQL; **no caller ever sends raw SPARQL** (see
  [Query](#query) and [Design rules](#design-rules)).
- The **MCP bridge** (`cmd/awareness-mcp`) is optional. It translates MCP
  `tools/call` requests into gRPC calls and formats responses for an agent.

---

## Design rules

These constraints are baked into the proto and the handlers; clients should
rely on them:

1. **Briefing is the primary consumer surface.** One prose call instead of
   dozens of lookups. `Impact` returns the same data as typed nodes.
2. **No arbitrary SPARQL is exposed.** `Query` is a *typed, whitelisted*
   browse (`BY_FILE | BY_ID | BY_CLASS | RELATED`). The proto reserves the
   old `sparql`/`accept` fields so they cannot be reintroduced on the wire.
3. **Domain scoping fails closed.** On a graph that hosts more than one repo
   domain, a call with an empty `domain` returns nothing rather than mixing
   domains. A single-domain graph (or one holding only shared meta-principles)
   resolves an empty domain trivially. See `golang/server/scope.go`.
4. **Status is explicit.** `OK` / `EMPTY` / `DEGRADED` are distinct so a
   caller can tell "you forgot to anchor this file" from "the backend is
   down." `EMPTY` is never silently upgraded to "safe."
5. **Provenance travels with every node.** A `CodeAnchor` says whether a fact
   came from a YAML file or an `@awareness` annotation in code (file + symbol +
   line range).

---

## Service: `AwarenessGraph`

Nine RPCs. Default listen address `:10120` (gRPC). In standalone mode the
transport is plaintext gRPC; under Globular it is cluster mTLS with etcd
service discovery.

| RPC | Shape | Audience | Server needed |
|---|---|---|---|
| [`Briefing`](#briefing) | prose | agents (primary) | yes |
| [`Impact`](#impact) | typed nodes | tools, review, guards | yes |
| [`Preflight`](#preflight) | risk class + actions | agents (pre-edit branch) | yes |
| [`EditCheck`](#editcheck) | advisory warnings | edit-time guard | yes |
| [`Resolve`](#resolve) | one node | follow a referenced id | yes |
| [`Query`](#query) | typed rows | operator/debug browse | yes |
| [`Metadata`](#metadata) | graph-level counts | session bootstrap | yes |
| [`Propose`](#propose) | review-queue candidate | durable agent feedback | yes |
| `ReferenceSites` | inbound code-symbol references | completeness tooling | yes |

---

### Briefing

Composes a ~500-token prose briefing for editing a file or tackling a task.
The single entry point an agent should call before significant changes.
Composed from the same matching engine that backs `Impact` — the difference is
presentation, not data.

**`BriefingRequest`**

| Field | Type | Notes |
|---|---|---|
| `file` | string | repo-relative path, e.g. `golang/foo/bar.go`. Set `file` **xor** `task`. |
| `task` | string | natural-language task, used as a keyword anchor. |
| `depth` | string | `agent_compact` \| `compact` \| `standard` \| `deep`. `agent_compact` is the narrowest agent-safe payload. |
| `domain` | string | repo/domain scope. Required (fails closed) on a multi-domain graph. A repo value selects that domain plus shared meta-principles. |

**`BriefingResponse`**

| Field | Type | Notes |
|---|---|---|
| `prose` | string | the narration — ready to display or feed back as context. |
| `generated_in_ms` | int64 | Oxigraph latency budget for the call (not a token count). |
| `referenced_ids` | string[] | class-qualified ids (e.g. `invariant:reconcile.foo`, `failure_mode:service.bar`) — pass straight to `Resolve`. |
| `status` | `BriefingStatus` | `OK` / `EMPTY` / `DEGRADED`. |
| `implementation_patterns` | `MatchedImplementationPattern`[] | ≤3, strongest first. Empty when nothing matches — never padded. |

**`BriefingStatus`**

| Value | Meaning | Action |
|---|---|---|
| `BRIEFING_STATUS_OK` (0) | anchors found; prose composed normally | treat as active constraints |
| `BRIEFING_STATUS_EMPTY` (1) | zero direct or inferred anchors | author awareness here, or treat as low-coverage — **not** "safe" |
| `BRIEFING_STATUS_DEGRADED` (2) | backend partially unavailable; prose is best-effort | retry, or fall back to reading local YAML |

---

### Impact

The structured equivalent of `Briefing`: the same data as typed
`KnowledgeNode`s rather than prose. Used by code-review tools, the pre-commit
guard, and anything that needs to chain on the data.

**`ImpactRequest`**: `file` (string), `domain` (string, same semantics as
`BriefingRequest.domain`).

**`ImpactResponse`** — `KnowledgeNode` lists, partitioned **Direct** (the file
is the explicit subject of the node) vs **Inferred** (reached via
package/symbol/service walks):

| Field | Partition |
|---|---|
| `direct_invariants`, `direct_failure_modes`, `direct_incident_patterns` | direct |
| `inferred_invariants`, `inferred_failure_modes`, `inferred_incident_patterns` | inferred |
| `required_tests`, `forbidden_fixes` | flat lists |
| `direct_intents`, `inferred_intents` | intent partition |
| `direct_architecture` | spine + design-pattern nodes (Component / Boundary / Contract / Decision / Evidence / DesignPattern / ImplementationPattern / PatternMisuse) — one flat list keyed by each node's `class` |

---

### Preflight

The agent's pre-edit *decision-support* call. Given a task and optional file
list, returns a single `risk_class` to branch on, plus action-oriented lists
assembled from anchored facts (never invented). Reuses Briefing's matching
engine and adds risk classification on top.

**`PreflightRequest`**

| Field | Type | Notes |
|---|---|---|
| `task` | string | matched against `aw:activationTrigger` literals |
| `files` | string[] | files the agent intends to touch; each runs the same Impact query |
| `mode` | `PreflightMode` | `PREFLIGHT_COMPACT` (default) or `PREFLIGHT_STANDARD` |
| `domain` | string | scope, passed through to the per-file Impact query |

**`PreflightMode`**: `PREFLIGHT_COMPACT` (top 3 invariants / 2 failure_modes /
1 pattern; ≤5 action entries) · `PREFLIGHT_STANDARD` (top 7 / 5 / 3; ≤10).

**`PreflightResponse`**

| Field | Type | Notes |
|---|---|---|
| `status` | `PreflightStatus` | `OK` / `EMPTY` / `DEGRADED` |
| `risk_class` | `RiskClass` | the branch input — see below |
| `confidence` | `Confidence` | `HIGH` / `MEDIUM` / `LOW` |
| `direct_invariants`, `direct_failure_modes`, `direct_intents`, `direct_forbidden_fixes`, `direct_required_tests` | `KnowledgeNode`[] | reuses `collectImpact` per file |
| `implementation_patterns` | `MatchedImplementationPattern`[] | matched recipes |
| `required_actions`, `forbidden_fixes`, `tests_to_run`, `files_to_read` | string[] | bounded by `mode` |
| `blind_spots` | string[] | gaps the classifier itself noted (e.g. `files_not_indexed: 2`) |
| `coverage` | `CoverageSummary` | `direct_anchor_count`, `file_count`, `indexed_file_count`, `sufficient` (load-bearing), `note` |
| `direct_architecture` | `KnowledgeNode`[] | spine + pattern nodes governing the files |
| `generated_in_ms` | int64 | |

**`RiskClass`** — branch on this:

| Value | Meaning |
|---|---|
| `LOW_RISK` | proceed; action list is short or empty |
| `ARCHITECTURE_SENSITIVE` | read `files_to_read`; brief every file in the edit set |
| `CONVERGENCE_RISK` | touches the desired/installed/runtime/repository truth model |
| `SECURITY_RISK` | touches auth/tokens/PKI/RBAC/secret keys — get user approval |
| `DATA_LOSS_RISK` | touches install/wipe/rollback/migration — get user approval |
| `UNKNOWN_IMPACT` | the graph cannot classify (degraded or sparse) — behave as SECURITY_RISK until proven otherwise |

**`PreflightStatus`**: `OK` (≥1 direct anchor or ≥1 pattern) · `EMPTY` (zero
anchors **and** zero patterns **and** coverage sufficient) · `DEGRADED` (store
unavailable; best-effort with `UNKNOWN_IMPACT`).

**`Confidence`**: `HIGH` (≥3 direct anchors AND coverage sufficient) ·
`MEDIUM` (1–2 direct anchors OR strong pattern) · `LOW` (anything else,
including degraded).

> **Critical rule:** `EMPTY` is **not** `LOW_RISK`. The server only collapses
> to `EMPTY` when coverage is explicitly sufficient. If the graph does not
> know the area, it returns `OK` with `risk_class=UNKNOWN_IMPACT` and populated
> `blind_spots`. Always read **both** `status` and `risk_class`.

Store unavailable degrades to `PREFLIGHT_STATUS_DEGRADED` /
`UNKNOWN_IMPACT` / `blind_spots="awareness_store_unavailable"` rather than
hard-failing; `codes.Unavailable` only if no response can be built at all.

---

### EditCheck

Evaluates a proposed edit's **content** against the active repo-scoped advisory
rules (`detect` blocks) that apply to the file. **Warning-only**: it never
blocks, never modifies code. Returns warnings naming the rule a bad-shape edit
would violate (e.g. replacing `dispenser.Errf` with `fmt.Errorf` in a Caddy
unmarshaler). Domain scoping is identical to Briefing/Impact.

**`EditCheckRequest`**: `file` (string, repo-relative), `proposed_content`
(string — full file or edited region; the MCP bridge accepts up to 8 MB),
`domain` (string).

**`EditCheckResponse`**

| Field | Type | Notes |
|---|---|---|
| `warnings` | `EditWarning`[] | empty = nothing tripped |
| `rules_evaluated` | int32 | how many in-scope rules carried a `detect` block — distinguishes "checked, clean" from "nothing to check" |
| `generated_in_ms` | int64 | |

**`EditWarning`**: `rule_id` (bare id) · `class` (`Invariant` \| `ForbiddenFix`)
· `severity` (always `warning` in v1) · `message` · `detail` (which pattern
tripped) · `provenance` (one-line: repo · origin · review · bundle · range ·
cites) · `enforcement` (`warn` default, or `block` for a would-block under a
hard gate; EditCheck itself stays advisory regardless).

---

### Resolve

Fetches a single node by its annotation id (e.g. after reading a
`// @invariant: X` comment, or expanding a `referenced_id` from a briefing).

**`ResolveRequest`**

| Field | Type | Notes |
|---|---|---|
| `id` | string | bare node id, **no** class prefix (e.g. `reconcile.foo`, `pat.bar`) |
| `class` | string | **required** — names the awareness class. snake_case, case-insensitive. |
| `domain` | string | optional scope; a node outside the scope resolves to `found=false` |

Accepted `class` values (authoritative whitelist is `resolveIRIForClassAndID`
in `golang/server/resolve.go`): `invariant`, `failure_mode`,
`incident_pattern`, `intent`, `source_file`, `symbol`, `code_symbol`,
`forbidden_fix`, `test`, `meta_principle`, `component`, `boundary`, `contract`,
`decision`, `evidence`, `proof_obligation`, `proof_slot`, `design_pattern`,
`implementation_pattern`, `pattern_misuse`. A class outside the list returns
`InvalidArgument` rather than guessing. `meta_principle` resolves the dual-typed
`meta.*` invariant node. These are the same tokens `QueryRow.class` returns, so
a class-qualified id like `incident_pattern:pat.foo` splits directly into
`(class, id)`.

**`ResolveResponse`**: `node` (`KnowledgeNode`, set when `found`) · `found`
(bool).

---

### Query

Typed, constrained browse of the graph — **not** raw SPARQL. RBAC-gated to the
awareness.admin role under Globular; intended for operator/debug workflows.

**`QueryRequest`**

| Field | Type | Notes |
|---|---|---|
| `mode` | `QueryMode` | required |
| `file` | string | required for `BY_FILE` |
| `id` | string | class-qualified; required for `BY_ID` and `RELATED` |
| `class` | `QueryClass` | required for `BY_CLASS` |
| `limit` | int32 | optional; server enforces bounds |
| `domain` | string | optional repo/domain scope for `BY_CLASS` / `BY_FILE`; empty follows server scope rules |

(The old `sparql` and `accept` fields are **reserved** — they cannot appear on
the wire.)

**`QueryMode`**: `BY_FILE` (nodes whose anchor names the file) · `BY_ID` (the
one node) · `BY_CLASS` (all nodes of a class, use `limit`) · `RELATED` (nodes
the id points at).

**`QueryClass`**: `INVARIANT`, `FAILURE_MODE`, `INCIDENT_PATTERN`, `INTENT`,
`SYMBOL`, `SOURCE_FILE`, `CODE_SYMBOL`, `FORBIDDEN_FIX`, `TEST`,
`META_PRINCIPLE`, `COMPONENT`, `BOUNDARY`, `CONTRACT`, `DECISION`, `EVIDENCE`,
`DESIGN_PATTERN`, `IMPLEMENTATION_PATTERN`, `PATTERN_MISUSE`,
`ARCHITECTURE_CLAIM`, `OPEN_QUESTION`, `ARCHITECT_ANSWER`.

`ARCHITECTURE_CLAIM`, `OPEN_QUESTION`, and `ARCHITECT_ANSWER` are
non-authoritative and explicit-query-only. An OpenQuestion records uncertainty,
not truth or closure. An ArchitectAnswer records exact human input, not observed
behavior or governed architecture; `accepted_for_question` only resolves the
question artifact and does not promote architecture. Evidence pointers are
unverified literals until converted to Evidence.

**`QueryResponse`**: `rows` (`QueryRow`[]) · `generated_in_ms`. Each `QueryRow`
carries `id`, `class`, `label`, `severity`, `status`, `relation` (set in
related mode), `source_file`, and the optional UML triple `uml_kind` /
`uml_stereotype` / `uml_view`.

---

### Metadata

Graph-level coverage and freshness — **not** per-file context. Agents call it
once at session start to disambiguate an `EMPTY` briefing: "graph well-covered,
no rule here" vs "graph thin everywhere, empty means nothing." Cheap (bounded
SPARQL).

**`MetadataRequest`**: optional `domain` scope for per-class counts. Empty
returns graph-wide totals.

**`MetadataResponse`** (selected fields):

*Build provenance* (set via ldflags at build time):
`graph_build_commit`, `graph_build_time_unix`, `source_repo_commit`,
`server_version` (`0.0.0-dev` = no ldflags).

*Live counts*: `triple_count`, `invariant_count`, `failure_mode_count`,
`incident_pattern_count`, `intent_count`, `forbidden_fix_count`,
`required_test_count`, `source_file_count`, `code_symbol_count`.

*Architectural-spine counts*: `meta_principle_count` (also counted in
`invariant_count`), `component_count`, `boundary_count`, `contract_count`,
`decision_count`, `evidence_count`, `design_pattern_count`,
`implementation_pattern_count`, `pattern_misuse_count`.

*Server runtime*: `server_started_unix`, `generated_in_ms`.

*Usage counters* (reset on restart): `briefing_call_count`,
`briefing_agent_compact_count`, `resolve_call_count`, `resolve_found_count`,
`resolve_miss_count`.

*Embedded-seed / live-store alignment*: `embedded_seed_digest_sha256`,
`embedded_seed_marker_iri`, `live_store_contains_embedded_seed_marker`.

*Server-side verdicts* (interpret these rather than the raw fields):
- `build_provenance_state` — `STAMPED` / `DEV` / `INCOMPLETE`
- `coverage_state` — `EMPTY` / `THIN` / `SUFFICIENT`
- `seed_state` — `CURRENT` / `STALE` / `UNSTAMPED`

*Optional local summaries* (present when the server detects them under the
project root): `candidate_queue_state` + `local_candidate_file_count` /
`local_candidate_entry_count` (from `docs/awareness/candidates/`);
`benchmark_state` + `benchmark_contract_count` / `benchmark_learning_event_count`
/ `benchmark_latest_*` (from `eval/multi-swe-bench/`).

---

### Propose

Submits one typed feedback entry to the review queue. It is the agent write
path, but it is a safe write: it validates the proposal and writes a candidate
under `docs/awareness/candidates/`; it does not mutate the live graph and does
not promote candidate knowledge into active authority.

**`ProposeRequest`** selected fields: `kind` (`failure_mode`, `invariant`,
`required_test`, `forbidden_fix`, `contract_unknown`), `id`, `title`,
`description`, `severity`, `source_files[]`, `related_invariants[]`,
`related_failures[]`, `required_tests[]`, `forbidden_fixes[]`, `evidence[]`,
`repo`, `domain`, `contract`, `proposed_contract`, `revision_request`.

**`ProposeResponse`**: `status`, `candidate_path`, `node_ids[]`,
`validation_errors[]`, `note`, `generated_in_ms`.

Servers without a configured candidates directory return Unavailable. Treat that
as unavailable feedback, not as "no lesson needed."

---

## Shared message: `KnowledgeNode`

Returned by `Impact`, `Preflight`, and `Resolve`.

| Field | Notes |
|---|---|
| `iri` | full IRI, e.g. `<https://globular.io/awareness#invariant/reconcile.foo>` |
| `id` | short id, e.g. `reconcile.foo` |
| `class` | `Invariant` \| `FailureMode` \| … |
| `label` | rdfs:label (human title) |
| `severity` | `critical` \| `high` \| `warning` \| `info` \| `degraded` (if set) |
| `status` | `active` \| `planned` \| … (if set) |
| `anchor` | `CodeAnchor` — where the triple set was authored |
| `description` | rdfs:comment long-form note |
| `related_ids` | class-qualified ids this node points at via object edges |
| `uml_kind` / `uml_stereotype` / `uml_view` | optional UML profile classification (metadata, not authority) |
| `facts` | `NodeFact`[] — literal-valued rules-about-code (e.g. `requiresCall`, `forbidsCall`, `mustFollow`, `activationTrigger`) |

**`CodeAnchor`**: `source_yaml` (relative path if YAML-authored) · `file` (if
`@`-annotated in code) · `symbol` (function/method when symbol-scoped) ·
`line_start` / `line_end` (1-based).

**`NodeFact`**: `predicate` (short property name) · `value` (literal; never an
IRI — IRI-valued edges live in `related_ids`).

**`MatchedImplementationPattern`** (in Briefing/Preflight): `id`, `label`,
`match_strength` (`strong` > `medium` > `narrow`), `match_reason[]`,
`reference_files[]` (as `role:path`), `must_follow[]`, `required_calls[]`,
`forbidden_calls[]`, `rationale_summary`. Read as guidance for the *shape* of
code you are about to write — not as a runtime fact.

---

## MCP bridge (`awareness-mcp`)

A standalone process that speaks **JSON-RPC 2.0 over stdio** with
MCP-compatible `Content-Length` framing to an MCP-capable
client and forwards each call to the gRPC service. Point your agent's MCP
config at it (see [mcp-config.md](./mcp-config.md)).

**Flags**

| Flag | Default | Purpose |
|---|---|---|
| `-awareness-addr` | `localhost:10120` | gRPC address of the awareness-graph backend |
| `-timeout` | `5s` | per-request gRPC timeout |

The standalone bridge connects with **plaintext gRPC** (no mTLS). MCP protocol:
`initialize` handshake → `tools/list` → `tools/call` (`name` + `arguments`).
JSON-RPC notifications (no `id`) are ignored per spec.

**Tools** — safe agent-facing tools; arguments mirror the request messages:

| MCP tool | gRPC RPC | Required args | Optional args |
|---|---|---|---|
| `awareness_briefing` | `Briefing` | `file` **or** `task` | `depth` (default `agent_compact`), `domain` |
| `awareness_impact` | `Impact` | `file` | `domain` |
| `awareness_preflight` | `Preflight` | `task` | `files[]`, `mode` (`compact`/`standard`), `domain` |
| `awareness_edit_check` | `EditCheck` | `file`, `proposed_content` | `domain` |
| `awareness_resolve` | `Resolve` | `class`, `id` | `domain` |
| `awareness_query` | `Query` | `mode` | `file`/`id`/`class` (per mode), `limit`, `domain` |
| `awareness_metadata` | `Metadata` | — | `domain` |
| `awareness_propose` | `Propose` | `kind` | `title`, `contract`, `evidence[]`, `source_files[]`, related ids |
| `task_status` | local task artifacts | `repo` | `task`, `detail` (`compact`/`full`) |
| `advance_task` | local task artifacts | `repo` | `task`, `observed_at`, `max_probes` |
| `task_briefing` | local task artifacts | `repo`, `file` | `task` |
| `admit_change` | local convergence bundle | `bundle_dir`, `request_path`, `graph_nt`, `repo` | `policy`, `detail` |
| `verify_admission` | local admission bundle | `decision_path`, `bundle_dir`, `repo` | `detail` |

The task tools do not call the gRPC service. They use the same typed local
packages as the CLI. `advance_task` is the only state-changing task tool and is
restricted to the closed static-read probe registry plus one convergence
iteration.

The bridge enforces a **safe-tools-only whitelist** in `callTool()` and
validates `awareness_query`'s `mode`/`class` against fixed enums — there is no
path to send raw SPARQL through MCP.

> **Standalone vs Globular.** The standalone bridge above exposes
> `awareness_query` (typed only). Under Globular, the platform MCP server
> withholds `awareness_query` from agents by default (operator/debug, RBAC-
> gated), connects via etcd discovery + cluster mTLS, and adds a composite
> `awareness_diagnose` tool that correlates a briefing with live
> `cluster_doctor` / `cluster_controller` findings. `awareness_diagnose` is
> **not** part of this repo's gRPC service or standalone bridge.

---

## Generating the bindings

Go bindings live under `golang/pb/` (`import awarenesspb
"github.com/globulario/sensei/golang/pb"`). Regenerate after editing
the proto:

```bash
make proto
```
