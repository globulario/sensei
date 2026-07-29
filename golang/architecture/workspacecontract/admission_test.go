// SPDX-License-Identifier: AGPL-3.0-only

package workspacecontract

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	admissionpkg "github.com/globulario/sensei/golang/architecture/admission"
)

func minimalDecisionForBindingTest() admissionpkg.Decision {
	digest := strings.Repeat("1", 64)
	return admissionpkg.Decision{
		SchemaVersion: admissionpkg.SchemaVersion,
		GeneratedBy:   admissionpkg.GeneratedBy,
		AdmissionID:   "admission.bind-test",
		PolicyID:      admissionpkg.PolicyStrictID,
		PolicyVersion: admissionpkg.PolicyStrictVersion,
		Decision:      admissionpkg.DecisionAdmitted,
		RequestedMode: admissionpkg.ModeModify,
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  "github.com/example/bind-test",
			Revision:          "abc123",
			RevisionStatus:    architecture.RevisionResolved,
			GraphDigestSHA256: digest,
			GraphDigestStatus: architecture.GraphDigestResolved,
		},
		InspectionCapability: admissionpkg.CapabilityAdmitted,
		MutationCapability:   admissionpkg.CapabilityAdmitted,
		DecisionDigestSHA256: digest,
	}
}

// matchingVerificationForBindingTest returns a Verification whose
// AdmissionID/DecisionDigestSHA256/Binding all match d exactly — the only
// shape ProjectVerification may accept.
func matchingVerificationForBindingTest(d admissionpkg.Decision) admissionpkg.Verification {
	return admissionpkg.Verification{
		SchemaVersion:            admissionpkg.SchemaVersion,
		GeneratedBy:              admissionpkg.GeneratedBy,
		AdmissionID:              d.AdmissionID,
		DecisionDigestSHA256:     d.DecisionDigestSHA256,
		Status:                   admissionpkg.VerificationScopeCompliant,
		Binding:                  d.Binding,
		VerificationDigestSHA256: strings.Repeat("2", 64),
	}
}

// TestProjectVerification_AcceptsMatchingPair proves the ordinary, correct
// case still works: a verification whose identity/binding genuinely
// reference the decision projects successfully.
func TestProjectVerification_AcceptsMatchingPair(t *testing.T) {
	d := minimalDecisionForBindingTest()
	v := matchingVerificationForBindingTest(d)
	rec, err := ProjectVerification(d, v)
	if err != nil {
		t.Fatalf("expected a matching decision/verification pair to project, got: %v", err)
	}
	if rec.RecordKind != RecordKindVerification || rec.Verification == nil {
		t.Fatalf("unexpected record shape: %+v", rec)
	}
}

// TestProjectVerification_RejectsMismatchedAdmissionID proves the fail-closed
// repair: a caller (with no filesystem race involved at all — these are
// two independently-constructed in-memory values) supplying a verification
// bound to a DIFFERENT admission_id must be refused, never silently
// projected under the supplied decision's identity.
func TestProjectVerification_RejectsMismatchedAdmissionID(t *testing.T) {
	d := minimalDecisionForBindingTest()
	v := matchingVerificationForBindingTest(d)
	v.AdmissionID = "admission.someone-elses-decision"

	if _, err := ProjectVerification(d, v); err == nil {
		t.Fatal("expected a mismatched admission_id to be refused, got a projected record instead")
	}
}

// TestProjectVerification_RejectsMismatchedDecisionDigest proves the same
// fail-closed behavior for decision_digest_sha256.
func TestProjectVerification_RejectsMismatchedDecisionDigest(t *testing.T) {
	d := minimalDecisionForBindingTest()
	v := matchingVerificationForBindingTest(d)
	v.DecisionDigestSHA256 = strings.Repeat("9", 64)

	if _, err := ProjectVerification(d, v); err == nil {
		t.Fatal("expected a mismatched decision_digest_sha256 to be refused, got a projected record instead")
	}
}

// TestProjectVerification_RejectsMismatchedBinding proves the same
// fail-closed behavior for binding — a verification that shares the
// decision's admission_id/decision_digest by coincidence (or forgery) but
// was actually computed against a different repository/revision/graph
// binding must still be refused.
func TestProjectVerification_RejectsMismatchedBinding(t *testing.T) {
	d := minimalDecisionForBindingTest()
	v := matchingVerificationForBindingTest(d)
	v.Binding.RepositoryDomain = "github.com/someone/else"

	if _, err := ProjectVerification(d, v); err == nil {
		t.Fatal("expected a mismatched binding to be refused, got a projected record instead")
	}
}
