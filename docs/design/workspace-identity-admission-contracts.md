# Workspace Identity and Admission Contracts

Status: additive implementation contract for Sensei Dashboard Workspace O1 closure  
Base revision: `4691b9977469285c234e529189068bd528aebed5`  
Target repository: `globulario/sensei`  
Consumer repository: `globulario/sensei-dashboard`

## 1. Problem

Workspace Phase O1 in `globulario/sensei-dashboard` implemented the seven Dashboard/runner-owned orchestration contracts, but correctly stopped before inventing two Sensei-owned semantics:

- `sensei.workspace.identity.v1`
- `sensei.workspace.admission.v1`

The semantic ingredients already exist in Sensei, but not as canonical external contracts:

- checkout repository identity is owned by `.sensei/config.yaml` and `ClaimDocumentBinding`;
- revision, tree digest, graph digest, and graph freshness already have typed owners;
- graph coverage and seed state already exist in `MetadataResponse` / `GraphAuthority`;
- task identity already exists in the local task-session artifacts;
- mutation admission and post-change verification already exist as `admission.Decision` and `admission.Verification`;
- `admit_change` and `verify_admission` are local MCP operations, not gRPC RPCs.

Today no single canonical artifact lets an external runner prove what Sensei says about the selected checkout, and no stable downstream schema lets the Dashboard consume admission without defining its own approximation of Sensei truth.

That gap blocks the honest completion of Workspace O1 and makes Phase O2 runner work premature.

## 2. Mission

Make Sensei the canonical producer of two closed, versioned JSON contracts:

1. **Workspace identity**: a deterministic composition of existing checkout binding, graph authority, coverage, and optional task identity.
2. **Workspace admission**: a stable external projection of the existing admission decision and verification owners.

Expose those contracts through explicit safe MCP tools without adding new admission semantics, a new gRPC service, process orchestration, or Dashboard-local authority.

After this lands, `globulario/sensei-dashboard` can pin the exact schemas and fixtures, generate TypeScript types, and close Workspace O1 before beginning O2.

## 3. Architectural decision

### 3.1 JSON Schema is the canonical cross-repository wire contract

Sensei owns and versions the external schemas under:

```text
docs/schemas/workspace/v1/
  workspace-identity-v1.schema.json
  workspace-admission-v1.schema.json
```

Required schema identifiers:

```text
sensei.workspace.identity.v1
sensei.workspace.admission.v1
```

The schemas are the cross-repository contract. Go structs and MCP serialization must conform to them; generated downstream TypeScript must come from pinned copies of them. Internal Go structs remain implementation authorities for their existing domains, but they are not silently treated as stable external wire formats.

Every schema must:

- use JSON Schema Draft 2020-12;
- be closed with `additionalProperties: false` at every contract-owned object boundary;
- use explicit required fields and closed string enums;
- distinguish `null`, unknown, unavailable, and not requested where they mean different things;
- contain an ownership/pinning `$comment` requiring consumers to pin an exact Sensei commit and digest;
- avoid frontend, runner, provider, or transport-specific fields.

### 3.2 Workspace identity is evidence, not permission

`sensei.workspace.identity.v1` reports independently owned facts. It must not collapse them into a broad `READY` or `SAFE` verdict.

The contract must include at least:

```text
schema_version
generated_by
composition_state
binding
repository_domain_source
graph_authority
coverage_state
task_identity
limitations
```

`binding` reuses the semantics of `architecture.ClaimDocumentBinding`:

```text
repository_domain
revision
revision_status
tree_digest_sha256
graph_digest_sha256
graph_digest_status
```

`graph_authority` is a bounded external projection of the current authoritative `GraphAuthority` / `MetadataResponse` facts needed by a runner:

```text
authoritative
graph_freshness_state
graph_freshness_detail
seed_state
build_provenance_state
live_store_graph_digest_sha256
live_store_graph_triple_count
embedded_seed_digest_sha256
embedded_transaction_stamp_present
embedded_transaction_matches_seed
certified_awareness_graph_commit
certified_services_repo_commit
```

`coverage_state` uses the current closed Sensei vocabulary, not a Dashboard-authored translation.

`task_identity` must distinguish:

```text
not_requested
resolved
unavailable
```

and carry a deterministic Sensei `task_id` only when resolved.

`composition_state` means only whether the receipt could be composed completely:

```text
complete
partial
unavailable
```

It is not mutation admission, correctness certification, provider authorization, or a merge recommendation.

### 3.3 Governed workspace identity uses configured checkout identity only

