// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/convergence"
	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/plane"
	"github.com/globulario/sensei/golang/architecture/probe"
	"github.com/globulario/sensei/golang/rdf"
)

const (
	testRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testGraph    = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testIter     = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	testSemantic = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
)

func TestAdmissionRequestRequiresResolvedBinding(t *testing.T) {
	req := testRequest()
	req.Binding.RevisionStatus = architecture.RevisionUnavailable
	if _, err := NormalizeRequest(req); err == nil {
		t.Fatal("expected unresolved binding to be rejected")
	}
}

func TestAdmissionRequestRejectsUnknownMode(t *testing.T) {
	req := testRequest()
	req.Mode = "execute"
	if _, err := NormalizeRequest(req); err == nil {
		t.Fatal("expected unknown mode to be rejected")
	}
}

func TestInspectRequestRejectsModifyOperation(t *testing.T) {
	req := testRequest()
	req.Mode = ModeInspect
	req.Scope.Files = []FileOperation{{Path: "a.go", Operation: OperationModify}}
	if _, err := NormalizeRequest(req); err == nil {
		t.Fatal("expected inspect modify operation to be rejected")
	}
}

func TestModifyRequestRequiresModifyPath(t *testing.T) {
	req := testRequest()
	req.Scope.Files = []FileOperation{{Path: "a.go", Operation: OperationRead}}
	if _, err := NormalizeRequest(req); err == nil {
		t.Fatal("expected modify request without modify operation to be rejected")
	}
}

func TestAdmissionRequestRejectsEscapingPath(t *testing.T) {
	req := testRequest()
	req.Scope.Files = []FileOperation{{Path: "../a.go", Operation: OperationModify}}
	if _, err := NormalizeRequest(req); err == nil {
		t.Fatal("expected escaping path to be rejected")
	}
}

func TestAdmissionRequestRejectsConflictingOperations(t *testing.T) {
	req := testRequest()
	req.Scope.Files = []FileOperation{{Path: "a.go", Operation: OperationRead}, {Path: "./a.go", Operation: OperationModify}}
	if _, err := NormalizeRequest(req); err == nil {
		t.Fatal("expected conflicting operations to be rejected")
	}
}

