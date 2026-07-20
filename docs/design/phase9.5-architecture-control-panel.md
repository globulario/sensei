# Phase 9.5 — Ontology-Aligned Architectural Control Panel (opening contract)

> **Status: Phase 9.5 opened design-first after the Phase 9.6 merge; opening under review, this
> revision applies the opening-review repair.** DESIGN ONLY: no production Go, protobuf,
> generated artifacts, VS Code implementation, governed YAML, tests, or checkpoint
> implementation. Phase 9.5 checkpoints begin only when separately reviewed and unlocked. Phase
> 6 remains the sole correctness certifier; `CorrectnessCertified` stays false.

Base: clean `main` at `15bdc2f`. Prerequisite (met): Phase 9.6 merged at `2ef5884`
(`BriefingResponse.feedback` field 7, `TaskBriefing.FeedbackProjection`, the canonical
`briefingfeedback` owner). Supersedes the saved plan
(`docs/design/phase9.5-architecture-control-panel-plan.md`, branch
`phase9.5-architecture-control-panel-plan`).

The accepted opening kept its product direction, one-screen information architecture, simplicity
rules, question lifecycle, state distinctions, six-checkpoint sequence, and explicit exclusions.
This repair corrects the **semantic ownership and artifact-closure model** before Checkpoint 1.

---

## 1. Product objective (accepted)

Make Sensei's VS Code extension the **primary architectural control panel** — a simple,
single-screen view answering five questions immediately:

1. What is the state of this architecture?
2. What is the state of this selected architectural artifact?
3. What requires the architect's attention now?
4. Which Sensei questions require an architect answer?
5. What is the next permitted action?

It must display the following **without conflating them**: graph authority; repository
architecture posture; artifact lifecycle; artifact closure; task closure; task completion;
correctness certification; briefing-feedback availability and lineage. It must remain useful to
someone who does not yet understand the complete Sensei ontology.

("Object" remains an acceptable conversational UI word; the *schema* term is **artifact** — an
explicitly eligible architectural graph entity, §5.)

## 2. Core product law (accepted)

```text
Sensei owners classify.
VS Code explains.
Guarded owners mutate.
VS Code delegates.
```

The editor must NEVER: infer closure from edge counts; decide which closure dimensions apply;
manufacture severity; derive warnings from colors/labels/graph shape; reinterpret Phase 9.6
feedback; treat graph adjacency as authority; promote architect answers directly; complete
tasks; certify correctness; write governed YAML; or hide unknown/degraded state behind a green
display. **Every visible state originates from a typed projection or typed owner outcome** — no
client-side closure, severity, applicability, or adjudication.

## 3. State distinctions that must remain visible (accepted)

Graph authority · repository architecture posture · artifact lifecycle · artifact closure · task
closure · task completion · correctness certification (Phase 6 only) · briefing-feedback
availability+lineage. These never collapse into one "done"/"OK" signal. **No misleading single
percentage** — typed counts and conditions only; a score may be added later only through a
separately reviewed, owner-defined projection.

## 4. Grounding against the current implementation

- **VS Code extension** (`editor/vscode/`): webview cockpit (`src/dashboardPanel.ts` host +
  `media/dashboard.js` client, no framework) + sidebar tree (`src/awarenessProvider.ts`). The
  gRPC client (`src/grpcClient.ts`) wires **only** `Preflight`, `Metadata`, `Query`, `Resolve`;
  there is **no `Briefing` call**. `BriefingResponse.feedback` (field 7) and
  `TaskBriefing.FeedbackProjection` are **not consumed** by any TS/JS.
- **Hardcoded taxonomy**: `media/dashboard.js` `ASPECTS` (flat, with a `group` label) + duplicate
  class tables (`CLASS_COLOR`, `SEV_COLOR`, `DIR`) + a second copy in `dashboardPanel.ts`
  (`RESOLVABLE`, `PROMOTE_TARGET`) — class membership + capabilities hardcoded in ≥2 files.
- **Generic detail renderer** `renderDetail(m)`: one renderer over the `Resolve`
  `KnowledgeNode` — it shows **graph facts, not owner-projected artifact state**.
- **Attention/warnings are computed CLIENT-SIDE today** (banner warn-span, tree degraded rows,
  risk icons, control-warn blocks) — precisely the derivation Phase 9.5 moves behind a typed
  owner.
- **Existing "Closure & Control" cockpit** is **CLI/file-sourced** (`src/taskSession.ts` +
  `sensei task-status`), not projection-sourced.
- **Owner substrate that exists** (§6): `closure.Report`/`DimensionAssessment` (a **task/scope**
  closure owner — see §6), `completion.CompletionProjection`, the dialogue owners
  (`questiongen`/`questiondisposition`/`questionpromotion`/`questionresolution`),
  `briefingfeedback.Projection`, `GraphAuthority`+`seedmeta`, `coverage.Report`. **What does not
  exist and Checkpoint 1 introduces**: a canonical composition owner, per-artifact assessment, a
  unified attention/severity owner, an authored ontology descriptor, and lifecycle/availability
  models.

## 5. Semantic ownership — the canonical architectural-state owner

The Phase 9.5 read models are **not** "server-owned." They are owned by a **canonical
architectural-state projection owner** — a proposed package boundary equivalent to:

```text
golang/architecture/controlstate
```

(equivalent naming acceptable if it clearly describes architectural-state *projection ownership*,
not UI rendering). It is the **sole semantic composition owner** for Phase 9.5 read models. The
dependency direction is strict:

