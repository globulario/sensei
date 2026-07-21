// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/graphbuild"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/taskcontrol"
	"github.com/globulario/sensei/golang/rdf"
	"gopkg.in/yaml.v3"
)

func TestTaskIDIsDeterministicAndIgnoresRequester(t *testing.T) {
	req := sampleTaskRequest()
	first := StableTaskID(req)
	second := StableTaskID(req)
	if first == "" || first != second {
		t.Fatalf("stable task id = %q then %q", first, second)
	}
	req.RequestedBy = "human"
	if got := StableTaskID(req); got != first {
		t.Fatalf("requester changed task id: %s -> %s", first, got)
	}
}

func TestTaskIDChangesWithBindingScopeAndDescription(t *testing.T) {
	req := sampleTaskRequest()
	base := StableTaskID(req)
	req.Binding.Revision = strings.Repeat("2", 40)
	if got := StableTaskID(req); got == base {
		t.Fatal("revision change did not change task id")
	}
	req = sampleTaskRequest()
	req.Binding.GraphDigestSHA256 = strings.Repeat("b", 64)
	if got := StableTaskID(req); got == base {
		t.Fatal("graph digest change did not change task id")
	}
	req = sampleTaskRequest()
	req.Scope.Files = append(req.Scope.Files, FileOperation{Path: "extra.go", Operation: admission.OperationModify})
	if got := StableTaskID(req); got == base {
		t.Fatal("scope change did not change task id")
	}
	req = sampleTaskRequest()
	req.Description = "different task"
	if got := StableTaskID(req); got == base {
		t.Fatal("description change did not change task id")
	}
}