func TestAdmissionRequestDescriptiveFieldsDoNotAffectIdentity(t *testing.T) {
	a := testRequest()
	b := testRequest()
	a.RequestedBy = "agent-a"
	a.Note = "first note"
	b.RequestedBy = "agent-b"
	b.Note = "second note"
	da, err := MarshalCanonicalRequestYAML(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := MarshalCanonicalRequestYAML(b)
	if err != nil {
		t.Fatal(err)
	}
	// The canonical request preserves descriptive fields, but admission identity does not.
	ba := testBundle(closure.VerdictClosed, convergence.StatusClosed, nil)
	repo := tempRepoFile(t, "a.go")
	graph := closure.GraphIndex{}
	ra, _ := NormalizeRequest(a)
	rb, _ := NormalizeRequest(b)
	ad, err := EvaluateLoaded(mustPolicy(t), ra, ba, graph, nil, repo, testRevision)
	if err != nil {
		t.Fatal(err)
	}
	bd, err := EvaluateLoaded(mustPolicy(t), rb, ba, graph, nil, repo, testRevision)
	if err != nil {
		t.Fatal(err)
	}
	if ad.AdmissionID != bd.AdmissionID {
		t.Fatalf("descriptive fields changed admission id: %s != %s", ad.AdmissionID, bd.AdmissionID)
	}
	if string(da) == string(db) {
		t.Fatal("canonical request should preserve descriptive fields for receipt transparency")
	}
}

func TestStrictAdmissionPolicyIsStable(t *testing.T) {
	p := mustPolicy(t)
	if p.ID != PolicyStrictID || p.Version != PolicyStrictVersion {
		t.Fatalf("unexpected strict policy: %+v", p)
	}
	if !p.AllowInspectionWhenClosureOpen || !p.RequireConditionAcknowledgement || !p.AllowConditionalMutation {
		t.Fatalf("strict policy flags changed: %+v", p)
	}
}

func TestClosureAndAdmissionAgreeOnAuthoredInRepresentation(t *testing.T) {
	repo, rel := tempAuthoredRepoFile(t, "docs/awareness/architecture/decisions.yaml")
	graph := authoredInGraph(t, "decision", rdf.ClassDecision, "decision.scope", authoredNodeOptions{
		Status:     "accepted",
		AuthoredIn: []string{rel},
	})
	req := testRequest()
	req.Scope.Files = []FileOperation{{Path: rel, Operation: OperationModify}}
	closureReq := closure.Request{
		SchemaVersion: closure.SchemaVersion,
		Binding:       req.Binding,
		Scope: closure.Scope{
			Domain:               req.Binding.RepositoryDomain,
			TaskClass:            req.TaskClass,
			RiskClass:            closure.RiskArchitectureSensitive,
			AccessMode:           closure.AccessReadWrite,
			DirectionRequirement: closure.DirectionEvolve,
			Files:                []string{rel},
		},
	}
	planeReport := plane.Report{
		SchemaVersion: "1",
		GeneratedBy:   "test",
		ClaimBinding: plane.ClaimBindingReport{
			RepositoryDomain:  req.Binding.RepositoryDomain,
			Revision:          req.Binding.Revision,
			RevisionStatus:    req.Binding.RevisionStatus,
			GraphDigestSHA256: req.Binding.GraphDigestSHA256,
			GraphDigestStatus: req.Binding.GraphDigestStatus,
		},
		GraphSnapshot: plane.GraphSnapshotReport{DigestSHA256: req.Binding.GraphDigestSHA256, DigestStatus: req.Binding.GraphDigestStatus},
	}
	report, err := closure.Evaluate(closure.Context{
		Request:          closureReq,
		Claims:           architecture.ClaimDocument{SchemaVersion: "1", GeneratedBy: "test", Binding: req.Binding},
		Maintenance:      &maintenance.Report{SchemaVersion: "1", GeneratedBy: "test", CurrentBinding: req.Binding, ObservedBinding: req.Binding},
		Plane:            &planeReport,
		Dialogue:         &architecture.DialogueDocument{SchemaVersion: "1", CompiledBy: "test", Binding: req.Binding},
		Evidence:         &maintenance.EvidenceStateDocument{SchemaVersion: "1", GeneratedBy: "test", Binding: req.Binding},
		Graph:            graph,
		GraphReceipt:     graphsnapshot.Receipt{Status: architecture.GraphDigestResolved, DigestSHA256: req.Binding.GraphDigestSHA256, Verified: true},
		RepositoryRoot:   repo,
		RepositoryRev:    req.Binding.Revision,
		RepositoryStatus: architecture.RevisionResolved,
	})
	if err != nil {
		t.Fatalf("closure Evaluate: %v", err)
	}
	if !containsPath(report.ScopeReceipt.Files, rel) {
		t.Fatalf("closure did not represent %s: %#v", rel, report.ScopeReceipt)
	}
	if len(scopeContainment(req, Bundle{ClosureAfter: report}, graph, repo)) != 0 {
		t.Fatalf("admission rejected authoredIn-represented file")
	}
}

func TestCandidateAuthoredInDoesNotRepresentFileForAdmission(t *testing.T) {
	repo, rel := tempAuthoredRepoFile(t, "docs/awareness/architecture/decisions.yaml")
	graph := authoredInGraph(t, "decision", rdf.ClassDecision, "decision.scope", authoredNodeOptions{
		Status:          "candidate",
		PromotionStatus: "candidate",
		AuthoredIn:      []string{rel},
	})
	report := closure.Report{
		ScopeReceipt:  closure.ScopeReceipt{Files: []string{rel}},
		RelevantNodes: []closure.NodeReceipt{{ID: "decision.scope"}},
	}
	if fileRepresentedByClosureNode(rel, report, graph, repo) {
		t.Fatal("candidate authoredIn unexpectedly represented file")
	}
}

func TestUnknownAdmissionPolicyRejected(t *testing.T) {
	if _, ok := PolicyByID("admission.unknown"); ok {
		t.Fatal("unknown policy resolved")
	}
}

func TestStrictPolicySupportsReadAndModifyOnly(t *testing.T) {
	p := mustPolicy(t)
	got := strings.Join(p.SupportedOperations, ",")
	if got != "read,modify" {
		t.Fatalf("unexpected supported operations: %s", got)
	}
}

func TestClosedSessionAdmitsMutation(t *testing.T) {
	d := evaluateFixture(t, testRequest(), testBundle(closure.VerdictClosed, convergence.StatusClosed, nil))
	if d.Decision != DecisionAdmitted || d.MutationCapability != CapabilityAdmitted {
		t.Fatalf("expected admitted mutation, got %+v", d)
	}
}

func TestConditionalSessionAdmitsWithAcknowledgedConditions(t *testing.T) {
	req := testRequest()
	req.AcceptedConditionIDs = []string{"condition.one"}
	conds := []closure.Condition{{ID: "condition.one", Dimension: closure.DimensionEvidence, Code: "closure.evidence.conditional", Summary: "pending runtime proof", RequiredNextAction: "preserve_condition"}}
	d := evaluateFixture(t, req, testBundle(closure.VerdictConditionallyClosed, convergence.StatusConditionallyClosed, conds))
	if d.Decision != DecisionAdmittedWithConditions {
		t.Fatalf("expected admitted_with_conditions, got %s reasons=%+v", d.Decision, d.Reasons)
	}
	if len(d.Conditions) != 1 || d.Conditions[0].ID != "condition.one" {
		t.Fatalf("condition was not preserved: %+v", d.Conditions)
	}
}

func TestConditionalSessionWaitsWithoutAcknowledgement(t *testing.T) {
	conds := []closure.Condition{{ID: "condition.one", Dimension: closure.DimensionEvidence, Code: "closure.evidence.conditional", Summary: "pending runtime proof", RequiredNextAction: "preserve_condition"}}
	d := evaluateFixture(t, testRequest(), testBundle(closure.VerdictConditionallyClosed, convergence.StatusConditionallyClosed, conds))
	if d.Decision != DecisionWaiting || !hasReason(d.Reasons, ReasonConditionAcknowledgementMissing) {
		t.Fatalf("expected waiting for condition acknowledgement, got %s reasons=%+v", d.Decision, d.Reasons)
	}
}

func TestConditionalSessionRefusesUnknownConditionID(t *testing.T) {
	req := testRequest()
	req.AcceptedConditionIDs = []string{"stale.condition"}
	conds := []closure.Condition{{ID: "condition.one", Dimension: closure.DimensionEvidence, Code: "closure.evidence.conditional", Summary: "pending runtime proof", RequiredNextAction: "preserve_condition"}}
	d := evaluateFixture(t, req, testBundle(closure.VerdictConditionallyClosed, convergence.StatusConditionallyClosed, conds))
	if d.Decision != DecisionRefused || !hasReason(d.Reasons, ReasonConditionUnknownOrStale) {
		t.Fatalf("expected refused stale condition, got %s reasons=%+v", d.Decision, d.Reasons)
	}
}

func TestInvalidBootstrapDirectionMakesAdmissionUncertifiable(t *testing.T) {
	req := testRequest()
	b := testBundle(closure.VerdictConditionallyClosed, convergence.StatusConditionallyClosed, []closure.Condition{{
		ID: "condition.direction.bootstrap", Dimension: closure.DimensionDirection, Code: "closure.direction.desired.bootstrap", Summary: "bootstrap", RequiredNextAction: "acknowledge_bootstrap_direction_authorization",
	}})
	b.ClosureAfter.Request.TaskID = "task.bootstrap.direction"
	b.ClosureAfter.Request.DirectionBootstrap = &closure.DirectionBootstrapAuthorization{
		SchemaVersion:                closure.DirectionBootstrapSchemaVersion,
		PolicyID:                     closure.DirectionBootstrapPolicyID,
		TaskID:                       "task.bootstrap.direction",
		BaseRevision:                 testRevision,
		GraphDigestSHA256:            testGraph,
		File:                         "wrong.yaml",
		GovernedRecordIDs:            []string{"decision.desired"},
		ExpectedMutationDigestSHA256: strings.Repeat("e", 64),
		ApprovedBy:                   "architect",
		ApprovalMechanism:            closure.DirectionBootstrapMechanismFile,
		ApprovalStatement:            "bootstrap once",
		UsagePolicy:                  closure.DirectionBootstrapUsageOneUse,
		IssuedAt:                     "2026-07-15T00:00:00Z",
		ExpiresAt:                    "2026-07-16T00:00:00Z",
		ApprovalSourcePath:           "/approved/bootstrap-direction.yaml",
		ApprovalSourceDigestSHA256:   strings.Repeat("d", 64),
	}
	b.ClosureAfter.Request.DirectionBootstrap.AuthorizationDigestSHA256 = closure.DirectionBootstrapAuthorizationDigest(*b.ClosureAfter.Request.DirectionBootstrap)
	d := evaluateFixture(t, req, b)
	if d.Decision != DecisionUncertifiable || !hasReason(d.Reasons, ReasonBootstrapDirectionInvalid) {
		t.Fatalf("expected uncertifiable bootstrap refusal, got %s reasons=%+v", d.Decision, d.Reasons)
	}
}

func TestWaitingSessionMayAdmitInspection(t *testing.T) {
	req := testRequest()
	req.Mode = ModeInspect
	req.Scope.Files = []FileOperation{{Path: "a.go", Operation: OperationRead}}
	b := testBundle(closure.VerdictOpen, convergence.StatusWaiting, nil)
	b.ClosureAfter.Request.Scope.AccessMode = closure.AccessRead
	d := evaluateFixture(t, req, b)
	if d.Decision != DecisionAdmitted || d.MutationCapability != CapabilityWaiting {
		t.Fatalf("expected inspection admitted and mutation waiting, got decision=%s mutation=%s", d.Decision, d.MutationCapability)
	}
}

func TestStalledSessionRefusesMutation(t *testing.T) {
	d := evaluateFixture(t, testRequest(), testBundle(closure.VerdictOpen, convergence.StatusStalled, nil))
	if d.Decision != DecisionRefused || !hasReason(d.Reasons, ReasonSessionStalled) {
		t.Fatalf("expected stalled refusal, got %s reasons=%+v", d.Decision, d.Reasons)
	}
}

func TestReadClosureRefusesMutation(t *testing.T) {
	b := testBundle(closure.VerdictClosed, convergence.StatusClosed, nil)
	b.ClosureAfter.Request.Scope.AccessMode = closure.AccessRead
	d := evaluateFixture(t, testRequest(), b)
	if d.Decision != DecisionRefused || !hasReason(d.Reasons, ReasonAccessExceedsClosedScope) {
		t.Fatalf("expected access refusal, got %s reasons=%+v", d.Decision, d.Reasons)
	}
}

func TestOutOfScopePathIsRefused(t *testing.T) {
	req := testRequest()
	req.Scope.Files = []FileOperation{{Path: "b.go", Operation: OperationModify}}
	d := evaluateFixture(t, req, testBundle(closure.VerdictClosed, convergence.StatusClosed, nil))
	if d.Decision != DecisionRefused || !hasReason(d.Reasons, ReasonScopeOutsideClosedScope) {
		t.Fatalf("expected out-of-scope refusal, got %s reasons=%+v", d.Decision, d.Reasons)
	}
}

func TestSymbolOnlyMutationWithoutFileIsRefused(t *testing.T) {
	req := testRequest()
	req.Scope.Files = nil
	req.Scope.Symbols = []string{"symbol.Save"}
	if _, err := NormalizeRequest(req); err == nil {
		t.Fatal("expected symbol-only mutation to be rejected before admission")
	}
}

func TestAdmissionIDIsDeterministic(t *testing.T) {
	a := evaluateFixture(t, testRequest(), testBundle(closure.VerdictClosed, convergence.StatusClosed, nil))
	b := evaluateFixture(t, testRequest(), testBundle(closure.VerdictClosed, convergence.StatusClosed, nil))
	if a.AdmissionID != b.AdmissionID || a.DecisionDigestSHA256 != b.DecisionDigestSHA256 {
		t.Fatalf("admission identity not deterministic: %s/%s vs %s/%s", a.AdmissionID, a.DecisionDigestSHA256, b.AdmissionID, b.DecisionDigestSHA256)
	}
}

func TestDecisionDigestDetectsMutation(t *testing.T) {
	d := evaluateFixture(t, testRequest(), testBundle(closure.VerdictClosed, convergence.StatusClosed, nil))
	orig := d.DecisionDigestSHA256
	d.Decision = DecisionRefused
	if decisionDigest(d) == orig {
		t.Fatal("decision digest did not detect mutation")
	}
}

func TestReadOnlyAdmissionRejectsAnyChange(t *testing.T) {
	req := testRequest()
	req.Mode = ModeInspect
	req.Scope.Files = []FileOperation{{Path: "a.go", Operation: OperationRead}}
	b := testBundle(closure.VerdictOpen, convergence.StatusWaiting, nil)
	b.ClosureAfter.Request.Scope.AccessMode = closure.AccessRead
	d := evaluateFixture(t, req, b)
	violations := envelopeViolations(d, []ChangeReceipt{{Path: "a.go", ChangeType: ChangeModified}})
	if len(violations) != 1 || violations[0].Code != VerifyReadOnlyMutation {
		t.Fatalf("expected read-only violation, got %+v", violations)
	}
}

func TestAllowedModifiedFilesAreScopeCompliant(t *testing.T) {
	d := evaluateFixture(t, testRequest(), testBundle(closure.VerdictClosed, convergence.StatusClosed, nil))
	if violations := envelopeViolations(d, []ChangeReceipt{{Path: "a.go", ChangeType: ChangeModified}}); len(violations) != 0 {
		t.Fatalf("expected no violations, got %+v", violations)
	}
}

func TestOutOfEnvelopeFileIsViolation(t *testing.T) {
	d := evaluateFixture(t, testRequest(), testBundle(closure.VerdictClosed, convergence.StatusClosed, nil))
	violations := envelopeViolations(d, []ChangeReceipt{{Path: "b.go", ChangeType: ChangeModified}})
	if len(violations) != 1 || violations[0].Code != VerifyPathOutsideEnvelope {
		t.Fatalf("expected path outside envelope, got %+v", violations)
	}
}

func TestVerifyAwarenessMutationProducesVerificationReceipt(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "docs", "awareness", "architecture"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "docs", "awareness", "architecture", "components.yaml"), []byte("components:\n  - id: component.demo\n    name: Demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := closure.AwarenessMutationEnforcementDocument{
		SchemaVersion:      "1",
		PolicyID:           closure.AwarenessMutationEnforcementPolicyV1,
		TaskID:             "task.demo",
		RepositoryRevision: testRevision,
		GraphDigestSHA256:  testGraph,
		Plans: []closure.AwarenessMutationEnforcementPlan{{
			SourcePath:           "docs/awareness/architecture/components.yaml",
			SourceClass:          "canonical_awareness_component_registry",
			ImporterID:           "awareness.component_yaml_import.v1",
			RequiredVerification: []string{"sensei_check", "sensei_validate", "strict_build"},
		}},
	}
	data, err := closure.MarshalCanonicalAwarenessMutationEnforcementYAML(doc)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(repo, ".sensei", "tasks", "task.demo", "source")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "awareness-mutation-enforcement.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := closure.AwarenessMutationEnforcementDigest(doc)
	if err != nil {
		t.Fatal(err)
	}
	receipt, violations, reasons := verifyAwarenessMutation(repo, Decision{
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  "github.com/example/project",
			Revision:          testRevision,
			RevisionStatus:    architecture.RevisionResolved,
			GraphDigestSHA256: testGraph,
			GraphDigestStatus: architecture.GraphDigestResolved,
		},
		AwarenessMutationBinding: &closure.AwarenessMutationBinding{
			TaskID:           "task.demo",
			Path:             ".sensei/tasks/task.demo/source/awareness-mutation-enforcement.yaml",
			PlanDigestSHA256: digest,
			PolicyID:         closure.AwarenessMutationEnforcementPolicyV1,
		},
		AwarenessMutation: &closure.AwarenessMutationReceipt{Status: "consumed", PolicyID: closure.AwarenessMutationEnforcementPolicyV1, PlanDigestSHA256: digest},
	})
	if len(violations) != 0 || len(reasons) != 0 {
		t.Fatalf("violations=%+v reasons=%+v receipt=%+v", violations, reasons, receipt)
	}
	if receipt == nil || receipt.SenseiCheck != "passed" || receipt.SenseiValidate != "passed" || receipt.StrictBuild != "passed" {
		t.Fatalf("receipt=%+v", receipt)
	}
}