```text
existing domain owners
        ↓
golang/architecture/controlstate        (composes + classifies: closure, lifecycle,
        ↓                                 severity, applicability, artifact state, posture)
server RPC + protobuf adapters            (thin transport: exposes, never composes)
        ↓
VS Code rendering                         (explains: styling + layout only)
```

**The server remains a thin transport consumer.** The server must NOT assign closure, assign
severity, decide lifecycle, decide dimension applicability, compose attention policy
independently, invent artifact state, or derive repository posture. Every projection in §7 is
**owned by `controlstate` and exposed by the server** (never "server-owned"). Below `controlstate`
sit `golang/server`, protobuf adapters, VS Code, and CLI rendering — all consumers.

## 6. Artifact identity and the eligibility/assessment registry

### 6.1 `closure.Report` is a task/scope owner, not a per-artifact owner (frozen)
`closure.Report` assesses a **bounded request** (task identity, binding, files, components,
claims, propositions, scoped inputs). Therefore:
- it may contribute **typed evidence** to artifact state, and may describe **task closure**
  separately;
- its aggregate `Verdict` must **not** be copied onto an arbitrary graph artifact;
- its `DimensionAssessment` list must **not** be treated as universally applicable to contracts,
  invariants, components, decisions, tests, or other classes;
- **task closure and artifact closure remain independent projections.**

Phase 9.5 therefore requires a **new per-artifact assessment composition owned by
`controlstate`** — it is not `closure.Report` renamed.

### 6.2 Artifact identity (frozen)
An **artifact** is an explicitly eligible architectural graph entity — not an arbitrary RDF node.
Identity is:
- exact node IRI;
- exact canonical class;
- exact repository/domain identity;
- graph authority identity;
- source/provenance identity where available.

No label, graph position, edge count, or client-selected class establishes artifact identity.

### 6.3 Artifact eligibility & assessment registry (Checkpoint 1)
An owner-side typed registry describes, per supported class:
- canonical class identity; display label; ontology family;
- whether the class is an **assessable artifact**;
- which assessment owner / rule family applies;
- which closure dimensions **can** apply;
- lifecycle source owner;
- supported evidence types; supported question types; resolver capabilities;
- whether the class may appear in the architecture overview.

**Unknown classes never disappear.** They appear under `unclassified` with assessment coverage
`unsupported`/`unknown`, artifact closure `unknown`, and an explicit limitation — **never**
defaulting to `closed` or `not_applicable`.

## 7. Canonical read models (owned by `controlstate`, exposed by the server)

Five typed projections, each deterministic, non-authoritative
(`non_authoritative_projection: true`), digest-bearing, with a closed vocabulary, a validator,
explicit limitations, and **an explicit availability** (§9). No client computes their fields.

1. **`architecture.control_snapshot/v1`** — the one-screen overview: repository+domain identity;
   graph authority+freshness; digest+provenance; artifact counts by class; artifact-closure
   distribution; critical/warning/degraded/unknown attention counts; open-question count;
   unresolved-contradiction count; missing-evidence count; coverage+blind-spot state; active-task
   summary; task-closure summary; completion summary; **feedback capability/availability**
   (not a repo-wide feedback dump — §12); top bounded attention items; per-source availability
   (§9); limitations. It must NOT declare repository correctness or completion.
2. **`architecture.artifact_index/v1`** — a paginated, deterministic list of artifact summaries
   powering navigation + "all objects" (§8).
3. **`architecture.artifact_state/v1`** — one exact selected artifact: owner-derived applicable
   dimensions, lifecycle (§10), attention, questions, evidence, promoted-feedback lineage
   (exact-scope, §12), next permitted action, availability, digest.
4. **`architecture.attention_item/v1`** — the shared canonical attention record (§11).
5. **`ontology.navigation_descriptor/v1`** — the authored ontology families/classes/capabilities
   (§8.2), MANDATORY.

## 8. Artifact index + mandatory navigation descriptor

### 8.1 `architecture.artifact_index/v1` (pagination, frozen)
`control_snapshot` holds summary counts; `artifact_state` describes one artifact; neither can
efficiently power "All objects" with closure badges. The index is a paginated list of artifact
summaries: exact artifact identity; class; label; lifecycle summary; closure state;
open-dimension count; highest attention severity; owner summary; assessment coverage; attention
count. Requirements: **stable deterministic ordering; bounded page size; visible truncation; a
stable cursor bound to repository/domain identity + the snapshot digest; no N+1 client
reconstruction; no client joins across Query/Resolve results.**

### 8.2 `ontology.navigation_descriptor/v1` (mandatory, frozen)
Justified: the extension duplicates class/capability tables in ≥2 files and the ontology evolves.
It is produced from an **authored canonical registry (§6.3), not inferred** from whichever classes
happen to exist in the current graph. It contains at least: schema + digest; family identity;
family label + order; class identity; class label; class order; assessable-artifact flag; query
capability; resolve capability; inspector capability; question capability; default visibility;
unknown-class fallback policy. **The client owns visual styling + responsive layout only — never
class membership, capabilities, ontology grouping, or semantic visibility.**

## 9. Projection availability + source availability (frozen)

Every Phase 9.5 projection carries an explicit availability:

```text
available | partial | unavailable | invalid
```

