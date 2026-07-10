// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestBuildRuntimeCandidate_Mappings(t *testing.T) {
	cases := []struct {
		verdict  string
		wantKind string
	}{
		{rrForbidden, candForbiddenAction},
		{vIdentityMismatch, candInvariant},
		{rrUnproven, candFailureMode},
		{vEvidenceMissing, candRequiredLane},
		{vEvidenceStale, candFailureMode},
		{rrOwnerMismatch, candFailureMode},
		{rrScopeDrift, candFailureMode},
		{vBlockedQuorum, candFailureMode},
		{vBlockedDependency, candFailureMode},
		{vNotConverged, candFailureMode},
	}
	for _, tc := range cases {
		t.Run(tc.verdict, func(t *testing.T) {
			c, ok := buildRuntimeCandidate(runtimeReportInput{Verdict: tc.verdict, Subject: "service:x@n", Action: "some_action"})
			if !ok {
				t.Fatalf("expected a candidate for %q", tc.verdict)
			}
			if c.Kind != tc.wantKind {
				t.Fatalf("kind = %q, want %q", c.Kind, tc.wantKind)
			}
			if c.ProposedID == "" || c.Title == "" || c.Rationale == "" {
				t.Fatalf("candidate must be fully described: %+v", c)
			}
		})
	}
}

// Healthy verdicts produce NO candidate — there is nothing to govern.
func TestBuildRuntimeCandidate_HealthyHasNoCandidate(t *testing.T) {
	for _, v := range []string{vConverged, rrValid} {
		if _, ok := buildRuntimeCandidate(runtimeReportInput{Verdict: v}); ok {
			t.Fatalf("verdict %q is healthy and must not yield a candidate", v)
		}
	}
}

// THE governance invariant (meta.discovery_produces_candidates_not_facts for the
// runtime lane): every emitted candidate is status=candidate + requires_review —
// memory SUGGESTS, it never produces an active/enforced rule. Run it across every
// verdict the engine can emit, including unknown ones.
func TestBuildRuntimeCandidate_AlwaysCandidateNeverFact(t *testing.T) {
	allVerdicts := []string{
		rrForbidden, vIdentityMismatch, rrUnproven, vEvidenceMissing, vEvidenceStale,
		rrOwnerMismatch, rrScopeDrift, vBlockedQuorum, vBlockedDependency, vBlockedAdmission,
		vBlockedDesiredMiss, rrStillNotConv, vNotConverged, "some_unknown_future_verdict", "",
	}
	for _, v := range allVerdicts {
		c, ok := buildRuntimeCandidate(runtimeReportInput{Verdict: v, Subject: "s"})
		if !ok {
			continue // healthy / no-candidate is fine
		}
		if c.Status != "candidate" {
			t.Fatalf("verdict %q produced status %q — MUST be candidate (never an enforced fact)", v, c.Status)
		}
		if !c.RequiresReview {
			t.Fatalf("verdict %q produced requires_review=false — every candidate needs governed review", v)
		}
		if c.SchemaVersion != runtimeCandidateSchema {
			t.Fatalf("verdict %q produced schema %q", v, c.SchemaVersion)
		}
	}
}

// An unknown verdict still yields a (review-gated) candidate, not a silent drop.
func TestBuildRuntimeCandidate_UnknownVerdictStillCandidate(t *testing.T) {
	c, ok := buildRuntimeCandidate(runtimeReportInput{Verdict: "brand_new_verdict", Subject: "s"})
	if !ok || c.Kind != candFailureMode || c.Status != "candidate" {
		t.Fatalf("unknown verdict should yield a review-gated failure_mode candidate; got ok=%v %+v", ok, c)
	}
}