The workspace proof surface is stricter than an ordinary cross-domain query.

For `sensei_workspace_status`:

- `repo` is required and identifies the checkout whose local state is inspected;
- repository domain must come from canonical `.sensei/config.yaml` `repository.domain`;
- an explicit domain override is not accepted;
- `SENSEI_DOMAIN` and legacy `AWG_DOMAIN` must not establish governed workspace identity;
- Git origin may be reported as diagnostic evidence but must not silently replace absent configuration;
- missing or malformed configured identity produces an honest partial/unavailable receipt or typed refusal, never a guessed domain.

This does not make the filesystem path a Sensei identity field. The caller owns process/worktree placement; Sensei proves the repository content and governed identity facts it actually owns.

### 3.4 No repository-root hash or MCP session identity is invented

Sensei currently and deliberately delegates checkout/process context to the caller. This slice must not introduce fictitious fields merely to resemble the Dashboard's earlier illustrative sketch.

The canonical identity contract must not contain:

```text
repository_root_identity
server_session
provider_session
worktree_id
job_id
runner_instance_id
```

A future runner binds its own worktree and job envelope, then compares the returned repository domain, revision/tree digest, graph identity, and task identity against its own expected facts.

### 3.5 Workspace admission is a stable projection of existing owners

`sensei.workspace.admission.v1` must not redefine admission. It projects the existing `admission.Decision` and `admission.Verification` into one external, closed contract.

The schema must represent two record kinds:

```text
decision
verification
```

Every record carries the stable decision identity and binding:

```text
schema_version
record_kind
admission_id
decision_digest_sha256
policy_id
policy_version
decision
requested_mode
binding
session_receipt
request_receipt
inspection_capability
mutation_capability
envelope
reasons
limitations
scope_only
correctness_certified
verification
```

`decision` must use the current admission vocabulary exactly:

```text
admitted
admitted_with_conditions
waiting
refused
uncertifiable
```

`verification`, when present, must include at least:

```text
status
verification_digest_sha256
iteration_digest_sha256
patch_digest_sha256
changes
violations
pending_condition_ids
pending_test_ids
pending_proof_obligation_ids
pending_runtime_evidence_ids
reasons
limitations
scope_only
correctness_certified
```

Verification status must use the current closed vocabulary exactly:

```text
scope_compliant
scope_violated
stale
uncertifiable
```

A decision record requires `verification: null`. A verification record requires a non-null verification object and must remain bound to the original decision's exact `admission_id`, `decision_digest_sha256`, and binding.

### 3.6 Permission to attempt is not correctness

The external contract must preserve these independent facts:

- `decision` says whether an exact bounded action may be attempted;
- verification says whether the observed change remained inside that admitted envelope;
- `scope_only` identifies scope-only decisions/verification;
- `correctness_certified` must remain whatever the existing owner reports and may not be inferred from admission, tests, CI, or scope compliance;
- a successful verification does not imply architectural approval or mergeability.

No adapter, MCP formatter, or schema default may turn absence of a violation into correctness.

### 3.7 Existing owners are delegated to exactly once

The new producer layer must compose or project existing owners; it must not copy their decision logic.

- repository/revision/tree/graph binding comes from the canonical existing binding and revision helpers;
- graph authority and coverage come from existing metadata/authority owners;
- task identity comes from the existing task-session owner;
- admission decisions come from `admission.Evaluate`;
- admission verification comes from `admission.Verify` and the exact referenced decision artifact.

The workspace package may validate, normalize, summarize, and serialize. It may not decide closure, invent a policy, infer a task, broaden a change envelope, or independently certify correctness.

## 4. Canonical producer package

Add a package such as:

```text
golang/architecture/workspacecontract/
  types.go
  identity.go
  admission.go
  normalize.go
  validate.go
  schema.go
  *_test.go
```

Exact filenames may follow repository conventions. The semantic boundary is mandatory.

The package owns:

- Go types mirroring the two external schemas;
- deterministic normalization;
- real Draft 2020-12 schema validation against the vendored canonical schemas;
- identity composition from explicit existing inputs;
- admission decision/verification projection from existing typed owners;
- fail-closed validation before any MCP result is returned;
- fixtures used by both schema and producer tests.

The package does not own:

- Git worktree creation;
- provider authentication;
- local IPC;
- process or PTY execution;
- GitHub writes;
- admission policy evaluation;
- graph queries beyond the existing typed clients;
- Dashboard state.

## 5. MCP surface

### 5.1 `sensei_workspace_status`