plus **typed per-source statuses** for each composed owner. A missing source must NEVER become
zero open questions, zero warnings, zero contradictions, zero blind spots, closed architecture,
or an empty attention queue. **Zero is data only when the relevant owner was successfully
observed;** otherwise the state is `partial`/`unavailable` with a reason code.

## 10. Artifact-closure aggregation (frozen)

States: `closed | open | degraded | unknown | not_applicable`. **Do not map
`closure.Report.Verdict` directly onto artifact closure.** Frozen meanings:

- **closed** — the class is explicitly assessable; the graph authority the assessment requires is
  current; every *applicable required* dimension is satisfied; no relevant contradiction remains;
  all required owner inputs were available.
- **open** — at least one applicable required dimension has a definitive unsatisfied condition or
  blocker.
- **degraded** — a usable assessment exists, but one or more relevant sources/evidence
  channels/non-blocking dimensions are degraded. Degraded must NOT conceal a definitive open
  blocker: when a definitive blocker exists, aggregate closure stays **open** and the degradation
  is recorded separately in projection/source availability.
- **unknown** — authority, applicability, evidence, or required owner input is insufficient to
  decide closure safely.
- **not_applicable** — the canonical artifact policy explicitly states closure assessment does not
  apply to this class/artifact. **Absence of rules is not `not_applicable`** (that is `unknown`).

## 11. Per-class dimensions — an explicit matrix, not a universal list (frozen)

The dimension examples are illustrative only. Checkpoint 1 publishes an **explicit
supported-class matrix**. For each initially supported artifact class it records: applicable
dimension identities; the **source owner for each dimension**; satisfaction rule; open rule;
unknown rule; evidence identities; blocker identities; question linkage; next-action owner.
Classes without a reviewed policy remain `unknown`/`unsupported`. **No generic "7 dimensions for
every node" model.** Each dimension is typed:

```text
dimension identity · label · applicable(owner-decided) · state
  (satisfied|open|degraded|unknown|not_applicable) · reason code · blockers · evidence ·
  associated questions · owner · next permitted action
```

Where a class's closure draws on `closure.Report`, `controlstate` maps that owner's
`DimensionAssessment{Dimension,Required,Applicable,State,Reasons,BlockerIDs,ConditionIDs}` as
**scoped evidence into the per-class rule** — it never copies the task verdict.

## 12. Lifecycle assessment (typed separately from closure, frozen)

```text
LifecycleAssessment {
    applicability
    state
    source_owner
    source_identity
    reason_code
}
```

Rules: absence of deprecated/superseded evidence does NOT synthesize `active`; revocation is not a
universal node lifecycle unless the class owner defines it (in the graph, "revoked" is a
`RevocationReceipt`, not a node status); each class may have a different lifecycle vocabulary or
none; unavailable lifecycle evidence yields `unknown`; `not_applicable` is explicit, never
inferred from absence. Lifecycle sources today are scattered (`PropPromotionStatus`
`candidate|governed|rejected|superseded`, `PropStability` `stable|…|deprecated`,
`PropSupersededBy`, claim `StatusSupported/Superseded`, `QuestionStatus*`); the registry (§6.3)
names the lifecycle source owner per class.

## 13. Attention ownership + severity mapping (frozen)

`controlstate` assigns the control-panel **attention class and severity** as a canonical
**non-authoritative prioritization projection** — it does not replace the underlying domain
owner's truth. Each `attention_item` includes: source owner; source schema; source identity;
source digest/authority identity where available; attention class; severity
(`informational|attention|warning|critical`); **severity basis**; affected artifacts; reason code;
blocking status; evidence; next permitted action; architect-input-required flag.

A **governed / code-reviewed mapping** freezes `typed source condition → attention class →
severity`. Rules: no error-text parsing; no label/color inference; no client mapping; an explicit
critical source condition cannot be silently downgraded; unknown source classification stays
visible and fails closed; duplicate source findings are deterministically deduplicated; one
source condition cannot generate unstable IDs across runs. Severity's typed sources exist today
only scattered (`risk_classify.go` `RiskClass`; contradiction validators; `claimaudit.Warning`,
`ledger.VerificationWarning`, `EditWarning`; the `PropSeverity` predicate) — `controlstate`
composes them; the client never does.

## 14. Phase 9.6 feedback scope (frozen)

Phase 9.6 feedback is **scope-specific** — never a repository-wide collection:
- `control_snapshot` exposes feedback **capability/context availability** only;
- an exact **active-task** snapshot may include the already-authoritative
  `TaskBriefing.FeedbackProjection` summary;
- a selected **file/artifact** consumes the exact server `Briefing` feedback for that scope;
- no repository-wide promotion scan is invented; no feedback record is associated with an artifact
  merely because it is graph-adjacent.

## 15. Repository-context law (reuse Phase 9.6, frozen)

Phase 9.5 reuses the **immutable startup-owned repository root + domain** context established by
Phase 9.6. It must NOT introduce a second repository-root flag, caller-selected filesystem roots,
cwd inference, workspace-folder guessing as authority, or `homeDomain` as filesystem repository
identity. When repository context is absent: graph-backed architectural state may remain visible;
filesystem/task/completion/feedback sources report `unavailable`; the control snapshot becomes
`partial`/`unavailable` honestly; **the client never fills missing sections from local guesses.**

## 16. One-screen information architecture (accepted)

One dominant question per screen; the **attention queue is the default**; important state without
scrolling; provenance collapsible.

- **Top strip:** repository/domain · graph authority · architecture posture · critical issues ·
  open architect questions · active task · completion state. (Digests/provenance behind an
  expander.)
