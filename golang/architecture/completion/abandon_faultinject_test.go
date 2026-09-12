// SPDX-License-Identifier: AGPL-3.0-only

//go:build sensei_faultinject

package completion

import (
	"context"
	"testing"

	"github.com/globulario/sensei/golang/architecture/ledger"
)

// This test needs a post-commit HEAD publication fault. That seam exists ONLY
// under the sensei_faultinject build tag (ledger's faultinject.go), so it ships in
// no normal build; the test is compiled under the same tag. Run:
//
//	go test -tags sensei_faultinject ./golang/architecture/completion/ -run TestADurableAppendCompletesTheRecoveryPath

// R4-F4. A durable entry whose HEAD write failed is post-commit, not a failure.
//
// Store.Append returns ErrEntryDurable to say the terminal fact IS committed and
// only HEAD.yaml is unwritten. Treating it as an ordinary append failure left the
// fact committed, the projection unrebuilt and the pointer live -- and a retry
// with the original expected head then reported stale, because verification
// derives the new durable head. The caller was told the write failed, then that
// it was too late to try again.
//
// Driven through the public AbandonTask with one HEAD-write fault armed at the
// exact boundary, so this proves the BRANCH runs, not merely that the verifier it
// calls works.
func TestADurableAppendCompletesTheRecoveryPath(t *testing.T) {
	w := seedWorldWithoutResult(t)
	id := taskID(t, w.TaskDir)
	setActivePointer(t, w, id)
	before := currentHead(t, w.TaskDir)

	ledger.InjectHeadWriteFaults(1)
	defer ledger.InjectHeadWriteFaults(0)
	res, err := AbandonTask(context.Background(), AbandonRequest{
		RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir, IdentityRoot: w.IdentityRoot,
		ExpectedLedgerHeadDigestSHA256: before, Reason: whyAbandoned,
	})
	if err != nil {
		t.Fatalf("abandon: %v", err)
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
	// Projection recovery completed: the task reconstructs as abandoned.
	a, ierr := InspectTerminalState(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	if ierr != nil || a.State != TerminalAbandoned {
		t.Fatalf("terminal state = %q err=%v, want abandoned", a.State, ierr)
	}

	// A retry is idempotent, and the fault was consumed so this takes the real path.
	retry := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned)
	if retry.Outcome != OutcomeExactReplay {
		t.Fatalf("retry = %q (%s), want exact_replay", retry.Outcome, retry.Detail)
	}
	if n := abandonedEvents(t, w.TaskDir); n != 1 {
		t.Fatalf("abandoned events = %d after retry, want exactly 1", n)
	}
}
