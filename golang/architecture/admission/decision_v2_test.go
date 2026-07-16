// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

const v2DecidedAt = "2026-07-16T12:05:00Z"

func v2ActorBinding() closureprotocol.ActorBinding {
	return closureprotocol.ActorBinding{
		PrincipalID: "actor.dave",
		ActorKind:   closureprotocol.ActorHuman,
		Roles:       []string{"owner"},
		Issuer:      "local-review",
	}
}

func v2BaseBinding() closureprotocol.BaseBinding {
	return closureprotocol.BaseBinding{
		Repository: closureprotocol.RepositorySnapshot{Domain: "github.com/globulario/sensei", Revision: "abcdef1234", RevisionStatus: "resolved"},
		Graph:      closureprotocol.GraphSnapshot{DigestSHA256: "graph123", DigestStatus: "resolved"},
		Task:       closureprotocol.TaskBinding{ID: "task.v2", SessionID: "session.v2"},
		Policies: closureprotocol.PolicyBinding{
			Admission:        "admission.strict.v2",
			Certification:    "certification.architectural_closure.v1",
			Completion:       "completion.architectural_closure.v1",
			Canonicalization: "canonicalization.architectural_closure.v1",
		},
	}
}

func v2ChangePlan() closureprotocol.ChangePlan {
	return closureprotocol.ChangePlan{
		PlanID: "plan.v2",
		Operations: []closureprotocol.ChangeOperation{{
			OperationID:       "op.modify.admission",
			Kind:              closureprotocol.OperationModify,
			TargetKind:        "file",
			Target:            "golang/architecture/admission/admission.go",
			SelectedMechanism: closureprotocol.MechanismRepositoryEdit,
		}},
	}
}

func v2Policy() AdmissionV2Policy {
	return AdmissionV2Policy{
		PolicyID:                 "admission.strict.v2",
		CompletionPolicyID:       "completion.architectural_closure.v1",
		RequiredProofSlots:       []string{"slot.tests"},
		RequiredEvidenceProfiles: []string{"profile.test"},
		ValidityWindow:           24 * time.Hour,
	}
}

// v2Fixture builds a matched request+resolution whose digests line up so
// DecideAdmission admits. Tests mutate the pair (recomputing the resolution
// digest when they touch the resolution) to exercise each refusal path.
func v2Fixture(t *testing.T) (closureprotocol.AdmissionRequest, closureprotocol.AuthorityResolution) {
	t.Helper()
	actor := v2ActorBinding()
	base := v2BaseBinding()
	res := closureprotocol.AuthorityResolution{
		ActorBindingDigestSHA256:         closureprotocol.MustSemanticDigest(actor),
		BaseBindingDigestSHA256:          closureprotocol.MustSemanticDigest(base),
		ClosureAssessmentDigestSHA256:    "closure123",
		OperationSetDigestSHA256:         "opset123",
		AuthorityPolicyGraphDigestSHA256: "authpolicy123",
		PolicyID:                         "admission.strict.v2",
		EvaluatedAt:                      "2026-07-16T12:00:00Z",
		Status:                           closureprotocol.ReceiptValid,
		OperationResults: []closureprotocol.AuthorityResolutionOperation{{
			OperationID:       "op.modify.admission",
			Status:            closureprotocol.ReceiptValid,
			SelectedMechanism: closureprotocol.MechanismRepositoryEdit,
			LegalMechanisms:   []string{"repository_edit"},
		}},
	}
	req := closureprotocol.AdmissionRequest{
		ActorBinding:                    actor,
		BaseBinding:                     base,
		ChangePlan:                      v2ChangePlan(),
		AuthorityResolutionDigestSHA256: rebindResolution(t, &res),
		PolicyID:                        "admission.strict.v2",
	}
	return req, res
}

// rebindResolution recomputes the resolution's self-digest, stamps it, and
// returns it (so a matching request can bind to it).
func rebindResolution(t *testing.T, res *closureprotocol.AuthorityResolution) string {
	t.Helper()
	digest, err := closureprotocol.AuthorityResolutionDigest(*res)
	if err != nil {
		t.Fatal(err)
	}
	res.AuthorityResolutionDigestSHA256 = digest
	return digest
}

func TestDecideAdmissionAdmitsResolvedOperation(t *testing.T) {
	req, res := v2Fixture(t)
	d, err := DecideAdmission(req, res, v2Policy(), v2DecidedAt)
	if err != nil {
		t.Fatalf("DecideAdmission: %v", err)
	}
	if !AllAdmitted(d) {
		t.Fatalf("expected all admitted, got verdicts %+v", d.OperationVerdicts)
	}
	if d.CapabilityID == "" || !strings.HasPrefix(d.CapabilityID, "capability.") {
		t.Fatalf("expected minted capability id, got %q", d.CapabilityID)
	}
	if d.CapabilityExpiry != "2026-07-17T12:05:00Z" {
		t.Fatalf("expected expiry decided_at+24h, got %q", d.CapabilityExpiry)
	}
	if d.RequestDigestSHA256 == "" || d.CompletionPolicyID != "completion.architectural_closure.v1" {
		t.Fatalf("decision missing request digest or completion policy: %+v", d)
	}
	if err := closureprotocol.ValidateAdmissionDecision(d); err != nil {
		t.Fatalf("decision failed frozen validation: %v", err)
	}
}