- **Left rail:** grouped ontology *families* (Knowledge · Architecture · Realization · Patterns ·
  Dialogue & closure) — **from `navigation_descriptor/v1`**, not a client table.
- **Center:** the attention queue by default, with filters (critical · warnings · open artifacts ·
  questions · contradictions · missing evidence · degraded · unknown · all artifacts), backed by
  `artifact_index/v1`. Rows show only: label · class · closure badge · open-dimension count ·
  highest severity · optional owner.
- **Right inspector** (order): identity & purpose · authority · lifecycle · artifact closure ·
  open dimensions · warnings & blockers · architect questions · next permitted action · evidence
  & tests · promoted-feedback provenance · relationships · focus graph. The focus graph is
  supporting evidence — never dominant, never animated, never rearranging while reading.

## 17. Architect-question workflow — guarded delegation only (accepted)

The only interactive mutation family; a guarded delegation to existing dialogue owners — never a
generic mutation RPC or arbitrary command. Each action maps to a specific typed owner operation
(explicit request schema, actor identity, repo/task/session binding, stable outcome vocabulary,
replay + refusal behavior, audit result). Refusals are visible and write nothing. The visible,
non-collapsible lifecycle:

```text
question open → answer recorded → awaiting evidence or governance
→ accepted for the question → optional governed promotion → future briefing feedback
```

A **recorded answer is not accepted truth**; an **accepted answer is not automatically promoted**;
a **promoted governed record is reusable architectural knowledge** (and only then does Phase 9.6
supply its feedback lineage). Raw answer text never becomes governed-artifact prose (Phase 9.6
provenance≠authority carries forward).

## 18. Checkpoints

- **Checkpoint 1 — Canonical architectural-state owner and schemas.** Includes: govern-first
  invariants/failure-modes/forbidden-fixes; the `controlstate` owner package; typed models;
  validators; deterministic digests; projection/source availability; the artifact eligibility
  registry; the mandatory navigation descriptor; the paginated `artifact_index` model;
  artifact-state aggregation laws; the lifecycle assessment model; the attention identity/severity
  model; an explicit initially-supported-class matrix; and honest unknown/unsupported behavior.
  It includes **no** server RPC, protobuf, VS Code change, question mutation, completion mutation,
  promotion mutation, or certification behavior.
- **Checkpoint 2** — server + protobuf read surfaces for those projections (additive, typed; the
  server stays thin transport). No architect mutation workflow.
- **Checkpoint 3** — dashboard IA + ontology-navigation refactor (grouped families from the
  descriptor, compact top strip, default attention queue, closure badges, responsive layout). No
  dialogue mutation.
- **Checkpoint 4** — artifact inspector + closure dimensions (applicable-dimension matrix,
  blockers, warnings, evidence, tests, questions, next action, collapsible relationships + focus
  graph).
- **Checkpoint 5** — guarded architect-question + answer workflow (question workspace, answer/
  adjudication delegation where authorized, typed outcomes, visible answer lifecycle). No
  automatic promotion/completion.
- **Checkpoint 6** — completion + Phase 9.6 feedback integration, accessibility, adversarial
  closure, Ubuntu + Windows + packaged-extension validation, closure documentation.

Each checkpoint is design-reviewed before it begins.

## 19. Simplicity requirements (accepted, frozen)

One dominant question per screen · important state visible without scrolling · attention queue is
the default · provenance collapsible · no unexplained acronyms · no color-only meaning · every
warning has a reason and a next action · no synthetic architecture percentage · no badge wall · no
animated graph · stable layout while reading · keyboard navigation throughout · accessible
contrast · usable at normal laptop width with VS Code sidebars visible · at most one primary
action per panel · no raw digests unless expanded.

## 20. Explicit exclusions (accepted, frozen)

No automatic architectural decisions · no automatic question answers · no automatic adjudication ·
no automatic promotion · no automatic task completion · no correctness certification · no
client-side closure algorithm · no client-side severity calculation · no generic RDF/graph
mutation console · no visual graph editor · no GitHub issue/PR mutation · no GNN or model-ranking
work · no repository-wide architecture scoring without a separate governed owner.

## 21. Required design proofs (forward-declared)

Implementation checkpoints must carry executable tests proving:

1. graph authority and artifact closure are never conflated;
2. lifecycle and closure are never conflated;
3. task closure and artifact closure are never conflated;
4. completion and correctness certification remain distinct;
5. an artifact shows only applicable dimensions;
6. missing non-applicable dimensions do not open an artifact;
7. the editor cannot calculate closure locally;
8. the editor cannot manufacture severity;
9. critical attention remains visible across navigation;
10. open questions link to affected artifacts and dimensions;
11. a recorded answer is not shown as accepted;
12. an accepted answer is not shown as promoted;
13. promoted knowledge shows Phase 9.6 lineage;
14. dialogue actions delegate only to authorized owners;
15. a refusal writes nothing;
16. raw answer text never becomes governed-artifact prose;
17. stale graph authority disables authoritative claims (and cannot yield `closed`);
18. degraded feedback does not erase base architecture state;
19. unknown state remains unknown;
20. no repository-wide correctness claim is produced;
21. keyboard-only operation works;
22. color is never the only state carrier;
23. the panel remains usable at constrained VS Code widths;
24. existing extension consumers remain compatible;
25. Phase 9.4 behavior remains unchanged;
26. `CorrectnessCertified` remains owner-controlled and unchanged;
27. the server does not own semantic classification (closure/severity/lifecycle/applicability);
28. `closure.Report`'s task verdict is never copied as artifact closure;
29. an unsupported class remains visible and `unknown`;
30. absent lifecycle does not synthesize `active`;
31. an absent source does not synthesize zero (yields `partial`/`unavailable` + reason);
32. `closed` requires every applicable required dimension satisfied;
33. `not_applicable` requires explicit policy (never absence-inferred);
34. artifact-index pagination is stable and digest-bound;
35. unknown ontology classes appear under `unclassified`;
36. `control_snapshot` performs no repository-wide feedback scan;
37. artifact feedback is exact-scope only;
38. missing repository context produces `partial`/`unavailable` state, not guesses;
39. severity IDs and ordering are deterministic;
40. a source critical severity cannot be silently downgraded;
41. the client contains no semantic class/severity/closure tables after the migration.

