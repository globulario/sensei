// SPDX-License-Identifier: AGPL-3.0-only

package runtimeboundary

import "testing"

// TestDeterminism_ObservationOrder proves the assessment digest does not depend on the input order
// of observations — the owner aggregates by fixed precedence and canonicalizes every collection.
func TestDeterminism_ObservationOrder(t *testing.T) {
	id, res := tID(t)
	pol := tPolicy(t)
	bind := tBinding(t)

	mk := func(sfx, contract string) RuntimeObservation {
		o := tObs()
		o.ObservationID = "obs-" + sfx
		o.EndpointOrContractIdentity = contract
		return o
	}
	// A mix: two satisfying (required contract) and one out-of-scope (other contract).
	obs := []RuntimeObservation{
		mk("a", tContract),
		mk("b", "contract.other"),
		mk("c", tContract),
	}
	rev := []RuntimeObservation{obs[2], obs[1], obs[0]}

	fwd := assess(t, AssessmentInput{Identity: id, IdentityResolution: res, Policy: &pol, Binding: &bind, Observations: obs, CollectorAvailable: true})
	bwd := assess(t, AssessmentInput{Identity: id, IdentityResolution: res, Policy: &pol, Binding: &bind, Observations: rev, CollectorAvailable: true})

	if fwd.Meta.DigestSHA256 != bwd.Meta.DigestSHA256 {
		t.Fatalf("assessment digest depends on observation input order: %s != %s", fwd.Meta.DigestSHA256, bwd.Meta.DigestSHA256)
	}
	if fwd.Verdict != VerdictSatisfied {
		t.Fatalf("expected the satisfying crossings to yield satisfied, got %s", fwd.Verdict)
	}
}

// TestDeterminism_ConflictOrder proves preserved conflicts are canonical (sorted+unique), so the
// digest is stable regardless of which conflicting observation arrived first.
func TestDeterminism_ConflictOrder(t *testing.T) {
	id, res := tID(t)
	pol := tPolicy(t)
	bind := tBinding(t)

	sat := tObs()
	sat.ObservationID = "obs-sat"
	forb := tObs()
	forb.ObservationID = "obs-forb"
	forb.AuthContextPresent = false

	fwd := assess(t, AssessmentInput{Identity: id, IdentityResolution: res, Policy: &pol, Binding: &bind, Observations: []RuntimeObservation{sat, forb}, CollectorAvailable: true})
	bwd := assess(t, AssessmentInput{Identity: id, IdentityResolution: res, Policy: &pol, Binding: &bind, Observations: []RuntimeObservation{forb, sat}, CollectorAvailable: true})

	if fwd.Meta.DigestSHA256 != bwd.Meta.DigestSHA256 {
		t.Fatalf("conflict digest depends on order: %s != %s", fwd.Meta.DigestSHA256, bwd.Meta.DigestSHA256)
	}
	if fwd.ResultKind != KindContradictoryObservations {
		t.Fatalf("expected contradictory_observations, got %s", fwd.ResultKind)
	}
}
