// SPDX-License-Identifier: AGPL-3.0-only

// Package admissioncomposition composes the accepted O1, O3, O4 and
// admission owners without replacing any of them. It validates the exact
// candidate-ready lineage, derives the requested mutation scope from sealed
// manifests, and records admission/verification evidence beside the frozen
// O1 receipt.
package admissioncomposition

import (
	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/evaluatorcomposition"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

const (
	RequestSchemaVersion = "sensei.admissioncomposition.request.v1"
	ReceiptSchemaVersion = "sensei.admissioncomposition.receipt.v1"
	GeneratedBy          = "sensei-admission-composition"
)

type Disposition string

const (
	DispositionUnsupportedOperationRefused Disposition = "unsupported-operation-refused"
	DispositionAdmissionDecided            Disposition = "admission-decided"
	DispositionVerificationRecorded        Disposition = "verification-recorded"
)

type UnsupportedOperation struct {
	Path      string `json:"path"`
	Operation string `json:"operation"`
	Detail    string `json:"detail"`
}

type Request struct {
	SchemaVersion string `json:"schema_version"`
	RequestID     string `json:"request_id"`
	GeneratedBy   string `json:"generated_by"`

	SynthesisReceiptDigestSHA256  string `json:"synthesis_receipt_digest_sha256"`
	RunnerReceiptDigestSHA256     string `json:"runner_receipt_digest_sha256"`
	EvaluationReceiptDigestSHA256 string `json:"evaluation_receipt_digest_sha256"`
	CandidateArtifactDigestSHA256 string `json:"candidate_artifact_digest_sha256"`

	RepositoryDomain string `json:"repository_domain"`
	BaseRevision     string `json:"base_revision"`

	DerivedScope          admission.ChangeScope    `json:"derived_scope"`
	UnsupportedOperations []UnsupportedOperation   `json:"unsupported_operations"`
	AdmissionEligible     bool                     `json:"admission_eligible"`

	AdmissionRequestDigestSHA256         *string `json:"admission_request_digest_sha256"`
	AdmissionRequestIdentityDigestSHA256 *string `json:"admission_request_identity_digest_sha256"`

	RequestDigestSHA256 string `json:"request_digest_sha256"`
}

type Receipt struct {
	SchemaVersion string `json:"schema_version"`
	ReceiptID     string `json:"receipt_id"`
	GeneratedBy   string `json:"generated_by"`

	RequestDigestSHA256           string `json:"request_digest_sha256"`
	SynthesisReceiptDigestSHA256  string `json:"synthesis_receipt_digest_sha256"`
	CandidateArtifactDigestSHA256 string `json:"candidate_artifact_digest_sha256"`

	AdmissionDecision                 *string `json:"admission_decision"`
	AdmissionDecisionDigestSHA256     *string `json:"admission_decision_digest_sha256"`
	AdmissionVerificationStatus       *string `json:"admission_verification_status"`
	AdmissionVerificationDigestSHA256 *string `json:"admission_verification_digest_sha256"`

	Disposition Disposition `json:"disposition"`
	Detail      string      `json:"detail"`
	CompletedAt string      `json:"completed_at"`

	ReceiptDigestSHA256 string `json:"receipt_digest_sha256"`
}

type ComposeInput struct {
	SynthesisReceipt  synthesis.Receipt
	RunnerReceipt     runnercomposition.RunnerReceipt
	EvaluationReceipt evaluatorcomposition.EvaluationReceipt
	CandidateArtifact runnercomposition.CandidateArtifact
	BaseManifest      []runnercomposition.CandidateManifestEntry
	AdmissionTemplate admission.Request
}