func TestBootstrapPatchMismatchIsVerificationViolation(t *testing.T) {
	b := testBundle(closure.VerdictConditionallyClosed, convergence.StatusConditionallyClosed, []closure.Condition{{
		ID: "condition.direction.bootstrap", Dimension: closure.DimensionDirection, Code: "closure.direction.desired.bootstrap", Summary: "bootstrap", RequiredNextAction: "acknowledge_bootstrap_direction_authorization",
	}})
	b.ClosureAfter.Request.TaskID = "task.bootstrap.direction"
	b.ClosureAfter.Request.Scope.Files = []string{closure.DirectionBootstrapFile}
	repo := tempRepoFile(t, closure.DirectionBootstrapFile)
	b.ClosureAfter.Request.Binding.Revision = repoHead(t, repo)
	auth := validBootstrapDirectionAuthorization(t, repo, []byte("package p\n"), []string{"decision.desired", "decision.intended"})
	auth.ExpectedMutationDigestSHA256 = strings.Repeat("e", 64)
	auth.AuthorizationDigestSHA256 = closure.DirectionBootstrapAuthorizationDigest(auth)
	b.ClosureAfter.Request.DirectionBootstrap = &auth
	violations := bootstrapMutationViolations(repo, b)
	if len(violations) != 1 || violations[0].Code != VerifyBootstrapMutationMismatch {
		t.Fatalf("expected bootstrap patch mismatch, got %+v", violations)
	}
}

