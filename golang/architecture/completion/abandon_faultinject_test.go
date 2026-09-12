// SPDX-License-Identifier: AGPL-3.0-only

//go:build sensei_faultinject

// This file is compiled ONLY under the sensei_faultinject build tag. It holds the
// abandonment tests that need a HEAD-publication fault, which the ledger exposes
// nowhere in a default build. Keeping them here is what lets abandon.go carry no
// dependency-substitution seam at all.

package completion

import (
	"context"
	"testing"

	"github.com/globulario/sensei/golang/architecture/ledger"
)

// R4-F4. A durable entry whose HEAD write failed is post-commit, not a failure.
//
// Store.Append returns ErrEntryDurable to say the terminal fact IS committed and
// only HEAD.yaml is unwritten. Treating it as an ordinary append failure left the
// fact committed, the projection unrebuilt and the pointer live -- and a retry
// with the original expected head then reported stale, because verification
// derives the new durable head. The caller was told the write failed, then that
// it was too late to try again.
//
// Driven through the real AbandonTask entry point with the HEAD-publication
// fault armed at the exact boundary, so this proves the BRANCH runs, not
// merely that the verifier it calls works. The fault seam is compiled only under
// sensei_faultinject, so nothing here is reachable from a default build.
func TestADurableAppendCompletesTheRecoveryPath(t *testing.T) {
	w := seedWorldWithoutResult(t)
	id := taskID(t, w.TaskDir)
	setActivePointer(t, w, id)
	before := currentHead(t, w.TaskDir)

	// Arm the HEAD write that the abandonment append is about to perform, so the
	// post-commit condition is real rather than assumed.
	// Clear before arming as well as after: the counters are process-global, so a
	// count inherited from an earlier test would make the assertion below read a
	// fault this call never fired.
	ledger.ClearHeadWriteFaults()
	t.Cleanup(ledger.ClearHeadWriteFaults)
	ledger.InjectHeadWriteFaults(1)

	res, err := AbandonTask(context.Background(), AbandonRequest{
		RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir, IdentityRoot: w.IdentityRoot,
		ExpectedLedgerHeadDigestSHA256: before, Reason: whyAbandoned,
	})
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}

	// THE DISCRIMINATOR. Every other assertion below also holds on the ordinary
	// path -- that is what a transparent recovery means -- so without this one a
	// disarmed fault would leave the test passing while proving nothing.
	// Publication really failed, so the armed fault was really consumed.
	if n := ledger.ConsumedHeadWriteFaults(); n != 1 {
		t.Fatalf("%d injected fault(s) fired, want 1: HEAD publication never failed, so the "+
			"recovery branch was never exercised", n)
	}

	// The event is durable despite the failed publication -- that is what makes
	// this post-commit rather than a failed append.
	if n := abandonedEvents(t, w.TaskDir); n != 1 {
		t.Fatalf("abandoned events = %d, want 1: the injected fault must fire AFTER the entry is durable", n)
	}
	// The branch ran and completed the cleanup rather than stranding the fact.
	if res.Outcome != OutcomeCommitted {
		t.Fatalf("outcome = %q (%s): a durable entry was not carried through the recovery path",
			res.Outcome, res.Detail)
	}
	if res.Receipt == nil || res.ReceiptPath == "" {
		t.Fatalf("recovery returned receipt=%v path=%q; both are returned on the ordinary path",
			res.Receipt != nil, res.ReceiptPath)
	}
	if !res.ActivePointerCleared || pointerExists(t, w.Repo) {
		t.Fatal("the pointer was not retired after a durable append")
	}
	// Derived-state recovery completed. A stale published pointer is a typed
	// integrity error, so this verifying at all is what proves the recovery path
	// reconciled HEAD instead of repairing projections and leaving the pointer
	// pointing at the previous entry for good.
	if rep, verr := ledger.NewStore(w.TaskDir).Verify(); verr != nil || !rep.Valid {
		t.Fatalf("the task ledger does not verify after recovery (err=%v errors=%+v): "+
			"the durable-append path left derived state unreconciled", verr, rep.Errors)
	}

	// Projection recovery completed: the task reconstructs as abandoned.
	a, ierr := InspectTerminalState(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	if ierr != nil || a.State != TerminalAbandoned {
		t.Fatalf("terminal state = %q err=%v, want abandoned", a.State, ierr)
	}

	// A retry is idempotent, and the fault is spent so this takes the real path.
	retry := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned)
	if retry.Outcome != OutcomeExactReplay {
		t.Fatalf("retry = %q (%s), want exact_replay", retry.Outcome, retry.Detail)
	}
	if n := abandonedEvents(t, w.TaskDir); n != 1 {
		t.Fatalf("abandoned events = %d after retry, want exactly 1", n)
	}
}
