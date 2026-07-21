// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

// LifecycleSource is a TYPED lifecycle observation. Degraded/unavailable/invalid are preserved
// distinctly (never collapsed). Absence (Observed=false) yields unknown — never active.
type LifecycleSource struct {
	Observed     bool               `json:"observed" yaml:"observed"`
	Owner        string             `json:"owner,omitempty" yaml:"owner,omitempty"`
	Schema       string             `json:"schema,omitempty" yaml:"schema,omitempty"`
	Identity     string             `json:"identity,omitempty" yaml:"identity,omitempty"`
	Digest       string             `json:"digest,omitempty" yaml:"digest,omitempty"`
	Availability SourceAvailability `json:"availability,omitempty" yaml:"availability,omitempty"`
	ReasonCode   string             `json:"reason_code,omitempty" yaml:"reason_code,omitempty"`
	Status       string             `json:"status,omitempty" yaml:"status,omitempty"`
}

// lifecyclePolicyNotApplicable marks a class whose lifecycle explicitly does not apply.
const lifecyclePolicyNotApplicable = "not_applicable"

// assessLifecycle types lifecycle independently of closure. A class with no lifecycle policy
// keeps lifecycle applicable but unknown; an explicitly not-applicable policy → not_applicable; a
// governed-status policy maps an observed authoritative status, preserving degraded/unavailable/
// invalid, and absence → unknown (never active). An available source with no owner/schema/
// identity is treated as invalid (no ownerless status can label an artifact active).
func assessLifecycle(policy ClassPolicy, src LifecycleSource) LifecycleAssessment {
	switch policy.LifecyclePolicyID {
	case lifecyclePolicyNotApplicable:
		return LifecycleAssessment{Applicable: false, State: LifecycleNotApplicable, SourceAvailability: SourceAvailable, ReasonCode: "class_lifecycle_not_applicable"}
	case "":
		return LifecycleAssessment{Applicable: true, State: LifecycleUnknown, SourceAvailability: SourceUnavailable, ReasonCode: "no_lifecycle_source"}
	}
	base := LifecycleAssessment{Applicable: true, Vocabulary: policy.LifecyclePolicyID, SourceOwner: src.Owner, SourceIdentity: src.Identity}
	if !src.Observed {
		base.State, base.SourceAvailability, base.ReasonCode = LifecycleUnknown, SourceUnavailable, "lifecycle_source_unavailable"
		return base
	}
	// An available source must carry a complete owner/schema/identity, else it is invalid.
	if src.Availability == SourceAvailable && (src.Owner == "" || src.Schema == "" || src.Identity == "") {
		base.State, base.SourceAvailability, base.ReasonCode = LifecycleUnknown, SourceInvalid, "lifecycle_source_ownerless"
		return base
	}
	switch src.Availability {
	case SourceAvailable:
		state, reason := mapGovernedStatus(src.Status)
		base.State, base.SourceAvailability, base.ReasonCode = state, SourceAvailable, reason
	case SourceDegraded:
		base.State, base.SourceAvailability, base.ReasonCode = LifecycleUnknown, SourceDegraded, "lifecycle_source_degraded"
	case SourceInvalid:
		// Preserve the observation's typed reason (e.g. lifecycle_status_ambiguous when distinct
		// governed statuses conflict) — never overwrite it with a generic one.
		base.State, base.SourceAvailability, base.ReasonCode = LifecycleUnknown, SourceInvalid, nonEmpty(src.ReasonCode, "lifecycle_source_invalid")
	default: // SourceUnavailable or empty
		base.State, base.SourceAvailability, base.ReasonCode = LifecycleUnknown, SourceUnavailable, "lifecycle_source_unavailable"
	}
	return base
}

// mapGovernedStatus maps an observed authoritative status token to a lifecycle state. Only an
// explicit governed/stable/active status yields active; an unrecognized token yields unknown.
func mapGovernedStatus(status string) (LifecycleState, string) {
	switch status {
	case "superseded":
		return LifecycleSuperseded, "superseded"
	case "deprecated":
		return LifecycleDeprecated, "deprecated"
	case "revoked":
		return LifecycleRevoked, "revoked"
	case "governed", "stable", "active":
		return LifecycleActive, "governed_active"
	case "candidate", "proposed", "experimental":
		return LifecycleProposed, "proposed"
	default:
		return LifecycleUnknown, "lifecycle_status_unrecognized"
	}
}

// lifecycleSourceStatus returns the SourceStatus for the ledger when lifecycle applies.
func lifecycleSourceStatus(policy ClassPolicy, src LifecycleSource) (SourceStatus, bool) {
	if policy.LifecyclePolicyID == "" || policy.LifecyclePolicyID == lifecyclePolicyNotApplicable {
		return SourceStatus{}, false
	}
	avail := SourceUnavailable
	reason := "lifecycle_source_unavailable"
	if src.Observed {
		if src.Availability == SourceAvailable && (src.Owner == "" || src.Schema == "" || src.Identity == "") {
			avail, reason = SourceInvalid, "lifecycle_source_ownerless"
		} else {
			avail, reason = src.Availability, src.ReasonCode
		}
	}
	return srcStatus(nonEmpty(src.Owner, "lifecycle"), nonEmpty(src.Schema, policy.LifecyclePolicyID), src.Identity, src.Digest, avail, ImpactRelevant, reason), true
}

func nonEmpty(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
