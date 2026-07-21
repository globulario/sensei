// SPDX-License-Identifier: AGPL-3.0-only

package probesource

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/probe"
	"github.com/globulario/sensei/golang/architecture/runtimeprobe"
)

// TestFromProbeResult_RealProbeData proves CP2 consumes a REAL probe.ProbeResult and produces an
// honest input + a valid receipt + a well-formed (but crossing-unresolved) observation — with no
// fabricated caller/callee/contract.
func TestFromProbeResult_RealProbeData(t *testing.T) {
	pr := probe.ProbeResult{
		ID: "result-xyz", ProbeID: "probe-abc", QuestionID: "q1", ResultStatus: "completed",
		ExecutedBy: "sensei static-probe-executor/v1", ObservedAt: "2026-07-21T00:00:00Z",
		EvidenceID: "evidence:seed_owner_path", EvidenceStatus: "pass", EvidenceFreshness: "current",
		ObservationSource: "owner:///seedmeta",
		Artifacts:         []probe.ArtifactReceipt{{Path: "docs/a.yaml", Kind: "yaml", SHA256: "deadbeef", Size: 42}},
	}
	in := FromProbeResult(pr)
	if in.ResultID != pr.ID || in.ProbeID != pr.ProbeID || in.ExecutedBy != pr.ExecutedBy {
		t.Fatal("seam must copy probe identity fields verbatim")
	}
	if len(in.Artifacts) != 1 || in.Artifacts[0].SHA256 != "deadbeef" {
		t.Fatal("seam must copy artifact digests")
	}
	if err := runtimeprobe.ValidateInput(in); err != nil {
		t.Fatalf("projected input must validate: %v", err)
	}
	receipt, err := runtimeprobe.ToEvidenceReceipt(in)
	if err != nil {
		t.Fatalf("real probe data must yield a valid receipt: %v", err)
	}
	obs, err := runtimeprobe.ToRuntimeObservation(in, receipt)
	if err != nil {
		t.Fatalf("real probe data must yield a well-formed observation: %v", err)
	}
	if obs.CallerIdentity != "" {
		t.Fatal("real probe data has no caller; none must be invented")
	}
}
