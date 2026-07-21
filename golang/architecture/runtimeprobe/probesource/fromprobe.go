// SPDX-License-Identifier: AGPL-3.0-only

// Package probesource is the thin seam that projects a real probe.ProbeResult into the pure
// runtimeprobe adapter's honest input. It is the ONLY place that imports the probe package (which
// imports os), keeping the runtimeprobe core pure and guard-clean. It is a straight, lossy-toward-
// honesty field copy: it carries the fields a probe genuinely has and invents nothing.
package probesource

import (
	"github.com/globulario/sensei/golang/architecture/probe"
	"github.com/globulario/sensei/golang/architecture/runtimeprobe"
)

// FromProbeResult projects a probe.ProbeResult into a runtimeprobe.ProbeObservationInput. It copies
// only honest probe fields — there is no caller/callee/contract to copy, so none is produced.
func FromProbeResult(pr probe.ProbeResult) runtimeprobe.ProbeObservationInput {
	arts := make([]runtimeprobe.ArtifactDigest, 0, len(pr.Artifacts))
	for _, a := range pr.Artifacts {
		arts = append(arts, runtimeprobe.ArtifactDigest{Path: a.Path, SHA256: a.SHA256, Size: a.Size})
	}
	return runtimeprobe.ProbeObservationInput{
		ResultID:          pr.ID,
		ProbeID:           pr.ProbeID,
		ExecutedBy:        pr.ExecutedBy,
		ObservedAt:        pr.ObservedAt,
		EvidenceID:        pr.EvidenceID,
		EvidenceStatus:    pr.EvidenceStatus,
		EvidenceFreshness: pr.EvidenceFreshness,
		ObservationSource: pr.ObservationSource,
		ResultStatus:      pr.ResultStatus,
		Artifacts:         arts,
	}
}
