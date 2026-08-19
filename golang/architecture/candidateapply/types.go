// SPDX-License-Identifier: AGPL-3.0-only

// Package candidateapply implements O5B of the governed synthesis loop. It
// applies only an admitted, sealed CandidateArtifact to a clean, dedicated
// Git worktree at the admitted base revision, then records existing admission
// verification evidence without committing, pushing, approving, or merging.
// Transaction staging and backup files are removed before the admission owner
// observes the resulting patch, while rollback remains bound to base evidence.
package candidateapply

import (
	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/admissioncomposition"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
)

const (
	RequestSchemaVersion = "sensei.candidateapply.request.v1"
	ReceiptSchemaVersion = "sensei.candidateapply.receipt.v1"
	// VerificationRecordSchemaVersion is the immutable LINK between an
	// application that already closed and an admission verification produced
	// afterwards. It exists because those are two facts, observed at two
	// different times: an application receipt closes when materialization
	// succeeds and must not be rewritten later merely because verification now
	// exists (Decision A).
	VerificationRecordSchemaVersion = "sensei.candidateapply.verification-record.v1"
	GeneratedBy                     = "sensei-candidate-apply"
)

type Disposition string

const (
	DispositionApplied              Disposition = "applied"
	DispositionVerificationRecorded Disposition = "verification-recorded"
)

type Request struct {
	SchemaVersion string `json:"schema_version"`
	RequestID     string `json:"request_id"`
	GeneratedBy   string `json:"generated_by"`

	AdmissionCompositionRequestDigestSHA256 string `json:"admission_composition_request_digest_sha256"`
	AdmissionCompositionReceiptDigestSHA256 string `json:"admission_composition_receipt_digest_sha256"`
	AdmissionDecisionDigestSHA256           string `json:"admission_decision_digest_sha256"`
	CandidateArtifactDigestSHA256           string `json:"candidate_artifact_digest_sha256"`

	RepositoryDomain                  string   `json:"repository_domain"`
	BaseRevision                      string   `json:"base_revision"`
	InputCandidateDigestSHA256        string   `json:"input_candidate_digest_sha256"`
	FinalCandidateContentDigestSHA256 string   `json:"final_candidate_content_digest_sha256"`
	ProposedChangeDigestSHA256        string   `json:"proposed_change_digest_sha256"`
	ModifyPaths                       []string `json:"modify_paths"`

	RequestDigestSHA256 string `json:"request_digest_sha256"`
}

type Receipt struct {
	SchemaVersion string `json:"schema_version"`
	ReceiptID     string `json:"receipt_id"`
	GeneratedBy   string `json:"generated_by"`

	RequestDigestSHA256                     string   `json:"request_digest_sha256"`
	AdmissionCompositionReceiptDigestSHA256 string   `json:"admission_composition_receipt_digest_sha256"`
	AdmissionDecisionDigestSHA256           string   `json:"admission_decision_digest_sha256"`
	CandidateArtifactDigestSHA256           string   `json:"candidate_artifact_digest_sha256"`
	InputCandidateDigestSHA256              string   `json:"input_candidate_digest_sha256"`
	FinalCandidateContentDigestSHA256       string   `json:"final_candidate_content_digest_sha256"`
	PatchDigestSHA256                       string   `json:"patch_digest_sha256"`
	AppliedPaths                            []string `json:"applied_paths"`

	AdmissionVerificationStatus       *string `json:"admission_verification_status"`
	AdmissionVerificationDigestSHA256 *string `json:"admission_verification_digest_sha256"`

	Disposition Disposition `json:"disposition"`
	Detail      string      `json:"detail"`
	CompletedAt string      `json:"completed_at"`

	ReceiptDigestSHA256 string `json:"receipt_digest_sha256"`
}

// VerificationRecord binds one already-recorded application to one
// already-produced admission.Verification.
//
// Every field is either observed at recording time or copied from a document
// that already exists; nothing here is reconstructed. In particular the record
// carries the application receipt's own digest, so the application it
// describes is named by identity rather than by position in a directory, and
// the record can be checked against that receipt without either document
// having to be mutated.
//
// It states no verdict of its own. AdmissionVerificationStatus is the status
// the admission owner produced, carried verbatim: this owner links, it does
// not judge.
type VerificationRecord struct {
	SchemaVersion string `json:"schema_version"`
	RecordID      string `json:"record_id"`
	GeneratedBy   string `json:"generated_by"`

	ApplicationReceiptDigestSHA256 string `json:"application_receipt_digest_sha256"`
	RequestDigestSHA256            string `json:"request_digest_sha256"`
	AdmissionDecisionDigestSHA256  string `json:"admission_decision_digest_sha256"`
	CandidateArtifactDigestSHA256  string `json:"candidate_artifact_digest_sha256"`
	PatchDigestSHA256              string `json:"patch_digest_sha256"`

	AdmissionVerificationDigestSHA256 string `json:"admission_verification_digest_sha256"`
	AdmissionVerificationStatus       string `json:"admission_verification_status"`

	ObservedAt string `json:"observed_at"`

	RecordDigestSHA256 string `json:"record_digest_sha256"`
}

type ApplyInput struct {
	AdmissionRequest  admissioncomposition.Request
	AdmissionReceipt  admissioncomposition.Receipt
	Decision          admission.Decision
	CandidateArtifact runnercomposition.CandidateArtifact
	BaseManifest      []runnercomposition.CandidateManifestEntry
	TargetRoot        string
}