func TestBootstrapRecordIDMismatchIsVerificationViolation(t *testing.T) {
	b := testBundle(closure.VerdictConditionallyClosed, convergence.StatusConditionallyClosed, []closure.Condition{{
		ID: "condition.direction.bootstrap", Dimension: closure.DimensionDirection, Code: "closure.direction.desired.bootstrap", Summary: "bootstrap", RequiredNextAction: "acknowledge_bootstrap_direction_authorization",
	}})
	b.ClosureAfter.Request.TaskID = "task.bootstrap.direction"
	b.ClosureAfter.Request.Scope.Files = []string{closure.DirectionBootstrapFile}
	repo := tempRepoFile(t, closure.DirectionBootstrapFile)
	b.ClosureAfter.Request.Binding.Revision = repoHead(t, repo)
	auth := validBootstrapDirectionAuthorization(t, repo, []byte("package p\n"), []string{"decision.desired"})
	auth.GovernedRecordIDs = []string{"decision.intended"}
	auth.AuthorizationDigestSHA256 = closure.DirectionBootstrapAuthorizationDigest(auth)
	b.ClosureAfter.Request.DirectionBootstrap = &auth
	violations := bootstrapMutationViolations(repo, b)
	if len(violations) != 1 || violations[0].Code != VerifyBootstrapMutationMismatch {
		t.Fatalf("expected bootstrap record id mismatch, got %+v", violations)
	}
}

