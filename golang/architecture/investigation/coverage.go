// SPDX-License-Identifier: AGPL-3.0-only

package investigation

import (
	"github.com/globulario/sensei/golang/architecture"
)

type CoverageStatus string

const (
	CoverageSupporting    CoverageStatus = "searched_supporting"
	CoverageRefuting      CoverageStatus = "searched_refuting"
	CoverageMixed         CoverageStatus = "searched_mixed"
	CoverageNoResult      CoverageStatus = "searched_no_result"
	CoverageUnavailable   CoverageStatus = "unavailable"
	CoverageNotConfigured CoverageStatus = "not_configured"
	CoverageSkipped       CoverageStatus = "skipped_with_reason"
	CoverageInvalid       CoverageStatus = "invalid"
)

type TimeRange struct {
	Start string `json:"start,omitempty" yaml:"start,omitempty"`
	End   string `json:"end,omitempty" yaml:"end,omitempty"`
}

type CoverageEntry struct {
	ProviderID                 string                    `json:"provider_id" yaml:"provider_id"`
	ProviderVersion            string                    `json:"provider_version" yaml:"provider_version"`
	Category                   EvidenceCategory          `json:"category" yaml:"category"`
	TargetDigestSHA256         string                    `json:"target_digest_sha256" yaml:"target_digest_sha256"`
	SearchedTimeRange          *TimeRange                `json:"searched_time_range,omitempty" yaml:"searched_time_range,omitempty"`
	SourceSnapshotDigestSHA256 string                    `json:"source_snapshot_digest_sha256,omitempty" yaml:"source_snapshot_digest_sha256,omitempty"`
	ResultEvidenceIDs          []string                  `json:"result_evidence_ids,omitempty" yaml:"result_evidence_ids,omitempty"`
	Status                     CoverageStatus            `json:"status" yaml:"status"`
	Reason                     string                    `json:"reason,omitempty" yaml:"reason,omitempty"`
	Limitations                []architecture.Limitation `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

func IsValidCoverageStatus(status CoverageStatus) bool {
	switch status {
	case CoverageSupporting, CoverageRefuting, CoverageMixed, CoverageNoResult,
		CoverageUnavailable, CoverageNotConfigured, CoverageSkipped, CoverageInvalid:
		return true
	default:
		return false
	}
}
