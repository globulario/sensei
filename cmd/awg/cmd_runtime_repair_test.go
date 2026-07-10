// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func parseSnap(t *testing.T, src string) runtimeEvidenceSnapshot {
	t.Helper()
	var s runtimeEvidenceSnapshot
	if err := yaml.Unmarshal([]byte(src), &s); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	return s
}

const (
	beforeBlockedQuorum = `
schema_version: runtime-evidence/v1
platform: globular
generated_at: t
subject: {type: service, id: sidekick, node: nuc}
lanes:
  desired_state: {freshness: fresh, owner: cc, facts: {desired_build_id: abc}}
  observed_state: {freshness: fresh, owner: na, facts: {installed: false}}
  quorum: {freshness: fresh, owner: doc, facts: {quorum_met: false, required_members: 3, available_members: 2}}`

	afterConverged = `
schema_version: runtime-evidence/v1
platform: globular
generated_at: t
subject: {type: service, id: sidekick, node: nuc}
lanes:
  desired_state: {freshness: fresh, owner: cc, facts: {desired_build_id: abc}}
  observed_state: {freshness: fresh, owner: na, facts: {installed: true, running: true}}`
)

func TestClassifyRuntimeRepair(t *testing.T) {
	cases := []struct {
		name        string
		before      string
		after       string
		action      string
		contract    string
		wantVerdict string
		wantExit    int
	}{
		{
			name:   "valid repair — quorum restored, converged",
			before: beforeBlockedQuorum, after: afterConverged,
			action: "restore_missing_quorum_member", contract: "sidekick_requires_object_store_quorum",
			wantVerdict: rrValid, wantExit: 0,
		},
		{
			// forbidden overrides otherwise-good evidence (after IS converged).
			name:   "forbidden action overrides good after-state",
			before: beforeBlockedQuorum, after: afterConverged,
			action: "bypass_quorum_gate", contract: "c",
			wantVerdict: rrForbidden, wantExit: 1,
		},
		{
			name:   "no governing contract",
			before: beforeBlockedQuorum, after: afterConverged,
			action: "restore_missing_quorum_member", contract: "",
			wantVerdict: rrUnproven, wantExit: 1,
		},
		{
			name:   "unknown action not in safe allowlist",
			before: beforeBlockedQuorum, after: afterConverged,
			action: "twiddle_the_thing", contract: "c",
			wantVerdict: rrUnproven, wantExit: 1,
		},
		{
			// allowed action + contract, but after still blocked → honest, not valid.
			name:   "after still blocked by quorum",
			before: beforeBlockedQuorum,
			after: `
schema_version: runtime-evidence/v1
platform: globular
generated_at: t
subject: {type: service, id: sidekick, node: nuc}
lanes:
  desired_state: {freshness: fresh, owner: cc, facts: {desired_build_id: abc}}
  observed_state: {freshness: fresh, owner: na, facts: {installed: false}}
  quorum: {freshness: fresh, owner: doc, facts: {quorum_met: false}}`,
			action: "restore_missing_quorum_member", contract: "c",
			wantVerdict: rrStillNotConv, wantExit: 1,
		},
		{
			name:   "after evidence stale cannot validate repair",
			before: beforeBlockedQuorum,
			after: `
schema_version: runtime-evidence/v1
platform: globular
generated_at: t
subject: {type: service, id: sidekick, node: nuc}
lanes:
  desired_state: {freshness: fresh, owner: cc, facts: {desired_build_id: abc}}
  observed_state: {freshness: stale, owner: na, facts: {installed: true, running: true}}`,
			action: "restore_missing_quorum_member", contract: "c",
			wantVerdict: vEvidenceStale, wantExit: 1,
		},
		{
			name:   "after required evidence missing",
			before: beforeBlockedQuorum,
			after: `
schema_version: runtime-evidence/v1
platform: globular
generated_at: t
subject: {type: service, id: sidekick, node: nuc}
lanes:
  desired_state: {freshness: fresh, owner: cc, facts: {desired_build_id: abc}}`,
			action: "restore_missing_quorum_member", contract: "c",
			wantVerdict: vEvidenceMissing, wantExit: 1,
		},
		{
			name:   "after lane has no owner authority anchor",
			before: beforeBlockedQuorum,
			after: `
schema_version: runtime-evidence/v1
platform: globular
generated_at: t
subject: {type: service, id: sidekick, node: nuc}
lanes:
  desired_state: {freshness: fresh, owner: cc, facts: {desired_build_id: abc}}
  observed_state: {freshness: fresh, owner: "", facts: {installed: true, running: true}}`,
			action: "restore_missing_quorum_member", contract: "c",
			wantVerdict: rrOwnerMismatch, wantExit: 1,
		},
		{
			// after evidence is for a different subject than the claim.
			name:   "scope drift — after subject differs",
			before: beforeBlockedQuorum,
			after: `
schema_version: runtime-evidence/v1
platform: globular
generated_at: t
subject: {type: service, id: OTHER, node: nuc}
lanes:
  desired_state: {freshness: fresh, owner: cc, facts: {desired_build_id: abc}}
  observed_state: {freshness: fresh, owner: na, facts: {installed: true, running: true}}`,
			action: "restore_missing_quorum_member", contract: "c",
			wantVerdict: rrScopeDrift, wantExit: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := classifyRuntimeRepair(parseSnap(t, tc.before), parseSnap(t, tc.after), tc.action, tc.contract)
			if rep.Verdict != tc.wantVerdict {
				t.Fatalf("verdict = %q, want %q (reason: %s)", rep.Verdict, tc.wantVerdict, rep.Reason)
			}
			if ec := exitCodeForRepair(rep.Verdict); ec != tc.wantExit {
				t.Fatalf("exit = %d, want %d for verdict %s", ec, tc.wantExit, rep.Verdict)
			}
		})
	}
}

func TestClassifyActionSafety(t *testing.T) {
	if classifyActionSafety("restore_missing_quorum_member") != "allowed" {
		t.Error("restore_missing_quorum_member should be allowed")
	}
	if classifyActionSafety("bypass_quorum_gate") != "forbidden" {
		t.Error("bypass_quorum_gate should be forbidden")
	}
	if classifyActionSafety("brand_new_action") != "unknown" {
		t.Error("an unrecognized action should be unknown (not auto-safe)")
	}
}

// The valid-repair report records the before→after transition honestly.
func TestClassifyRuntimeRepair_RecordsTransition(t *testing.T) {
	rep := classifyRuntimeRepair(parseSnap(t, beforeBlockedQuorum), parseSnap(t, afterConverged),
		"restore_missing_quorum_member", "c")
	if rep.BeforeVerdict != vBlockedQuorum || rep.AfterVerdict != vConverged {
		t.Fatalf("transition not recorded: before=%q after=%q", rep.BeforeVerdict, rep.AfterVerdict)
	}
}
