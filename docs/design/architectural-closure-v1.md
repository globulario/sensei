<!-- SPDX-License-Identifier: Apache-2.0 -->

# Architectural Closure v1

## Status

This document freezes the `architectural-closure/v1` protocol.

Phase 0 defines the contract. It does not retroactively certify historical
tasks and it does not yet implement the append-only ledger, Admission v2,
runtime evidence engine, proof discharge engine, certification engine, or
terminal completion transaction.

## Scope

The protocol separates three truth surfaces:

1. Governed knowledge sources
2. Compiled architectural graph
3. Task closure ledger

These surfaces are intentionally non-substitutable.

### Governed knowledge sources

Governed knowledge sources are durable architectural authority. Examples:

- authored awareness YAML
- approved decisions
- contracts
- invariants
- failure modes
- authority grants
- evidence profiles
- completion policies
- migration plans

Governed sources define reusable architectural meaning.

### Compiled architectural graph

The graph is deterministic, digest-bound, derived from governed sources, and
queryable. The graph projects architectural meaning but is not itself a mutable
task-history surface.

The graph must not become authoritative merely because a node exists in it.

### Task closure ledger

The ledger records what happened during one governed task. Examples:

- task preparation
- closure assessment
- authority resolution
- admission
- capability consumption
- mutation observation
- evidence receipts
- proof discharge
- certification
- completion
- revocation

The ledger is operational history. It is not a second authored architectural
corpus.

## Frozen laws

### Truth-surface law

Governed sources define reusable architectural meaning.

The graph projects that meaning.

The ledger records what happened during one governed task.

### Verified-diff law

A verified diff proves scope compliance only.

It does not prove:

- correct behavior
- satisfied contracts
- passing tests
- fresh runtime truth
- certification
- completion

### Learning law

A learning event creates candidates.

It never creates authority directly.

### Runtime-evidence law

`RuntimeEvidence` retains its historical IRI for compatibility, but in v1 it is
interpreted as a runtime evidence profile, not an observation receipt.

### Completion law

Reasoning closure and terminal completion are distinct.

`verdict: closed` from the existing closure engine means the reasoning
requirements for the bound task scope are satisfied. It does not mean a task has
been certified and completed under the v1 protocol.

Only a valid `CompletionReceipt` may establish terminal completion.

## Semantic collisions resolved

### Reasoning closure versus terminal closure

`ReasoningClosureVerdict` answers whether architectural reasoning requirements
are satisfied for one bound task scope.

Allowed values:

- `open`
- `conditional`
- `closed`
- `uncertifiable`
- `stale`

`TaskTerminalStatus` answers whether a task session has reached a terminal
operational outcome.

Allowed values:

- `completed`
- `completed_with_exception`
- `refused`
- `abandoned`
- `revoked`

### RuntimeEvidence

`RuntimeEvidence` means a runtime evidence profile:

- what runtime observation is required
- who owns the observation
- which observation path is legal
- freshness constraints
- trust level
- runtime target requirements

It is not an observation receipt.

### Evidence versus EvidenceReceipt

`Evidence` is the durable evidence definition or proof-stream identity.

`EvidenceReceipt` is one concrete observation or execution bound to a specific
result and runtime target.

### ActionCompleteTask

`ActionCompleteTask` remains a task-control recommendation. It is not terminal
task completion.

## Common binding model

Every operational record binds explicitly to the world it is talking about.

### Repository snapshot

```yaml
repository:
  domain: github.com/globulario/sensei
  revision: <commit>
  revision_status: resolved
  tree_digest_sha256: <digest>
```

### Graph snapshot

```yaml
graph:
  digest_sha256: <digest>
  digest_status: resolved
  schema_version: awareness-ontology/0.2
```

Absolute graph paths are diagnostic only and do not participate in semantic
identity.

### Task binding

```yaml
task:
  id: task.<id>
  session_id: session.<id>
  iteration_digest_sha256: <digest>
```

### Policy binding

```yaml
policies:
  admission: admission.strict.v2
  certification: certification.architectural_closure.v1
  completion: completion.architectural_closure.v1
  revocation: revocation.architectural_closure.v1
  ledger: ledger.task.v1
  canonicalization: canonicalization.architectural_closure.v1
```

