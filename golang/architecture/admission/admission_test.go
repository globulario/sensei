// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/convergence"
	"github.com/globulario/sensei/golang/architecture/probe"
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
	ad, err := EvaluateLoaded(mustPolicy(t), ra, ba, graph, repo, testRevision)
	if err != nil {
		t.Fatal(err)
	}
	bd, err := EvaluateLoaded(mustPolicy(t), rb, ba, graph, repo, testRevision)
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
			Request: closure.Request{Scope: closure.Scope{
				TaskClass:  "modify_repository_admission",
				AccessMode: closure.AccessReadWrite,
				Files:      []string{"a.go"},
			}},
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
	d, err := EvaluateLoaded(mustPolicy(t), req, b, closure.GraphIndex{}, repo, testRevision)
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
	return dir
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
