// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/taskcontrol"
)

// Witness for Antigravity chunk test-c P2 on ede08ebe. TestGovernanceDispositionReachesTheSelector
// cannot fail: its fixture's open architect question wins selection before governed selection is read,
// and replacing the governed disposition with an empty one at control.go (W1: :349/:666, W2: :701) left the
// whole package green. refreshCachedEvaluation takes the cached projection as a parameter by design (it
// re-derives only what the ledger can invalidate), so a cached state with nothing ahead of the governed
// branch shows whether the ledger's disposition actually reaches the selector.
func TestCacheOverlayPassesTheGovernedDispositionToTheSelector(t *testing.T) {
	_, taskDir := enrolledPreparedTask(t)
	decision := recordAdmissionDecision(t, taskDir, time.Now().UTC())

	// Nothing that selectNextAction checks before the governed branch: binding current, no eligible
	// evidence, no open question, no active root blocker.
	cached := taskcontrol.TaskControlState{TaskID: "task.cache-overlay", BindingHealth: "current"}

	available, err := refreshCachedEvaluation(taskDir, cached)
	if err != nil {
		t.Fatalf("refresh with an unconsumed capability: %v", err)
	}
	if available.Permission.Modify != admission.CapabilityAdmitted {
		t.Fatalf("precondition: an unconsumed typed capability published modify=%q", available.Permission.Modify)
	}
	if available.NextAction.Kind != taskcontrol.ActionConsumeCapability {
		t.Errorf("an unconsumed typed capability selected %q, want %q: the ledger disposition did not reach the selector",
			available.NextAction.Kind, taskcontrol.ActionConsumeCapability)
	}

	recordCapabilityConsumption(t, taskDir, decision, time.Now().UTC().Add(time.Minute))
	spent, err := refreshCachedEvaluation(taskDir, cached)
	if err != nil {
		t.Fatalf("refresh after consumption: %v", err)
	}
	if spent.NextAction.Kind != taskcontrol.ActionVerifyAdmission {
		t.Errorf("a spent typed capability selected %q, want %q", spent.NextAction.Kind, taskcontrol.ActionVerifyAdmission)
	}
	if spent.ReceiptDigestSHA256 != taskcontrol.StateDigest(spent) {
		t.Errorf("the refreshed receipt digest does not identify the state returned")
	}
}
