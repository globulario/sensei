// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"time"

	"github.com/globulario/sensei/golang/architecture/investigation"
)

// BuildRunReceipt constructs the final immutable run receipt for the investigation document.
func BuildRunReceipt(doc *investigation.Document, candidateDigests map[string]string, opts Options) investigation.RunReceipt {
	capturedAt := opts.CapturedAt
	if capturedAt == "" {
		capturedAt = time.Now().Format(time.RFC3339)
	}

	limits := opts.ResourceLimits
	if len(limits) == 0 {
		limits = map[string]string{"composer": "local"}
	}

	return investigation.RunReceipt{
		SchemaVersion:                "investigation.schema.v1",
		GeneratedBy:                  "sensei.investigator.v1",
		Repository:                   doc.Binding.Repository,
		GraphDigestSHA256:            doc.Binding.Repository.GraphDigestSHA256,
		PlanDigestSHA256:             doc.Binding.InvestigationPlanDigestSHA256,
		ExtractorProfileDigestSHA256: doc.Binding.ExtractorProfileDigestSHA256,
		EvidenceSnapshotDigestSHA256: doc.Binding.EvidenceSnapshotDigestSHA256,
		Model:                        doc.Binding.Model,
		PostProcessingVersion:        "investigator.postprocessor.v1",
		OutputCandidateIDsAndDigests: candidateDigests,
		TimestampSource:              capturedAt,
		ResourceLimits:               limits,
		NondeterminismDeclaration:    "deterministic_only",
	}
}
