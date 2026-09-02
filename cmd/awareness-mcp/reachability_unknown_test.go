// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// A GRAPH THAT STATES NO REVISION IS UNKNOWN, AND UNKNOWN MUST BE SAID OUT LOUD.
//
// reachabilityMap returned nil for an empty build commit, so every surface below
// emitted no reachability signal at all. The justification was that "the question
// was never askable" -- but reachability.Assess answers exactly this input in its
// first case, as Unknown. The report was therefore not withheld for want of an
// answer; the answer was discarded.
//
// This is the state measured live on 2026-09-01: graph_build_commit empty while
// the same response said state=current, verdict=authoritative. An agent reading
// that response had nothing to distinguish it from a graph whose generation was
// established and fresh.
func TestAGraphStatingNoRevisionIsReportedUnknownNotOmitted(t *testing.T) {
	m := reachabilityMap(context.Background(), "")
	if m == nil {
		t.Fatal("no reachability report for a graph that states no revision: " +
			"Unknown was deleted rather than reported")
	}
	if got := m["state"]; got != "unknown" {
		t.Fatalf("state=%v, want unknown", got)
	}
	if r, _ := m["reachable"].(bool); r {
		t.Fatal("a graph of unestablished generation reported as reachable")
	}
	// The whole point of the state is that it does NOT license "no such rule".
	if aa, _ := m["asserts_absence"].(bool); aa {
		t.Fatal("an unknown reachability state claimed to assert absence")
	}
	if d, _ := m["detail"].(string); !strings.Contains(d, "does not state which revision") {
		t.Fatalf("detail does not name the reason: %q", d)
	}
}

// Whitespace is not a revision. A build commit of spaces must reach the owner as
// empty, not be measured against the corpus as if it named something.
func TestABlankBuildCommitIsNotARevision(t *testing.T) {
	m := reachabilityMap(context.Background(), "   \t ")
	if m == nil {
		t.Fatal("no reachability report for a blank build commit")
	}
	if got := m["state"]; got != "unknown" {
		t.Fatalf("state=%v, want unknown", got)
	}
}

// EVERY SURFACE, NOT THE HELPER ALONE.
//
// The helper was already "the ONE place the assessment is built" and the nil
// return still reached four renderers, because each guarded on nil separately.
// Fixing the helper without these four would leave the same hole behind a
// different line of code, which is the failure this repository keeps recording:
// a mechanism repaired at one surface while its siblings are forgotten.
func TestEverySurfaceReportsUnknownWhenTheGraphStatesNoRevision(t *testing.T) {
	ctx := context.Background()
	authority := &awarenesspb.GraphAuthority{
		Authoritative:       true,
		GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
		GraphBuildCommit:    "", // the measured live condition
	}
	resp := &awarenesspb.MetadataResponse{
		GraphBuildCommit: "",
		Authority:        authority,
	}

	t.Run("formatMetadata", func(t *testing.T) {
		requireUnknownLine(t, formatMetadata(ctx, resp))
	})
	t.Run("formatGraphAuthority", func(t *testing.T) {
		requireUnknownLine(t, formatGraphAuthority(ctx, authority))
	})
	t.Run("authorityStruct", func(t *testing.T) {
		requireUnknownKey(t, authorityStruct(ctx, authority)["reachability"])
	})
	t.Run("structMetadata", func(t *testing.T) {
		auth, ok := structMetadata(ctx, resp)["authority"].(map[string]interface{})
		if !ok {
			t.Fatal("structMetadata emitted no authority object")
		}
		requireUnknownKey(t, auth["reachability"])
	})
}

func requireUnknownLine(t *testing.T, out string) {
	t.Helper()
	if !strings.Contains(out, "reachability:") {
		t.Fatalf("surface emitted no reachability line at all:\n%s", out)
	}
	if !strings.Contains(out, "unknown") {
		t.Fatalf("reachability line does not report unknown:\n%s", out)
	}
}

func requireUnknownKey(t *testing.T, v interface{}) {
	t.Helper()
	m, ok := v.(map[string]interface{})
	if !ok {
		t.Fatalf("surface emitted no reachability object (got %T): "+
			"an absent key is indistinguishable from a build that never reports one", v)
	}
	if got := m["state"]; got != "unknown" {
		t.Fatalf("state=%v, want unknown", got)
	}
}
