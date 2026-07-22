# Transactional Publication Report: `.sensei/project`

This report documents the implementation and verification of transactional, crash-recoverable, and generation-consistent publication of the `.sensei/project` family.

## Changed Files

1. **[cmd/awg/cmd_import.go](file:///home/dave/Documents/github.com/globulario/sensei/cmd/awg/cmd_import.go)**
   - Modified `classifyActiveProjectFamily` to:
     - Read and validate files relative to target directory `projectDir` instead of repository root `repoRoot` (enabling offline validation of staging/fixture dirs).
     - Populate detailed status reports to prevent accidental match rejections.
   - Replaced `reconstructImportedProject` and `activateProjectGeneration` with a unified transaction publication flow:
     - **Transaction Locks**: Added a kernel-level file lock (`project.lock` using `syscall.Flock`) to serialize or refuse concurrent publishers, preventing overwrite of the marker, staging path, or active family.
     - **Transaction Recovery**: Initiated at the start of any reconstruction. Uses transaction markers (`tx-marker.yaml`) to clean up stale resources and roll back to the prior coherent authority or safely preserve invalid diagnostics.
     - **Durability Hardening**: Added fsync/sync operations to staged files, the staging directory, the transaction marker, and the parent `.sensei` directory.
     - **Failure Injection**: Hook seam (`injectFailure` and `testFailureHook`) added to intercept execution at 14 distinct boundary failure points.
     - **Revision Rechecking**: Guarantees transaction fails closed if the source repository revision shifts during reconstruction.

2. **[cmd/awg/cmd_import_test.go](file:///home/dave/Documents/github.com/globulario/sensei/cmd/awg/cmd_import_test.go)**
   - Added `TestProjectFamilyClassifier` validating the coherent fixture and all 23 distinct corruption cases.
   - Added `TestProjectPublication` covering boundary failure injection points and transactional recovery.
   - Added `TestProjectPublicationWithInvalidAndAbsentPrior` testing edge cases for recovery behaviors with invalid/absent initial configurations.
   - Added `TestProjectPublicationRevisionChange` validating that revision divergence during staging fails closed.
   - Added `TestProjectPublicationConcurrency` verifying that concurrent reconstruction attempts are safely refused using kernel locks.

---

## 1. Classifier Matrix Result

All **23 corruption cases** were constructed programmatically from a freshly reconstructed coherent family. Every case successfully classified as `invalid` with the expected reason:

| Case | Mutation Type | Expected Reason | Actual Classification Status & Reason |
|:---|:---|:---|:---|
| **Baseline** | None (freshly reconstructed) | `coherent` | **coherent** |
| **Malformed Graph** | Overwrote `graph.nt` with invalid NTriples | `graph` | **invalid** (`graph`) |
| **Graph Digest Mismatch** | Mutated `FinalGraphDigestSHA256` in receipt | `graph` | **invalid** (`graph`) |
| **Missing Graph** | Deleted `graph.nt` from disk | `digest` | **invalid** (`digest`) |
| **Missing Claims** | Deleted `claims.yaml` from disk | `digest` | **invalid** (`digest`) |
| **Missing Audit** | Deleted `claim-audit.yaml` from disk | `digest` | **invalid** (`digest`) |
| **Missing Readiness** | Deleted `readiness.yaml` from disk | `digest` | **invalid** (`digest`) |
| **Missing Adoption** | Deleted `knowledge/adoption-report.yaml` from disk | `digest` | **invalid** (`digest`) |
| **Duplicate Receipt Path** | Appended a duplicate entry in receipt `artifacts` | `path` | **invalid** (`path`) |
| **Non-Canonical Path** | Changed entry path prefix to empty | `path` | **invalid** (`path`) |
| **Path Traversal** | Injected `../` path traversal into receipt path | `path` | **invalid** (`path`) |
| **Artifact Digest Mismatch** | Tampered with `sha256_digest` of an artifact in receipt | `digest` | **invalid** (`digest`) |
| **Claims Domain Mismatch** | Mutated `Binding.RepositoryDomain` in claims | `claims` | **invalid** (`claims`) |
| **Claims Revision Mismatch** | Mutated `Binding.Revision` in claims | `claims` | **invalid** (`claims`) |
| **Unresolved Claims Revision** | Changed `Binding.RevisionStatus` to unresolved | `claims` | **invalid** (`claims`) |
| **Claims Graph Mismatch** | Mutated `Binding.GraphDigestSHA256` in claims | `claims` | **invalid** (`claims`) |
| **Unresolved Claims Graph** | Changed `Binding.GraphDigestStatus` to unresolved | `claims` | **invalid** (`claims`) |
| **Readiness Domain Mismatch** | Mutated `RepositoryDomain` in readiness | `readiness` | **invalid** (`readiness`) |
| **Readiness Graph Mismatch** | Mutated `GraphDigestSHA256` in readiness | `readiness` | **invalid** (`readiness`) |
| **Readiness State Mismatch** | Mutated `State` in readiness | `readiness` | **invalid** (`readiness`) |
| **Readiness Graph Path** | Mutated `GraphPath` in readiness | `readiness` | **invalid** (`readiness`) |
| **Readiness Claims Path** | Mutated `ClaimsPath` in readiness | `readiness` | **invalid** (`readiness`) |
| **Readiness Audit Path** | Mutated `ClaimAuditPath` in readiness | `readiness` | **invalid** (`readiness`) |
| **Readiness Adoption Path** | Mutated `AdoptionReportPath` in readiness | `readiness` | **invalid** (`readiness`) |

---

## 2. Failure-Injection & Crash-Recovery Matrix

Each boundary failure point was deterministically simulated in isolation. In all cases, recovery successfully restored the correct final state:

- **Phase `staging` / `validated`**:
  - *Simulation*: Injected failure during files generation or validation checks.
  - *Result*: Prior active family remains active on disk, stale staging directories are deleted, marker is cleaned up.
- **Phase `prior_moved`**:
  - *Simulation*: Injected failure after prior family is backed up but before staging renamed to active.
  - *Result*:
    - If prior was **coherent**: Prior family restored back to active `.sensei/project`, staging deleted, marker cleaned.
    - If prior was **invalid**: Legacy invalid family remains diagnostic in `.sensei/project-invalid-<txID>`, active is left absent, staging deleted, marker cleaned.
    - If prior was **absent**: Active left absent, staging deleted, marker cleaned.
- **Phase `activated`**:
  - *Simulation*: Staging renamed to active but process interrupted before marker cleanup.
  - *Result*: Active directory validated. If coherent, backup removed, marker deleted. If invalid, rolled back to prior coherent or left absent.

---

## 3. Serialization and Lock Refusal

Concurrent publication attempts are explicitly refused. Using kernel-level file locking (`syscall.Flock`), a second publisher trying to run `reconstructImportedProject` while another holds the lock will immediately exit with `concurrent reconstruction in progress (lock busy)` without corrupting/overwriting the active marker, the active family, or staging paths.

---

## 4. Test Results Summary

All verification command suites run on the local filesystem completed successfully:

### Repeated Matrix Run (`-count=10`)
Detects test-fixture state leakage or nondeterminism:
```bash
go test ./cmd/awg -run 'ProjectFamilyClassifier|ProjectPublication' -count=10
```
- **Result**: `ok  github.com/globulario/sensei/cmd/awg  272.056s` (PASSED)

### Race Detector Run (`-race`)
Ensures no concurrency issues or data races:
```bash
go test -race ./cmd/awg -run 'ProjectFamilyClassifier|ProjectPublication' -count=1
```
- **Result**: `ok  github.com/globulario/sensei/cmd/awg  100.338s` (PASSED)

### Concurrency Lock Test
```bash
go test ./cmd/awg -run 'ProjectPublicationConcurrency' -count=1
```
- **Result**: `ok  github.com/globulario/sensei/cmd/awg  1.154s` (PASSED)

### Full awg Package Run
```bash
go test ./cmd/awg -count=1
```
- **Result**: `ok  github.com/globulario/sensei/cmd/awg  78.893s` (PASSED)

### Full Repository Run
```bash
go test ./... -timeout 120s
```
- **Result**: `ok` (All tests across the repository packages completed and PASSED)

---

## 5. Commit SHA
The work is committed as a single transactional repair commit:
- **Commit SHA**: `4d6cfea0590e1f079112795796ed24c7ec4add36`
