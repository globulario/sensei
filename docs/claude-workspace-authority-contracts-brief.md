# Claude implementation brief: canonical workspace authority contracts

Status: implementation authorized on `feat/workspace-identity-admission-contracts` only  
Architect contract: `docs/design/workspace-identity-admission-contracts.md`  
Base SHA: `4691b9977469285c234e529189068bd528aebed5`  
Initial contract commit: `e1985d74b9349b4f90e3a54e3c0312d177a5d239`  
Target repository: `globulario/sensei`  
Implementer: Claude

## 1. Mission

Implement the accepted contract exactly enough to make Sensei the canonical producer of:

- `sensei.workspace.identity.v1`;
- `sensei.workspace.admission.v1`;
- `sensei_workspace_status`;
- canonical workspace admission/verification MCP tools that delegate to the existing admission owners.

This PR closes the Sensei-core half of Sensei Dashboard Workspace Phase O1. It does not begin the runner or any O2 behavior.

## 2. Read first

Before editing, read all of:

1. `docs/design/workspace-identity-admission-contracts.md`
2. `docs/api-reference.md`, especially the MCP tool table and the note that task/admission tools are local
3. `docs/design/checkout-repository-domain-binding.md`
4. `cmd/awareness-mcp/main.go`
5. `golang/architecture/claim_document.go`
6. `golang/architecture/binding/`
7. `golang/architecture/admission/admission.go`
8. `golang/architecture/tasksession/`
9. `golang/server/metadata.go`
10. `proto/awareness_graph.proto` definitions for `GraphAuthority`, coverage, seed, provenance, and freshness
11. `golang/architecture/dashboardprojection/` as the nearest established schema/type/fixture/validation precedent
12. the merged O1 evidence in `globulario/sensei-dashboard` PR #11 and `docs/architecture-workspace-contracts-v1.md` there

Use Sensei's own briefing/preflight/admission workflow for the files you change. Use explicit repository domain `github.com/globulario/sensei` where required.

## 3. Deliverables

### Deliverable A: canonical schemas

Add the two Draft 2020-12 schemas under `docs/schemas/workspace/v1/` with exact schema-version constants:

```text
sensei.workspace.identity.v1
sensei.workspace.admission.v1
```

The schemas must implement the accepted contract, remain closed, and contain downstream pinning/ownership comments.

Do not copy the Dashboard-local schemas back into Sensei. The Dashboard records are orchestration records; these are Sensei-owned truth receipts.

### Deliverable B: producer package

Add one bounded package under `golang/architecture/workspacecontract/` or an architecturally equivalent location.

It must provide:

- typed identity and admission records;
- normalization;
- real schema validation;
- identity composition from existing owners;
- admission decision projection;
- admission verification projection bound to the exact original decision;
- no copied freshness, task, closure, admission, or correctness decision logic.

Prefer reuse over extraction churn. A small generic schema-validation helper may be extracted from an existing package only when the extraction is mechanical and fully covered; do not refactor unrelated contract packages.

### Deliverable C: canonical fixtures and adversarial proof

Add the positive fixtures required by the contract and test every fixture against both the real schema and the typed producer validator.

Add adversarial tests for closedness, malformed digests, illegal state combinations, invented runner-owned fields, decision/verification mismatch, and false correctness implication.

Fixtures must be normalized and deterministic. Report their exact SHA-256 digests in the final handoff.

### Deliverable D: `sensei_workspace_status`

Add the safe read-only MCP tool with runtime argument enforcement.

Hard rules:

- `repo` is required;
- `task` is optional;
- no domain override is accepted;
- configured `.sensei/config.yaml` identity is mandatory for a complete receipt;
- environment variables and store contents never establish governed checkout identity;
- the output contains no filesystem root, root hash, MCP session, job, worktree, or provider identity;
- metadata/backend failure is unavailable, not empty;
- the canonical receipt is schema-validated before return.

Use existing revision, tree-binding, domain-config, metadata, and task-session owners. Do not create parallel Git or YAML parsing.

### Deliverable E: canonical workspace admission tools

Add new tools, using the names in the contract unless an existing naming collision is proven:

```text
sensei_workspace_admit_change
sensei_workspace_verify_admission
```

They may mirror the existing tools' input arguments, but must:

- enforce runtime types and reject unknown properties;
- delegate exactly once to `admission.Evaluate` or `admission.Verify`;
- project into `sensei.workspace.admission.v1`;
- for verification, load and preserve the exact referenced decision identity and binding;
- schema-validate before return;
- leave the existing `admit_change` and `verify_admission` tools unchanged.

