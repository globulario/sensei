// SPDX-License-Identifier: AGPL-3.0-only

package admission

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

const (
	v2BaseTree   = "basetree123"
	v2ResultTree = "resulttree123"
	v2VerifiedAt = "2026-07-16T13:30:00Z"
	v2Target     = "golang/architecture/admission/admission.go"
)

// scopeFixture builds an admitted decision, its consumption, and a matching
// in-envelope observed change. Tests mutate the pair to exercise each violation.
func scopeFixture(t *testing.T) (ScopeExpectation, ObservedChangeSet) {
	t.Helper()
	req, res := v2Fixture(t)
	d, err := DecideAdmission(req, res, v2Policy(), v2DecidedAt)
	if err != nil {
		t.Fatalf("DecideAdmission: %v", err)
	}
	cons, err := ConsumeCapability(d, v2Task(), v2ActorBinding(), []string{"op.modify.admission"}, v2ConsumedAt)
	if err != nil {
		t.Fatalf("ConsumeCapability: %v", err)
	}
	actorDigest := closureprotocol.MustSemanticDigest(v2ActorBinding())
	exp := ScopeExpectation{
		Decision:                        d,
		Operations:                      req.ChangePlan.Operations,
		Consumption:                     cons,
		ActorBindingDigestSHA256:        actorDigest,
		AuthorityResolutionDigestSHA256: req.AuthorityResolutionDigestSHA256,
		BaseTreeDigestSHA256:            v2BaseTree,
	}
	observed := ObservedChangeSet{
		BaseTreeDigestSHA256:            v2BaseTree,
		ResultTreeDigestSHA256:          v2ResultTree,
		ActorBindingDigestSHA256:        actorDigest,
		AuthorityResolutionDigestSHA256: req.AuthorityResolutionDigestSHA256,
		Files:                           []ObservedFile{{Path: v2Target, ChangeType: "modify"}},
	}
	return exp, observed
}

func firstViolation(v ScopeVerification) string {
	if len(v.Violations) == 0 {
		return ""
	}
	return v.Violations[0].Code
}

func hasViolation(v ScopeVerification, code string) bool {
	for _, viol := range v.Violations {
		if viol.Code == code {
			return true
		}
	}
	return false
}

func TestVerifyScopeAcceptsInEnvelopeChange(t *testing.T) {
	exp, observed := scopeFixture(t)
	v, err := VerifyScope(exp, observed, v2VerifiedAt)
	if err != nil {
		t.Fatalf("VerifyScope: %v", err)
	}
	if !ScopeVerified(v) {
		t.Fatalf("expected verified scope, got status=%s violations=%+v", v.Status, v.Violations)
	}
	if len(v.VerifiedOperationIDs) != 1 || v.VerifiedOperationIDs[0] != "op.modify.admission" {
		t.Fatalf("unexpected verified operations: %+v", v.VerifiedOperationIDs)
	}
	if v.ScopeVerificationDigestSHA256 == "" || v.ResultTreeDigestSHA256 != v2ResultTree {
		t.Fatalf("verification missing digest or result tree: %+v", v)
	}
}

