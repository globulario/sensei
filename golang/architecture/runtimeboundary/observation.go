// SPDX-License-Identifier: AGPL-3.0-only

package runtimeboundary

import (
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// ObservationSchema is the canonical schema id of a runtime observation envelope.
const ObservationSchema = "runtime.observation/v1"

// Freshness is the CLOSED observation-freshness vocabulary. Zero value fails closed.
type Freshness string

const (
	FreshnessFresh   Freshness = "fresh"
	FreshnessStale   Freshness = "stale"
	FreshnessUnknown Freshness = "unknown"
)

func validFreshness(f Freshness) bool {
	switch f {
	case FreshnessFresh, FreshnessStale, FreshnessUnknown:
		return true
	}
	return false
}

// RuntimeObservation is a typed, versioned, NON-AUTHORITATIVE record of one observed crossing. It
// carries only crossing facts Sensei can verify — never a compliance verdict. The owner (not the
// collector) classifies a crossing as authorized or forbidden by applying the boundary policy; the
// observation never pre-decides that. Unknown/ambiguous runtime identity is preserved (empty caller
// or callee means unknown), never guessed.
type RuntimeObservation struct {
	ObservationID string `json:"observation_id" yaml:"observation_id"`
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`

	Direction                  CrossingDirection `json:"direction" yaml:"direction"`
	CallerIdentity             string            `json:"caller_identity,omitempty" yaml:"caller_identity,omitempty"`
	CalleeIdentity             string            `json:"callee_identity,omitempty" yaml:"callee_identity,omitempty"`
	EndpointOrContractIdentity string            `json:"endpoint_or_contract_identity,omitempty" yaml:"endpoint_or_contract_identity,omitempty"`
	InteractionKind            InteractionKind   `json:"interaction_kind" yaml:"interaction_kind"`
	// AuthContextPresent + AuthorityClass are the OBSERVED authentication context — a claim the owner
	// verifies against policy, never a self-granted authorization.
	AuthContextPresent bool   `json:"auth_context_present" yaml:"auth_context_present"`
	AuthorityClass     string `json:"authority_class,omitempty" yaml:"authority_class,omitempty"`

	// Bounded observation window (opaque strings; the owner never reads the clock).
	WindowStart string `json:"window_start,omitempty" yaml:"window_start,omitempty"`
	WindowEnd   string `json:"window_end,omitempty" yaml:"window_end,omitempty"`

	RuntimeTarget    closureprotocol.RuntimeTarget `json:"runtime_target" yaml:"runtime_target"`
	CollectorID      string                        `json:"collector_id" yaml:"collector_id"`
	CollectorVersion string                        `json:"collector_version,omitempty" yaml:"collector_version,omitempty"`

	EvidenceDigestSHA256 string   `json:"evidence_digest_sha256,omitempty" yaml:"evidence_digest_sha256,omitempty"`
	Provenance           []string `json:"provenance,omitempty" yaml:"provenance,omitempty"`

	// Availability is the collector-source status of THIS observation; freshness/integrity/truncation
	// are honest quality signals. A missing observation is never a silent "no violation".
	Availability      SourceAvailability `json:"availability" yaml:"availability"`
	Freshness         Freshness          `json:"freshness" yaml:"freshness"`
	IntegrityVerified bool               `json:"integrity_verified" yaml:"integrity_verified"`
	Truncated         bool               `json:"truncated" yaml:"truncated"`
	ReasonCode        string             `json:"reason_code,omitempty" yaml:"reason_code,omitempty"`
}

// ValidateObservation canonicalizes and rejects a malformed observation. It does NOT reject an
// unknown caller/callee — unknown identity is preserved and later prevents a positive verdict.
func ValidateObservation(o RuntimeObservation) error {
	if !trimmedNonEmpty(o.ObservationID) {
		return fmt.Errorf("observation id is empty or padded")
	}
	if o.SchemaVersion != ObservationSchema {
		return fmt.Errorf("observation schema %q is not %q", o.SchemaVersion, ObservationSchema)
	}
	if !validDirection(o.Direction) {
		return fmt.Errorf("observation %q direction %q is off-vocabulary", o.ObservationID, o.Direction)
	}
	if !validInteractionKind(o.InteractionKind) {
		return fmt.Errorf("observation %q interaction kind %q is off-vocabulary", o.ObservationID, o.InteractionKind)
	}
	if !validSourceAvailability(o.Availability) {
		return fmt.Errorf("observation %q availability %q is off-vocabulary", o.ObservationID, o.Availability)
	}
	if !validFreshness(o.Freshness) {
		return fmt.Errorf("observation %q freshness %q is off-vocabulary", o.ObservationID, o.Freshness)
	}
	for label, v := range map[string]string{
		"caller_identity":   o.CallerIdentity,
		"callee_identity":   o.CalleeIdentity,
		"endpoint_identity": o.EndpointOrContractIdentity,
		"authority_class":   o.AuthorityClass,
		"collector_id":      o.CollectorID,
		"evidence_digest":   o.EvidenceDigestSHA256,
	} {
		if v != "" && (v != strings.TrimSpace(v) || isAbsolutePath(v)) {
			return fmt.Errorf("observation %q %s is padded or an absolute path", o.ObservationID, label)
		}
	}
	if o.RuntimeTarget.Platform != "" && o.RuntimeTarget.Platform != strings.TrimSpace(o.RuntimeTarget.Platform) {
		return fmt.Errorf("observation %q runtime target platform is padded", o.ObservationID)
	}
	if !equalStrings(o.Provenance, sortedUnique(o.Provenance)) {
		return fmt.Errorf("observation %q provenance is not canonical (sorted+unique)", o.ObservationID)
	}
	// An available observation must identify its collector and carry an evidence digest; an
	// unavailable/degraded/invalid one must carry a typed reason.
	switch o.Availability {
	case SourceAvailable:
		if !trimmedNonEmpty(o.CollectorID) {
			return fmt.Errorf("available observation %q missing collector id", o.ObservationID)
		}
		if !trimmedNonEmpty(o.EvidenceDigestSHA256) {
			return fmt.Errorf("available observation %q missing evidence digest", o.ObservationID)
		}
		if o.ReasonCode != "" {
			return fmt.Errorf("available observation %q must carry no failure reason", o.ObservationID)
		}
	case SourceDegraded, SourceUnavailable, SourceInvalid:
		if o.ReasonCode == "" {
			return fmt.Errorf("%s observation %q must carry a typed reason", o.Availability, o.ObservationID)
		}
	}
	return nil
}

// hasResolvedIdentity reports whether the crossing's caller, callee, and endpoint are all resolved.
// Ambiguous/unknown identity means the observation can never establish a positive verdict.
func (o RuntimeObservation) hasResolvedIdentity() bool {
	return trimmedNonEmpty(o.CallerIdentity) && trimmedNonEmpty(o.CalleeIdentity) &&
		trimmedNonEmpty(o.EndpointOrContractIdentity)
}
