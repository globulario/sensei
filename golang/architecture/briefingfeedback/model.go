// SPDX-License-Identifier: Apache-2.0

// Package briefingfeedback is the SOLE canonical, deterministic, read-only owner of the
// governed briefing-feedback projection (Phase 9.6). It admits a promoted governed record
// only after questionpromotion.VerifyCommittedPromotion re-proves the complete committed
// conjunction, applies the VERIFIED effective scope with exact identity, preserves exact
// lineage provenance, isolates unrelated broken-promotion debris, classifies failures by
// TYPED verification cause (never error text), and asserts no certification/completion. Both
// the task briefing and the future server briefing consume this one owner.
package briefingfeedback

// SchemaVersion identifies the canonical projection.
const SchemaVersion = "briefing.feedback_projection/v1"

// ProducerName / ProducerVersion identify the projection producer.
const (
	ProducerName    = "sensei.briefingfeedback"
	ProducerVersion = "v1"
)

// BoundStatement is a fixed constant (never caller-supplied prose).
const BoundStatement = "This projection reports independently verified, scope-relevant governed promotions and their provenance. It is not promotion authority, task completion, correctness certification, or merge approval."

// provenanceInterpretation is the fixed per-record statement.
const provenanceInterpretation = "the governed record is the reusable architectural truth; the question and answer identities are lineage/provenance, never governed knowledge"

// Availability is the CLOSED projection availability vocabulary. The zero value ("") is not
// a member → fails closed.
type Availability string

const (
	FeedbackAvailable   Availability = "feedback_available"
	FeedbackEmpty       Availability = "feedback_empty"
	FeedbackDegraded    Availability = "feedback_degraded"
	FeedbackUnavailable Availability = "feedback_unavailable"
	FeedbackInvalid     Availability = "feedback_invalid"
)

func validAvailability(a Availability) bool {
	switch a {
	case FeedbackAvailable, FeedbackEmpty, FeedbackDegraded, FeedbackUnavailable, FeedbackInvalid:
		return true
	}
	return false
}

// FindingClass is the CLOSED candidate/finding class vocabulary. Zero fails closed.
type FindingClass string

const (
	PromotionVerified              FindingClass = "promotion_verified"
	PromotionOutOfScope            FindingClass = "promotion_out_of_scope"
	PromotionIncomplete            FindingClass = "promotion_incomplete"
	PromotionIntegrityFailure      FindingClass = "promotion_integrity_failure"
	PromotionContradictory         FindingClass = "promotion_contradictory"
	PromotionStale                 FindingClass = "promotion_stale"
	PromotionUnverifiable          FindingClass = "promotion_unverifiable"
	PromotionDiscoveryUnavailable  FindingClass = "promotion_discovery_unavailable"
	PromotionScopeIdentityInvalid  FindingClass = "promotion_scope_identity_invalid"
	PromotionUnknownClassification FindingClass = "promotion_unknown_classification"
)

func validFindingClass(c FindingClass) bool {
	switch c {
	case PromotionVerified, PromotionOutOfScope, PromotionIncomplete, PromotionIntegrityFailure,
		PromotionContradictory, PromotionStale, PromotionUnverifiable, PromotionDiscoveryUnavailable,
		PromotionScopeIdentityInvalid, PromotionUnknownClassification:
		return true
	}
	return false
}

// Disposition is a finding's admission disposition.
type Disposition string

const (
	DispositionAdmitted    Disposition = "admitted"
	DispositionExcluded    Disposition = "excluded"
	DispositionUnavailable Disposition = "unavailable"
)

// TaskBinding is the exact stable task identity a caller may supply (never discovered here).
type TaskBinding struct {
	TaskID           string   `json:"task_id" yaml:"task_id"`
	SessionID        string   `json:"session_id" yaml:"session_id"`
	RepositoryDomain string   `json:"repository_domain,omitempty" yaml:"repository_domain,omitempty"`
	Files            []string `json:"files,omitempty" yaml:"files,omitempty"`
}

