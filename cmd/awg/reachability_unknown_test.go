// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// THE HUMAN BLOCK NAMES REACHABILITY EVEN WHEN THE GRAPH NAMES NO REVISION.
//
// The reachability line was printed only when GetGraphBuildCommit() was
// non-empty. An empty build commit is not a missing question -- it is the
// FIRST Unknown case in reachability.Assess -- so the guard removed the line
// from exactly the graph that cannot account for itself, and printed
// "Authority: authoritative (current)" with nothing beside it.
//
// That output is the pre-#321 block this line exists to replace, reproduced by
// the condition #321 was written for. Measured live on 2026-09-01.
func TestTheAuthorityBlockNamesReachabilityWhenTheGraphStatesNoRevision(t *testing.T) {
	out := captureStdout(t, func() {
		printGraphAuthority(&awarenesspb.GraphAuthority{
			Authoritative:       true,
			GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
			GraphBuildCommit:    "", // the measured live condition
		})
	})
	if !strings.Contains(out, "Reachability:") {
		t.Fatalf("no reachability line for a graph that states no revision; "+
			"the block claims authority and says nothing about it:\n%s", out)
	}
	if !strings.Contains(out, "UNKNOWN") {
		t.Fatalf("reachability line does not report UNKNOWN:\n%s", out)
	}
	// An Unknown that does not warn against being read as absence is the
	// failure mode itself, one indirection later.
	if !strings.Contains(out, "not as absent") {
		t.Fatalf("UNKNOWN does not warn against reading a missing rule as an "+
			"absent rule:\n%s", out)
	}
}

// A --json CALLER GETS THE KEY, CARRYING "unknown".
//
// withReachability returned the encoded response untouched when the authority
// stated no build commit, on the grounds that an absent key beat a "fabricated"
// unknown. It is not fabricated: the owner returns it. And for an automated
// caller -- which is most callers, and the reason this moved out of the human
// renderer -- a missing key reads as a field this build does not emit, while
// "unknown" reads as a generation that could not be established.
func TestJSONCallersSeeUnknownRatherThanAMissingKey(t *testing.T) {
	resp := &awarenesspb.MetadataResponse{
		Authority: &awarenesspb.GraphAuthority{
			Authoritative:       true,
			GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
			GraphBuildCommit:    "",
		},
	}
	encoded, err := json.Marshal(map[string]interface{}{"authoritative": true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(withReachability(encoded, resp), &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	r, ok := obj["reachability"].(map[string]interface{})
	if !ok {
		t.Fatalf("no reachability key for a graph that states no revision (got %T); "+
			"an absent key is indistinguishable from a build that never emits one", obj["reachability"])
	}
	if got := r["state"]; got != "unknown" {
		t.Fatalf("state=%v, want unknown", got)
	}
	if reachable, _ := r["reachable"].(bool); reachable {
		t.Fatal("a graph of unestablished generation reported as reachable")
	}
	if aa, _ := r["asserts_absence"].(bool); aa {
		t.Fatal("an unknown reachability state claimed to assert absence")
	}
}
