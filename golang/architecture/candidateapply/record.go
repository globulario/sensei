// SPDX-License-Identifier: AGPL-3.0-only

// record.go owns the immutable link between an application that already
// closed and an admission verification produced afterwards.
//
// The two are separate facts observed at separate times. An O5B application
// receipt is the consumption record for one application: it closes when
// materialization succeeds, and it must not be rewritten later merely because
// verification now exists. But a verification that names no application is not
// evidence about anything -- so the link itself has to be a document.
//
// This is deliberately NOT solved by deleting the application receipt and
// re-applying, nor by mutating it in place. A recording failure must never be
// repairable by performing a second mutation of the repository.
package candidateapply

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/globulario/sensei/golang/architecture/admission"
)

// verificationLineage is the single rule deciding whether a verification
// describes a given application. AttachVerification (the historical, receipt-
// mutating path) and RecordVerification (the production path) both call it, so
// the two can never drift into disagreeing about what "bound to this
// application" means -- which would be worse than either rule alone, because
// whichever surface an operator used would decide the answer.
func verificationLineage(receipt Receipt, decision admission.Decision, verification admission.Verification) (admission.Decision, admission.Verification, error) {
	if err := ValidateReceipt(receipt); err != nil {
		return admission.Decision{}, admission.Verification{}, err
	}
	if receipt.Disposition != DispositionApplied && receipt.Disposition != DispositionVerificationRecorded {
		return admission.Decision{}, admission.Verification{}, errors.New("candidateapply: verification requires an applied receipt")
	}
	canonicalDecision, err := canonicalDecision(decision)
	if err != nil {
		return admission.Decision{}, admission.Verification{}, err
	}
	if canonicalDecision.DecisionDigestSHA256 != receipt.AdmissionDecisionDigestSHA256 {
		return admission.Decision{}, admission.Verification{}, errors.New("candidateapply: admission decision changed before verification")
	}
	canonicalVerification, err := canonicalVerification(verification)
	if err != nil {
		return admission.Decision{}, admission.Verification{}, err
	}
	if canonicalVerification.AdmissionID != canonicalDecision.AdmissionID ||
		canonicalVerification.DecisionDigestSHA256 != canonicalDecision.DecisionDigestSHA256 ||
		canonicalVerification.PatchDigestSHA256 != receipt.PatchDigestSHA256 ||
		!reflect.DeepEqual(canonicalVerification.Binding, canonicalDecision.Binding) {
		return admission.Decision{}, admission.Verification{}, errors.New("candidateapply: verification is not bound to the applied candidate")
	}
	return canonicalDecision, canonicalVerification, nil
}

// RecordVerification composes the immutable binding between one already-closed
// application and one already-produced admission verification.
//
// It never mutates the application receipt, never applies files, and never
// judges correctness: the status it carries is the admission owner's, verbatim.
// A scope-violated verification records exactly as successfully as a compliant
// one -- the record is a statement that this verification describes this
// application, and "the verification says the applied result violated scope"
// is a fact worth recording, not a failure to record.
func RecordVerification(receipt Receipt, decision admission.Decision, verification admission.Verification, observedAt string) (VerificationRecord, error) {
	_, canonicalVerification, err := verificationLineage(receipt, decision, verification)
	if err != nil {
		return VerificationRecord{}, err
	}
	if strings.TrimSpace(observedAt) == "" {
		return VerificationRecord{}, errors.New("candidateapply: observed_at is required")
	}
	if strings.TrimSpace(receipt.ReceiptDigestSHA256) == "" {
		return VerificationRecord{}, errors.New("candidateapply: the application receipt carries no digest to bind to")
	}

	record := VerificationRecord{
		SchemaVersion: VerificationRecordSchemaVersion,
		// Keyed by BOTH sides. One application may be described by several
		// later verifications, and each is its own immutable record rather
		// than a rewrite of the last.
		RecordID:                          "o5b-verification." + receipt.ReceiptDigestSHA256[:12] + "." + canonicalVerification.VerificationDigestSHA256[:12],
		GeneratedBy:                       GeneratedBy,
		ApplicationReceiptDigestSHA256:    receipt.ReceiptDigestSHA256,
		RequestDigestSHA256:               receipt.RequestDigestSHA256,
		AdmissionDecisionDigestSHA256:     receipt.AdmissionDecisionDigestSHA256,
		CandidateArtifactDigestSHA256:     receipt.CandidateArtifactDigestSHA256,
		PatchDigestSHA256:                 receipt.PatchDigestSHA256,
		AdmissionVerificationDigestSHA256: canonicalVerification.VerificationDigestSHA256,
		AdmissionVerificationStatus:       canonicalVerification.Status,
		ObservedAt:                        observedAt,
	}
	digest, err := VerificationRecordDigest(record)
	if err != nil {
		return VerificationRecord{}, err
	}
	record.RecordDigestSHA256 = digest
	return record, ValidateVerificationRecord(record)
}

// ValidateVerificationRecord fails closed on anything that would make the
// record unable to name what it claims to link.
func ValidateVerificationRecord(in VerificationRecord) error {
	r := NormalizeVerificationRecord(in)
	if r.SchemaVersion != VerificationRecordSchemaVersion || r.GeneratedBy != GeneratedBy || r.RecordID == "" {
		return errors.New("candidateapply: unsupported verification record identity")
	}
	for _, digest := range []string{
		r.ApplicationReceiptDigestSHA256,
		r.RequestDigestSHA256,
		r.AdmissionDecisionDigestSHA256,
		r.CandidateArtifactDigestSHA256,
		r.PatchDigestSHA256,
		r.AdmissionVerificationDigestSHA256,
		r.RecordDigestSHA256,
	} {
		if !isSHA256(digest) {
			return errors.New("candidateapply: verification record contains a non-SHA-256 digest")
		}
	}
	if r.AdmissionVerificationStatus == "" {
		return errors.New("candidateapply: verification record carries no admission verification status")
	}
	if r.ObservedAt == "" {
		return errors.New("candidateapply: verification record carries no observation time")
	}
	expected, err := VerificationRecordDigest(r)
	if err != nil {
		return err
	}
	if expected != r.RecordDigestSHA256 {
		return fmt.Errorf("candidateapply: verification record digest mismatch: got %s want %s", r.RecordDigestSHA256, expected)
	}
	return nil
}