## 22. Bounded open design questions (for Checkpoint-1 review)

1. **`controlstate` composition granularity** — is `artifact_state/v1` computed per-artifact on
   inspector selection while `artifact_index/v1` is batch-paginated? Freeze request granularity +
   caps (with visible truncation).
2. **Attention composition sources per class** — enumerate the exact owner input feeding each
   attention class (contradiction, missing evidence/test, enforcement missing, ownership
   unresolved, scope ambiguous, blind spot, integrity/provenance failure, forbidden move) so the
   mapping is auditable, not invented.
3. **Initially supported class set** — which classes get a reviewed assessment policy in
   Checkpoint 1 (the rest stay `unsupported`/`unknown` under `unclassified`)?
4. **Registry authoring surface** — is the eligibility/assessment registry authored YAML under
   `docs/awareness/` (govern-first) or a Go-owned constant? Decide before Checkpoint 1 so the
   navigation descriptor's source of truth is fixed.
5. **CP5 delegation surface** — which exact `questiondisposition`/`questionpromotion` operations
   become guarded editor targets, and through which additive typed RPC (deferred to CP5 design).

## 23. Planned guarantee & stop boundary

> Sensei's VS Code extension presents a simple, typed, owner-derived architectural control panel.
> It shows the authority and architecture posture of the current repository, the applicable
> closure state of each architectural artifact, the exact warnings and questions requiring
> attention, and the next permitted action. It allows architects to answer Sensei questions only
> by delegating to guarded owners, while preserving the distinctions between answer, governance,
> promotion, task closure, completion, and correctness certification.

This opening freezes the semantic owner boundary, the five projection contracts, artifact
identity + eligibility, closure aggregation, lifecycle, attention ownership + severity mapping,
availability, the repository-context law, Phase 9.6 feedback scope, the six checkpoints, the
simplicity/accessibility rules, the exclusions, and the proof matrix. It adds NO implementation.
Checkpoint 1 begins only when this repaired opening is reviewed and explicitly unlocked. Phase 6
remains the sole correctness certifier; `CorrectnessCertified` remains false.

## 24. Checkpoint 1 rulings (frozen)

Resolves the §22 bounded questions for the Checkpoint-1 slice:

- **Composition granularity** — `ControlSnapshot` is built for one exact repository/domain
  context; `ArtifactState` on demand for one exact artifact identity; `ArtifactIndex` is
  batch-projected + paginated; `NavigationDescriptor` is registry-derived and
  repository-independent (except schema/version metadata); `AttentionItem` is a reusable record
  embedded/referenced by the others. Full `ArtifactState` is never batched into snapshot/index;
  index summaries carry only bounded summary fields. Page size: default 100, max 250,
  non-positive → default, **above max fails validation (fail-closed)**. Ordering: family order →
  class order → normalized label → canonical class → exact IRI (IRI is the final tie-breaker).
- **Registry authoring surface** — a **Go-owned immutable typed registry** under
  `golang/architecture/controlstate` (authored + code-reviewed, not graph-inferred, not loaded
  from ambient config). Govern-first awareness records describe/constrain it; no second YAML
  runtime registry. A future move to governed external data is a separately reviewed migration.
- **Initially supported assessable classes** — exactly `aw:Contract`, `aw:Invariant`,
  `aw:Component`, `aw:Boundary`. Every other known class is classified by the registry as
  `assessable` | `explicitly_not_applicable` | `unsupported`. `unsupported` closure is `unknown`
  (never `not_applicable`). Unknown graph classes remain visible under the `unclassified` family
  with coverage `unknown`, closure `unknown`, and a typed limitation.
- **Attention composition scope (CP1)** — implements the attention families: graph authority +
  seed verification; artifact-dimension blockers; relevant contradictions; missing required
  enforcement / verification-or-tests / evidence; unresolved ownership; ambiguous scope; open
  architect questions; high-risk coverage blind spots; integrity/provenance verification
  failures; typed forbidden-move findings. Task-admission/completion/runtime families may have
  vocabulary entries but are not semantically integrated until later checkpoints. **No
  placeholder condition appears as a live attention item.**
- **CP5 delegation** — the exact mutation RPC is deferred. CP1 may model question capability,
  architect-input-required, and next-action owner identity, but adds no answer/disposition/
  adjudication/promotion operation.

The `controlstate` package must not import `golang/server`, generated protobuf packages, editor
code, or CLI rendering packages. The server remains outside Checkpoint 1.

## 25. Checkpoint 2 rulings (frozen)

