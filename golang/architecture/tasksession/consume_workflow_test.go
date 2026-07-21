// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import "testing"

// The capability is consumed before the mutation, so the governed next action
// at ready_for_mutation is consume-admission (not "perform edit"), and once
// admitted it is verify-admission. Consumption is never hidden.

func TestGovernedNextActionSurfacesConsumeThenVerify(t *testing.T) {
	var res StatusResult
	applyGovernedDisposition(&res, governanceState{Status: StatusReadyForMutation, Resolved: true}, StatusReadyForMutation)
	if res.Next.Action != NextConsumeCapability {
		t.Fatalf("ready_for_mutation next = %q, want %q", res.Next.Action, NextConsumeCapability)
	}

	res = StatusResult{}
	applyGovernedDisposition(&res, governanceState{Status: StatusAdmitted, Resolved: true}, StatusAdmitted)
	if res.Next.Action != NextVerifyAdmission {
		t.Fatalf("admitted next = %q, want %q", res.Next.Action, NextVerifyAdmission)
	}
}
