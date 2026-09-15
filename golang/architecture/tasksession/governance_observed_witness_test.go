// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/lifecycleaction"
)

// An observed mutation is DOWNSTREAM of the spent capability, and the fold must
// say so. change_observed is read before admission_consumed in foldGovernance;
// reversing that order, or dropping the branch, projects "admitted" for a task
// whose mutation already happened, and the next action becomes reconciliation
// instead of scope verification.
//
// Found by adversarial review of aa9e8dd2: the reviewer's claim that the branch
// was missing was false, but deleting it left the whole package green. This is
// the witness that deletion now has to get past, built on a real ledger chain
// rather than a hand-constructed governanceState.
func TestAnObservedMutationFoldsToScopeVerificationNotAdmitted(t *testing.T) {
	_, taskDir := enrolledPreparedTask(t)
	dec := recordAdmissionDecision(t, taskDir, time.Now().UTC())
	recordCapabilityConsumption(t, taskDir, dec, time.Now().UTC())

	consumed := mustDisposition(t, taskDir, time.Now().UTC())
	if consumed.Status != StatusAdmitted {
		t.Fatalf("precondition: a consumed capability with no observed change folded to %q, want %q", consumed.Status, StatusAdmitted)
	}

	ensureChangeObserved(t, taskDir)

	observed := mustDisposition(t, taskDir, time.Now().UTC())
	if observed.Status != StatusMutationObserved || observed.Phase != closureprotocol.PhaseMutationObserved {
		t.Fatalf("an observed mutation folded to phase=%q status=%q, want %q/%q",
			observed.Phase, observed.Status, closureprotocol.PhaseMutationObserved, StatusMutationObserved)
	}
	if observed.GrantModify {
		t.Fatal("an observed mutation re-granted modification")
	}
	if got := lifecycleaction.Select(lifecycleDisposition(observed, true)); got != lifecycleaction.VerifyScope {
		t.Fatalf("an observed mutation selected %q, want %q", got, lifecycleaction.VerifyScope)
	}
}
