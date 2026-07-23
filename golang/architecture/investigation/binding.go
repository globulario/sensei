// SPDX-License-Identifier: AGPL-3.0-only

package investigation

import (
	"github.com/globulario/sensei/golang/architecture"
)

const (
	ModelStatusDisabled     = "disabled"
	ModelStatusNotRequested = "not_requested"
	ModelStatusUnavailable  = "unavailable"
	ModelStatusResolved     = "resolved"
	ModelStatusInvalid      = "invalid"
)

type ProviderBinding struct {
	ID      string `json:"id" yaml:"id"`
	Version string `json:"version" yaml:"version"`
}

type ModelBinding struct {
	Status            string `json:"status" yaml:"status"`
	ModelName         string `json:"model_name,omitempty" yaml:"model_name,omitempty"`
	ModelDigestSHA256 string `json:"model_digest_sha256,omitempty" yaml:"model_digest_sha256,omitempty"`
}

// WhyBinding pins a WHY investigation to the exact HOW document and local
// history snapshot it was allowed to inspect.
type WhyBinding struct {
	HowDocumentDigestSHA256   string   `json:"how_document_digest_sha256,omitempty" yaml:"how_document_digest_sha256,omitempty"`
	QueryDigestSHA256         string   `json:"query_digest_sha256,omitempty" yaml:"query_digest_sha256,omitempty"`
	TargetObservationIDs      []string `json:"target_observation_ids,omitempty" yaml:"target_observation_ids,omitempty"`
	TargetEvidenceIDs         []string `json:"target_evidence_ids,omitempty" yaml:"target_evidence_ids,omitempty"`
	HistoryRangeStart         string   `json:"history_range_start,omitempty" yaml:"history_range_start,omitempty"`
	HistoryRangeEnd           string   `json:"history_range_end,omitempty" yaml:"history_range_end,omitempty"`
	ResolvedHistoryRangeStart string   `json:"resolved_history_range_start,omitempty" yaml:"resolved_history_range_start,omitempty"`
	ResolvedHistoryRangeEnd   string   `json:"resolved_history_range_end,omitempty" yaml:"resolved_history_range_end,omitempty"`
}

type Binding struct {
	Repository                    architecture.ClaimDocumentBinding `json:"repository" yaml:"repository"`
	EvidenceSnapshotDigestSHA256  string                            `json:"evidence_snapshot_digest_sha256,omitempty" yaml:"evidence_snapshot_digest_sha256,omitempty"`
	InvestigationPlanDigestSHA256 string                            `json:"investigation_plan_digest_sha256" yaml:"investigation_plan_digest_sha256"`
	ExtractorProfileDigestSHA256  string                            `json:"extractor_profile_digest_sha256" yaml:"extractor_profile_digest_sha256"`
	Model                         ModelBinding                      `json:"model" yaml:"model"`
	Why                           WhyBinding                        `json:"why,omitempty" yaml:"why,omitempty"`
}

func IsValidModelStatus(status string) bool {
	switch status {
	case ModelStatusDisabled, ModelStatusNotRequested, ModelStatusUnavailable, ModelStatusResolved, ModelStatusInvalid:
		return true
	default:
		return false
	}
}
