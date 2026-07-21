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
		Explanation:      runtimeExplanation(a),
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

// runtimeExplanation projects the owner's ALREADY-DECIDED reason space into an actionable
// incompleteness explanation — read-only, never a re-assessment. It is TOTAL over the owner's closed
// ResultKind vocabulary and fail-honest: a satisfied crossing has nothing to explain (nil), and any
// unrecognized kind still yields an explicit generic explanation, never empty strings. The stable
// semantic identity is the ResultKind itself (carried as Explanation.Kind); the prose is presentation.
func runtimeExplanation(a rb.RuntimeBoundaryAssessment) *controlstate.DimensionExplanation {
	if a.Verdict == rb.VerdictSatisfied {
		return nil
	}
	next := a.NextActionOwner
	if next == "" {
		next = "the architect"
	}
	kind := string(a.ResultKind)
	switch a.ResultKind {
	case rb.KindObservedForbiddenCrossing:
		return &controlstate.DimensionExplanation{Kind: kind,
			Known:            "a runtime crossing was observed and admitted against the governed boundary",
			Missing:          "the crossing conforming to the boundary's policy",
			WhyNotImprovable: "the observed crossing is forbidden by the boundary policy (a violation)",
			NextEvidence:     "correct the crossing or the policy so the runtime path conforms"}
	case rb.KindCrossingStaleAuthority:
		return &controlstate.DimensionExplanation{Kind: kind,
			Known:            "the boundary and a runtime observation are present",
			Missing:          "current, integrity-verified graph authority",
			WhyNotImprovable: "the graph authority backing the boundary is stale or unverified, so the crossing cannot be trusted",
			NextEvidence:     "refresh and integrity-verify the graph authority"}
	case rb.KindEvidenceTruncated:
		return &controlstate.DimensionExplanation{Kind: kind,
			Known:            "a satisfying crossing was observed",
			Missing:          "complete (non-truncated) runtime evidence",
			WhyNotImprovable: "the runtime evidence was truncated, so the assessment degrades",
			NextEvidence:     "collect complete runtime evidence for the crossing"}
	case rb.KindContradictoryObservations:
		return &controlstate.DimensionExplanation{Kind: kind,
			Known:            "multiple runtime observations for the same crossing were admitted",
			Missing:          "the contradiction between observations resolved",
			WhyNotImprovable: "admitted observations classify the same crossing differently",
			NextEvidence:     "reconcile the conflicting runtime observations"}
	case rb.KindRequiredEvidenceAbsent, rb.KindNoRuntimeEvidence:
		return &controlstate.DimensionExplanation{Kind: kind,
			Known:            "the boundary and its policy are valid and runtime-assessable",
			Missing:          "an admissible native crossing observation (caller, callee, crossing binding)",
			WhyNotImprovable: "no runtime crossing evidence has been observed, so compliance cannot be established",
			NextEvidence:     "a native crossing observation admitted through an explicit binding"}
	case rb.KindCollectorUnavailable:
		return &controlstate.DimensionExplanation{Kind: kind,
			Known:            "the boundary and its policy are valid",
			Missing:          "an available evidence collector",
			WhyNotImprovable: "the runtime evidence collector cannot currently provide evidence",
			NextEvidence:     "restore the runtime evidence collector"}
	case rb.KindPolicyAbsent:
		return &controlstate.DimensionExplanation{Kind: kind,
			Known:            "the boundary identity is valid and runtime-assessable",
			Missing:          "an explicit boundary policy",
			WhyNotImprovable: "no policy governs the crossing, and absence is never treated as permission",
			NextEvidence:     "declare an explicit boundary policy"}
	case rb.KindEvidenceOutOfScope:
		return &controlstate.DimensionExplanation{Kind: kind,
			Known:            "runtime observations were admitted for the boundary",
			Missing:          "an in-scope, on-contract crossing observation",
			WhyNotImprovable: "the admitted evidence is out of scope or off the required contract, so nothing conclusive was observed",
			NextEvidence:     "observe an in-scope crossing on the required contract"}
	case rb.KindBoundaryNotAssessable:
		return &controlstate.DimensionExplanation{Kind: kind,
			Known:            "the boundary is governed but declared not runtime-assessable (or its runtime proof is unsupported)",
			WhyNotImprovable: "the owner explicitly ruled runtime assessment outside this boundary's scope",
			NextEvidence:     "none — no runtime action is required"}
	case rb.KindBoundaryRevoked:
		return &controlstate.DimensionExplanation{Kind: kind,
			Known:            "the boundary exists in the graph",
			WhyNotImprovable: "the boundary lifecycle is revoked or deprecated, so it is not runtime-assessable",
			NextEvidence:     "none — reinstate the boundary if runtime assessment is intended"}
	case rb.KindIdentityUnresolved:
		return &controlstate.DimensionExplanation{Kind: kind,
			Known:            "an artifact was supplied for runtime-boundary assessment",
			Missing:          "a valid, unambiguous boundary identity / policy / binding",
			WhyNotImprovable: "the identity, policy, or binding inputs are malformed or contradictory and cannot be trusted",
			NextEvidence:     "supply a valid boundary identity, policy, and binding"}
	default:
		return &controlstate.DimensionExplanation{Kind: "runtime_generic_incomplete",
			Known:            "a runtime-boundary assessment was produced",
			Missing:          "a decisive, trustworthy runtime crossing observation",
			WhyNotImprovable: "the runtime-boundary verdict is non-positive (reason: " + reasonOrUnspecified(a.ReasonCode) + ")",
			NextEvidence:     "consult " + next + " for the required runtime evidence"}
	}
}

func reasonOrUnspecified(reason string) string {
	if reason == "" {
		return "unspecified"
	}
	return reason
}

// untrustworthyAvailability maps the non-conclusive verdicts to a source availability that yields
// DimUnknown: unavailable/unknown → SourceUnavailable; invalid → SourceInvalid.
func untrustworthyAvailability(v rb.Verdict) controlstate.SourceAvailability {
	if v == rb.VerdictInvalid {
		return controlstate.SourceInvalid
	}
	return controlstate.SourceUnavailable
}
