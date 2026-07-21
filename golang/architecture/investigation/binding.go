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

type Binding struct {
	Repository                    architecture.ClaimDocumentBinding `json:"repository" yaml:"repository"`
	EvidenceSnapshotDigestSHA256  string                            `json:"evidence_snapshot_digest_sha256,omitempty" yaml:"evidence_snapshot_digest_sha256,omitempty"`
	InvestigationPlanDigestSHA256 string                            `json:"investigation_plan_digest_sha256" yaml:"investigation_plan_digest_sha256"`
	ExtractorProfileDigestSHA256  string                            `json:"extractor_profile_digest_sha256" yaml:"extractor_profile_digest_sha256"`
	Model                         ModelBinding                      `json:"model" yaml:"model"`
}

func IsValidModelStatus(status string) bool {
	switch status {
	case ModelStatusDisabled, ModelStatusNotRequested, ModelStatusUnavailable, ModelStatusResolved, ModelStatusInvalid:
		return true
	default:
		return false
	}
}
