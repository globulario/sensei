// SPDX-License-Identifier: AGPL-3.0-only

package admissioncomposition

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/evaluatorcomposition"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
)

func TestComposeRequestDerivesExactModifyScope(t *testing.T) {
	in := validInput(t)
	req, concrete, err := ComposeRequest(in)
	if err != nil {
		t.Fatal(err)
	}
	if !req.AdmissionEligible || concrete == nil {
		t.Fatal("expected admission-eligible request")
	}
	if len(req.DerivedScope.Files) != 1 || req.DerivedScope.Files[0] != (admission.FileOperation{Path: "a.txt", Operation: admission.OperationModify}) {
		t.Fatalf("unexpected derived scope: %#v", req.DerivedScope)
	}
	if len(req.UnsupportedOperations) != 0 {
		t.Fatalf("unexpected unsupported operations: %#v", req.UnsupportedOperations)
	}
	if err := ValidateRequest(req); err != nil {
		t.Fatal(err)
	}
}

func TestComposeRequestRejectsCrossLayerLineageTampering(t *testing.T) {
	in := validInput(t)
	in.EvaluationReceipt.RunnerReceiptDigestSHA256 = hex64("wrong-runner")
	digest, err := evaluatorcomposition.EvaluationReceiptDigest(in.EvaluationReceipt)
	if err != nil {
		t.Fatal(err)
	}
	in.EvaluationReceipt.ReceiptDigestSHA256 = digest
	if _, _, err := ComposeRequest(in); err == nil {
		t.Fatal("expected O3/O4 lineage mismatch")
	}
}

func TestDeriveScopePreservesUnsupportedOperations(t *testing.T) {
	base := []runnercomposition.CandidateManifestEntry{
		manifestEntry("delete.txt", []byte("old"), runnercomposition.ModeRegular),
		manifestEntry("mode.txt", []byte("same"), runnercomposition.ModeRegular),
	}
	final := []runnercomposition.CandidateManifestEntry{
		manifestEntry("add.txt", []byte("new"), runnercomposition.ModeRegular),
		manifestEntry("mode.txt", []byte("same"), runnercomposition.ModeExecutable),
	}
	scope, unsupported, err := deriveScope(base, final)
	if err != nil {
		t.Fatal(err)
	}
	if len(scope.Files) != 0 {
		t.Fatalf("unsupported operations leaked into modify scope: %#v", scope.Files)
	}
	if len(unsupported) != 3 {
		t.Fatalf("expected add/delete/type-change, got %#v", unsupported)
	}
}

func TestUnsupportedReceiptCarriesNoAdmissionEvidence(t *testing.T) {
	req := NormalizeRequest(Request{
		SchemaVersion:                 RequestSchemaVersion,
		RequestID:                     "o5a.unsupported",
		GeneratedBy:                   GeneratedBy,
		SynthesisReceiptDigestSHA256:  hex64("o1"),
		RunnerReceiptDigestSHA256:     hex64("o3"),
		EvaluationReceiptDigestSHA256: hex64("o4"),
		CandidateArtifactDigestSHA256: hex64("artifact"),
		RepositoryDomain:              "github.com/globulario/sensei",
		BaseRevision:                  "0123456789012345678901234567890123456789",
		DerivedScope:                  admission.ChangeScope{},
		UnsupportedOperations: []UnsupportedOperation{{
			Path:      "new.txt",
			Operation: admission.ChangeAdded,
			Detail:    "existing admission supports read/modify only",
		}},
	})
	digest, err := RequestDigest(req)
	if err != nil {
		t.Fatal(err)
	}
	req.RequestDigestSHA256 = digest
	receipt, err := ComposeUnsupportedReceipt(req, "2026-08-01T23:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.AdmissionDecision != nil || receipt.AdmissionDecisionDigestSHA256 != nil || receipt.AdmissionVerificationStatus != nil || receipt.AdmissionVerificationDigestSHA256 != nil {
		t.Fatal("unsupported receipt manufactured admission evidence")
	}
}

func TestDecisionAndVerificationRemainBesideFrozenO1Receipt(t *testing.T) {
	in := validInput(t)
	req, concrete, err := ComposeRequest(in)
	if err != nil {
		t.Fatal(err)
	}
	decision := canonicalDecision(t, *concrete, *req.AdmissionRequestIdentityDigestSHA256)
	receipt, err := ComposeDecisionReceipt(req, *concrete, decision, "2026-08-01T23:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if in.SynthesisReceipt.AdmissionDecisionDigestSHA256 != nil || in.SynthesisReceipt.AdmissionVerificationDigestSHA256 != nil {
		t.Fatal("O5A mutated the frozen O1 receipt")
	}
	verification := canonicalVerification(t, decision)
	finalReceipt, err := AttachVerification(receipt, decision, verification, "2026-08-01T23:01:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if finalReceipt.Disposition != DispositionVerificationRecorded || finalReceipt.AdmissionVerificationDigestSHA256 == nil {
		t.Fatalf("verification not recorded: %#v", finalReceipt)
	}
}

func TestReceiptIdentityExcludesObservationTime(t *testing.T) {
	in := validInput(t)
	req, concrete, err := ComposeRequest(in)
	if err != nil {
		t.Fatal(err)
	}
	decision := canonicalDecision(t, *concrete, *req.AdmissionRequestIdentityDigestSHA256)
	a, err := ComposeDecisionReceipt(req, *concrete, decision, "2026-08-01T23:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ComposeDecisionReceipt(req, *concrete, decision, "2026-08-02T01:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if a.ReceiptDigestSHA256 != b.ReceiptDigestSHA256 {
		t.Fatal("observation time changed semantic receipt identity")
	}
}
