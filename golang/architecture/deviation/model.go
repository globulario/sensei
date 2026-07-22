// SPDX-License-Identifier: AGPL-3.0-only

package deviation

import "github.com/globulario/sensei/golang/architecture"

const (
	SchemaVersion             = "architecture.deviation.v1"
	GeneratedBy               = "sensei deviation analyze"
	RulesetVersion            = "deviation.rules.v1"
	DefaultMinimumOccurrences = 2
	NondeterminismNone        = "none; pure deterministic processing of bound receipts"
)

// Kind is the closed vocabulary of implementation friction that may expose
// an architectural gap. A receipt is evidence only; it is never authority.
type Kind string

const (
	KindUndeclaredParameter Kind = "implementation_required_undeclared_parameter"
	KindBypassedOwnerPath   Kind = "implementation_bypassed_owner_path"
	KindRepeatedLocking     Kind = "implementation_needed_repeated_locking"
	KindRepeatedEscapeHatch Kind = "implementation_added_same_escape_hatch"
	KindUnsatisfiedBoundary Kind = "implementation_could_not_satisfy_boundary"
	KindMissingState        Kind = "implementation_discovered_missing_state"
)

func IsValidKind(kind Kind) bool {
	switch kind {
	case KindUndeclaredParameter, KindBypassedOwnerPath, KindRepeatedLocking,
		KindRepeatedEscapeHatch, KindUnsatisfiedBoundary, KindMissingState:
		return true
	default:
		return false
	}
}

// CandidateKind classifies the advisory architectural proposition created from
// a repeated pattern. It does not alter promotion status or proof strength.
type CandidateKind string

const (
	CandidateContract       CandidateKind = "contract"
	CandidateOwner          CandidateKind = "owner"
	CandidateFailureMode    CandidateKind = "failure_mode"
	CandidateGovernanceDebt CandidateKind = "governance_debt"
	CandidateBoundary       CandidateKind = "boundary"
)

// Shape is the structured, prose-independent signature used for clustering.
type Shape struct {
	Subject   string `json:"subject" yaml:"subject"`
	Predicate string `json:"predicate" yaml:"predicate"`
	Object    string `json:"object" yaml:"object"`
}

// Receipt is one immutable observation of implementation friction.
type Receipt struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	GeneratedBy   string `json:"generated_by" yaml:"generated_by"`
	ID            string `json:"id" yaml:"id"`
	Kind          Kind   `json:"kind" yaml:"kind"`

	Binding architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	Scope   architecture.ClaimScope           `json:"scope" yaml:"scope"`
	Shape   Shape                             `json:"shape" yaml:"shape"`

	ExpectedBehavior string `json:"expected_behavior" yaml:"expected_behavior"`
	ObservedBehavior string `json:"observed_behavior" yaml:"observed_behavior"`

	TaskID        string `json:"task_id" yaml:"task_id"`
	TaskSessionID string `json:"task_session_id" yaml:"task_session_id"`
	AgentID       string `json:"agent_id" yaml:"agent_id"`

	ChangeDigestSHA256         string `json:"change_digest_sha256" yaml:"change_digest_sha256"`
	SourceArtifactDigestSHA256 string `json:"source_artifact_digest_sha256" yaml:"source_artifact_digest_sha256"`
	IndependenceKey            string `json:"independence_key" yaml:"independence_key"`

	RelatedClaimIDs []string `json:"related_claim_ids,omitempty" yaml:"related_claim_ids,omitempty"`
	EvidenceRefs    []string `json:"evidence_refs" yaml:"evidence_refs"`

	RecordedAt      string `json:"recorded_at" yaml:"recorded_at"`
	TimestampSource string `json:"timestamp_source" yaml:"timestamp_source"`

	SemanticDigestSHA256 string `json:"semantic_digest_sha256" yaml:"semantic_digest_sha256"`
}

// RecordInput contains exact caller-supplied event data. Record never invents
// timestamps, task identity, repository state, or evidence.
type RecordInput struct {
	Kind          Kind
	Binding       architecture.ClaimDocumentBinding
	Scope         architecture.ClaimScope
	Shape         Shape
	Expected      string
	Observed      string
	TaskID        string
	TaskSessionID string
	AgentID       string
	ChangeDigest  string
	SourceDigest  string
	RelatedClaims []string
	EvidenceRefs  []string
	RecordedAt    string
	Timestamp     string
}