### Result binding

Base binding and result binding are distinct.

```yaml
result:
  base_revision: <commit>
  patch_digest_sha256: <digest>
  result_tree_digest_sha256: <digest>
  result_revision: <commit-or-empty>
  graph_digest_sha256: <digest>
  generated_artifacts:
    - path: golang/server/embeddata/awareness.nt
      digest_sha256: <digest>
```

The base graph authorizes and informs the change. The result graph describes
the world that must be certified.

### Runtime target binding

```yaml
runtime_target:
  platform: globular
  environment_id: cluster.<uuid>
  deployment_id: deployment.<id>
  node_ids:
    - node.<id>
  service_instances:
    - service_instance.<id>
  release_revision: <release>
  configuration_generation: <generation>
```

Runtime evidence from another target cannot discharge a proof obligation.

### Actor binding and attestation

`ActorBinding` identifies the principal attempting the operation and the role
claims it is asking the system to consider.

`ActorBinding` alone does not prove authorization.

The claimed roles in `ActorBinding.roles` are request-time role claims only.
They become usable authority inputs only when all of the following are present
and valid:

- a bound authentication receipt
- one or more bound role-attestation receipts
- a trusted issuer or governed principal assignment
- any required concrete delegation receipts

The binding therefore distinguishes:

- principal identity
- requested or claimed role IDs
- authentication proof
- role attestation proof
- concrete delegation references

### Authentication receipt

Authentication is represented by a concrete operational receipt that binds:

- principal identity
- issuer
- one exact authentication artifact reference
- authenticated time
- expiry when applicable
- receipt status

The authentication artifact reference participates in verification and semantic
identity. Absolute host paths never do.

### Role attestation receipt

Role ownership is represented by a concrete operational receipt that binds:

- principal identity
- actor kind
- issuer
- exact attested role IDs
- the authentication receipt it depends on when required
- issuance and validity bounds
- receipt status

This prevents a caller from becoming authorized merely by listing role strings
inside `ActorBinding`.

### Delegation policy, delegation receipt, and delegation chain

The protocol distinguishes three separate concepts:

- `DelegationPolicy`: reusable governed policy that constrains delegation
- `DelegationReceipt`: one concrete delegated authority instance
- `AuthorityResolution.delegation_chain`: the ordered chain of concrete
  delegation receipts used for one operation

A `DelegationReceipt` narrows authority from either:

- a parent governed grant, or
- a parent delegation receipt

It must bind:

- delegator principal
- delegated principal
- narrowed role IDs
- narrowed authority domains
- narrowed actions
- narrowed mechanisms
- narrowed target scope
- risk ceiling
- validity interval
- delegation policy ID
- receipt status

The delegation chain is ordered operational history, not a set.

### Authority resolution

`AuthorityResolution` is the deterministic operational record that answers:

> Is this concrete actor authorized to perform this exact operation on this
> governed target through this selected mechanism under this exact task and
> base binding?

The resolution is bound to:

- one actor binding digest
- the authentication receipt digest used to verify the actor
- one base binding digest
- one closure-assessment digest
- one operation-set digest
- one authority-policy graph digest
- one authority-resolution policy ID
- one evaluation time

It contains one or more per-operation results. Each operation result binds:

- operation ID
- receipt status
- applicable authority-domain IDs
- selected grant IDs
- ordered delegation chain
- legal mechanisms
- selected repository-edit or runtime mechanism
- preserved runtime mechanisms that remain behavioral or proof obligations
- limitations

The top-level resolution does not become valid merely because one operation is
authorized. Every mutation-capable operation must have a valid per-operation
result within the same bound resolution.

#### Concrete delegation evidence is bound to the resolution

An operation result's `delegation_chain` is a list of delegation *ids* — it
asserts which delegations were relied on, but does not itself carry the evidence
to prove them. To keep the resolution independently verifiable, when a
resolution consumes delegated authority the `authority_resolved` ledger event
records the concrete `DelegationReceipt`s the resolver verified as their own
content-addressed artifact alongside the resolution, actor binding, change plan,
and base binding. Non-delegated resolutions record no such artifact and stay
byte-identical to a direct-grant event.