Checkpoint 2 exposes the five canonical controlstate read models through additive protobuf and
read-only `AwarenessGraph` RPCs. **No semantic ownership moves.** The dependency direction is
one-way:

```
typed domain owners
        ↓
controlstate builders and validators   (golang/architecture/controlstate)
        ↓
pure protobuf mapper                   (golang/server/controlstateproto)
        ↓
read-only server RPC                   (golang/server)
        ↓
future VS Code renderer                (Checkpoint 3+, not in CP2)
```

The server transports Sensei's conclusions. It does not recreate them.

- **Additive RPC surface** — exactly four read-only RPCs:
  `GetArchitectureControlSnapshot`, `ListArchitectureArtifacts`,
  `GetArchitectureArtifactState`, `GetOntologyNavigationDescriptor`. No mutation RPC.
  `architecture.attention_item/v1` is ONE shared canonical message embedded in snapshot and
  artifact-state responses; a separate attention mutation/classification endpoint is forbidden.
  Requests carry logical repository/domain identity only — **no repository-root, cwd, or
  workspace-folder field; no caller-supplied canonical class; no search-text field while
  Checkpoint-1 search is unsupported**. The index cursor stays opaque and owner-validated;
  page size stays subject to controlstate's bounds. `expected_graph_authority_identity` and
  `expected_registry_digest` are preconditions (FailedPrecondition on mismatch), never
  alternate authorities.
- **Protobuf model laws** — the five schema identities are represented exactly; shared messages
  for projection metadata, source status, artifact identity, lifecycle, dimension assessment,
  attention item, keyed counts, and navigation families/classes. Closed vocabularies are proto
  enums with `*_UNSPECIFIED = 0`; **UNSPECIFIED always fails validation and never means
  unknown**. Unknown-versus-zero is preserved with proto3 `optional` fields. The mapper copies
  the canonical controlstate digest verbatim — it never recomputes a digest from the protobuf
  representation. Not serialized: absolute repository roots, raw internal errors, raw architect
  answers, unstable timestamps, UI colors, layout hints, synthetic scores, or
  correctness-certification claims. All additions are additive with new field numbers; the
  Phase 9.6 `BriefingResponse.feedback` field 7 is untouched.
- **Pure mapping owner** — `golang/server/controlstateproto` is transport-only: it imports
  controlstate, generated protobuf, and stdlib conversion helpers ONLY (no graph stores, no
  governed YAML readers, no mutation owners, no certification writers). Mapping failures are
  errors; malformed fields are never silently omitted. The mapper validates the canonical
  projection (and every nested attention item) BEFORE mapping. proto→model helpers are
  test-only round-trip proof aids, not production surfaces.
- **Catalog composition stays in the semantic owner** — Checkpoint 2 adds an ADDITIVE
  controlstate builder that composes `ArtifactSummary`/`CatalogSnapshot` rows from typed
  observations (observed class IRIs, governed-status observation, availability). This keeps
  the status→lifecycle and coverage/closure vocabularies inside controlstate; the server holds
  no closure/severity/lifecycle tables. The catalog is constructed in ONE bounded batch (per
  registered class), never by building a full `ArtifactState` per index row.
- **Typed provider boundary** — handlers acquire inputs only through a
  `ControlStateReadProvider` seam. Provider outputs are controlstate inputs or owner-projected
  typed observations — never `map[string]any`, raw triples interpreted by the handler, raw
  YAML records, error prose as severity, or request-selected filesystem roots. Where no
  authoritative typed adapter exists yet, the provider supplies a typed unavailable source
  with an exact reason code — it never infers satisfaction from graph adjacency, manufactures
  zero, or weakens unknown.
- **Handler law** — each handler only: authenticate under existing read policy → validate
  request shape and immutable repository/domain context → acquire typed inputs via the
  provider → call the canonical controlstate builder → validate the returned projection → map
  losslessly → return. Handlers never change closure, assign severity, select lifecycle,
  change applicability, fill missing counts with zero, reorder semantic lists, inspect reason
  prose, choose a canonical class, recompute digests, or write repository/dialogue state.
- **Semantic availability is response data** — a partial/unavailable/degraded/unknown
  projection is a SUCCESSFUL RPC response carrying its typed state. Transport errors are
  reserved: InvalidArgument (malformed request/filter/page/cursor), FailedPrecondition
  (expected authority/registry mismatch), NotFound (absence proven by an available
  authoritative source), Unavailable (infrastructure could not execute), Internal (impossible
  mapper/validator contradiction). Raw internal errors and filesystem paths never appear in
  client-facing messages.
- **Repository/scope authority** — the immutable startup-owned repository root + domain
  context (Phase 9.6) is reused as-is. No os.Getwd authority, workspace guessing,
  home-directory fallback, caller path override, or artifact-class override. With repository
  context unconfigured: the navigation descriptor stays available; filesystem-backed inputs
  stay typed-unavailable; graph-backed state is returned only where its authority is valid.
- **Explicit exclusions (CP2)** — no VS Code dashboard/inspector behavior, webview
  layout/styling, architect-answer mutation, question disposition/adjudication/promotion,
  completion mutation, correctness certification, governed-YAML mutation through an RPC,
  repository-wide feedback discovery, or client-side closure/lifecycle/severity/applicability
  logic. Phase 6 remains the only correctness certifier.

## 26. Checkpoint 3 rulings (frozen)

