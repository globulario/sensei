// SPDX-License-Identifier: Apache-2.0

//go:build sensei_faultinject

package tasksession

import (
	"context"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/resultrecording"
)

// Scenario 6 — post-commit recovery. Requires the non-shipping HEAD-write fault
// seam (ledger.InjectHeadWriteFaults), so it is compiled only under the
// sensei_faultinject build tag and is absent from every normal build. Run:
//
//	go test -tags sensei_faultinject ./golang/architecture/tasksession/ -run TestE2EPostCommit
//
// The transition entry becomes durable but HEAD reconciliation fails, so the
// orchestrator surfaces the committed identity and a recovery action instead of a
// false success; an exact retry after the fault clears reconciles with no second
// event.
func TestE2EPostCommitRecoveryRetriesWithoutSecondEvent(t *testing.T) {
	repo, taskDir, resultRev := e2eSeedClean(t)
	req := AdvanceResultRequest{
		RepositoryRoot: repo, TaskDirectory: taskDir, RepositoryDomain: e2eDomain, ResultRevision: resultRev,
		Now: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
	}

	// Fail the append's HEAD write AND the reconciliation's HEAD write.
	ledger.InjectHeadWriteFaults(2)
	defer ledger.InjectHeadWriteFaults(0)

	res, err := AdvanceResultTransition(context.Background(), req)
	if err != nil {
		t.Fatalf("a post-commit condition must be a result, not a hard error: %v", err)
	}
	if res.Outcome != OutcomePostCommitIncomplete {
		t.Fatalf("outcome = %s, want post_commit_incomplete", res.Outcome)
	}
	if res.PostCommitEntryDigestSHA256 == "" || res.PostCommitRecoveryAction == "" {
		t.Fatal("post-commit must expose the committed entry identity and a recovery action")
	}
	if e2eCountTransitions(e2eLedgerEvents(t, taskDir)) != 1 {
		t.Fatal("the durable entry must exist exactly once")
	}

	// Exact retry after the obstruction clears: reconcile, no second event.
	ledger.InjectHeadWriteFaults(0)
	retry, err := AdvanceResultTransition(context.Background(), req)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if retry.Outcome != OutcomeRecorded {
		t.Fatalf("retry outcome = %s, want recorded", retry.Outcome)
	}
	if retry.TransitionDisposition == resultrecording.DispositionRecorded {
		t.Fatal("retry must reconcile the durable entry, not perform a fresh record")
	}
	if e2eCountTransitions(e2eLedgerEvents(t, taskDir)) != 1 {
		t.Fatal("retry appended a second transition event")
	}
}
