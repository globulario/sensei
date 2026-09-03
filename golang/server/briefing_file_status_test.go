// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"testing"

	"github.com/globulario/sensei/golang/architecture/briefingfeedback"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/store"
)

// THE FILE'S VERDICT AND THE SUBSYSTEM'S HEALTH ARE TWO FACTS.
//
// combineBriefingStatus answers "how much should you trust this whole answer",
// and returning DEGRADED for an unavailable feedback subsystem is right for
// that question. It is wrong as the ONLY answer, because it also erases
// "does anything govern this file" — and the two are orthogonal.
//
// Measured 2026-09-02: a server running without --repo-root has no feedback
// projection, so every briefing came back DEGRADED. An automatic pre-edit
// briefing therefore could not distinguish a file governed by a direct anchor
// from a README the graph knows nothing about, and interrupted both
// identically. file_status is the first fact, preserved.
func TestFileStatusSurvivesADegradedFeedbackSubsystem(t *testing.T) {
	for _, base := range []awarenesspb.BriefingStatus{
		awarenesspb.BriefingStatus_BRIEFING_STATUS_OK,
		awarenesspb.BriefingStatus_BRIEFING_STATUS_INFERRED_ONLY,
		awarenesspb.BriefingStatus_BRIEFING_STATUS_CONTEXT_ONLY,
		awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY,
	} {
		t.Run(base.String(), func(t *testing.T) {
			combined := combineBriefingStatus(base, briefingfeedback.FeedbackUnavailable)
			if combined != awarenesspb.BriefingStatus_BRIEFING_STATUS_DEGRADED {
				t.Fatalf("combined = %v; the fixture no longer reaches the case under test "+
					"(an unavailable subsystem must still degrade the composite)", combined)
			}
			// The point: the base is recoverable. If a caller had only the
			// combined value it could not tell these four apart.
			if base == combined {
				t.Fatalf("base and combined are indistinguishable for %v", base)
			}
		})
	}
}

// AND THE REAL RPC MUST SET IT.
//
// A field that exists in the proto and is never populated is the same hole one
// indirection later. The first version of this test built a BriefingResponse
// literal by hand and asserted the two fields differed — which proves the
// struct has two fields and nothing about the server. Deleting `FileStatus:`
// from the response construction survived it.
//
// This drives the actual Briefing RPC on an unconfigured server, which is the
// deployment where feedback is unavailable and every status composes to
// DEGRADED.
func TestTheBriefingRPCPopulatesFileStatus(t *testing.T) {
	s := newTestServer(fakeStore{impactForFile: func(context.Context, string) ([]store.ImpactFact, error) {
		return nil, nil // no anchors: the file's own verdict is not OK
	}})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatal(err)
	}
	// Positive control: the fixture must actually reach the degraded composite,
	// or the assertion below is about a case that never happened.
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_DEGRADED {
		t.Fatalf("status = %v; this fixture no longer reaches the unavailable-feedback case",
			resp.GetStatus())
	}
	// ASSERT THE CONCRETE VALUE, NOT "DIFFERENT FROM status".
	//
	// The zero value of BriefingStatus is OK. Asserting only that file_status
	// differs from status passes when the field is never populated at all —
	// deleting `FileStatus:` from the response construction survived exactly
	// that assertion. This fixture has no anchors, so the file's own verdict is
	// EMPTY; an unset field would read OK and fail here.
	if got := resp.GetFileStatus(); got != awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY {
		t.Fatalf("file_status = %v, want BRIEFING_STATUS_EMPTY for a file with no anchors. "+
			"OK here means the field was never populated and its zero value is being read "+
			"as \"this file is governed\".", got)
	}
	if resp.GetFileStatus() == resp.GetStatus() {
		t.Fatalf("file_status collapsed into status (%v): a caller cannot tell a governed "+
			"file from an ungoverned one while feedback is unavailable", resp.GetStatus())
	}
}