Checkpoint 3 migrates the VS Code dashboard onto the four accepted read-only RPCs. **No semantic
ownership moves to the client; no mutation is added.** The editor renders owner projections
verbatim and classifies nothing.

- **Surface** — the new control panel is a self-contained webview module
  (`editor/vscode/media/controlPanel.js` + the shared pure formatter
  `media/controlPanelFmt.js`), rendered as the default view. It **replaces the Phase-1 "Project
  Awareness" IA**; the Phase-2 "Closure & Control" explorer (including its gated candidate/promote
  mutations) is retained unchanged as a visually separated **legacy** view and never overrides the
  canonical header. The panel shares the single `acquireVsCodeApi()` handle and adds its own
  message listener; the four RPCs are wired through the host router (thin transport).
- **Top strip** — repository/domain · graph authority (three independent booleans:
  observed/current/integrity, **never a single healthy badge**) · projection availability
  (available/partial/unavailable/invalid, explicit) · critical count · warning count ·
  open-question count · active-task state · completion state. Digests/provenance/sources/limitations
  live behind an expander. Unknown counts render **Unknown/Unavailable, never 0**.
- **Left rail** — descriptor-driven families → classes **only**, in exact server order, with
  Unclassified visible; visibility and capabilities are read from the descriptor. The client keeps
  **no** class-membership, capability, default-visibility, or family table.
- **Center** — default is the bounded attention queue (`top_attention`, server order). Filter chips
  {Critical, Warnings, Questions, Contradictions, Missing evidence} SELECT owner-provided
  severity/attention-class values (never reinterpret); {Open, Degraded, Unknown, All artifacts}
  query the artifact index with the owner closure enum, paginated by the **opaque cursor unchanged**.
  Rows show only label · class · closure token · open-dimension count · highest severity · optional
  owner, with **no local synthesis** from missing fields and no client re-sort.
- **Artifact header** — thin, owner-honest: exact identity, canonical class, lifecycle, closure
  (+reason), availability, highest attention. **No dimension/evidence/question inspector — that is
  Checkpoint 4.**
- **Palette** — enum token → visual CSS class only; a text label is **always** present, and no
  color/icon/wording upgrades or downgrades semantic state. Any `*_UNSPECIFIED`/missing enum
  renders an explicit **Invalid** badge, never a neutral or OK default.
- **Unknown-versus-zero (frozen decode contract)** — proto3 `optional` counts decode to `undefined`
  when unobserved and to a numeric string when observed; presence is detected via the
  `oneofs:true` synthetic-oneof marker, never value-vs-zero. Absent sub-messages decode to `null`
  (unavailable). Proven by `controlPanelDecode.test.ts`.
- **Compatibility** — existing graph rendering, the Phase-9.4 "This File" tree, `graphAuthority.ts`,
  `taskSession.ts`, and unrelated commands stay working; legacy semantic tables remain **only** in
  `dashboard.js` (quarantined, documented). No legacy path is deleted before the new read-only
  surface is covered by tests.
- **Proofs** — the aggressive anti-duplication proof (`controlPanelTables.test.ts`) fails if
  `ASPECTS`, semantic class membership, severity/closure ordering, resolver-capability, or
  ontology-family tables reappear in the control-panel surface; `controlPanelFmt.test.ts` locks
  verbatim badge rendering + unknown-versus-zero; `protoContract.test.ts` pins the four RPCs and
  vocabularies. Deep-inspector, question-workflow, and promotion proofs are **not** asserted in CP3.

**Exclusions (CP3)** — no dialogue mutation, answer, disposition, adjudication, promotion,
completion, or feedback-write action; no client-side closure/severity/lifecycle/class/applicability
logic; no deep artifact inspector (CP4); no question workspace (CP5). Phase 6 remains the sole
correctness certifier; `CorrectnessCertified` stays false.

## 27. Checkpoint 4 rulings (frozen)

Checkpoint 4 expands the CP3 thin header into the full **read-only** artifact inspector. It renders
`architecture.artifact_state/v1` VERBATIM and infers nothing; it adds no RPC, host, or protobuf
change (the state was wired in CP3). It touches no switches — answering/disposition/promotion is
CP5, completion/accessibility/packaged validation is CP6.

- **Surface** — the inspector renders in the CP3 right-hand `#cpHeader` pane (scrollable), in the
  design §16 order: identity → authority → lifecycle → artifact closure → open dimensions →
  warnings & blockers → architect questions → next permitted action → evidence & tests →
  promoted-feedback provenance → relationships → focus graph. Every section is owner-sourced or
  explicitly unavailable.
- **Applicable dimensions only** — only `applicable` dimensions are rendered; a non-applicable
  dimension is never shown and never counts as open (design §11; proofs 5/6). A muted
  "(N not applicable)" count is a pure count of `applicable === false`, never a synthesized state.
  Each dimension shows its owner `state` (enum → badge, no client computation), `required`,
  `reason_code`, and its own `blockers`/`evidence`/`questions`/`owner`/`next_action_owner` verbatim.
- **No client inference** — the inspector computes no closure, lifecycle, severity, question-state,
  or next-action. Questions/evidence/blockers are owner id strings and render verbatim;
  `next_action_owner` is an owner id rendered as text with no control. A missing/`*_UNSPECIFIED`
  enum renders an explicit Invalid badge, never a neutral/OK default.
- **Questions display-only** — `questions[]` render as ids annotated (by lookup, not inference) with
  the dimension(s) that reference them; there is no answer/disposition/promotion affordance (CP5).