This makes the event self-verifiable rather than self-asserting. A later
consumer — in particular the Phase 6 certification authority lane — does not
trust the claimed chain. It resolves each `delegation_chain` id to a recorded
receipt, binds that receipt to a digest the actor committed to (in
`ActorBinding.delegation_receipt_digests_sha256`), and re-runs the single shared
monotonicity verdict against the *governed* grants and delegation policy loaded
independently from `docs/awareness/`. A resolution therefore cannot certify a
delegation the governed grants do not actually permit (any broadening of role,
domain, action, target, mechanism, risk ceiling, or validity window, or an
expired or revoked receipt, refuses), and cannot invent a delegation whose
concrete record was never preserved. Absent governed grants, delegated
authority fails closed. The monotonicity check is one function
(`authority.CheckDelegationForOperation`) so the resolver's admission gate and
the certifier's independent re-derivation can never drift apart.

## Closed operational vocabularies

Unknown values are invalid in v1 operational records.

### Actor kind

- `human`
- `agent`
- `service`
- `ci`
- `system`

### Operation kind

- `read`
- `create`
- `modify`
- `delete`
- `rename`
- `execute`
- `migrate`
- `rebuild`
- `observe`

#### Reserved but unsupported: repository rename

`rename` is a reserved operation kind in architectural-closure/v1. It is not
currently admissible for repository mutations because `ChangeOperation` does not
encode distinct source and destination targets. A rename must fail closed until
a later protocol version introduces exact endpoint representation and
verification. Implementations must not approximate rename as a single-target
operation or silently rewrite it as delete plus create.

A known vocabulary value is not the same as a supported operation in this schema
version: `rename` remains in the closed vocabulary (reserved for a later
protocol version), but the v1 change-plan schema rejects any `ChangeOperation`
whose `kind` is `rename`, and every v1 validation surface — change-plan,
admission-request, admission-v2 decision, and legacy scope-to-operation
synthesis — refuses it with `protocol.rename_requires_explicit_source_and_destination`.

Explicit user-authored `delete` and `create` operations remain legal when the
intended architecture truly is deletion followed by creation. They must not be
used as a compatibility encoding for rename. Automatic translation is forbidden
because rename can differ from delete-plus-create in architectural identity,
ownership, authority domain, operation budget, risk classification, historical
continuity, required tests, generated-artifact rules, and review interpretation.

When Git reports a rename in an observed change set, Phase 3 preserves both
endpoints diagnostically (`ObservedFile.FromPath` / `ToPath`) and scope
verification returns an invalid receipt with `scope.operation.rename_unsupported`
naming both paths; no `scope_verified` event is appended. A future compatible
protocol version may define rename as a typed operation carrying distinct
`source_target` and `destination_target` endpoints, with authority over both
endpoints, source-existence and destination-absence rules, cross-domain
ownership, overwrite policy, lineage continuity, exact observed-rename matching,
rollback, and proof requirements. Those semantics are deferred; v1 must not
guess them.

### Mechanism kind

- `repository_edit`
- `owner_rpc`
- `governed_workflow`
- `migration_runner`
- `generated_artifact_rebuild`
- `test_runner`
- `runtime_adapter`
- `manual_authorized`

### Evidence kind

- `static`
- `test`
- `runtime`
- `artifact`
- `review`
- `authority`
- `hybrid`

### Receipt status

- `valid`
- `invalid`
- `stale`
- `conflicted`
- `superseded`
- `revoked`
- `unknown`

### Certification verdict

- `certified`
- `certified_with_conditions`
- `review_required`
- `blocked`
- `uncertifiable`
- `stale`
- `revoked`

### Dimension status

- `pass`
- `pass_with_exception`
- `blocked`
- `unknown`
- `stale`
- `conflicted`
- `not_applicable`

### Task phase

- `prepared`
- `converging`
- `ready_for_admission`
- `admitted`
- `mutation_observed`
- `scope_verified`
- `proving`
- `certified`
- `completed`
- `waiting_architect`
- `waiting_evidence`
- `waiting_governance`
- `waiting_mechanical_repair`
- `refused`
- `stale`
- `uncertifiable`
- `abandoned`
- `revoked`

