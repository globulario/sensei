// SPDX-License-Identifier: Apache-2.0

package resultrecording

import (
	"context"
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/proofrequirements"
	"github.com/globulario/sensei/golang/architecture/resultpipeline"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

// blockedCandidate seeds a genuine complete-but-blocked task: a low-risk result
// whose closure is closed but whose evolve-direction requirement (with no intended
// basis) yields architect-required questions, so proving is blocked while
// extraction stays complete.
func blockedCandidate(t *testing.T) (taskDir string, c resultpipeline.TransitionCandidate) {
	t.Helper()
	repo, taskDir, rev := seedTask(t, func(tt *testing.T, r string) {
		rwrite(tt, r, "src/model.go", "package src\n\n// Publish is a no-op.\nfunc Publish() {}\n")
	}, []string{"src/model.go"}, closure.DirectionEvolve)
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	c, err = resultpipeline.PrepareTransition(context.Background(), resultpipeline.PrepareTransitionRequest{
		Build: resultpipeline.BuildRequest{RepositoryRoot: repo, TaskDirectory: taskDir,
			ResultMode: resulttransition.ResultModeRevision, ResultRevision: rev, RepositoryDomain: rDomain},
		ExpectedLedgerHeadDigestSHA256: head, RecordedAt: recAt,
	})
	if err != nil {
		t.Fatalf("prepare blocked candidate: %v", err)
	}
	if c.BuildResult.ProofRequirements.ExtractionCompleteness != proofrequirements.ExtractionComplete ||
		c.BuildResult.ProofRequirements.ProvingDisposition != proofrequirements.ProvingBlocked {
		t.Fatalf("fixture is not complete+blocked: %s/%s",
			c.BuildResult.ProofRequirements.ExtractionCompleteness, c.BuildResult.ProofRequirements.ProvingDisposition)
	}
	return taskDir, c
}

func TestRecordCompleteButBlocked(t *testing.T) {
	taskDir, c := blockedCandidate(t)
	proof := c.BuildResult.ProofRequirements

	res, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c})
	if err != nil {
		t.Fatalf("record blocked: %v", err)
	}
	if res.Disposition != DispositionRecorded {
		t.Fatalf("disposition = %s", res.Disposition)
	}
	// One event; phase stays scope_verified; does NOT enter proving.
	if countTransitionEvents(t, taskDir) != 1 {
		t.Fatal("expected exactly one event")
	}
	if res.TaskPhase != closureprotocol.PhaseScopeVerified {
		t.Fatalf("blocked result entered %s, must stay scope_verified", res.TaskPhase)
	}
	if res.TaskPhase == closureprotocol.PhaseProving {
		t.Fatal("blocked result must not enter proving")
	}
	// The correct waiting class and primary action are projected, and every blocker
	// reason survives.
	next, err := ClassifyNextState(proof)
	if err != nil {
		t.Fatal(err)
	}
	if res.OperationalStatus != next.OperationalStatus || res.NextAction != next.NextAction {
		t.Fatalf("projected status/action %s/%s != classified %s/%s", res.OperationalStatus, res.NextAction, next.OperationalStatus, next.NextAction)
	}
	if len(next.WaitingOn) < 2 {
		t.Fatalf("expected multiple retained blocker reasons, got %v", next.WaitingOn)
	}
	// Every architect question and closure blocker id is retained.
	for _, q := range proof.ArchitectQuestions {
		if !contains(next.WaitingOn, q.ID) {
			t.Fatalf("architect blocker %q not retained", q.ID)
		}
	}
	for _, b := range proof.ClosureBlockers {
		if !contains(next.WaitingOn, b.ID) {
			t.Fatalf("closure blocker %q not retained", b.ID)
		}
	}

	// Reload + validate; projections carry the blocked state.
	rt, err := LoadRecordedTransition(taskDir, c.Receipt.TransitionID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := ValidateRecordedTransition(rt); err != nil {
		t.Fatalf("recorded transition invalid: %v", err)
	}

	// Replay stays one event.
	if _, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c}); err != nil {
		t.Fatal(err)
	}
	if countTransitionEvents(t, taskDir) != 1 {
		t.Fatal("replay appended a second event")
	}

	// No correctness / certification / completion event or claim.
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(recordingPayloadValidator))
	chain, _ := store.VerifyChain()
	for _, ve := range chain.Entries {
		switch ve.Entry.EventType {
		case closureprotocol.LedgerEventCertified, closureprotocol.LedgerEventCompleted:
			t.Fatalf("forbidden event %s recorded", ve.Entry.EventType)
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
