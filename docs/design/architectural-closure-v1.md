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
- payload digest
- producer
- produced_at

It excludes only the ledger entry digest field itself.

This is tamper evidence, distinct from semantic payload identity.

## Operational records

Every v1 operational record has a closed schema with
`additionalProperties: false`.

The record set includes:

- ActorBinding
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
