// SPDX-License-Identifier: AGPL-3.0-only

package candidateapply

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/admission"
)

func ValidateRequest(in Request) error {
	r := NormalizeRequest(in)
	if r.SchemaVersion != RequestSchemaVersion || r.GeneratedBy != GeneratedBy || strings.TrimSpace(r.RequestID) == "" {
		return errors.New("candidateapply: unsupported request identity")
	}
	for _, digest := range []string{
		r.AdmissionCompositionRequestDigestSHA256,
		r.AdmissionCompositionReceiptDigestSHA256,
		r.AdmissionDecisionDigestSHA256,
		r.CandidateArtifactDigestSHA256,
		r.InputCandidateDigestSHA256,
		r.FinalCandidateContentDigestSHA256,
		r.ProposedChangeDigestSHA256,
		r.RequestDigestSHA256,
	} {
		if !isSHA256(digest) {
			return errors.New("candidateapply: request contains a non-SHA-256 digest")
		}
	}
	if r.RepositoryDomain == "" || r.BaseRevision == "" || len(r.ModifyPaths) == 0 {
		return errors.New("candidateapply: repository, base revision, and modify paths are required")
	}
	seen := map[string]bool{}
	for _, path := range r.ModifyPaths {
		if path == "" || seen[path] {
			return fmt.Errorf("candidateapply: invalid or duplicate modify path %q", path)
		}
		seen[path] = true
	}
	want, err := RequestDigest(r)
	if err != nil {
		return err
	}
	if r.RequestDigestSHA256 != want {
		return fmt.Errorf("candidateapply: request digest mismatch: got %s want %s", r.RequestDigestSHA256, want)
	}
	return nil
}

func ValidateReceipt(in Receipt) error {
	r := NormalizeReceipt(in)
	if r.SchemaVersion != ReceiptSchemaVersion || r.GeneratedBy != GeneratedBy || strings.TrimSpace(r.ReceiptID) == "" {
		return errors.New("candidateapply: unsupported receipt identity")
	}
	for _, digest := range []string{
		r.RequestDigestSHA256,
		r.AdmissionCompositionReceiptDigestSHA256,
		r.AdmissionDecisionDigestSHA256,
		r.CandidateArtifactDigestSHA256,
		r.InputCandidateDigestSHA256,
		r.FinalCandidateContentDigestSHA256,
		r.ReceiptDigestSHA256,
	} {
		if !isSHA256(digest) {
			return errors.New("candidateapply: receipt contains a non-SHA-256 digest")
		}
	}
	if len(r.AppliedPaths) == 0 {
		return errors.New("candidateapply: applied paths are required")
	}
	switch r.Disposition {
	case DispositionApplied:
		if r.AdmissionVerificationStatus != nil || r.AdmissionVerificationDigestSHA256 != nil {
			return errors.New("candidateapply: applied receipt carries premature verification evidence")
		}
	case DispositionVerificationRecorded:
		if r.AdmissionVerificationStatus == nil || r.AdmissionVerificationDigestSHA256 == nil || !isSHA256(*r.AdmissionVerificationDigestSHA256) {
			return errors.New("candidateapply: verification-recorded receipt lacks verification evidence")
		}
		if !oneOf(*r.AdmissionVerificationStatus,
			admission.VerificationScopeCompliant,
			admission.VerificationScopeViolated,
			admission.VerificationStale,
			admission.VerificationUncertifiable,
		) {
			return errors.New("candidateapply: unknown verification status")
		}
	default:
		return errors.New("candidateapply: unknown receipt disposition")
	}
	want, err := ReceiptDigest(r)
	if err != nil {
		return err
	}
	if r.ReceiptDigestSHA256 != want {
		return fmt.Errorf("candidateapply: receipt digest mismatch: got %s want %s", r.ReceiptDigestSHA256, want)
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