### Ledger event type

- `legacy_import`
- `task_prepared`
- `convergence_advanced`
- `closure_assessed`
- `admission_decided`
- `authority_resolved`
- `admission_consumed`
- `change_observed`
- `scope_verified`
- `evidence_recorded`
- `proof_discharged`
- `certified`
- `completed`
- `revoked`
- `migration_executed`
- `task_control_projected`
- `task_marked_stale`

## Task state machine

Primary path:

```text
prepared
  → converging
  → ready_for_admission
  → admitted
  → mutation_observed
  → scope_verified
  → proving
  → certified
  → completed
```

Legal side transitions:

```text
converging → waiting_architect
converging → waiting_evidence
converging → waiting_governance
converging → waiting_mechanical_repair

any nonterminal state → stale
any nonterminal state → uncertifiable
any nonterminal state → abandoned

certified → revoked
completed → revoked
```

Illegal examples:

```text
prepared → completed
admitted → certified
scope_verified → completed
completed → admitted
revoked → completed
```

Frozen rules:

1. `completed` is terminal for one session.
2. A new mutation after certification creates a new result binding.
3. A completed task is never silently reopened.
4. Revocation creates a new ledger event and usually a remediation task.
5. Waiting states do not erase prior receipts.
6. Re-entering the primary path preserves the same task identity and uses a new
   iteration or session where policy requires it.

## Closure predicate

The v1 closure predicate evaluates ten dimensions:

- `identity`
- `scope`
- `direction`
- `authority`
- `mutation`
- `protection`
- `epistemic`
- `proof`
- `freshness`
- `completion`

Full architectural closure is true only when all required dimensions satisfy
their policy.

The mandatory dimensions for a completed mutation task may not be
`not_applicable` for:

- `identity`
- `scope`
- `authority`
- `mutation`
- `epistemic`
- `freshness`
- `completion`

`pass_with_exception` is valid only when:

- completion policy permits the exception
- the exact waiver exists
- the waiver is valid and unexpired
- the waiver applies to the exact dimension or obligation

Unsafe states for a mandatory dimension are:

- `blocked`
- `unknown`
- `stale`
- `conflicted`

## Canonicalization and digests

Semantic identity is canonical JSON, not authoring YAML.

### Semantic payload digest

```text
SHA-256(canonical JSON payload)
```

Canonical JSON rules:

- UTF-8
- object keys sorted lexicographically
- no insignificant whitespace
- normalized booleans and numbers
- duplicate object keys rejected
- set-like arrays sorted and deduplicated
- sequence arrays preserve declared order
- empty optional values omitted consistently
- absolute input paths excluded
- generated display text excluded
- a receipt digest field is omitted from the digest of that same receipt

### Set-like arrays

Examples:

- `roles`
- `authority_domain_ids`
- `grant_ids`
- `delegation_ids`
- `evidence_receipt_ids`
- `artifact_receipt_ids`
- `reason_codes`
- `limitations`
- `conflicts_with`

### Ordered arrays

Examples:

- migration steps
- delegation chain
- ledger sequence
- repair steps
- state-transition history

### Time fields

Time participates in semantic identity only when time changes meaning.

Included:

- `observed_at`
- `expires_at`
- `valid_from`
- `valid_until`
- waiver expiry
- `completed_at`
- `revoked_at`

Excluded:

- `generated_at`
- `report_path`
- `source_input_path`
- `command_hint`

### Ledger integrity digest

The future ledger integrity digest covers:

- sequence
- previous entry digest
- event type
- task ID
- session ID
- payload reference
- producer
- produced_at

It excludes only the ledger entry digest field itself.

This is tamper evidence, distinct from semantic payload identity.

### Ledger payload reference

A ledger entry does not embed mutable task state directly. It points to one
content-addressed payload artifact with:

- repository-confined relative path
- media type
- semantic payload digest

The payload reference participates in the ledger integrity digest.

The payload digest is the semantic payload digest of the typed canonical
payload. It is not a display-path digest and it is not a substitute for the
entry integrity digest.

## Operational records

Every v1 operational record has a closed schema with
`additionalProperties: false`.

The record set includes:

- ActorBinding
- AuthenticationReceipt
- RoleAttestationReceipt
- DelegationReceipt
- ChangePlan
- ChangeOperation
- AuthorityResolution
- AdmissionRequest
- AdmissionDecision
- CapabilityConsumption
- LedgerEntry
- EvidenceProfile
- EvidenceReceipt
- ProofDischarge
- CertificationReceipt
- CompletionReceipt
- WaiverReceipt
- RevocationReceipt
- MigrationExecutionReceipt

## Capability consumption workflow

The single-use mutation capability is consumed **before** the mutation, never
retroactively during verification. The operational sequence for an
architecture-sensitive repository change is:

```text
sensei advance-task       -> ready_for_mutation
sensei consume-admission  -> spends the single-use capability for this exact
                             operation set (admission_consumed)
<agent applies the mutation in the working tree>
sensei verify-admission   -> records the exact observed change (change_observed)
                             then verifies scope (scope_verified)
```

`consume-admission` is a distinct, explicit protocol action. `verify-admission`
does **not** consume the capability: it requires an existing `admission_consumed`
receipt and fails closed when none is present. At `ready_for_mutation`,
`advance-task` reports the single next legal command — `consume-admission` — so
the consumption step is never hidden. verify-admission observes the working tree
by default (staged and unstaged changes against the admitted base); an explicit
`--result-revision` observes that exact committed revision instead. A later
protocol version may let a hook call consumption automatically at the actual
edit boundary, but consumption remains an explicit, visible transition in v1.

## Ontology classes and relationships

Phase 0 adds stable governed classes for reusable protocol vocabulary:

- `ActorRole`
- `AuthorityGrant`
- `DelegationPolicy`
- `RuntimeTargetKind`
- `EvidenceProfile`
- `GovernanceException`
- `MigrationPlan`
- `CertificationPolicy`
- `CompletionPolicy`
- `RevocationPolicy`

Phase 0 also adds operational projection classes:

- `Task`
- `TaskSession`
- `RepositorySnapshot`
- `GraphSnapshot`
- `RuntimeTarget`
- `ActorBinding`
- `AuthenticationReceipt`
- `RoleAttestationReceipt`
- `DelegationReceipt`
- `ClosureAssessment`
- `ClosureBlocker`
- `Abstention`
- `ChangePlan`
- `ChangeOperation`
- `AuthorityResolution`
- `AdmissionRequest`
- `AdmissionDecision`
- `CapabilityConsumption`
- `MutationReceipt`
- `ProbeResult`
- `EvidenceReceipt`
- `TestReceipt`
- `RuntimeObservationReceipt`
- `ArtifactReceipt`
- `ProofDischarge`
- `CertificationReceipt`
- `CompletionReceipt`
- `WaiverReceipt`
- `RevocationReceipt`
- `MigrationExecutionReceipt`

Each relationship added to the ontology must define:

- domain
- range
- meaning
- whether it grants authority
- whether it is governed or operational
- whether it may appear in authored sources
- whether it participates in semantic identity

## Backward-compatibility contract

Phase 0 documents, but does not fully implement, legacy interpretation:

- Admission v1 may be read as legacy scope admission.
- `requested_by` remains display-only.
- Existing task-session YAML is not a ledger.
- Existing `RuntimeEvidence` is interpreted as an evidence profile.
- Existing `Evidence` records retain explicit legacy semantics.
- `ActionCompleteTask` remains advisory.
- Historical success is not retroactive certification.
- Legacy imports must carry limitations.
- v1 readers never treat incomplete legacy records as v1-complete closure
  records.
- Legacy raw role strings remain guidance only until validated through a typed
  attestation path.

Historical labels allowed by v1 interpretation:

- `historically_successful`
- `scope_verified_legacy`
- `certification_unavailable_legacy`

## Admission limitation for Phase 0

Phase 0 creates new files while existing admission is primarily built around
existing read and modify paths.

Phase 0 must therefore be described honestly:

- it is governed by the existing reasoning and review process
- new-file creation predates Admission v2 create-operation support
- the resulting commits are not retroactively certified

Phase 0 is the protocol freeze, not proof that the repository is already fully
self-hosting that protocol.
