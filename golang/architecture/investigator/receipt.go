// SPDX-License-Identifier: AGPL-3.0-only

package investigator

// RunReceipt freezes exact inputs, bounded resources, and semantic output indexes.
type RunReceipt struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	GeneratedBy   string `json:"generated_by" yaml:"generated_by"`

	InputBinding                  Binding `json:"input_binding" yaml:"input_binding"`
	GroundingSnapshotDigestSHA256 string  `json:"grounding_snapshot_digest_sha256" yaml:"grounding_snapshot_digest_sha256"`
	HowDocumentDigestSHA256       string  `json:"how_document_digest_sha256" yaml:"how_document_digest_sha256"`
	WhyDocumentDigestSHA256       string  `json:"why_document_digest_sha256" yaml:"why_document_digest_sha256"`
	GraphDigestSHA256             string  `json:"graph_digest_sha256" yaml:"graph_digest_sha256"`
	CurrentClaimsDigestSHA256     string  `json:"current_claims_digest_sha256" yaml:"current_claims_digest_sha256"`
	ClosureStateDigestSHA256      string  `json:"closure_state_digest_sha256" yaml:"closure_state_digest_sha256"`
	ExistingQuestionsDigestSHA256 string  `json:"existing_questions_digest_sha256" yaml:"existing_questions_digest_sha256"`
	ReviewHistoryDigestSHA256     string  `json:"review_history_digest_sha256" yaml:"review_history_digest_sha256"`

	GeneratorVersion string `json:"generator_version" yaml:"generator_version"`
	RulesetVersion   string `json:"ruleset_version" yaml:"ruleset_version"`

	CandidateIDsAndDigests       map[string]string `json:"candidate_ids_and_digests" yaml:"candidate_ids_and_digests"`
	ChallengeIDsAndDigests       map[string]string `json:"challenge_ids_and_digests" yaml:"challenge_ids_and_digests"`
	CounterexampleIDsAndDigests  map[string]string `json:"counterexample_ids_and_digests" yaml:"counterexample_ids_and_digests"`
	EvidenceRequestIDsAndDigests map[string]string `json:"evidence_request_ids_and_digests" yaml:"evidence_request_ids_and_digests"`
	RankingDigestSHA256          string            `json:"ranking_digest_sha256" yaml:"ranking_digest_sha256"`
	ExactResultDigestSHA256      string            `json:"exact_result_digest_sha256" yaml:"exact_result_digest_sha256"`

	TimestampSource           string            `json:"timestamp_source" yaml:"timestamp_source"`
	ResourceLimits            map[string]string `json:"resource_limits" yaml:"resource_limits"`
	NondeterminismDeclaration string            `json:"nondeterminism_declaration" yaml:"nondeterminism_declaration"`
}