Do not add another policy engine, permission layer, or correctness gate.

### Deliverable F: documentation and awareness records

Update `docs/api-reference.md` and relevant bridge comments.

Add or update governed awareness records only where required to capture durable laws, failure modes, forbidden shortcuts, and proving tests introduced by this feature. Regenerate owned generated artifacts using the repository's canonical commands. Do not hand-edit generated awareness outputs.

## 4. Required behavioral laws

The implementation must preserve all of these:

1. Sensei owns repository/graph/task/admission truth.
2. The runner will own process/worktree/job/provider identity later.
3. Workspace identity is not admission.
4. Admission is permission to attempt, not correctness.
5. Scope compliance is not correctness certification.
6. Configured MCP is not verified workspace identity.
7. Unknown, unavailable, not requested, stale, refused, violated, and uncertifiable remain distinct.
8. Configured checkout domain is authority; environment/store order is not.
9. Existing typed owners are called, not reimplemented.
10. Every external object is closed and schema-validated before publication.

## 5. Forbidden scope

Do not add or modify:

- `sensei-runner`;
- Tauri;
- local IPC or listeners;
- provider SDKs or authentication;
- process/PTY execution;
- worktree creation or cleanup;
- GitHub API writes;
- Dashboard code;
- a new gRPC RPC or proto message;
- a new admission policy/outcome;
- a repository-root identity or MCP session identity;
- automatic architectural approval, completion, or merge behavior;
- O2+ code.

Do not change existing `admit_change` / `verify_admission` structured outputs in this PR.

## 6. Implementation sequence

Use this order unless evidence requires an `ARCHITECT QUESTION`:

1. map every external field to its existing owner and record that mapping in code comments/tests;
2. add and validate schemas;
3. add typed records and normalization;
4. add fixtures and adversarial schema tests;
5. add identity composition tests before MCP wiring;
6. add admission projection tests before MCP wiring;
7. add the three MCP tools and JSON-RPC integration proof;
8. update documentation/awareness;
9. run full repository verification;
10. push and wait for CI;
11. post an exact-head implementation handoff and stop.

## 7. Required tests

At minimum, implement the test matrix in the architect contract. Additionally prove:

- tool discovery returns closed schemas for all three tools;
- JSON `null`, numbers, arrays, and objects do not silently coerce into string arguments;
- an unknown argument fails at runtime;
- an unconfigured checkout cannot produce `composition_state=complete` even when the graph contains only one domain;
- `SENSEI_DOMAIN` and `AWG_DOMAIN` do not change workspace identity output;
- a current configured checkout and current graph produce a complete identity fixture;
- stale graph, missing task, and unreachable backend each remain distinguishable;
- verification cannot be projected from a decision artifact with a mismatched digest or binding;
- existing MCP admission tests continue to pass without expected-output updates caused by this feature.

## 8. Verification commands

Run the repository's current canonical equivalents of all of these and report exact results:

```bash
gofmt -l .
go build ./...
go vet ./...
go test ./...
sensei check
make proto-contracts-check
make import-graph-check
scripts/build-awareness-graph-self.sh --check
```

Also run focused tests for:

```text
golang/architecture/workspacecontract
cmd/awareness-mcp
real Draft 2020-12 validation of every workspace fixture
MCP JSON-RPC discovery/status/admit/verify flow
```

If a named command no longer exists, use the current repository equivalent and document the substitution. Do not silently skip it.

## 9. Stop conditions

Post `ARCHITECT QUESTION` and stop the affected portion before:

- inventing any field that lacks a 1:1 Sensei owner;
- accepting environment or store-derived checkout identity;
- adding a new proto/gRPC surface;
- weakening schema closedness;
- dropping decision/verification reasons, limitations, identity, or scope facts;
- duplicating admission/freshness/task logic;
- changing the compatibility behavior of existing MCP tools;
- treating a runner-owned fact as Sensei-owned;
- declaring correctness because admission or verification succeeded.

Continue unrelated deliverables only when they remain valid and independent. Do not conceal a blocked portion behind a local substitute.

## 10. Handoff protocol

When implementation is complete:

1. push all changes to this PR branch;
2. wait for all GitHub Actions checks on the exact head SHA;
3. post `IMPLEMENTATION READY FOR ARCHITECT REVIEW`;
4. include the complete evidence template from the architect contract;
5. list every changed file and any bounded deviations;
6. state explicitly whether either canonical schema or tool differs from the accepted contract and why;
7. stop. Do not merge.

The human maintainer retains merge authority. The architect reviews the exact handed-off SHA and either approves it or returns bounded corrections.