// Pattern groups independent receipts with the same structured shape.
type Pattern struct {
	ID                            string                  `json:"id" yaml:"id"`
	Kind                          Kind                    `json:"kind" yaml:"kind"`
	Scope                         architecture.ClaimScope `json:"scope" yaml:"scope"`
	Shape                         Shape                   `json:"shape" yaml:"shape"`
	ReceiptIDs                    []string                `json:"receipt_ids" yaml:"receipt_ids"`
	IndependenceKeys              []string                `json:"independence_keys" yaml:"independence_keys"`
	AgentIDs                      []string                `json:"agent_ids,omitempty" yaml:"agent_ids,omitempty"`
	TaskIDs                       []string                `json:"task_ids,omitempty" yaml:"task_ids,omitempty"`
	RelatedClaimIDs               []string                `json:"related_claim_ids,omitempty" yaml:"related_claim_ids,omitempty"`
	EvidenceRefs                  []string                `json:"evidence_refs" yaml:"evidence_refs"`
	IndependentOccurrenceCount    int                     `json:"independent_occurrence_count" yaml:"independent_occurrence_count"`
	MinimumIndependentOccurrences int                     `json:"minimum_independent_occurrences" yaml:"minimum_independent_occurrences"`
	FirstObservedAt               string                  `json:"first_observed_at" yaml:"first_observed_at"`
	LastObservedAt                string                  `json:"last_observed_at" yaml:"last_observed_at"`
	CandidateEligible             bool                    `json:"candidate_eligible" yaml:"candidate_eligible"`
	SemanticDigestSHA256          string                  `json:"semantic_digest_sha256" yaml:"semantic_digest_sha256"`
}

// Candidate is an advisory claim derived from a repeated deviation pattern.
type Candidate struct {
	ID                   string             `json:"id" yaml:"id"`
	PatternID            string             `json:"pattern_id" yaml:"pattern_id"`
	Kind                 CandidateKind      `json:"kind" yaml:"kind"`
	Claim                architecture.Claim `json:"claim" yaml:"claim"`
	ReceiptIDs           []string           `json:"receipt_ids" yaml:"receipt_ids"`
	SemanticDigestSHA256 string             `json:"semantic_digest_sha256" yaml:"semantic_digest_sha256"`
}

// Analysis is the exact, receipt-bound Phase 10.6 output. Recurrence can raise
// review priority, but this document never grants promotion authority.
type Analysis struct {
	SchemaVersion                 string                            `json:"schema_version" yaml:"schema_version"`
	GeneratedBy                   string                            `json:"generated_by" yaml:"generated_by"`
	Binding                       architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	MinimumIndependentOccurrences int                               `json:"minimum_independent_occurrences" yaml:"minimum_independent_occurrences"`
	Receipts                      []Receipt                         `json:"receipts" yaml:"receipts"`
	Patterns                      []Pattern                         `json:"patterns" yaml:"patterns"`
	Candidates                    []Candidate                       `json:"candidates" yaml:"candidates"`
	Receipt                       RunReceipt                        `json:"receipt" yaml:"receipt"`
}

// RunReceipt freezes the exact semantic indexes of an Analysis.
type RunReceipt struct {
	SchemaVersion                 string                            `json:"schema_version" yaml:"schema_version"`
	GeneratedBy                   string                            `json:"generated_by" yaml:"generated_by"`
	Binding                       architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	RulesetVersion                string                            `json:"ruleset_version" yaml:"ruleset_version"`
	MinimumIndependentOccurrences int                               `json:"minimum_independent_occurrences" yaml:"minimum_independent_occurrences"`
	ReceiptIDsAndDigests          map[string]string                 `json:"receipt_ids_and_digests" yaml:"receipt_ids_and_digests"`
	PatternIDsAndDigests          map[string]string                 `json:"pattern_ids_and_digests" yaml:"pattern_ids_and_digests"`
	CandidateIDsAndDigests        map[string]string                 `json:"candidate_ids_and_digests" yaml:"candidate_ids_and_digests"`
	ExactAnalysisDigestSHA256     string                            `json:"exact_analysis_digest_sha256" yaml:"exact_analysis_digest_sha256"`
	NondeterminismDeclaration     string                            `json:"nondeterminism_declaration" yaml:"nondeterminism_declaration"`
}