func TestBootstrapDirectionConsumptionReceiptIsIdempotentForSameTask(t *testing.T) {
	repoRoot := t.TempDir()
	taskRoot := filepath.Join(repoRoot, ".sensei", "tasks", "task.one")
	auth := closure.DirectionBootstrapAuthorization{
		TaskID:                     "task.bootstrap.direction",
		ApprovalSourcePath:         "/approved/bootstrap-direction.yaml",
		ApprovalSourceDigestSHA256: strings.Repeat("d", 64),
		AuthorizationDigestSHA256:  strings.Repeat("e", 64),
	}
	decision := Decision{AdmissionID: "admission.one"}
	verification := Verification{VerificationDigestSHA256: strings.Repeat("f", 64)}
	if err := recordBootstrapDirectionConsumption(repoRoot, taskRoot, decision, &auth, verification); err != nil {
		t.Fatalf("first recordBootstrapDirectionConsumption: %v", err)
	}
	if err := recordBootstrapDirectionConsumption(repoRoot, taskRoot, decision, &auth, verification); err != nil {
		t.Fatalf("second recordBootstrapDirectionConsumption: %v", err)
	}
	receipt, err := LoadBootstrapDirectionConsumption(filepath.Join(taskRoot, "receipts", "bootstrap-direction-consumption.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.AuthorizationDigestSHA256 != auth.AuthorizationDigestSHA256 {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
}

func TestBootstrapDirectionConsumptionIsAtomicAcrossTasks(t *testing.T) {
	repoRoot := t.TempDir()
	auth := closure.DirectionBootstrapAuthorization{
		TaskID:                     "task.bootstrap.direction",
		ApprovalSourcePath:         "/approved/bootstrap-direction.yaml",
		ApprovalSourceDigestSHA256: strings.Repeat("d", 64),
		AuthorizationDigestSHA256:  strings.Repeat("e", 64),
	}
	verification := Verification{VerificationDigestSHA256: strings.Repeat("f", 64)}
	if err := recordBootstrapDirectionConsumption(repoRoot, filepath.Join(repoRoot, ".sensei", "tasks", "task.one"), Decision{AdmissionID: "admission.one"}, &auth, verification); err != nil {
		t.Fatalf("first recordBootstrapDirectionConsumption: %v", err)
	}
	err := recordBootstrapDirectionConsumption(repoRoot, filepath.Join(repoRoot, ".sensei", "tasks", "task.two"), Decision{AdmissionID: "admission.two"}, &auth, verification)
	if err == nil || !strings.Contains(err.Error(), "already consumed by task") {
		t.Fatalf("expected atomic cross-task rejection, got %v", err)
	}
}

func TestBootstrapDirectionAuthorizationRejectsRepoLocalApprovalSource(t *testing.T) {
	repo := tempRepoFile(t, closure.DirectionBootstrapFile)
	localPath := filepath.Join(repo, "bootstrap-direction-authorization.yaml")
	if err := os.WriteFile(localPath, []byte("approved"), 0o644); err != nil {
		t.Fatal(err)
	}
	auth := closure.DirectionBootstrapAuthorization{
		SchemaVersion:                closure.DirectionBootstrapSchemaVersion,
		PolicyID:                     closure.DirectionBootstrapPolicyID,
		TaskID:                       "task.bootstrap.direction",
		BaseRevision:                 testRevision,
		GraphDigestSHA256:            testGraph,
		File:                         closure.DirectionBootstrapFile,
		GovernedRecordIDs:            []string{"decision.desired"},
		ExpectedMutationDigestSHA256: strings.Repeat("e", 64),
		ApprovedBy:                   "Dave",
		ApprovalMechanism:            closure.DirectionBootstrapMechanismFile,
		ApprovalStatement:            "bootstrap once",
		UsagePolicy:                  closure.DirectionBootstrapUsageOneUse,
		IssuedAt:                     "2026-07-15T00:00:00Z",
		ExpiresAt:                    "2026-07-16T00:00:00Z",
		ApprovalSourcePath:           localPath,
		ApprovalSourceDigestSHA256:   digest([]byte("approved")),
	}
	auth.AuthorizationDigestSHA256 = closure.DirectionBootstrapAuthorizationDigest(auth)
	if err := closure.ValidateDirectionBootstrapApproval(auth, repo); err == nil || !strings.Contains(err.Error(), "outside the repository root") {
		t.Fatalf("expected repo-local approval source rejection, got %v", err)
	}
}

func TestScopeComplianceDoesNotCertifyCorrectness(t *testing.T) {
	d := evaluateFixture(t, testRequest(), testBundle(closure.VerdictClosed, convergence.StatusClosed, nil))
	v := verificationFromDecision(d, VerificationScopeCompliant, nil, nil, nil)
	v = finalizeVerification(v, d)
	if !v.ScopeOnly || v.CorrectnessCertified {
		t.Fatalf("verification overclaimed correctness: %+v", v)
	}
}

func testRequest() Request {
	return Request{
		SchemaVersion: SchemaVersion,
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  "github.com/example/project",
			Revision:          testRevision,
			RevisionStatus:    architecture.RevisionResolved,
			GraphDigestSHA256: testGraph,
			GraphDigestStatus: architecture.GraphDigestResolved,
		},
		Convergence: ConvergenceBinding{SessionID: "convergence.test", IterationDigestSHA256: testIter, SemanticStateDigestSHA256: testSemantic},
		Mode:        ModeModify,
		TaskClass:   "modify_repository_admission",
		Scope:       ChangeScope{Files: []FileOperation{{Path: "a.go", Operation: OperationModify}}},
	}
}

func testBundle(verdict, status string, conditions []closure.Condition) Bundle {
	binding := testRequest().Binding
	claim := architecture.Claim{ID: "claim.a", Statement: architecture.ClaimStatement{Subject: "a", Predicate: "is", Object: "bounded"}, Scope: architecture.ClaimScope{Files: []string{"a.go"}}, EpistemicStatus: architecture.StatusSupported, ArchitecturalPlane: architecture.PlaneIntended}
	return Bundle{
		Session: convergence.Session{SessionID: "convergence.test", Binding: binding},
		LatestIteration: convergence.Iteration{
			Index:                     1,
			IterationDigestSHA256:     testIter,
			SemanticStateDigestSHA256: testSemantic,
			Status:                    status,
			ClosureVerdict:            verdict,
			WaitClasses:               []string{convergence.WaitArchitect},
			NextActions:               []convergence.NextAction{{Class: "answer_question", Priority: "high", Reference: "question.one", Summary: "answer question.one"}},
		},
		MaintainedClaims: architecture.ClaimDocument{Binding: binding, Claims: []architecture.Claim{claim}},
		ClosureAfter: closure.Report{
			ObservedBinding: binding,
			Verdict:         verdict,
			Request: closure.Request{
				TaskID:  "task.modify_repository_admission",
				Binding: binding,
				Scope: closure.Scope{
					TaskClass:  "modify_repository_admission",
					AccessMode: closure.AccessReadWrite,
					Files:      []string{"a.go"},
				},
			},
			ScopeReceipt: closure.ScopeReceipt{Files: []string{"a.go"}, ClaimIDs: []string{"claim.a"}, PropositionKeys: []string{"a:is:bounded"}},
			Conditions:   conditions,
		},
		Dialogue: architecture.DialogueDocument{Binding: binding},
		Probes:   probe.ProbeDocument{Binding: binding},
		StageBytes: map[string][]byte{
			"closure-after-dialogue.yaml": []byte("closure-after-dialogue"),
		},
	}
}

func evaluateFixture(t *testing.T, req Request, b Bundle) Decision {
	t.Helper()
	req, err := NormalizeRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	repo := tempRepoFile(t, "a.go")
	d, err := EvaluateLoaded(mustPolicy(t), req, b, closure.GraphIndex{}, nil, repo, testRevision)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func tempRepoFile(t *testing.T, path string) string {
	t.Helper()
	dir := t.TempDir()
	full := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmds := [][]string{
		{"init"},
		{"config", "user.email", "sensei@example.test"},
		{"config", "user.name", "Sensei Test"},
		{"add", "."},
		{"commit", "-m", "initial"},
	}
	for _, args := range cmds {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func validBootstrapDirectionAuthorization(t *testing.T, repo string, postContent []byte, recordIDs []string) closure.DirectionBootstrapAuthorization {
	t.Helper()
	sourcePath := writeExternalApprovalArtifact(t, []byte("approved by human"))
	revision := repoHead(t, repo)
	auth := closure.DirectionBootstrapAuthorization{
		SchemaVersion:              closure.DirectionBootstrapSchemaVersion,
		PolicyID:                   closure.DirectionBootstrapPolicyID,
		TaskID:                     "task.bootstrap.direction",
		BaseRevision:               revision,
		GraphDigestSHA256:          testGraph,
		File:                       closure.DirectionBootstrapFile,
		GovernedRecordIDs:          recordIDs,
		ApprovedBy:                 "architect",
		ApprovalMechanism:          closure.DirectionBootstrapMechanismFile,
		ApprovalStatement:          "bootstrap once",
		UsagePolicy:                closure.DirectionBootstrapUsageOneUse,
		IssuedAt:                   time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
		ExpiresAt:                  time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339),
		ApprovalSourcePath:         sourcePath,
		ApprovalSourceDigestSHA256: digest([]byte("approved by human")),
	}
	d, err := DirectionBootstrapMutationDigest(repo, revision, closure.DirectionBootstrapFile, recordIDs, postContent)
	if err != nil {
		t.Fatal(err)
	}
	auth.ExpectedMutationDigestSHA256 = d
	auth.AuthorizationDigestSHA256 = closure.DirectionBootstrapAuthorizationDigest(auth)
	return auth
}

func repoHead(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func writeExternalApprovalArtifact(t *testing.T, data []byte) string {
	t.Helper()
	base, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(base) == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			t.Fatalf("resolve home for approval artifact: %v / %v", err, homeErr)
		}
		base = filepath.Join(home, ".cache")
	}
	dir := filepath.Join(base, "sensei-bootstrap-tests", strings.ReplaceAll(t.Name(), "/", "_"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(closure.DirectionBootstrapApprovalDirEnv, dir)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "bootstrap-direction-authorization.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

type authoredNodeOptions struct {
	Status          string
	PromotionStatus string
	ReviewStatus    string
	SourceKind      string
	AuthoredIn      []string
}

func tempAuthoredRepoFile(t *testing.T, rel string) (string, string) {
	t.Helper()
	repo := tempRepoFile(t, "placeholder.go")
	full := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("kind: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo, rel
}

func authoredInGraph(t *testing.T, class, classIRIValue, id string, opts authoredNodeOptions) closure.GraphIndex {
	t.Helper()
	iri := authoredClassIRI(class, id)
	lines := []string{ntTriple(iri, rdf.PropType, classIRIValue, true)}
	if opts.Status != "" {
		lines = append(lines, ntTriple(iri, rdf.PropStatus, opts.Status, false))
	}
	if opts.PromotionStatus != "" {
		lines = append(lines, ntTriple(iri, rdf.PropPromotionStatus, opts.PromotionStatus, false))
	}
	if opts.ReviewStatus != "" {
		lines = append(lines, ntTriple(iri, rdf.PropReviewStatus, opts.ReviewStatus, false))
	}
	if opts.SourceKind != "" {
		lines = append(lines, ntTriple(iri, rdf.PropSourceKind, opts.SourceKind, false))
	}
	for _, path := range opts.AuthoredIn {
		lines = append(lines, ntTriple(iri, rdf.PropAuthoredIn, path, false))
	}
	triples, err := graphsnapshot.Read(strings.NewReader(strings.Join(lines, "\n") + "\n"))
	if err != nil {
		t.Fatalf("graphsnapshot.Read: %v", err)
	}
	return closure.BuildGraphIndex(triples)
}

func authoredClassIRI(class, id string) string {
	classIRI := map[string]string{
		"decision":         rdf.ClassDecision,
		"invariant":        rdf.ClassInvariant,
		"failure_mode":     rdf.ClassFailureMode,
		"authority_domain": rdf.ClassAuthorityDomain,
	}[class]
	return strings.Trim(strings.TrimSuffix(rdf.MintIRI(classIRI, id), ">"), "<")
}

func ntTriple(subject, predicate, object string, objectIRI bool) string {
	if objectIRI {
		return "<" + subject + "> <" + predicate + "> <" + object + "> ."
	}
	return "<" + subject + "> <" + predicate + "> \"" + strings.ReplaceAll(object, "\"", "\\\"") + "\" ."
}

func containsPath(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func mustPolicy(t *testing.T) Policy {
	t.Helper()
	p, ok := PolicyByID(PolicyStrictID)
	if !ok {
		t.Fatal("missing strict policy")
	}
	return p
}

func hasReason(reasons []Reason, code string) bool {
	for _, r := range reasons {
		if r.Code == code {
			return true
		}
	}
	return false
}