func TestDecideAdmissionIsDeterministic(t *testing.T) {
	req, res := v2Fixture(t)
	a, err := DecideAdmission(req, res, v2Policy(), v2DecidedAt)
	if err != nil {
		t.Fatal(err)
	}
	b, err := DecideAdmission(req, res, v2Policy(), v2DecidedAt)
	if err != nil {
		t.Fatal(err)
	}
	if a.CapabilityID != b.CapabilityID || a.RequestDigestSHA256 != b.RequestDigestSHA256 {
		t.Fatal("admission decision is not deterministic for the same request")
	}
}

func TestDecideAdmissionRejectsForgedAuthorityDigest(t *testing.T) {
	req, res := v2Fixture(t)
	req.AuthorityResolutionDigestSHA256 = strings.Repeat("f", 64)
	if _, err := DecideAdmission(req, res, v2Policy(), v2DecidedAt); err == nil || !strings.Contains(err.Error(), "authority resolution digest does not match") {
		t.Fatalf("expected forged authority digest rejection, got %v", err)
	}
}

func TestDecideAdmissionRejectsActorMismatch(t *testing.T) {
	req, res := v2Fixture(t)
	res.ActorBindingDigestSHA256 = strings.Repeat("a", 64) // resolution about a different actor
	req.AuthorityResolutionDigestSHA256 = rebindResolution(t, &res)
	if _, err := DecideAdmission(req, res, v2Policy(), v2DecidedAt); err == nil || !strings.Contains(err.Error(), "actor binding does not match") {
		t.Fatalf("expected actor mismatch rejection, got %v", err)
	}
}

func TestDecideAdmissionRejectsBaseMismatch(t *testing.T) {
	req, res := v2Fixture(t)
	res.BaseBindingDigestSHA256 = strings.Repeat("b", 64)
	req.AuthorityResolutionDigestSHA256 = rebindResolution(t, &res)
	if _, err := DecideAdmission(req, res, v2Policy(), v2DecidedAt); err == nil || !strings.Contains(err.Error(), "base binding does not match") {
		t.Fatalf("expected base mismatch rejection, got %v", err)
	}
}

func TestDecideAdmissionRefusesUnresolvedOperation(t *testing.T) {
	req, res := v2Fixture(t)
	// A second operation the authority resolution never covered.
	req.ChangePlan.Operations = append(req.ChangePlan.Operations, closureprotocol.ChangeOperation{
		OperationID:       "op.modify.other",
		Kind:              closureprotocol.OperationModify,
		TargetKind:        "file",
		Target:            "golang/architecture/closure/model.go",
		SelectedMechanism: closureprotocol.MechanismRepositoryEdit,
	})
	d, err := DecideAdmission(req, res, v2Policy(), v2DecidedAt)
	if err != nil {
		t.Fatalf("DecideAdmission should record the refusal, not error: %v", err)
	}
	if AllAdmitted(d) {
		t.Fatal("expected not-all-admitted for an unresolved operation")
	}
	if got := d.OperationVerdicts[1]; got.Verdict != AdmissionVerdictRefused || got.ReasonCodes[0] != "admission.authority.unresolved" {
		t.Fatalf("expected unresolved refusal, got %+v", got)
	}
}

func TestDecideAdmissionRefusesInvalidAuthority(t *testing.T) {
	req, res := v2Fixture(t)
	res.OperationResults[0].Status = closureprotocol.ReceiptInvalid
	req.AuthorityResolutionDigestSHA256 = rebindResolution(t, &res)
	d, err := DecideAdmission(req, res, v2Policy(), v2DecidedAt)
	if err != nil {
		t.Fatalf("DecideAdmission should record the refusal, not error: %v", err)
	}
	if AllAdmitted(d) {
		t.Fatal("expected refusal when authority is not valid")
	}
	if d.OperationVerdicts[0].ReasonCodes[0] != "admission.authority.not_valid" {
		t.Fatalf("expected not_valid reason, got %+v", d.OperationVerdicts[0])
	}
}

func TestDecideAdmissionRefusesMechanismMismatch(t *testing.T) {
	req, res := v2Fixture(t)
	req.ChangePlan.Operations[0].SelectedMechanism = closureprotocol.MechanismOwnerRPC // resolution authorized repository_edit
	d, err := DecideAdmission(req, res, v2Policy(), v2DecidedAt)
	if err != nil {
		t.Fatalf("DecideAdmission should record the refusal, not error: %v", err)
	}
	if AllAdmitted(d) {
		t.Fatal("expected refusal when the selected mechanism is not the authorized one")
	}
	if d.OperationVerdicts[0].ReasonCodes[0] != "admission.mechanism.mismatch" {
		t.Fatalf("expected mechanism mismatch, got %+v", d.OperationVerdicts[0])
	}
}

func TestDecideAdmissionRejectsPolicyMismatch(t *testing.T) {
	req, res := v2Fixture(t)
	p := v2Policy()
	p.PolicyID = "admission.other"
	if _, err := DecideAdmission(req, res, p, v2DecidedAt); err == nil || !strings.Contains(err.Error(), "policy id does not match") {
		t.Fatalf("expected policy mismatch rejection, got %v", err)
	}
}