func TestVerifyScopeRejectsExtraFile(t *testing.T) {
	exp, observed := scopeFixture(t)
	observed.Files = append(observed.Files, ObservedFile{Path: "README.md", ChangeType: "modify"})
	v, err := VerifyScope(exp, observed, v2VerifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	if ScopeVerified(v) || !hasViolation(v, "scope.file.out_of_envelope") {
		t.Fatalf("expected out-of-envelope violation, got %+v", v.Violations)
	}
}

func TestVerifyScopeRejectsBaseTreeChange(t *testing.T) {
	exp, observed := scopeFixture(t)
	observed.BaseTreeDigestSHA256 = strings.Repeat("9", 64)
	v, err := VerifyScope(exp, observed, v2VerifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	if ScopeVerified(v) || !hasViolation(v, "scope.base_tree.changed") {
		t.Fatalf("expected base-tree-changed violation, got %+v", v.Violations)
	}
}

func TestVerifyScopeRejectsGeneratedOmission(t *testing.T) {
	exp, observed := scopeFixture(t)
	exp.RequiredGeneratedArtifacts = []string{"golang/server/embeddata/awareness.nt"}
	// observed does not include the required rebuilt artifact
	v, err := VerifyScope(exp, observed, v2VerifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	if ScopeVerified(v) || !hasViolation(v, "scope.generated.omitted") {
		t.Fatalf("expected generated-omitted violation, got %+v", v.Violations)
	}
}

func TestVerifyScopeAcceptsRequiredRebuild(t *testing.T) {
	exp, observed := scopeFixture(t)
	exp.RequiredGeneratedArtifacts = []string{"golang/server/embeddata/awareness.nt"}
	observed.Files = append(observed.Files, ObservedFile{Path: "golang/server/embeddata/awareness.nt", ChangeType: "modify"})
	v, err := VerifyScope(exp, observed, v2VerifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !ScopeVerified(v) {
		t.Fatalf("expected verified scope with the rebuilt artifact present, got %+v", v.Violations)
	}
}

func TestVerifyScopeRejectsUnboundConsumption(t *testing.T) {
	exp, observed := scopeFixture(t)
	exp.Consumption.DecisionDigestSHA256 = strings.Repeat("c", 64)
	v, err := VerifyScope(exp, observed, v2VerifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	if ScopeVerified(v) || !hasViolation(v, "scope.capability.unbound") {
		t.Fatalf("expected capability-unbound violation, got %+v", v.Violations)
	}
}

func TestVerifyScopeRejectsActorMismatch(t *testing.T) {
	exp, observed := scopeFixture(t)
	observed.ActorBindingDigestSHA256 = strings.Repeat("a", 64)
	v, err := VerifyScope(exp, observed, v2VerifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	if ScopeVerified(v) || !hasViolation(v, "scope.actor.mismatch") {
		t.Fatalf("expected actor-mismatch violation, got %+v", v.Violations)
	}
}

func TestVerifyScopeRejectsProhibitedPath(t *testing.T) {
	exp, observed := scopeFixture(t)
	exp.ProhibitedPathPrefixes = []string{"secrets/"}
	observed.Files = append(observed.Files, ObservedFile{Path: "secrets/key.pem", ChangeType: "create"})
	v, err := VerifyScope(exp, observed, v2VerifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	if ScopeVerified(v) || !hasViolation(v, "scope.file.prohibited") {
		t.Fatalf("expected prohibited-path violation, got %+v", v.Violations)
	}
}

func TestVerifyScopeRejectsOperationBudget(t *testing.T) {
	op1 := closureprotocol.ChangeOperation{OperationID: "op.a", Kind: closureprotocol.OperationModify, TargetKind: "file", Target: "a.go", SelectedMechanism: closureprotocol.MechanismRepositoryEdit}
	op2 := closureprotocol.ChangeOperation{OperationID: "op.b", Kind: closureprotocol.OperationModify, TargetKind: "file", Target: "b.go", SelectedMechanism: closureprotocol.MechanismRepositoryEdit}
	d := closureprotocol.AdmissionDecision{
		RequestDigestSHA256: strings.Repeat("d", 64),
		PolicyID:            "admission.strict.v2",
		OperationVerdicts: []closureprotocol.OperationAdmissionVerdict{
			{OperationID: "op.a", Verdict: AdmissionVerdictAdmitted},
			{OperationID: "op.b", Verdict: AdmissionVerdictAdmitted},
		},
		CapabilityID:       "capability.budget",
		CompletionPolicyID: "completion.architectural_closure.v1",
		OperationBudget:    1,
	}
	cons, err := ConsumeCapability(d, v2Task(), v2ActorBinding(), []string{"op.a", "op.b"}, v2ConsumedAt)
	if err != nil {
		t.Fatalf("ConsumeCapability: %v", err)
	}
	actorDigest := closureprotocol.MustSemanticDigest(v2ActorBinding())
	exp := ScopeExpectation{
		Decision:                        d,
		Operations:                      []closureprotocol.ChangeOperation{op1, op2},
		Consumption:                     cons,
		ActorBindingDigestSHA256:        actorDigest,
		AuthorityResolutionDigestSHA256: "authority123",
		BaseTreeDigestSHA256:            v2BaseTree,
	}
	observed := ObservedChangeSet{
		BaseTreeDigestSHA256:            v2BaseTree,
		ResultTreeDigestSHA256:          v2ResultTree,
		ActorBindingDigestSHA256:        actorDigest,
		AuthorityResolutionDigestSHA256: "authority123",
		Files:                           []ObservedFile{{Path: "a.go", ChangeType: "modify"}, {Path: "b.go", ChangeType: "modify"}},
	}
	v, err := VerifyScope(exp, observed, v2VerifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	if ScopeVerified(v) || !hasViolation(v, "scope.budget.operations_exceeded") {
		t.Fatalf("expected operation-budget violation, got %+v", v.Violations)
	}
}

func TestVerifyScopeDigestOmitsSelf(t *testing.T) {
	exp, observed := scopeFixture(t)
	v, err := VerifyScope(exp, observed, v2VerifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	d1, err := ScopeVerificationDigest(v)
	if err != nil {
		t.Fatal(err)
	}
	v.ScopeVerificationDigestSHA256 = "different"
	d2, err := ScopeVerificationDigest(v)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatal("self digest field changed the semantic digest")
	}
}