- **Feedback exact-scope provenance** — `feedback` renders the `ScopedFeedbackRef` as provenance
  only (scope, availability, verified records, lineage, limitations); never a repo-wide scan, never
  authoritative (design §14; proof 37).
- **Relationships & focus graph** — no owner projection carries relationship/edge data, so both
  render an honest **unavailable** placeholder. The legacy graph-adjacency / `CLASS_COLOR` / `DIR`
  tables are never reused (they stay quarantined in `dashboard.js`).
- **Strictly read-only** — the panel posts only the four read message types
  (getNavigationDescriptor / getControlSnapshot / listArtifacts / getArtifactState); it enqueues no
  mutation, answer, disposition, promotion, completion, feedback-write, or certification.
- **Proofs** — `controlPanelInspector.test.ts` (applicable-only, enum-verbatim, display-only
  questions, exact-scope feedback, honest-unavailable relationships) + the extended aggressive
  anti-duplication gate (read-only message set; no legacy semantic tables; no client graph).
  Phase-9.4 authority path and the legacy explorer stay unchanged.

**Exclusions (CP4)** — no dialogue mutation, answer, disposition, adjudication, promotion,
completion, or feedback-write; no client-side closure/lifecycle/severity/question-state/next-action
inference; no CP5 question workspace; no CP6 completion/accessibility/packaged validation. Phase 6
remains the sole correctness certifier; `CorrectnessCertified` stays false.

## 28. Checkpoint 5 rulings (frozen)

Checkpoint 5 opens the FIRST and ONLY guarded mutation family in the control panel: record an
architect answer and accept/reject it — a deliberate, separately triggered, guarded delegation to an
existing owner. Everything else stays read-only.

- **Governed promotion is DEFERRED (ruling)** — implementing promotion as a direct
  `server → questionpromotion` RPC on the served surface violated two load-bearing invariants:
  `graph_truth_must_come_from_approved_corpus` (the served `AwarenessGraph` surface exposes no
  fact-writing RPC) and the briefing-feedback ownership boundary (`golang/server` must not consume
  `questionpromotion` directly — promotion verification is owned by `briefingfeedback`). Rather than
  weaken those guards, CP5 ships only the clean disposition family; governed promotion is re-designed
  through the feedback owner in a later checkpoint. Its `accepted ≠ promoted` boundary is still
  visibly enforced (an accepted disposition is marked `reusable_candidate`; nothing promotes it).
- **Owners (delegation only)** — record/accept/reject delegates to `questiondisposition`
  (`Prepare` pure → `RecordDisposition`; the disposition vocabulary answered/dismissed/deferred/
  task_local IS the accept/reject). `questiongen` is unguarded and is NEVER exposed as authority. The
  server/handlers assign no authority of their own.
- **Claims, not authority** — the client-supplied repository/domain/task/session/actor fields are
  CLAIMS verified against server-resolved authority (startup-owned repository root, active task
  pointer, enrolled identity manifest). Any mismatch is a typed refusal. Filesystem roots are never
  taken from the request.
- **Refusal = typed non-mutation receipt** — a domain refusal (unconfigured, mismatch, unauthorized,
  ineligible, stale head, contested) is a SUCCESSFUL RPC carrying `mutation_applied=false`, the
  refusal owner + code, and the UNCHANGED ledger identity (previous == resulting head). It NEVER
  masquerades as a mutation and NEVER becomes a transport error (transport errors are reserved for
  malformed requests). Configuration absence is a stable typed refusal, not an internal failure.
- **Prepare/commit/replay** — Prepare writes nothing; Record commits exactly one disposition against
  the client's expected-ledger-head precondition (stale → refuse, never silently re-prepare); exact
  replay returns the original receipt (`mutation_applied=false`); a conflicting disposition is
  CONTESTED — a new immutable record referencing (never overwriting) the prior. Idempotency is the
  owner's content-addressed receipt identity.
- **No hidden lifecycle chaining** — a successful record NEVER promotes; a successful promotion NEVER
  completes or certifies. `CorrectnessCertified` stays false.
- **Raw-answer isolation** — the raw answer travels as opaque bytes, is hashed by the owner
  (`AnswerID` + digest), and NEVER appears in a receipt, refusal, audit field, log, or governed
  record. Promotion's proposal is structurally separate from the answer bytes.
- **Extension is painfully literal** — choose → prepare → show the owner candidate/consequences →
  explicit confirm → commit once → typed receipt/refusal → refresh the artifact state from the
  owner. No optimistic lifecycle: the DISPLAYED architectural lifecycle comes ONLY from the refreshed
  owner projection — a stale/unavailable refresh after a successful commit is shown honestly, never
  as an invented "accepted". No second commit while one is in flight; the server remains the replay
  authority.
- **Proofs** — Go adversarial proofs snapshot the task ledger byte-for-byte around every refusal
  (prepare-writes-nothing, unauthorized, stale head, repo/domain/task mismatch, unknown question,
  contested, replay, no-auto-promote); the extension state-machine proofs lock the literal flow +
  the stale-refresh discipline; the anti-duplication gate proves "read-only EXCEPT the guarded
  family" with no auto-chaining.

**Exclusions (CP5)** — no task completion, no correctness certification, no generic graph/YAML
mutation, no auto-chaining, no unguarded dialogue write. **CP6 has NOT begun** (completion +
Phase-9.6 feedback integration, accessibility, packaged/Ubuntu+Windows validation, closure docs).
