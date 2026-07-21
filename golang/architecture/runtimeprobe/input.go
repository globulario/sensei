// SPDX-License-Identifier: AGPL-3.0-only

// Package runtimeprobe is the Phase 9.7 CP2 bounded typed adapter: it maps Sensei's own owner-path
// probe observations into an immutable evidence receipt and a runtime-boundary observation, and
// composes them through the accepted (frozen) runtimeboundary owner.
//
// The load-bearing honesty law: a probe is OWNER-PATH EVIDENCE, not a caller→callee crossing. Probe
// data carries a collector, an evidence digest, a timestamp, declared freshness, and content
// integrity — but NO caller, NO callee/contract, NO interaction, and no concrete runtime target. The
// adapter therefore NEVER invents caller/callee/contract to make an observation admissible. It fills
// only what a probe honestly has and leaves the governed-crossing identity unresolved; the frozen
// runtimeboundary owner then correctly refuses it (ambiguous_identity) and the boundary reads
// unknown/required_evidence_absent — evidence-level proof never satisfies a crossing.
//
// This package is pure and transport-neutral: it imports only the runtimeboundary owner and the
// closure-protocol receipt primitives (never probe/os/net/server/pb/store/editor/mutation) and emits
// no RDF. The single thin seam that reads the os-importing probe.ProbeResult lives in the
// runtimeprobe/probesource subpackage.
package runtimeprobe

import (
	"fmt"
	"sort"
	"strings"
)

// Schema is the canonical schema id of a probe-observation input projection.
const Schema = "runtime.probe_observation_input/v1"

// Producer identifies this adapter as the receipt/observation producer.
const Producer = "sensei.runtimeprobe"

// ArtifactDigest is one content-addressed file the probe read (path + sha256 + size).
type ArtifactDigest struct {
	Path   string `json:"path" yaml:"path"`
	SHA256 string `json:"sha256" yaml:"sha256"`
	Size   int64  `json:"size" yaml:"size"`
}

// ProbeObservationInput is the HONEST projection of a probe.ProbeResult — only the fields a probe
// genuinely carries. It deliberately has no caller/callee/contract/runtime-target: those do not exist
// in probe data and must never be invented here. The probesource seam fills this from a real
// probe.ProbeResult; this pure package never imports probe.
type ProbeObservationInput struct {
	ResultID          string           `json:"result_id" yaml:"result_id"`
	ProbeID           string           `json:"probe_id" yaml:"probe_id"`
	ExecutedBy        string           `json:"executed_by" yaml:"executed_by"`
	ObservedAt        string           `json:"observed_at" yaml:"observed_at"`
	EvidenceID        string           `json:"evidence_id,omitempty" yaml:"evidence_id,omitempty"`
	EvidenceStatus    string           `json:"evidence_status,omitempty" yaml:"evidence_status,omitempty"`
	EvidenceFreshness string           `json:"evidence_freshness,omitempty" yaml:"evidence_freshness,omitempty"`
	ObservationSource string           `json:"observation_source,omitempty" yaml:"observation_source,omitempty"`
	OwnerService      string           `json:"owner_service,omitempty" yaml:"owner_service,omitempty"`
	ResultStatus      string           `json:"result_status" yaml:"result_status"`
	Artifacts         []ArtifactDigest `json:"artifacts,omitempty" yaml:"artifacts,omitempty"`
	ExpiresAt         string           `json:"expires_at,omitempty" yaml:"expires_at,omitempty"`
	BudgetExhausted   bool             `json:"budget_exhausted,omitempty" yaml:"budget_exhausted,omitempty"`
}

// ValidateInput rejects a malformed projection. It does NOT reject a missing owner service or
// evidence id — those are honestly-absent facts, preserved (and later they keep the observation
// unresolved), never fabricated.
func ValidateInput(in ProbeObservationInput) error {
	if !trimmed(in.ResultID) {
		return fmt.Errorf("probe observation result id is empty or padded")
	}
	if !trimmed(in.ProbeID) {
		return fmt.Errorf("probe observation probe id is empty or padded")
	}
	if !trimmed(in.ExecutedBy) {
		return fmt.Errorf("probe observation collector (executed_by) is empty or padded")
	}
	if !validResultStatus(in.ResultStatus) {
		return fmt.Errorf("probe observation result status %q is off-vocabulary", in.ResultStatus)
	}
	if in.EvidenceFreshness != "" && !validEvidenceFreshness(in.EvidenceFreshness) {
		return fmt.Errorf("probe observation evidence freshness %q is off-vocabulary", in.EvidenceFreshness)
	}
	for label, v := range map[string]string{
		"evidence_id":        in.EvidenceID,
		"owner_service":      in.OwnerService,
		"observation_source": in.ObservationSource,
	} {
		if v != "" && (v != strings.TrimSpace(v) || isAbsolutePath(v)) {
			return fmt.Errorf("probe observation %s is padded or an absolute path", label)
		}
	}
	for _, a := range in.Artifacts {
		if !trimmed(a.SHA256) || a.Size < 0 {
			return fmt.Errorf("probe artifact digest is malformed")
		}
	}
	return nil
}

// ResultStatus / EvidenceFreshness closed vocabularies mirror the probe result's own values.
func validResultStatus(s string) bool {
	switch s {
	case "completed", "inconclusive", "unavailable", "failed", "rejected":
		return true
	}
	return false
}

func validEvidenceFreshness(s string) bool {
	switch s {
	case "current", "stale", "unknown", "historical":
		return true
	}
	return false
}

func trimmed(s string) bool { return s != "" && s == strings.TrimSpace(s) }

func isAbsolutePath(s string) bool {
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, "\\") ||
		(len(s) >= 2 && s[1] == ':')
}

func sortedArtifacts(in []ArtifactDigest) []ArtifactDigest {
	out := append([]ArtifactDigest(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].SHA256 < out[j].SHA256
	})
	return out
}

func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
