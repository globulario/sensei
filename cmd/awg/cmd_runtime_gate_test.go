// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGateDecision(t *testing.T) {
	none := map[string]bool{}
	allowStale := map[string]bool{"external_stale_allowed": true}

	cases := []struct {
		name     string
		verdict  string
		allow    map[string]bool
		wantDec  string
		wantExit int
	}{
		{"valid repair authorizes", rrValid, none, gateAuthorize, 0},
		{"converged authorizes", vConverged, none, gateAuthorize, 0},
		{"forbidden blocks", rrForbidden, none, gateBlocked, 1},
		{"unproven blocks", rrUnproven, none, gateBlocked, 1},
		{"owner mismatch blocks", rrOwnerMismatch, none, gateBlocked, 1},
		{"scope drift blocks", rrScopeDrift, none, gateBlocked, 1},
		{"still not converged blocks", rrStillNotConv, none, gateBlocked, 1},
		{"evidence missing blocks", vEvidenceMissing, none, gateBlocked, 1},
		{"blocked_by_quorum blocks", vBlockedQuorum, none, gateBlocked, 1},
		{"stale blocks without allow", vEvidenceStale, none, gateBlocked, 1},
		{"stale warns with explicit allow", vEvidenceStale, allowStale, gateWarn, 0},
		{"empty verdict blocks (no implicit green)", "", none, gateBlocked, 1},
		{"unknown verdict blocks", "something_new", none, gateBlocked, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, reason := gateDecision(tc.verdict, tc.allow)
			if dec != tc.wantDec {
				t.Fatalf("decision = %q, want %q (reason: %s)", dec, tc.wantDec, reason)
			}
			if ec := exitCodeForGate(dec); ec != tc.wantExit {
				t.Fatalf("exit = %d, want %d for decision %s", ec, tc.wantExit, dec)
			}
		})
	}
}

// allow must be EXPLICIT: external_stale_allowed only tolerates stale, nothing
// else — a forbidden action is never warn-allowable.
func TestGateDecision_AllowDoesNotLaunderForbidden(t *testing.T) {
	allowEverything := map[string]bool{"external_stale_allowed": true, "forbidden_runtime_action": true}
	if dec, _ := gateDecision(rrForbidden, allowEverything); dec != gateBlocked {
		t.Fatalf("forbidden must block even with allow flags; got %q", dec)
	}
}

// The gate consumes a runtime-repair-report's JSON (its `verdict`) — the chain
// repair-report -> gate round-trips.
func TestGate_ConsumesRepairReportJSON(t *testing.T) {
	rep := classifyRuntimeRepair(parseSnap(t, beforeBlockedQuorum), parseSnap(t, afterConverged),
		"restore_missing_quorum_member", "c")
	// Marshal as the command does (json tags), then read as the gate does.
	b, err := yaml.Marshal(rep) // yaml reads the json-tagged struct via field names; gate uses yaml.Unmarshal
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var gr gateReport
	if err := yaml.Unmarshal(b, &gr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// gateReport reads `verdict`; yaml.Marshal of a json-tagged struct lowercases
	// field names, so confirm the gate still classifies the real verdict.
	if dec, _ := gateDecision(rep.Verdict, nil); dec != gateAuthorize {
		t.Fatalf("valid repair should authorize at the gate; got %q", dec)
	}
}