// Request selects the feedback scope. RepositoryRoot is an EXECUTION input for the promotion
// owner and is never serialized into the projection as a filesystem path.
type Request struct {
	RepositoryRoot     string
	RepositoryIdentity string
	RequestedDomain    string
	RequestedFiles     []string
	Task               *TaskBinding
}

// VerifiedRecord is one admitted, independently-verified committed promotion.
type VerifiedRecord struct {
	GovernedNodeIRI                string       `json:"governed_node_iri" yaml:"governed_node_iri"`
	GovernedKind                   string       `json:"governed_kind" yaml:"governed_kind"`
	CanonicalRecordID              string       `json:"canonical_record_id" yaml:"canonical_record_id"`
	SourceDocument                 string       `json:"source_document" yaml:"source_document"`
	PromotionLineageID             string       `json:"promotion_lineage_id" yaml:"promotion_lineage_id"`
	PromotionReceiptDigestSHA256   string       `json:"promotion_receipt_digest_sha256" yaml:"promotion_receipt_digest_sha256"`
	QuestionID                     string       `json:"question_id" yaml:"question_id"`
	AnswerID                       string       `json:"answer_id" yaml:"answer_id"`
	DispositionReceiptDigestSHA256 string       `json:"disposition_receipt_digest_sha256" yaml:"disposition_receipt_digest_sha256"`
	OriginatingTaskID              string       `json:"originating_task_id" yaml:"originating_task_id"`
	OriginatingSessionID           string       `json:"originating_session_id" yaml:"originating_session_id"`
	EffectiveDomain                string       `json:"effective_domain,omitempty" yaml:"effective_domain,omitempty"`
	EffectiveFileScope             []string     `json:"effective_file_scope" yaml:"effective_file_scope"`
	VerificationClass              FindingClass `json:"verification_class" yaml:"verification_class"`
	ProvenanceInterpretation       string       `json:"provenance_interpretation" yaml:"provenance_interpretation"`
}

// Finding is a typed candidate diagnostic (never prose-only). ClaimedDomain/ClaimedFiles are
// UNTRUSTED routing metadata; they never establish authority.
type Finding struct {
	Class          FindingClass `json:"class" yaml:"class"`
	ReasonCode     string       `json:"reason_code" yaml:"reason_code"`
	LineageID      string       `json:"lineage_id,omitempty" yaml:"lineage_id,omitempty"`
	ClaimedDomain  string       `json:"claimed_domain,omitempty" yaml:"claimed_domain,omitempty"`
	ClaimedFiles   []string     `json:"claimed_files,omitempty" yaml:"claimed_files,omitempty"`
	AffectedDomain string       `json:"affected_domain,omitempty" yaml:"affected_domain,omitempty"`
	AffectedFiles  []string     `json:"affected_files,omitempty" yaml:"affected_files,omitempty"`
	Disposition    Disposition  `json:"disposition" yaml:"disposition"`
	Detail         string       `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// Projection is the canonical briefing.feedback_projection/v1.
type Projection struct {
	SchemaVersion              string           `json:"schema_version" yaml:"schema_version"`
	ProducerName               string           `json:"producer_name" yaml:"producer_name"`
	ProducerVersion            string           `json:"producer_version" yaml:"producer_version"`
	RepositoryIdentity         string           `json:"repository_identity,omitempty" yaml:"repository_identity,omitempty"`
	RequestedDomain            string           `json:"requested_domain" yaml:"requested_domain"`
	RequestedFiles             []string         `json:"requested_files" yaml:"requested_files"`
	TaskID                     string           `json:"task_id,omitempty" yaml:"task_id,omitempty"`
	SessionID                  string           `json:"session_id,omitempty" yaml:"session_id,omitempty"`
	Availability               Availability     `json:"availability" yaml:"availability"`
	Records                    []VerifiedRecord `json:"records" yaml:"records"`
	Findings                   []Finding        `json:"findings" yaml:"findings"`
	NonAuthoritativeProjection bool             `json:"non_authoritative_projection" yaml:"non_authoritative_projection"`
	Bound                      string           `json:"bound" yaml:"bound"`
	DigestSHA256               string           `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
}
