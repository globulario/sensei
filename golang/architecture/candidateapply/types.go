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
	GeneratedBy          = "sensei-candidate-apply"
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

type ApplyInput struct {
	AdmissionRequest  admissioncomposition.Request
	AdmissionReceipt  admissioncomposition.Receipt
	Decision          admission.Decision
	CandidateArtifact runnercomposition.CandidateArtifact
	BaseManifest      []runnercomposition.CandidateManifestEntry
	TargetRoot        string
}
