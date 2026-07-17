// SPDX-License-Identifier: AGPL-3.0-only

package resultrecording

import (
	"context"
	"errors"
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/resultpipeline"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

const recAt = "2026-07-17T14:30:00Z"

func countTransitionEvents(t *testing.T, taskDir string) int {
	t.Helper()
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(recordingPayloadValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, ve := range chain.Entries {
		if ve.Entry.EventType == closureprotocol.LedgerEventResultTransitionRecorded {
			n++
		}
	}
	return n
}

func TestRecordCleanTransition(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	headBefore, _ := admission.TaskLedgerHead(taskDir)

	res, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if res.Disposition != DispositionRecorded {
		t.Fatalf("disposition = %s, want recorded", res.Disposition)
	}
	if res.TaskPhase != closureprotocol.PhaseProving || res.OperationalStatus != StatusReadyForProving {
		t.Fatalf("ready result did not enter proving: %s/%s", res.TaskPhase, res.OperationalStatus)
	}
	if res.PreviousLedgerHeadSHA256 != headBefore {
		t.Fatal("previous head not the pre-record head")
	}
	if len(res.StageRefs) != 10 {
		t.Fatalf("stage refs = %d", len(res.StageRefs))
	}
	if res.ProjectionState != "current" {
		t.Fatalf("projection state = %s", res.ProjectionState)
	}
	if countTransitionEvents(t, taskDir) != 1 {
		t.Fatal("expected exactly one transition event")
	}

	// Independent reload + validate from disk.
	rt, err := LoadRecordedTransition(taskDir, c.Receipt.TransitionID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := ValidateRecordedTransition(rt); err != nil {
		t.Fatalf("recorded transition invalid: %v", err)
	}
	if string(rt.ReceiptBytes) != string(c.ReceiptBytes) {
		t.Fatal("reloaded receipt bytes differ from the candidate")
	}
	if len(rt.Stages) != 10 {
		t.Fatalf("reloaded stages = %d", len(rt.Stages))
	}
	// No certification / completion event was created.
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(recordingPayloadValidator))
	chain, _ := store.VerifyChain()
	for _, ve := range chain.Entries {
		switch ve.Entry.EventType {
		case closureprotocol.LedgerEventCertified, closureprotocol.LedgerEventCompleted:
			t.Fatalf("forbidden event %s recorded", ve.Entry.EventType)
		}
	}
}

func TestRecordExactReplayAppendsNoSecondEvent(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	if _, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c}); err != nil {
		t.Fatal(err)
	}
	res2, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if res2.Disposition != DispositionReconciled {
		t.Fatalf("replay disposition = %s, want reconciled", res2.Disposition)
	}
	if countTransitionEvents(t, taskDir) != 1 {
		t.Fatal("replay appended a second event")
	}
	// Repeated replay still one event.
	if _, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c}); err != nil {
		t.Fatal(err)
	}
	if countTransitionEvents(t, taskDir) != 1 {
		t.Fatal("repeated replay appended another event")
	}
}

func TestRecordStaleExpectedHead(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	c.ExpectedLedgerHeadDigestSHA256 = "1111111111111111111111111111111111111111111111111111111111111111"
	_, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c})
	var re *Error
	if !errors.As(err, &re) || re.Code != CodeStaleExpectedHead {
		t.Fatalf("want stale_expected_head, got %v", err)
	}
	if countTransitionEvents(t, taskDir) != 0 {
		t.Fatal("stale head still recorded an event")
	}
}

func TestRecordTamperedCandidateRejectedBeforeStorage(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	c.ReceiptBytes = append(append([]byte(nil), c.ReceiptBytes...), ' ')
	_, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c})
	var re *Error
	if !errors.As(err, &re) || re.Code != CodeInvalidCandidate {
		t.Fatalf("want invalid_candidate, got %v", err)
	}
	if countTransitionEvents(t, taskDir) != 0 {
		t.Fatal("tampered candidate recorded an event")
	}
}

func TestRecordTransitionIDConflict(t *testing.T) {
	// Two candidates for the same logical transition (same task/result/policy) but
	// different recording times: same transition id, different receipt digest.
	repo, taskDir, resultRev := seedCleanTask(t, "package src\n\n// Publish is a no-op.\nfunc Publish() {}\n")
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	mk := func(at string) resultpipeline.TransitionCandidate {
		c, err := resultpipeline.PrepareTransition(context.Background(), resultpipeline.PrepareTransitionRequest{
			Build: resultpipeline.BuildRequest{RepositoryRoot: repo, TaskDirectory: taskDir,
				ResultMode: resulttransition.ResultModeRevision, ResultRevision: resultRev, RepositoryDomain: rDomain},
			ExpectedLedgerHeadDigestSHA256: head, RecordedAt: at,
		})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	c1 := mk("2026-07-17T14:30:00Z")
	c2 := mk("2026-07-18T09:00:00Z")
	if c1.Receipt.TransitionID != c2.Receipt.TransitionID {
		t.Fatal("expected the same transition id")
	}
	if _, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c1}); err != nil {
		t.Fatal(err)
	}
	_, err = RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c2})
	var re *Error
	if !errorsAs(err, &re) || re.Code != CodeTransitionIDConflict {
		t.Fatalf("want transition_id_conflict, got %v", err)
	}
	if countTransitionEvents(t, taskDir) != 1 {
		t.Fatal("conflict recorded a second event")
	}
}

func errorsAs(err error, target **Error) bool { return errors.As(err, target) }