Add one safe read-only MCP tool:

```text
sensei_workspace_status(repo, task?) -> sensei.workspace.identity.v1
```

Behavior:

1. validate arguments at runtime, not only through the advertised input schema;
2. resolve the exact absolute checkout root internally without returning it as contract identity;
3. load canonical configured repository identity without environment/domain override;
4. resolve current revision and canonical repository-tree digest;
5. request typed metadata/graph authority for the configured domain;
6. optionally resolve the requested or active task through the existing local task-session owner;
7. compose and schema-validate the identity receipt;
8. return compact human text plus the canonical receipt as `structuredContent`.

Backend transport failure is `unavailable`, never empty guidance. Malformed repository configuration is a typed failure/limitation, never repaired silently.

### 5.2 Canonical admission tools

Add versioned workspace-facing tools while preserving the existing `admit_change` and `verify_admission` tools unchanged for compatibility:

```text
sensei_workspace_admit_change(...) -> sensei.workspace.admission.v1 (record_kind=decision)
sensei_workspace_verify_admission(...) -> sensei.workspace.admission.v1 (record_kind=verification)
```

Inputs may mirror the existing tools exactly. The new tools must delegate to the same existing owners once, project the returned typed result into the canonical external contract, validate it, and return it.

The verification tool must load/project the exact referenced decision artifact so its returned record retains the original policy, decision, binding, session, request, capabilities, and envelope together with the new verification result.

Do not mutate the existing tools' structured-output shape in this slice.

### 5.3 Safe-tools boundary

The new tools are admitted to the existing safe whitelist because:

- status is read-only;
- workspace admission delegates to the already-authorized local admission owner;
- workspace verification delegates to the already-authorized verification owner;
- no arbitrary command, shell, network target, SPARQL, or caller-authored verdict is accepted.

Their runtime argument validation must reject unknown properties and wrong JSON types even when an MCP client ignores the advertised JSON Schema.

## 6. Fixtures

Add canonical fixtures under:

```text
docs/fixtures/workspace/v1/
  identity/
    complete.json
    partial.json
    unavailable.json
  admission/
    admitted.json
    admitted-with-conditions.json
    refused.json
    verification-compliant.json
    verification-violated.json
    verification-stale.json
```

Fixture names may vary, but required semantic coverage may not.

Every positive fixture must pass the real JSON Schema validator and package validation. Add adversarial tests proving rejection of at least:

- unknown root or nested properties;
- malformed SHA-256 digests;
- malformed/unbound repository domain represented as complete;
- `composition_state=complete` with unresolved binding or unavailable graph authority;
- invented repository-root/session/job/provider fields;
- unknown graph/coverage/seed/admission/verification enum values;
- a decision record with non-null verification;
- a verification record with null verification;
- mismatched decision and verification admission ids/digests/bindings;
- `scope_compliant` being rewritten into `correctness_certified=true` without the owner reporting it;
- missing decision identity, policy identity, or exact binding.

## 7. Cross-repository adoption contract

The schema `$comment` and implementation handoff must state:

- `globulario/sensei` is the canonical producer and owner;
- `globulario/sensei-dashboard` is a consumer only;
- the Dashboard must copy/pin exact schema and fixture bytes from an accepted Sensei commit;
- Dashboard CI must prove local digest and live cross-repository parity;
- generated TypeScript must be regenerated from those pinned schemas;
- Dashboard-local `GoverningSnapshot` / `AdmissionReference` encodings may map to these contracts, but may not replace or weaken them;
- O2 runner implementation does not begin until the Dashboard pin/parity follow-up is merged and O1 is explicitly declared complete.

## 8. Documentation

Update at least:

- `docs/api-reference.md` with the three new MCP tools and exact ownership boundary;
- the MCP bridge source comments/tool list;
- any schema index or README section already used for canonical external contracts.

Documentation must explicitly say:

- workspace identity is not admission;
- admission is not correctness;
- configured MCP is not verified workspace identity;
- the runner owns process/worktree/job identity;
- Sensei owns repository/graph/task/admission truth.

## 9. Required proof

### Schema and type proof

- both schemas compile under a real Draft 2020-12 validator;
- all positive fixtures validate;
- all adversarial cases fail for the intended reason;
- Go types preserve closed enums and nullable distinctions;
- normalization is deterministic across repeated runs;
- committed fixtures are byte-stable after normalization.

### Identity composition proof

