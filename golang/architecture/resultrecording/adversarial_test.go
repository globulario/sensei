// SPDX-License-Identifier: Apache-2.0

package resultrecording

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// mutatedPayload records a clean transition, returns its event payload for
// mutation, and provides a re-seal that rebuilds the entry+HEAD consistently so a
// VALID chain reaches the strict recorded-transition binding validator.
func mutatedPayload(t *testing.T) (taskDir, transitionID string, payload ledger.TaskEventPayload, reseal func(ledger.TaskEventPayload)) {
	t.Helper()
	taskDir, res := recordClean(t)
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(recordingPayloadValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		t.Fatal(err)
	}
	last := chain.Entries[len(chain.Entries)-1]
	p, err := loadEventPayload(last)
	if err != nil {
		t.Fatal(err)
	}
	reseal = func(mut ledger.TaskEventPayload) {
		if err := ledger.RewriteLatestPayloadForTest(taskDir, mut, "application/yaml", recordingPayloadValidator); err != nil {
			t.Fatalf("reseal: %v", err)
		}
	}
	return taskDir, res.TransitionID, p, reseal
}

func TestTwoStageRefsSwapped(t *testing.T) {
	taskDir, id, p, reseal := mutatedPayload(t)
	a := stageKey(closureprotocol.ResultPipelineStages[0])
	b := stageKey(closureprotocol.ResultPipelineStages[1])
	p.Artifacts[a], p.Artifacts[b] = p.Artifacts[b], p.Artifacts[a]
	reseal(p)
	_, err := LoadRecordedTransition(taskDir, id)
	var re *Error
	if !errors.As(err, &re) || re.Code != CodeStageMismatch {
		t.Fatalf("want stage_mismatch, got %v", err)
	}
}

func TestProjectionRefsSwapped(t *testing.T) {
	taskDir, id, p, reseal := mutatedPayload(t)
	p.Artifacts[KeySession], p.Artifacts[KeyStatus] = p.Artifacts[KeyStatus], p.Artifacts[KeySession]
	reseal(p)
	rt, err := LoadRecordedTransition(taskDir, id)
	if err == nil {
		err = ValidateRecordedTransition(rt)
	}
	var re *Error
	if !errors.As(err, &re) || re.Code != CodeProjectionDrift {
		t.Fatalf("want projection_drift, got %v", err)
	}
}

func TestEventTaskAltered(t *testing.T) {
	taskDir, id, p, reseal := mutatedPayload(t)
	p.TaskID = "task.other"
	reseal(p)
	if _, err := LoadRecordedTransition(taskDir, id); err == nil {
		t.Fatal("altered event task must fail reload")
	}
}

func TestEventSessionAltered(t *testing.T) {
	taskDir, id, p, reseal := mutatedPayload(t)
	p.SessionID = "session.other"
	reseal(p)
	if _, err := LoadRecordedTransition(taskDir, id); err == nil {
		t.Fatal("altered event session must fail reload")
	}
}

func TestEventResultBindingAltered(t *testing.T) {
	taskDir, id, p, reseal := mutatedPayload(t)
	rb := *p.ResultBinding
	rb.ResultRevision = "0000000000000000000000000000000000000000"
	p.ResultBinding = &rb
	reseal(p)
	_, err := LoadRecordedTransition(taskDir, id)
	var re *Error
	if !errors.As(err, &re) || re.Code != CodeRecordedTransitionInvalid {
		t.Fatalf("want recorded_transition_invalid, got %v", err)
	}
}

// TestUpstreamPayloadMutatedAfterVerification proves the byte-stable snapshot: a
// mutation of an upstream payload after chain verification fails closed on reload.
func TestUpstreamPayloadMutatedAfterVerification(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	if _, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c}); err != nil {
		t.Fatal(err)
	}
	// The seam mutates the scope_verified payload once, after the reload snapshot.
	fired := false
	afterChainSnapshot = func(td string) {
		if fired {
			return
		}
		fired = true
		store := ledger.NewStore(td, ledger.WithPayloadValidator(recordingPayloadValidator))
		chain, err := store.VerifyChain()
		if err != nil {
			t.Fatal(err)
		}
		for _, ve := range chain.Entries {
			if ve.Entry.EventType == closureprotocol.LedgerEventScopeVerified {
				// Corrupt the payload file content in place (path stays, semantic
				// digest drifts from the verified entry's recorded digest).
				if err := osWriteFile(ve.PayloadPath, []byte("tampered: true\n")); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	defer func() { afterChainSnapshot = nil }()

	_, err := LoadRecordedTransition(taskDir, c.Receipt.TransitionID)
	if !fired {
		t.Fatal("seam did not fire")
	}
	if err == nil {
		t.Fatal("a payload mutated after verification must fail reload closed")
	}
}

func osWriteFile(path string, data []byte) error { return os.WriteFile(path, data, 0o644) }
