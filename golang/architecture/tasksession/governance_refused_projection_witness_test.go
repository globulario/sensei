// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
)

// Witness for Antigravity chunk prod-a P2 on ede08ebe. A typed task whose unused
// capability expired folds to refused; its published permission must say refused, not waiting,
// while a SPENT capability keeps publishing waiting.
func TestRefusedTypedDecisionPublishesRefusedNotWaiting(t *testing.T) {
	_, taskDir := enrolledPreparedTask(t)
	decidedAt := time.Now().UTC().Add(-23 * time.Hour)
	dec := recordAdmissionDecision(t, taskDir, decidedAt)
	exp, err := time.Parse(time.RFC3339, dec.CapabilityExpiry)
	if err != nil {
		t.Fatalf("expiry: %v", err)
	}
	decision, err := loadCurrentAdmissionDecision(taskDir)
	if err != nil {
		t.Fatalf("load decision: %v", err)
	}

	// Unused and expired: refused.
	perm, err := resolveMutationPermission(taskDir, decision, exp.Add(time.Hour))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if perm.Disposition.Status != StatusRefused {
		t.Fatalf("precondition: expired unused capability folded to %q, want %q", perm.Disposition.Status, StatusRefused)
	}
	if perm.Capability != admission.CapabilityRefused {
		t.Errorf("a refused typed decision published Capability=%q, want %q", perm.Capability, admission.CapabilityRefused)
	}

	// Positive control: spent inside the window and read after expiry stays waiting.
	recordCapabilityConsumption(t, taskDir, dec, decidedAt.Add(time.Hour))
	spent, err := resolveMutationPermission(taskDir, decision, exp.Add(time.Hour))
	if err != nil {
		t.Fatalf("resolve spent: %v", err)
	}
	if spent.Capability != admission.CapabilityWaiting {
		t.Errorf("a spent capability published Capability=%q, want %q (spent, not repudiated)", spent.Capability, admission.CapabilityWaiting)
	}
}