func TestPrepareChangeCreatesCanonicalLayoutAndActivePointer(t *testing.T) {
	repo, graph := testRepo(t)
	res, err := Prepare(PrepareOptions{
		RepoRoot:             repo,
		RepositoryDomain:     "github.com/example/project",
		Description:          "Ensure literal colon routes resolve consistently.",
		Mode:                 admission.ModeModify,
		TaskClass:            "literal_colon_route_consistency",
		RiskClass:            closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve,
		Files: []FileOperation{
			{Path: "gin.go", Operation: admission.OperationModify},
			{Path: "gin_test.go", Operation: admission.OperationModify},
		},
		GraphNT:   graph,
		SetActive: true,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if res.TaskID == "" || res.Next.Action == "" {
		t.Fatalf("incomplete result: %+v", res)
	}
	taskDir := filepath.Join(repo, filepath.FromSlash(res.TaskDir))
	for _, rel := range []string{
		"ledger/HEAD.yaml",
		"projections/session.yaml",
		"projections/task-control.yaml",
		"projections/status.yaml",
		"projections/manifest.yaml",
		"session.yaml",
		"task-request.yaml",
		"closure-request.yaml",
		"source/claims.yaml",
		"source/dialogue.yaml",
		"source/evidence-state.yaml",
		"source/knowledge/manifest.yaml",
		"source/graph.nt",
		"source/graph-receipt.yaml",
		"convergence/session.yaml",
		"admission/request.yaml",
		"admission/decision.yaml",
		"receipts/prepare-change.yaml",
		"receipts/task-status.yaml",
	} {
		if _, err := os.Stat(filepath.Join(taskDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}
	ptr, err := LoadActivePointer(repo)
	if err != nil {
		t.Fatalf("LoadActivePointer: %v", err)
	}
	if ptr.TaskID != res.TaskID {
		t.Fatalf("active task id = %s, want %s", ptr.TaskID, res.TaskID)
	}
	if ptr.RepositoryDomain != "github.com/example/project" || ptr.Revision == "" || ptr.GraphDigestSHA256 == "" || ptr.LastTaskControlDigestSHA256 == "" {
		t.Fatalf("active pointer is not fully bound: %+v", ptr)
	}
	if ptr.LedgerPath == "" || ptr.LedgerHeadDigestSHA256 == "" || ptr.LedgerSequence == 0 {
		t.Fatalf("active pointer missing ledger binding: %+v", ptr)
	}
	if filepath.IsAbs(ptr.SessionPath) || !strings.HasPrefix(ptr.SessionPath, ".sensei/tasks/") {
		t.Fatalf("active pointer path is not repo-relative task path: %s", ptr.SessionPath)
	}
	st, err := Status(StatusOptions{RepoRoot: repo, Active: true, Verify: true})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Verified {
		t.Fatalf("status did not verify: %+v", st.VerifyErrors)
	}
}

func TestPrepareChangeRecordsGraphInputSnapshot(t *testing.T) {
	repo, graph := testRepo(t)
	snap := &graphbuild.GraphInputSnapshot{
		PolicyID:         "sensei.resultpipeline.graph-inputs/v1",
		RepositoryDomain: "github.com/example/project",
		SourceRoots:      []graphbuild.LogicalSourceRoot{{LogicalPath: "docs/awareness", SkipNestedGenerated: true}},
	}
	res, err := Prepare(PrepareOptions{
		RepoRoot: repo, RepositoryDomain: "github.com/example/project",
		Description: "Bind graph inputs.", Mode: admission.ModeModify,
		TaskClass: "graph_input_binding", RiskClass: closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve,
		Files:                []FileOperation{{Path: "gin.go", Operation: admission.OperationModify}},
		GraphNT:              graph,
		GraphInputSnapshot:   snap,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	taskDir := filepath.Join(repo, filepath.FromSlash(res.TaskDir))
	data, found, err := admission.LoadLatestArtifactBytes(taskDir, closureprotocol.LedgerEventTaskPrepared, "graph_input_snapshot")
	if err != nil || !found {
		t.Fatalf("graph_input_snapshot artifact missing on task_prepared: found=%v err=%v", found, err)
	}
	var got graphbuild.GraphInputSnapshot
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal recorded snapshot: %v", err)
	}
	if err := graphbuild.ValidateGraphInputSnapshot(got); err != nil {
		t.Fatalf("recorded snapshot must validate: %v", err)
	}
	if got.SnapshotDigestSHA256 == "" || got.PolicyID != snap.PolicyID {
		t.Fatalf("recorded snapshot is not the authoritative one: %+v", got)
	}
}

func TestPrepareChangeWithoutSnapshotRecordsUnavailable(t *testing.T) {
	repo, graph := testRepo(t)
	res, err := Prepare(PrepareOptions{
		RepoRoot: repo, RepositoryDomain: "github.com/example/project",
		Description: "No graph inputs supplied.", Mode: admission.ModeModify,
		TaskClass: "graph_input_absent", RiskClass: closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve,
		Files:                []FileOperation{{Path: "gin.go", Operation: admission.OperationModify}},
		GraphNT:              graph,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	taskDir := filepath.Join(repo, filepath.FromSlash(res.TaskDir))
	if _, found, _ := admission.LoadLatestArtifactBytes(taskDir, closureprotocol.LedgerEventTaskPrepared, "graph_input_snapshot"); found {
		t.Fatal("no snapshot was supplied, but a graph_input_snapshot artifact was recorded")
	}
}

func TestPrepareChangeCreatesVerifiedLedger(t *testing.T) {
	repo, graph := testRepo(t)
	res, err := Prepare(PrepareOptions{
		RepoRoot:             repo,
		RepositoryDomain:     "github.com/example/project",
		Description:          "Inspect route ownership.",
		Mode:                 admission.ModeInspect,
		TaskClass:            "route_ownership",
		RiskClass:            closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve,
		Files:                []FileOperation{{Path: "gin.go", Operation: admission.OperationRead}},
		GraphNT:              graph,
		SetActive:            true,
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := ledger.VerifyTaskLedger(filepath.Join(repo, filepath.FromSlash(res.TaskDir)))
	if err != nil {
		t.Fatal(err)
	}
	// task_prepared, convergence_advanced, closure_assessed, task_control_projected.
	// No admission_decided: preparation records no typed decision (authority
	// precedes admission; the decision is produced later by admit-change).
	if !report.Valid || report.EntryCount != 4 || report.ProjectionState != "current" {
		t.Fatalf("unexpected ledger report: %+v", report)
	}
}

func TestPrepareChangeSnapshotsBootstrapDirectionAuthorization(t *testing.T) {
	repo, graph := testRepo(t)
	authPath := writeBootstrapDirectionAuthorization(t, repo, graph, TaskRequest{
		SchemaVersion: SchemaVersion,
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  "github.com/example/project",
			Revision:          gitHeadForTest(t, repo),
			RevisionStatus:    architecture.RevisionResolved,
			GraphDigestSHA256: fileDigest(t, graph),
			GraphDigestStatus: architecture.GraphDigestResolved,
		},
		Description:          "Bootstrap governed direction records.",
		Mode:                 admission.ModeModify,
		TaskClass:            "bootstrap_direction_records",
		RiskClass:            closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionEvolve,
		Scope:                TaskScope{Files: []FileOperation{{Path: closure.DirectionBootstrapFile, Operation: admission.OperationModify}}},
		RequestedBy:          "coding_agent",
	})
	res, err := Prepare(PrepareOptions{
		RepoRoot:             repo,
		RepositoryDomain:     "github.com/example/project",
		Description:          "Bootstrap governed direction records.",
		Mode:                 admission.ModeModify,
		TaskClass:            "bootstrap_direction_records",
		RiskClass:            closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionEvolve,
		Files: []FileOperation{
			{Path: closure.DirectionBootstrapFile, Operation: admission.OperationModify},
		},
		GraphNT:                         graph,
		DirectionBootstrapAuthorization: authPath,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	taskDir := filepath.Join(repo, filepath.FromSlash(res.TaskDir))
	if _, err := os.Stat(filepath.Join(taskDir, "governance", "bootstrap-direction-authorization.yaml")); err != nil {
		t.Fatalf("missing bootstrap direction authorization: %v", err)
	}
	req, err := closure.LoadRequest(filepath.Join(taskDir, "closure-request.yaml"))
	if err != nil {
		t.Fatalf("LoadRequest: %v", err)
	}
	if req.TaskID != res.TaskID || req.DirectionBootstrap == nil {
		t.Fatalf("bootstrap receipt not bound into closure request: %+v", req)
	}
	if req.DirectionBootstrap.File != closure.DirectionBootstrapFile {
		t.Fatalf("bootstrap file = %s", req.DirectionBootstrap.File)
	}
}

func TestPrepareChangeRejectsUnknownBootstrapApprovalMechanism(t *testing.T) {
	repo, graph := testRepo(t)
	req := TaskRequest{
		SchemaVersion: SchemaVersion,
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  "github.com/example/project",
			Revision:          gitHeadForTest(t, repo),
			RevisionStatus:    architecture.RevisionResolved,
			GraphDigestSHA256: fileDigest(t, graph),
			GraphDigestStatus: architecture.GraphDigestResolved,
		},
		Description:          "Bootstrap governed direction records.",
		Mode:                 admission.ModeModify,
		TaskClass:            "bootstrap_direction_records",
		RiskClass:            closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionEvolve,
		Scope:                TaskScope{Files: []FileOperation{{Path: closure.DirectionBootstrapFile, Operation: admission.OperationModify}}},
		RequestedBy:          "coding_agent",
	}
	req.TaskID = StableTaskID(req)
	auth := closure.DirectionBootstrapAuthorization{
		SchemaVersion:                closure.DirectionBootstrapSchemaVersion,
		PolicyID:                     closure.DirectionBootstrapPolicyID,
		TaskID:                       req.TaskID,
		BaseRevision:                 req.Binding.Revision,
		GraphDigestSHA256:            req.Binding.GraphDigestSHA256,
		File:                         closure.DirectionBootstrapFile,
		GovernedRecordIDs:            []string{"decision.desired"},
		ExpectedMutationDigestSHA256: strings.Repeat("c", 64),
		ApprovedBy:                   "Dave",
		ApprovalMechanism:            "human_review",
		ApprovalStatement:            "bootstrap once",
		UsagePolicy:                  closure.DirectionBootstrapUsageOneUse,
		IssuedAt:                     "2026-07-15T00:00:00Z",
		ExpiresAt:                    "2026-07-16T00:00:00Z",
	}
	auth.ApprovalMechanism = closure.DirectionBootstrapMechanismFile
	data, err := closure.MarshalCanonicalDirectionBootstrapYAML(auth)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), closure.DirectionBootstrapMechanismFile, "human_review", 1))
	authPath := writeExternalBootstrapDirectionAuthorizationFile(t, data)
	_, err = Prepare(PrepareOptions{
		RepoRoot:                        repo,
		RepositoryDomain:                "github.com/example/project",
		Description:                     "Bootstrap governed direction records.",
		Mode:                            admission.ModeModify,
		TaskClass:                       "bootstrap_direction_records",
		RiskClass:                       closure.RiskArchitectureSensitive,
		DirectionRequirement:            closure.DirectionEvolve,
		Files:                           []FileOperation{{Path: closure.DirectionBootstrapFile, Operation: admission.OperationModify}},
		GraphNT:                         graph,
		DirectionBootstrapAuthorization: authPath,
	})
	if err == nil || !strings.Contains(err.Error(), "approval_mechanism is unknown") {
		t.Fatalf("expected approval mechanism rejection, got %v", err)
	}
}

func TestPrepareChangeRejectsConsumedBootstrapDirectionAuthorization(t *testing.T) {
	repo, graph := testRepo(t)
	authPath := writeBootstrapDirectionAuthorization(t, repo, graph, TaskRequest{
		SchemaVersion: SchemaVersion,
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  "github.com/example/project",
			Revision:          gitHeadForTest(t, repo),
			RevisionStatus:    architecture.RevisionResolved,
			GraphDigestSHA256: fileDigest(t, graph),
			GraphDigestStatus: architecture.GraphDigestResolved,
		},
		Description:          "Bootstrap governed direction records.",
		Mode:                 admission.ModeModify,
		TaskClass:            "bootstrap_direction_records",
		RiskClass:            closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionEvolve,
		Scope:                TaskScope{Files: []FileOperation{{Path: closure.DirectionBootstrapFile, Operation: admission.OperationModify}}},
		RequestedBy:          "coding_agent",
	})
	first, err := Prepare(PrepareOptions{
		RepoRoot:                        repo,
		RepositoryDomain:                "github.com/example/project",
		Description:                     "Bootstrap governed direction records.",
		Mode:                            admission.ModeModify,
		TaskClass:                       "bootstrap_direction_records",
		RiskClass:                       closure.RiskArchitectureSensitive,
		DirectionRequirement:            closure.DirectionEvolve,
		Files:                           []FileOperation{{Path: closure.DirectionBootstrapFile, Operation: admission.OperationModify}},
		GraphNT:                         graph,
		DirectionBootstrapAuthorization: authPath,
	})
	if err != nil {
		t.Fatalf("first Prepare: %v", err)
	}
	taskDir := filepath.Join(repo, filepath.FromSlash(first.TaskDir))
	req, err := closure.LoadRequest(filepath.Join(taskDir, "closure-request.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	receipt := admission.BootstrapDirectionConsumption{
		TaskID:                     req.TaskID,
		AdmissionID:                "admission.one",
		VerificationDigestSHA256:   strings.Repeat("d", 64),
		AuthorizationDigestSHA256:  req.DirectionBootstrap.AuthorizationDigestSHA256,
		ApprovalSourcePath:         req.DirectionBootstrap.ApprovalSourcePath,
		ApprovalSourceDigestSHA256: req.DirectionBootstrap.ApprovalSourceDigestSHA256,
		ConsumedAt:                 "2026-07-15T12:00:00Z",
	}
	data, err := admission.MarshalCanonicalBootstrapDirectionConsumptionYAML(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "receipts", "bootstrap-direction-consumption.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Prepare(PrepareOptions{
		RepoRoot:                        repo,
		RepositoryDomain:                "github.com/example/project",
		Description:                     "Bootstrap governed direction records.",
		Mode:                            admission.ModeModify,
		TaskClass:                       "bootstrap_direction_records",
		RiskClass:                       closure.RiskArchitectureSensitive,
		DirectionRequirement:            closure.DirectionEvolve,
		Files:                           []FileOperation{{Path: closure.DirectionBootstrapFile, Operation: admission.OperationModify}},
		GraphNT:                         graph,
		DirectionBootstrapAuthorization: authPath,
	})
	if err == nil || !strings.Contains(err.Error(), "already consumed") {
		t.Fatalf("expected reuse rejection, got %v", err)
	}
}

func TestPrepareChangeRejectsBootstrapDirectionAuthorizationTaskMismatch(t *testing.T) {
	repo, graph := testRepo(t)
	req := TaskRequest{
		SchemaVersion: SchemaVersion,
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  "github.com/example/project",
			Revision:          gitHeadForTest(t, repo),
			RevisionStatus:    architecture.RevisionResolved,
			GraphDigestSHA256: fileDigest(t, graph),
			GraphDigestStatus: architecture.GraphDigestResolved,
		},
		Description:          "Bootstrap governed direction records.",
		Mode:                 admission.ModeModify,
		TaskClass:            "bootstrap_direction_records",
		RiskClass:            closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionEvolve,
		Scope:                TaskScope{Files: []FileOperation{{Path: closure.DirectionBootstrapFile, Operation: admission.OperationModify}}},
		RequestedBy:          "coding_agent",
	}
	req.Description = "Different bootstrap task."
	authPath := writeBootstrapDirectionAuthorization(t, repo, graph, req)
	_, err := Prepare(PrepareOptions{
		RepoRoot:                        repo,
		RepositoryDomain:                "github.com/example/project",
		Description:                     "Bootstrap governed direction records.",
		Mode:                            admission.ModeModify,
		TaskClass:                       "bootstrap_direction_records",
		RiskClass:                       closure.RiskArchitectureSensitive,
		DirectionRequirement:            closure.DirectionEvolve,
		Files:                           []FileOperation{{Path: closure.DirectionBootstrapFile, Operation: admission.OperationModify}},
		GraphNT:                         graph,
		DirectionBootstrapAuthorization: authPath,
	})
	if err == nil || !strings.Contains(err.Error(), "task_id") {
		t.Fatalf("expected task mismatch rejection, got %v", err)
	}
}

func TestPrepareChangeRejectsBootstrapDirectionAuthorizationGraphMismatch(t *testing.T) {
	repo, graph := testRepo(t)
	binding := architecture.ClaimDocumentBinding{
		RepositoryDomain:  "github.com/example/project",
		Revision:          gitHeadForTest(t, repo),
		RevisionStatus:    architecture.RevisionResolved,
		GraphDigestSHA256: fileDigest(t, graph),
		GraphDigestStatus: architecture.GraphDigestResolved,
	}
	req := TaskRequest{
		SchemaVersion:        SchemaVersion,
		Binding:              binding,
		Description:          "Bootstrap governed direction records.",
		Mode:                 admission.ModeModify,
		TaskClass:            "bootstrap_direction_records",
		RiskClass:            closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionEvolve,
		Scope:                TaskScope{Files: []FileOperation{{Path: closure.DirectionBootstrapFile, Operation: admission.OperationModify}}},
		RequestedBy:          "coding_agent",
	}
	req.TaskID = StableTaskID(req)
	auth := closure.DirectionBootstrapAuthorization{
		SchemaVersion:                closure.DirectionBootstrapSchemaVersion,
		PolicyID:                     closure.DirectionBootstrapPolicyID,
		TaskID:                       req.TaskID,
		BaseRevision:                 req.Binding.Revision,
		GraphDigestSHA256:            strings.Repeat("a", 64),
		File:                         closure.DirectionBootstrapFile,
		GovernedRecordIDs:            []string{"decision.desired"},
		ExpectedMutationDigestSHA256: strings.Repeat("c", 64),
		ApprovedBy:                   "architect",
		ApprovalMechanism:            closure.DirectionBootstrapMechanismFile,
		ApprovalStatement:            "bootstrap once",
		UsagePolicy:                  closure.DirectionBootstrapUsageOneUse,
		IssuedAt:                     "2026-07-15T00:00:00Z",
		ExpiresAt:                    "2026-07-16T00:00:00Z",
	}
	data, err := closure.MarshalCanonicalDirectionBootstrapYAML(auth)
	if err != nil {
		t.Fatal(err)
	}
	authPath := writeExternalBootstrapDirectionAuthorizationFile(t, data)
	_, err = Prepare(PrepareOptions{
		RepoRoot:                        repo,
		RepositoryDomain:                "github.com/example/project",
		Description:                     "Bootstrap governed direction records.",
		Mode:                            admission.ModeModify,
		TaskClass:                       "bootstrap_direction_records",
		RiskClass:                       closure.RiskArchitectureSensitive,
		DirectionRequirement:            closure.DirectionEvolve,
		Files:                           []FileOperation{{Path: closure.DirectionBootstrapFile, Operation: admission.OperationModify}},
		GraphNT:                         graph,
		DirectionBootstrapAuthorization: authPath,
	})
	if err == nil || !strings.Contains(err.Error(), "graph_digest_sha256 does not match") {
		t.Fatalf("expected graph mismatch rejection, got %v", err)
	}
}

func TestPrepareChangeWritesAwarenessMutationEnforcementForCanonicalSource(t *testing.T) {
	repo, graph := testRepo(t)
	writeFile(t, repo, "docs/awareness/architecture/components.yaml", "components:\n  - id: component.demo\n    name: Demo\n")
	graphData := strings.Join([]string{
		triple("https://globular.io/awareness#sourceFile/gin.go", rdf.PropType, rdf.ClassSourceFile, true),
		triple("https://globular.io/awareness#sourceFile/gin.go", rdf.PropSourcePath, "gin.go", false),
		triple("https://globular.io/awareness#sourceFile/gin_test.go", rdf.PropType, rdf.ClassSourceFile, true),
		triple("https://globular.io/awareness#sourceFile/gin_test.go", rdf.PropSourcePath, "gin_test.go", false),
		triple("https://globular.io/awareness#sourceFile/docs%2Fawareness%2Farchitecture%2Fcomponents.yaml", rdf.PropType, rdf.ClassSourceFile, true),
		triple("https://globular.io/awareness#sourceFile/docs%2Fawareness%2Farchitecture%2Fcomponents.yaml", rdf.PropSourcePath, "docs/awareness/architecture/components.yaml", false),
		"",
	}, "\n")
	if err := os.WriteFile(graph, []byte(graphData), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestProjectClaims(t, repo, graph)
	res, err := Prepare(PrepareOptions{
		RepoRoot:             repo,
		RepositoryDomain:     "github.com/example/project",
		Description:          "Update canonical component ownership.",
		Mode:                 admission.ModeModify,
		TaskClass:            "update_component_owner",
		RiskClass:            closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve,
		Files:                []FileOperation{{Path: "docs/awareness/architecture/components.yaml", Operation: admission.OperationModify}},
		GraphNT:              graph,
	})
	if err != nil {
		t.Fatal(err)
	}
	taskDir := filepath.Join(repo, filepath.FromSlash(res.TaskDir))
	if _, err := os.Stat(filepath.Join(taskDir, "source", "awareness-mutation-enforcement.yaml")); err != nil {
		t.Fatalf("missing awareness mutation plan: %v", err)
	}
	requestData, err := os.ReadFile(filepath.Join(taskDir, "closure-request.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		ArchitectureClosureRequest closure.Request `yaml:"architecture_closure_request"`
	}
	if err := yaml.Unmarshal(requestData, &env); err != nil {
		t.Fatal(err)
	}
	if env.ArchitectureClosureRequest.AwarenessMutation == nil {
		t.Fatal("awareness mutation binding missing from closure request")
	}
}

func TestLegacyActivePointerProjectsAsStaleInsteadOfFailingToLoad(t *testing.T) {
	repo, graph := testRepo(t)
	_, err := Prepare(PrepareOptions{
		RepoRoot: repo, RepositoryDomain: "github.com/example/project", Description: "inspect gin",
		Mode: admission.ModeInspect, TaskClass: "inspect_gin", RiskClass: closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve, Files: []FileOperation{{Path: "gin.go", Operation: admission.OperationRead}},
		GraphNT: graph, SetActive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ptr, err := LoadActivePointer(repo)
	if err != nil {
		t.Fatal(err)
	}
	ptr.RepositoryDomain, ptr.Revision, ptr.GraphDigestSHA256, ptr.LastTaskControlDigestSHA256 = "", "", "", ""
	data, err := yaml.Marshal(pointerEnvelope{ArchitectureActiveTask: ptr})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".sensei", "tasks", "active.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	state, _, err := ControlStatus(repo, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if state.BindingHealth != "stale" || state.NextAction.Kind != taskcontrol.ActionRepairBinding {
		t.Fatalf("state=%+v", state)
	}
}

func TestLegacySessionDigestProjectsAsStaleButCannotAdvance(t *testing.T) {
	repo, graph := testRepo(t)
	res, err := Prepare(PrepareOptions{
		RepoRoot: repo, RepositoryDomain: "github.com/example/project", Description: "inspect gin",
		Mode: admission.ModeInspect, TaskClass: "inspect_gin", RiskClass: closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve, Files: []FileOperation{{Path: "gin.go", Operation: admission.OperationRead}},
		GraphNT: graph, SetActive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(repo, filepath.FromSlash(res.TaskDir), "session.yaml")
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "session_digest_sha256:", "session_digest_sha256: legacy-", 1))
	if err := os.WriteFile(sessionPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	state, _, err := ControlStatus(repo, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if state.BindingHealth != "stale" || state.NextAction.Kind != taskcontrol.ActionRepairBinding {
		t.Fatalf("state=%+v", state)
	}
	if _, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, Active: true}); err == nil {
		t.Fatal("advance-task accepted a legacy session digest")
	}
}

func TestPrepareChangeConsumesProjectKnowledge(t *testing.T) {
	assertPrepareConsumesProjectKnowledge(t)
}

func TestPrepareChangePreservesAdoptionReceipts(t *testing.T) {
	assertPrepareConsumesProjectKnowledge(t)
}

func assertPrepareConsumesProjectKnowledge(t *testing.T) {
	t.Helper()
	repo, graph := testRepo(t)
	projectKnowledge := filepath.Join(repo, ".sensei", "project", "knowledge", "invariants.yaml")
	content := []byte("invariants:\n  - id: invariant.route_state\n    status: machine_adopted\n    decision_actor: sensei\n    decision_policy: adoption.invariant.corroborated.v1\n")
	if err := os.MkdirAll(filepath.Dir(projectKnowledge), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectKnowledge, content, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Prepare(PrepareOptions{
		RepoRoot: repo, RepositoryDomain: "github.com/example/project", Description: "Inspect route knowledge.",
		Mode: admission.ModeInspect, TaskClass: "route_knowledge", RiskClass: closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve, Files: []FileOperation{{Path: "gin.go", Operation: admission.OperationRead}}, GraphNT: graph,
	})
	if err != nil {
		t.Fatal(err)
	}
	copied, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(res.TaskDir), "source", "knowledge", "invariants.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(copied) != string(content) {
		t.Fatalf("adoption receipt bytes changed:\n%s\nwant:\n%s", copied, content)
	}
}

func TestPrepareChangeConsumesProjectEvidenceState(t *testing.T) {
	repo, graph := testRepo(t)
	claims, err := architecture.LoadClaimDocument(filepath.Join(repo, ".sensei", "project", "claims.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	evidence := maintenance.EvidenceStateDocument{
		SchemaVersion: "1", GeneratedBy: "test", Binding: claims.Binding,
		Evidence: []maintenance.EvidenceState{{ID: "route.test", Status: maintenance.EvidenceStatusPass, Freshness: maintenance.EvidenceFreshnessCurrent, Source: "go test ./..."}},
	}
	data, err := maintenance.MarshalCanonicalEvidenceStateYAML(evidence)
	if err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(repo, ".sensei", "project", "evidence-state.yaml")
	if err := os.WriteFile(projectPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Prepare(PrepareOptions{
		RepoRoot: repo, RepositoryDomain: "github.com/example/project", Description: "Inspect route Evidence.",
		Mode: admission.ModeInspect, TaskClass: "route_evidence", RiskClass: closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve, Files: []FileOperation{{Path: "gin.go", Operation: admission.OperationRead}}, GraphNT: graph,
	})
	if err != nil {
		t.Fatal(err)
	}
	copied, err := maintenance.LoadEvidenceStateDocument(filepath.Join(repo, filepath.FromSlash(res.TaskDir), "source", "evidence-state.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(copied.Evidence) != 1 || copied.Evidence[0].ID != "route.test" {
		t.Fatalf("project Evidence was not consumed: %+v", copied)
	}
}

func TestPrepareChangeReplayDoesNotDuplicateTask(t *testing.T) {
	repo, graph := testRepo(t)
	opts := PrepareOptions{
		RepoRoot:             repo,
		RepositoryDomain:     "github.com/example/project",
		Description:          "Inspect route ownership.",
		Mode:                 admission.ModeInspect,
		TaskClass:            "route_ownership",
		RiskClass:            closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve,
		Files:                []FileOperation{{Path: "gin.go", Operation: admission.OperationRead}},
		GraphNT:              graph,
		SetActive:            true,
	}
	first, err := Prepare(opts)
	if err != nil {
		t.Fatalf("first Prepare: %v", err)
	}
	second, err := Prepare(opts)
	if err != nil {
		t.Fatalf("second Prepare: %v", err)
	}
	if first.TaskID != second.TaskID {
		t.Fatalf("replay changed task id: %s -> %s", first.TaskID, second.TaskID)
	}
	if second.Disposition != "replay_no_new_iteration" {
		t.Fatalf("second disposition = %q", second.Disposition)
	}
	taskRoot := filepath.Join(repo, ".sensei", "tasks")
	entries, err := os.ReadDir(taskRoot)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	taskDirs := 0
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "task.") {
			taskDirs++
		}
	}
	if taskDirs != 1 {
		t.Fatalf("task dirs = %d, want 1", taskDirs)
	}
}

func TestWorkingTreeChangeOutsideModifyEnvelopeMakesTaskStale(t *testing.T) {
	repo, graph := testRepo(t)
	_, err := Prepare(PrepareOptions{
		RepoRoot: repo, RepositoryDomain: "github.com/example/project", Description: "modify gin",
		Mode: admission.ModeModify, TaskClass: "modify_gin", RiskClass: closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve, Files: []FileOperation{{Path: "gin.go", Operation: admission.OperationModify}},
		GraphNT: graph, SetActive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "gin_test.go"), []byte("package gin\n// changed outside envelope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := Status(StatusOptions{RepoRoot: repo, Active: true, Verify: true})
	if err != nil {
		t.Fatal(err)
	}
	if status.Verified || !strings.Contains(strings.Join(status.VerifyErrors, " "), "task.binding.working_tree_outside_envelope") {
		t.Fatalf("status=%+v", status)
	}
}

func TestWorkingTreeChangeInsideModifyEnvelopeDoesNotInvalidateTaskBinding(t *testing.T) {
	repo, graph := testRepo(t)
	_, err := Prepare(PrepareOptions{
		RepoRoot: repo, RepositoryDomain: "github.com/example/project", Description: "modify gin",
		Mode: admission.ModeModify, TaskClass: "modify_gin", RiskClass: closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve, Files: []FileOperation{{Path: "gin.go", Operation: admission.OperationModify}},
		GraphNT: graph, SetActive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "gin.go"), []byte("package gin\n// admitted edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := Status(StatusOptions{RepoRoot: repo, Active: true, Verify: true})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Verified {
		t.Fatalf("inside-envelope edit invalidated task: %v", status.VerifyErrors)
	}
}

func TestTaskBriefingHonorsContextBudgetAndPermissionBoundary(t *testing.T) {
	repo, graph := testRepo(t)
	_, err := Prepare(PrepareOptions{
		RepoRoot: repo, RepositoryDomain: "github.com/example/project", Description: "modify gin",
		Mode: admission.ModeModify, TaskClass: "modify_gin", RiskClass: closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve, Files: []FileOperation{{Path: "gin.go", Operation: admission.OperationModify}},
		GraphNT: graph, SetActive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	brief, err := BuildTaskBriefing(repo, "", "gin.go", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(brief.RelevantClaims) > MaxBriefingClaims || len(brief.FailureModes) > MaxBriefingFailures || len(brief.Constraints) > MaxBriefingConstraints {
		t.Fatalf("briefing exceeded budget: %+v", brief)
	}
	if brief.Inspect == admission.CapabilityAdmitted && brief.Modify == admission.CapabilityAdmitted {
		t.Fatal("inspection admission was reported as mutation admission")
	}
}

func TestTaskBriefingRejectsGraphDigestMismatch(t *testing.T) {
	repo, graph := testRepo(t)
	res, err := Prepare(PrepareOptions{
		RepoRoot: repo, RepositoryDomain: "github.com/example/project", Description: "inspect gin",
		Mode: admission.ModeInspect, TaskClass: "inspect_gin", RiskClass: closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve, Files: []FileOperation{{Path: "gin.go", Operation: admission.OperationRead}},
		GraphNT: graph, SetActive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	taskDir := filepath.Join(repo, filepath.FromSlash(res.TaskDir))
	if err := os.WriteFile(filepath.Join(taskDir, "source", "graph.nt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildTaskBriefing(repo, "", "gin.go", true); err == nil {
		t.Fatal("stale graph was accepted for task briefing")
	}
}

func TestAdvanceTaskRunsOneAtomicGenerationAndReplays(t *testing.T) {
	repo, graph := testRepo(t)
	res, err := Prepare(PrepareOptions{
		RepoRoot: repo, RepositoryDomain: "github.com/example/project", Description: "modify gin",
		Mode: admission.ModeModify, TaskClass: "modify_gin", RiskClass: closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve, Files: []FileOperation{{Path: "gin.go", Operation: admission.OperationModify}},
		GraphNT: graph, SetActive: true, QuestionCreatedAt: "2026-07-14T18:30:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, Active: true, ObservedAt: "2026-07-14T18:31:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Disposition != AdvanceCompleted || first.Generation == "" {
		t.Fatalf("first advance=%+v", first)
	}
	generation := filepath.Join(repo, filepath.FromSlash(res.TaskDir), "control", "generations", first.Generation)
	for _, rel := range []string{"probe-results.yaml", "evidence-state.yaml", "convergence/session.yaml", "admission-decision.yaml", "task-control.yaml", "receipt.yaml"} {
		if _, err := os.Stat(filepath.Join(generation, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("atomic generation missing %s: %v", rel, err)
		}
	}
	latestPath := filepath.Join(repo, filepath.FromSlash(res.TaskDir), "control", "latest.yaml")
	before, err := os.ReadFile(latestPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, Active: true, ObservedAt: "2026-07-14T18:31:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(latestPath)
	if err != nil {
		t.Fatal(err)
	}
	if second.Disposition != AdvanceReplay || second.Probe.ProbesExecuted != 0 || string(before) != string(after) {
		t.Fatalf("replay changed task state: disposition=%s probes=%d", second.Disposition, second.Probe.ProbesExecuted)
	}
}

func TestConcurrentAdvanceIsRefused(t *testing.T) {
	repo, graph := testRepo(t)
	res, err := Prepare(PrepareOptions{
		RepoRoot: repo, RepositoryDomain: "github.com/example/project", Description: "inspect gin",
		Mode: admission.ModeInspect, TaskClass: "inspect_gin", RiskClass: closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve, Files: []FileOperation{{Path: "gin.go", Operation: admission.OperationRead}},
		GraphNT: graph, SetActive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	taskDir := filepath.Join(repo, filepath.FromSlash(res.TaskDir))
	unlock, err := acquireTaskLock(taskDir, time.Second, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	_, err = AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, Active: true, LockWait: 10 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), ReasonTaskLockHeld) {
		t.Fatalf("concurrent advance error=%v", err)
	}
}

func TestPrepareChangeDoesNotModifySource(t *testing.T) {
	repo, graph := testRepo(t)
	before := fileDigest(t, filepath.Join(repo, "gin.go"))
	if _, err := Prepare(PrepareOptions{
		RepoRoot:             repo,
		RepositoryDomain:     "github.com/example/project",
		Description:          "Check source mutation boundary.",
		Mode:                 admission.ModeModify,
		TaskClass:            "source_mutation_boundary",
		RiskClass:            closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve,
		Files:                []FileOperation{{Path: "gin.go", Operation: admission.OperationModify}},
		GraphNT:              graph,
		SetActive:            true,
	}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	after := fileDigest(t, filepath.Join(repo, "gin.go"))
	if before != after {
		t.Fatalf("source file changed: %s -> %s", before, after)
	}
}

func TestTaskStatusDetectsStaleRevision(t *testing.T) {
	repo, graph := testRepo(t)
	if _, err := Prepare(PrepareOptions{
		RepoRoot:             repo,
		RepositoryDomain:     "github.com/example/project",
		Description:          "Detect stale revision.",
		Mode:                 admission.ModeInspect,
		TaskClass:            "stale_revision",
		RiskClass:            closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve,
		Files:                []FileOperation{{Path: "gin.go", Operation: admission.OperationRead}},
		GraphNT:              graph,
		SetActive:            true,
	}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	writeFile(t, repo, "README.md", "changed\n")
	git(t, repo, "add", "README.md")
	git(t, repo, "commit", "-m", "change revision")
	st, err := Status(StatusOptions{RepoRoot: repo, Active: true, Verify: true})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Verified {
		t.Fatal("stale revision verified")
	}
	if st.Phase != PhaseStale || st.Status != StatusStale {
		t.Fatalf("stale status = phase %s status %s", st.Phase, st.Status)
	}
}

func TestPrepareChangeRejectsMissingExactScope(t *testing.T) {
	repo, graph := testRepo(t)
	_, err := Prepare(PrepareOptions{
		RepoRoot:             repo,
		RepositoryDomain:     "github.com/example/project",
		Description:          "Missing scope.",
		Mode:                 admission.ModeModify,
		TaskClass:            "missing_scope",
		RiskClass:            closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve,
		GraphNT:              graph,
		SetActive:            true,
	})
	if err == nil {
		t.Fatal("Prepare accepted missing scope")
	}
}

func TestPrepareChangeRejectsMissingProjectInferenceBeforeCreatingTask(t *testing.T) {
	repo, graph := testRepo(t)
	if err := os.Remove(filepath.Join(repo, ".sensei", "project", "claims.yaml")); err != nil {
		t.Fatal(err)
	}
	_, err := Prepare(PrepareOptions{
		RepoRoot:             repo,
		RepositoryDomain:     "github.com/example/project",
		Description:          "Inspect route ownership.",
		Mode:                 admission.ModeInspect,
		TaskClass:            "route_ownership",
		RiskClass:            closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve,
		Files:                []FileOperation{{Path: "gin.go", Operation: admission.OperationRead}},
		GraphNT:              graph,
	})
	if err == nil || !strings.Contains(err.Error(), "task input incomplete: inference not run") {
		t.Fatalf("missing project inference error=%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".sensei", "tasks")); !os.IsNotExist(statErr) {
		t.Fatalf("task workspace was created before input validation: %v", statErr)
	}
}

func TestPrepareChangeRejectsEmptyProjectClaims(t *testing.T) {
	repo, graph := testRepo(t)
	doc, err := architecture.LoadClaimDocument(filepath.Join(repo, ".sensei", "project", "claims.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc.Claims = nil
	doc.FactReceipts = nil
	data, err := architecture.MarshalCanonicalClaimDocumentYAML(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".sensei", "project", "claims.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Prepare(PrepareOptions{
		RepoRoot:             repo,
		RepositoryDomain:     "github.com/example/project",
		Description:          "Inspect route ownership.",
		Mode:                 admission.ModeInspect,
		TaskClass:            "route_ownership",
		RiskClass:            closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve,
		Files:                []FileOperation{{Path: "gin.go", Operation: admission.OperationRead}},
		GraphNT:              graph,
	})
	if err == nil || !strings.Contains(err.Error(), "inference produced no architecture claims") {
		t.Fatalf("empty project claims error=%v", err)
	}
}

func sampleTaskRequest() TaskRequest {
	return TaskRequest{
		SchemaVersion: SchemaVersion,
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  "github.com/example/project",
			Revision:          strings.Repeat("1", 40),
			RevisionStatus:    architecture.RevisionResolved,
			GraphDigestSHA256: strings.Repeat("a", 64),
			GraphDigestStatus: architecture.GraphDigestResolved,
		},
		Description:          "Ensure literal colon routes resolve consistently.",
		Mode:                 admission.ModeModify,
		TaskClass:            "literal_colon_route_consistency",
		RiskClass:            closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve,
		Scope:                TaskScope{Files: []FileOperation{{Path: "gin.go", Operation: admission.OperationModify}}},
		RequestedBy:          "coding_agent",
	}
}

func writeBootstrapDirectionAuthorization(t *testing.T, repo, graph string, req TaskRequest) string {
	t.Helper()
	req.TaskID = StableTaskID(req)
	auth := closure.DirectionBootstrapAuthorization{
		SchemaVersion:                closure.DirectionBootstrapSchemaVersion,
		PolicyID:                     closure.DirectionBootstrapPolicyID,
		TaskID:                       req.TaskID,
		BaseRevision:                 req.Binding.Revision,
		GraphDigestSHA256:            req.Binding.GraphDigestSHA256,
		File:                         closure.DirectionBootstrapFile,
		GovernedRecordIDs:            []string{"decision.desired", "decision.intended"},
		ExpectedMutationDigestSHA256: strings.Repeat("c", 64),
		ApprovedBy:                   "architect",
		ApprovalMechanism:            closure.DirectionBootstrapMechanismFile,
		ApprovalStatement:            "one-use bootstrap",
		UsagePolicy:                  closure.DirectionBootstrapUsageOneUse,
		IssuedAt:                     "2026-07-15T00:00:00Z",
		ExpiresAt:                    "2026-07-16T00:00:00Z",
	}
	data, err := closure.MarshalCanonicalDirectionBootstrapYAML(auth)
	if err != nil {
		t.Fatal(err)
	}
	return writeExternalBootstrapDirectionAuthorizationFile(t, data)
}

func writeExternalBootstrapDirectionAuthorizationFile(t *testing.T, data []byte) string {
	t.Helper()
	dir := externalApprovalDir(t)
	path := filepath.Join(dir, "bootstrap-direction-authorization.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func externalApprovalDir(t *testing.T) string {
	t.Helper()
	base, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(base) == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			t.Fatalf("resolve home for external approval dir: %v / %v", err, homeErr)
		}
		base = filepath.Join(home, ".cache")
	}
	dir := filepath.Join(base, "sensei-bootstrap-tests", strings.ReplaceAll(t.Name(), "/", "_"), strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", ""))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(closure.DirectionBootstrapApprovalDirEnv, dir)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func gitHeadForTest(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func testRepo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "gin.go", "package gin\n")
	writeFile(t, root, "gin_test.go", "package gin\n")
	git(t, root, "init")
	git(t, root, "config", "user.email", "sensei@example.test")
	git(t, root, "config", "user.name", "Sensei Test")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial")
	graph := strings.Join([]string{
		triple("https://globular.io/awareness#sourceFile/gin.go", rdf.PropType, rdf.ClassSourceFile, true),
		triple("https://globular.io/awareness#sourceFile/gin.go", rdf.PropSourcePath, "gin.go", false),
		triple("https://globular.io/awareness#sourceFile/gin_test.go", rdf.PropType, rdf.ClassSourceFile, true),
		triple("https://globular.io/awareness#sourceFile/gin_test.go", rdf.PropSourcePath, "gin_test.go", false),
		"",
	}, "\n")
	graphPath := filepath.Join(root, "graph.nt")
	if err := os.WriteFile(graphPath, []byte(graph), 0o644); err != nil {
		t.Fatalf("write graph: %v", err)
	}
	writeTestProjectClaims(t, root, graphPath)
	return root, graphPath
}

func writeTestProjectClaims(t *testing.T, root, graphPath string) {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("resolve revision: %v", err)
	}
	revision := strings.TrimSpace(string(out))
	domain := "github.com/example/project"
	provenance := architecture.Provenance{
		RepositoryDomain:       domain,
		RepositoryDomainStatus: architecture.RepositoryDomainResolved,
		Revision:               revision,
		RevisionStatus:         architecture.RevisionResolved,
		SourceDigest:           fileDigest(t, filepath.Join(root, "gin.go")),
		SourceDigestStatus:     architecture.SourceDigestResolved,
		SourceKind:             "source_file",
	}
	fact := architecture.Fact{
		ID:        "fact.task-session-test",
		Kind:      "guard",
		Subject:   "gin.Engine",
		Predicate: "refuses_when",
		Object:    "route state is invalid",
		Scope: architecture.Scope{
			Repository: domain,
			Files:      []string{"gin.go"},
			Symbols:    []string{"gin.Engine"},
		},
		Evidence:   architecture.Evidence{SourceFile: "gin.go", LineStart: 1, LineEnd: 1},
		Confidence: 0.6,
		Extractor:  "task_session_test",
		Provenance: &provenance,
	}
	claim := architecture.Claim{
		ID:                     "claim.task-session-test",
		Label:                  "Engine rejects invalid route state",
		Statement:              architecture.ClaimStatement{Subject: "gin.Engine", Predicate: "refuses_when", Object: "route state is invalid"},
		Scope:                  architecture.ClaimScope{Repository: domain, Repo: domain, Files: []string{"gin.go"}, Symbols: []string{"gin.Engine"}},
		ArchitecturalPlane:     architecture.PlaneObserved,
		AssertionOrigin:        architecture.OriginDerived,
		EpistemicStatus:        architecture.StatusSupported,
		InferenceRule:          "rule.task_session_test.v1",
		PremiseFacts:           []string{fact.ID},
		InvalidationConditions: []string{"The premise fact changes."},
		Confidence:             0.6,
		HumanReviewRequired:    true,
		PromotionStatus:        architecture.PromotionCandidate,
	}
	doc := architecture.ClaimDocument{
		SchemaVersion: "1",
		GeneratedBy:   "task session test",
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  domain,
			Revision:          revision,
			RevisionStatus:    architecture.RevisionResolved,
			GraphDigestSHA256: fileDigest(t, graphPath),
			GraphDigestStatus: architecture.GraphDigestResolved,
		},
		FactReceipts: []architecture.ClaimFactReceipt{{Fact: fact, Provenance: provenance}},
		Claims:       []architecture.Claim{claim},
	}
	data, err := architecture.MarshalCanonicalClaimDocumentYAML(doc)
	if err != nil {
		t.Fatalf("marshal project claims: %v", err)
	}
	path := filepath.Join(root, ".sensei", "project", "claims.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write project claims: %v", err)
	}
}

func triple(s, p, o string, iri bool) string {
	obj := quote(o)
	if iri {
		obj = "<" + o + ">"
	}
	return "<" + s + "> <" + p + "> " + obj + " ."
}

func quote(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return `"` + v + `"`
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func git(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