- configured checkout domain is returned exactly;
- absent/malformed configuration never falls back to environment or store order;
- exact Git revision and canonical tree digest are reported;
- uncommitted admitted trees can be represented by tree digest without pretending they have a revision;
- current, stale, unknown, empty, and backend-unavailable graph states remain distinct;
- coverage and seed states are preserved exactly;
- optional task identity resolves deterministically and `not_requested` differs from `unavailable`;
- no repository-root hash, server session, provider session, worktree id, or job id appears in the receipt.

### Admission projection proof

- every existing decision enum maps 1:1;
- every existing verification enum maps 1:1;
- exact policy, binding, decision digest, session, request, capability, and envelope identity are preserved;
- verification remains linked to the referenced decision;
- reasons and limitations are not discarded;
- scope compliance does not manufacture correctness certification;
- projection code invokes existing admission owners rather than recreating their logic.

### MCP proof

- tool discovery advertises all three new tools with closed input schemas;
- runtime rejects unknown properties and wrong types;
- structured output validates against the canonical schema before return;
- backend unavailable is reported as unavailable, not empty/complete;
- existing `admit_change` and `verify_admission` output compatibility is unchanged;
- a JSON-RPC integration test proves one complete identity call, one admitted decision, and one compliant or violated verification.

### Cross-repository proof

The handoff must report exact schema and fixture SHA-256 digests so the Dashboard follow-up can pin them without interpretation.

## 10. Required evidence

The implementation handoff must include:

```text
Workspace contracts:
- accepted contract/base SHA:
- final head SHA:
- identity schema path + sha256:
- admission schema path + sha256:
- fixture paths + sha256:
- canonical producer package:
- configured-domain/no-env-fallback proof:
- revision/tree binding proof:
- graph freshness/coverage/seed state matrix:
- task identity matrix:
- admission decision enum matrix:
- admission verification enum matrix:
- decision↔verification binding proof:
- no invented root/session/job/provider fields proof:
- MCP discovery/runtime-validation proof:
- existing-tool compatibility proof:
- full test/build/CI results:
```

## 11. Non-goals

This contract must not:

- implement `sensei-runner`;
- add Tauri, local IPC, provider authentication, PTY/process execution, worktree management, GitHub writes, or Dashboard UI;
- add a new gRPC RPC or proto message merely to transport this composition;
- introduce a new admission policy, outcome, reason vocabulary, or correctness claim;
- make the MCP bridge a source of repository identity independent from checkout configuration;
- infer identity from environment variables, directory basename, first loaded domain, or store row order;
- create repository-root hashes, MCP session ids, job ids, provider ids, or worktree ids as Sensei-owned facts;
- sign receipts or claim cryptographic remote attestation;
- change completion verification or merge authority;
- modify `globulario/sensei-dashboard` in this PR;
- begin Workspace O2.

## 12. Stop conditions

Stop and post `ARCHITECT QUESTION` before coding around any of these:

- a required external field cannot be mapped 1:1 to an existing authoritative Sensei owner;
- producing the identity receipt would require choosing repository domain from environment/store state rather than configured checkout identity;
- the current revision/tree/graph helpers disagree about exact binding semantics;
- admission decision or verification cannot be projected without dropping a field required to preserve identity, scope, reasons, or limitations;
- a proposed implementation would duplicate admission or freshness decision logic;
- schema validation would require loosening closedness or accepting unknown enum strings;
- the bridge cannot compose the receipt without a new gRPC RPC and no bounded local composition is possible;
- preserving existing `admit_change` / `verify_admission` behavior is impossible without a breaking change;
- any field requested by the Dashboard is actually runner/provider authority rather than Sensei authority.

## 13. Acceptance criteria

This contract is complete only when:

1. Sensei owns canonical, closed schemas for `sensei.workspace.identity.v1` and `sensei.workspace.admission.v1`.
2. A reusable Sensei package produces and validates both contracts from existing authoritative owners.
3. `sensei_workspace_status` returns exact checkout/revision/tree/graph/coverage/task facts without claiming mutation permission.
4. canonical workspace admission tools return exact decision and verification projections without redefining admission.
5. existing admission MCP tools remain compatible.
6. unavailable, unknown, partial, stale, refused, violated, and uncertifiable states remain explicit and distinct.
7. no runner/worktree/provider/session authority is smuggled into Sensei-owned receipts.
8. fixtures and digests are sufficient for an exact downstream pin.
9. repository-wide tests, freshness checks, and CI are green on the exact handed-off SHA.
10. the implementation handoff explicitly authorizes only the subsequent Dashboard pin/parity follow-up, not Workspace O2 itself.
