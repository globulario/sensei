// SPDX-License-Identifier: AGPL-3.0-only

// Package runtimeboundarycompose is the Phase 9.7 CP3 bridge: it projects a runtimeboundary
// assessment into the controlstate control-panel inputs (a "runtime" artifact dimension + an
// owner-supplied attention severity) WITHOUT re-deriving anything. It reads an ALREADY-DECIDED
// verdict and maps it one-to-one into controlstate's closed vocabulary; it never re-runs assessment
// (no crossing classification, no verdict computation). This is the single place the runtime-boundary
// owner's verdict→(dimension outcome, severity) projection lives.
//
// Dependency direction (the anti-doppelgänger DAG): this bridge imports BOTH runtimeboundary and
// controlstate; neither of those imports the other or this package. controlstate therefore only ever
// sees its own typed DimensionObservation and never reaches runtimeboundary's verdict logic.
package runtimeboundarycompose

import (
	"fmt"

	"github.com/globulario/sensei/golang/architecture/controlstate"
	rb "github.com/globulario/sensei/golang/architecture/runtimeboundary"
)

// The controlstate "runtime" dimension key + the owner identity that controlstate's boundary.v1
// policy expects (must match policy.go). Kept here so the bridge is the single source of the mapping.
const (
	DimensionKey = "runtime"
	SourceOwner  = "runtimeboundary"
)

// ToDimensionObservation projects an assessment into the controlstate "runtime" DimensionObservation.
// It is a pure, verbatim map of the owner's Verdict — controlstate then translates outcome→state.
// The (outcome, source-availability, blockers, severity, next-action) are chosen so the resulting
// controlstate DimensionState exactly preserves the verdict's meaning:
//
//	satisfied→DimSatisfied · violated→DimOpen · degraded→DimDegraded ·
//	unknown/unavailable/invalid→DimUnknown · not_applicable→DimNotApplicable
func ToDimensionObservation(a rb.RuntimeBoundaryAssessment) (controlstate.DimensionObservation, error) {
	if err := rb.ValidateAssessment(a); err != nil {
		return controlstate.DimensionObservation{}, fmt.Errorf("cannot compose an invalid runtime assessment: %w", err)
	}
	obs := controlstate.DimensionObservation{
		Dimension:        DimensionKey,
		SourceOwner:      SourceOwner,
		SourceSchema:     rb.SchemaAssessment,
		SourceIdentity:   a.BoundaryIRI,
		SourceDigest:     a.Meta.DigestSHA256,
		SourceReasonCode: a.ReasonCode,
	}
	switch a.Verdict {
	case rb.VerdictSatisfied:
		obs.SourceAvailability = controlstate.SourceAvailable
		obs.Outcome = controlstate.OutcomeSatisfied
		obs.EvidenceIDs = a.AdmittedEvidence
		obs.NextActionOwner = a.NextActionOwner
	case rb.VerdictViolated:
		obs.SourceAvailability = controlstate.SourceAvailable
		obs.Outcome = controlstate.OutcomeDefinitiveBlocker
		obs.BlockerIDs = []string{a.BoundaryIRI}
		obs.EvidenceIDs = a.AdmittedEvidence
		obs.SourceSeverity = controlstate.SeverityCritical // owner-supplied; controlstate uses verbatim
		obs.NextActionOwner = a.NextActionOwner
	case rb.VerdictDegraded:
		obs.SourceAvailability = controlstate.SourceDegraded
		obs.Outcome = controlstate.OutcomeDegraded
		obs.EvidenceIDs = a.AdmittedEvidence
		obs.NextActionOwner = a.NextActionOwner
	case rb.VerdictNotApplicable:
		obs.SourceAvailability = controlstate.SourceAvailable
		obs.Outcome = controlstate.OutcomeNotApplicable
		obs.NextActionOwner = a.NextActionOwner
	default: // unknown / unavailable / invalid → insufficient, from an untrustworthy source
		obs.SourceAvailability = untrustworthyAvailability(a.Verdict)
		obs.Outcome = controlstate.OutcomeInsufficient
		// An untrustworthy source admits no blocker/evidence/next-action (controlstate enforces this).
	}
	return obs, nil
}

// untrustworthyAvailability maps the non-conclusive verdicts to a source availability that yields
// DimUnknown: unavailable/unknown → SourceUnavailable; invalid → SourceInvalid.
func untrustworthyAvailability(v rb.Verdict) controlstate.SourceAvailability {
	if v == rb.VerdictInvalid {
		return controlstate.SourceInvalid
	}
	return controlstate.SourceUnavailable
}
