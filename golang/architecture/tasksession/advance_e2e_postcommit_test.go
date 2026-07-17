// SPDX-License-Identifier: AGPL-3.0-only

//go:build sensei_faultinject

package tasksession

import (
	"context"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/resultrecording"
	"github.com/globulario/sensei/internal/resulttestkit"
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
	r := e2eSeed(t, resulttestkit.Options{})
	taskDir := r.TaskDir
	req := AdvanceResultRequest{
		RepositoryRoot: r.Repo, TaskDirectory: r.TaskDir, RepositoryDomain: resulttestkit.Domain, ResultRevision: r.ResultRev,
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
	// Repair 2: report the durable transition's RECONSTRUCTED current state — the
	// entry is durable (phase proving), and the on-disk projection is honestly marked
	// drifted since reconciliation did not complete. Never the pre-attempt disp.
	if !res.CurrentStateAvailable {
		t.Fatal("a durable post-commit entry must report its reconstructed current state")
	}
	if res.TaskPhase != closureprotocol.PhaseProving {
		t.Fatalf("post-commit current phase = %s, want proving (the durable transition)", res.TaskPhase)
	}
	if res.ProjectionState != "projection_drift" {
		t.Fatalf("post-commit projection state = %q, want projection_drift (unreconciled)", res.ProjectionState)
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
