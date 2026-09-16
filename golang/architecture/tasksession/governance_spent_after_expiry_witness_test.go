// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// Witness for Antigravity chunk prod-a P1 on ede08ebe. A capability spent INSIDE its
// window must not be repudiated once the window closes: expiry withholds a NEW grant, it does
// not un-spend a recorded one. foldGovernance applies the temporal check before the consumption
// and observation branches.
func TestCapabilitySpentInsideItsWindowIsNotRefusedAfterExpiry(t *testing.T) {
	_, taskDir := enrolledPreparedTask(t)
	decidedAt := time.Now().UTC().Add(-23 * time.Hour) // 24h validity: expires in ~1h
	dec := recordAdmissionDecision(t, taskDir, decidedAt)
	exp, err := time.Parse(time.RFC3339, dec.CapabilityExpiry)
	if err != nil {
		t.Fatalf("expiry: %v", err)
	}
	recordCapabilityConsumption(t, taskDir, dec, decidedAt.Add(time.Hour))

	inside := mustDisposition(t, taskDir, exp.Add(-time.Minute))
	if inside.Status != StatusAdmitted {
		t.Fatalf("precondition: spent capability read inside the window = %q, want %q", inside.Status, StatusAdmitted)
	}
	after := mustDisposition(t, taskDir, exp.Add(time.Hour))
	if after.Status != StatusAdmitted {
		t.Errorf("spent capability read after expiry = phase %q status %q, want %q (spent, not repudiated)", after.Phase, after.Status, StatusAdmitted)
	}

	ensureChangeObserved(t, taskDir)
	observed := mustDisposition(t, taskDir, exp.Add(time.Hour))
	if observed.Status != StatusMutationObserved || observed.Phase != closureprotocol.PhaseMutationObserved {
		t.Errorf("observed mutation read after expiry = phase %q status %q, want mutation_observed", observed.Phase, observed.Status)
	}
}
