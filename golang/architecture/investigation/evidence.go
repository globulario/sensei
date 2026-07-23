// SPDX-License-Identifier: AGPL-3.0-only

package investigation

import (
	"github.com/globulario/sensei/golang/architecture"
)

type EvidenceCategory string

const (
	EvidenceSourceCode        EvidenceCategory = "source_code"
	EvidenceTests             EvidenceCategory = "tests"
	EvidenceDocumentation     EvidenceCategory = "documentation"
	EvidenceSourceControl     EvidenceCategory = "source_control"
	EvidenceIssues            EvidenceCategory = "issues"
	EvidenceDesignDocuments   EvidenceCategory = "design_documents"
	EvidenceTeamChat          EvidenceCategory = "team_chat"
	EvidenceRuntime           EvidenceCategory = "runtime_observability"
	EvidenceErrorTracking     EvidenceCategory = "error_tracking"
	EvidenceProductAnalytics  EvidenceCategory = "product_analytics"
	EvidenceArchitectFeedback EvidenceCategory = "architect_feedback"
)

type EvidenceReceipt struct {
	ID                  string                  `json:"id" yaml:"id"`
	Category            EvidenceCategory        `json:"category" yaml:"category"`
	Provider            ProviderBinding         `json:"provider" yaml:"provider"`
	ProofStrength       ProofStrength           `json:"proof_strength" yaml:"proof_strength"`
	SourceIdentity      string                  `json:"source_identity" yaml:"source_identity"`
	SourceDigestSHA256  string                  `json:"source_digest_sha256" yaml:"source_digest_sha256"`
	ContentDigestSHA256 string                  `json:"content_digest_sha256" yaml:"content_digest_sha256"`
	CapturedContent     string                  `json:"captured_content,omitempty" yaml:"captured_content,omitempty"`
	ContentLocation     string                  `json:"content_location,omitempty" yaml:"content_location,omitempty"`
	Scope               architecture.ClaimScope `json:"scope" yaml:"scope"`
	CapturedAt          string                  `json:"captured_at" yaml:"captured_at"`
}

func IsValidEvidenceCategory(cat EvidenceCategory) bool {
	switch cat {
	case EvidenceSourceCode, EvidenceTests, EvidenceDocumentation, EvidenceSourceControl,
		EvidenceIssues, EvidenceDesignDocuments, EvidenceTeamChat, EvidenceRuntime,
		EvidenceErrorTracking, EvidenceProductAnalytics, EvidenceArchitectFeedback:
		return true
	default:
		return false
	}
}
