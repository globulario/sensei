// SPDX-License-Identifier: AGPL-3.0-only

package admissioncomposition

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/admission"
)

func ValidateRequest(in Request) error {
	r := NormalizeRequest(in)
	if r.SchemaVersion != RequestSchemaVersion || r.GeneratedBy != GeneratedBy {
		return errors.New("admissioncomposition: unsupported request identity")
	}
	if strings.TrimSpace(r.RequestID) == "" || strings.TrimSpace(r.RepositoryDomain) == "" || strings.TrimSpace(r.BaseRevision) == "" {
		return errors.New("admissioncomposition: request identity fields are required")
	}
	for _, digest := range []string{
		r.SynthesisReceiptDigestSHA256,
		r.RunnerReceiptDigestSHA256,
		r.EvaluationReceiptDigestSHA256,
		r.CandidateArtifactDigestSHA256,
		r.RequestDigestSHA256,
	} {
		if !isSHA256(digest) {
			return errors.New("admissioncomposition: request contains a non-SHA-256 digest")
		}
	}
	seen := map[string]bool{}
	for _, op := range r.DerivedScope.Files {
		if strings.TrimSpace(op.Path) == "" || op.Operation != admission.OperationModify {
			return errors.New("admissioncomposition: derived scope may contain only exact modify operations")
		}
		if seen[op.Path] {
			return fmt.Errorf("admissioncomposition: duplicate derived path %q", op.Path)
		}
		seen[op.Path] = true
	}
	for _, op := range r.UnsupportedOperations {
		if strings.TrimSpace(op.Operation) == "" || strings.TrimSpace(op.Detail) == "" {
			return errors.New("admissioncomposition: unsupported operation requires operation and detail")
		}
	}
	if r.AdmissionEligible {
		if len(r.UnsupportedOperations) != 0 || len(r.DerivedScope.Files) == 0 || r.AdmissionRequestDigestSHA256 == nil || r.AdmissionRequestIdentityDigestSHA256 == nil {
			return errors.New("admissioncomposition: eligible request has incomplete admission evidence")
		}
		if !isSHA256(*r.AdmissionRequestDigestSHA256) || !isSHA256(*r.AdmissionRequestIdentityDigestSHA256) {
			return errors.New("admissioncomposition: admission request digests are invalid")
		}
	} else if r.AdmissionRequestDigestSHA256 != nil || r.AdmissionRequestIdentityDigestSHA256 != nil {
		return errors.New("admissioncomposition: ineligible request must not carry admission request digests")
	}
	want, err := RequestDigest(r)
	if err != nil {
		return err
	}
	if r.RequestDigestSHA256 != want {
		return fmt.Errorf("admissioncomposition: request digest mismatch: got %s want %s", r.RequestDigestSHA256, want)
	}
	return nil
}

func ValidateReceipt(r Receipt) error {
	if r.SchemaVersion != ReceiptSchemaVersion || r.GeneratedBy != GeneratedBy || strings.TrimSpace(r.ReceiptID) == "" {
		return errors.New("admissioncomposition: unsupported receipt identity")
	}
	for _, digest := range []string{
		r.RequestDigestSHA256,
		r.SynthesisReceiptDigestSHA256,
		r.CandidateArtifactDigestSHA256,
		r.ReceiptDigestSHA256,
	} {
		if !isSHA256(digest) {
			return errors.New("admissioncomposition: receipt contains a non-SHA-256 digest")
		}
	}
	switch r.Disposition {
	case DispositionUnsupportedOperationRefused:
		if r.AdmissionDecision != nil || r.AdmissionDecisionDigestSHA256 != nil || r.AdmissionVerificationStatus != nil || r.AdmissionVerificationDigestSHA256 != nil || strings.TrimSpace(r.Detail) == "" {
			return errors.New("admissioncomposition: unsupported-operation receipt has invalid evidence presence")
		}
	case DispositionAdmissionDecided:
		if r.AdmissionDecision == nil || r.AdmissionDecisionDigestSHA256 == nil || r.AdmissionVerificationStatus != nil || r.AdmissionVerificationDigestSHA256 != nil {
			return errors.New("admissioncomposition: admission-decided receipt has invalid evidence presence")
		}
	case DispositionVerificationRecorded:
		if r.AdmissionDecision == nil || r.AdmissionDecisionDigestSHA256 == nil || r.AdmissionVerificationStatus == nil || r.AdmissionVerificationDigestSHA256 == nil {
			return errors.New("admissioncomposition: verification-recorded receipt has invalid evidence presence")
		}
	default:
		return errors.New("admissioncomposition: unknown receipt disposition")
	}
	for _, p := range []*string{r.AdmissionDecisionDigestSHA256, r.AdmissionVerificationDigestSHA256} {
		if p != nil && !isSHA256(*p) {
			return errors.New("admissioncomposition: referenced admission digest is invalid")
		}
	}
	if r.AdmissionDecision != nil && !oneOf(*r.AdmissionDecision,
		admission.DecisionAdmitted,
		admission.DecisionAdmittedWithConditions,
		admission.DecisionWaiting,
		admission.DecisionRefused,
		admission.DecisionUncertifiable,
	) {
		return errors.New("admissioncomposition: unknown admission decision")
	}
	if r.AdmissionVerificationStatus != nil && !oneOf(*r.AdmissionVerificationStatus,
		admission.VerificationScopeCompliant,
		admission.VerificationScopeViolated,
		admission.VerificationStale,
		admission.VerificationUncertifiable,
	) {
		return errors.New("admissioncomposition: unknown verification status")
	}
	want, err := ReceiptDigest(r)
	if err != nil {
		return err
	}
	if r.ReceiptDigestSHA256 != want {
		return fmt.Errorf("admissioncomposition: receipt digest mismatch: got %s want %s", r.ReceiptDigestSHA256, want)
	}
	return nil
}

func isSHA256(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
