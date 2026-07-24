// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"github.com/globulario/sensei/golang/architecture"
)

// Binding defines the exact input hashes used for candidate synthesis.
type Binding struct {
	Repository architecture.ClaimDocumentBinding `json:"repository" yaml:"repository"`

	HowDocumentDigestSHA256 string `json:"how_document_digest_sha256" yaml:"how_document_digest_sha256"`
	WhyDocumentDigestSHA256 string `json:"why_document_digest_sha256" yaml:"why_document_digest_sha256"`

	GraphDigestSHA256             string `json:"graph_digest_sha256" yaml:"graph_digest_sha256"`
	CurrentClaimsDigestSHA256     string `json:"current_claims_digest_sha256" yaml:"current_claims_digest_sha256"`
	ClosureStateDigestSHA256      string `json:"closure_state_digest_sha256" yaml:"closure_state_digest_sha256"`
	ExistingQuestionsDigestSHA256 string `json:"existing_questions_digest_sha256" yaml:"existing_questions_digest_sha256"`
	ReviewHistoryDigestSHA256     string `json:"review_history_digest_sha256" yaml:"review_history_digest_sha256"`

	EvidenceSnapshotDigestSHA256  string `json:"evidence_snapshot_digest_sha256" yaml:"evidence_snapshot_digest_sha256"`
	GroundingSnapshotDigestSHA256 string `json:"grounding_snapshot_digest_sha256" yaml:"grounding_snapshot_digest_sha256"`

	GeneratorVersion string `json:"generator_version" yaml:"generator_version"`
	RulesetVersion   string `json:"ruleset_version" yaml:"ruleset_version"`
}
