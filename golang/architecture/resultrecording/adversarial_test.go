// SPDX-License-Identifier: AGPL-3.0-only

package resultrecording

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// recordMutatedTransition stores a real candidate's artifacts through the NORMAL
// content-addressed path, builds a valid result-transition event, applies mutate,
// and appends it as the SOLE transition through the normal Append path. No already
// committed ledger entry is ever rewritten: the chain the reload verifies is valid,
// so the malicious payload reaches the strict recorded-transition binding validator
// rather than a generic broken-chain rejection.
func recordMutatedTransition(t *testing.T, mutate func(*ledger.TaskEventPayload)) (taskDir, transitionID string) {
	t.Helper()
	taskDir, c := cleanCandidate(t, recAt)
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(recordingPayloadValidator))
	next, err := ClassifyNextState(c.BuildResult.ProofRequirements)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, _, _, _, err := storeArtifacts(store, c, next)
	if err != nil {
		t.Fatal(err)
	}
	payload := ledger.TaskEventPayload{
		SchemaVersion: ledger.EventPayloadSchemaVersion,
		EventType:     closureprotocol.LedgerEventResultTransitionRecorded,
		TaskID:        c.Receipt.Task.ID,
		SessionID:     c.Receipt.Task.SessionID,
		TaskPhase:     next.TaskPhase,
		Status:        next.OperationalStatus,
		ResultBinding: &c.Receipt.ResultBinding,
		Artifacts:     artifacts,
		Limitations:   c.Receipt.Limitations,
	}
	mutate(&payload)
	producedAt, err := time.Parse(time.RFC3339, c.Receipt.RecordedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(), ledger.AppendRequest{
		TaskID:                   c.Receipt.Task.ID,
		SessionID:                c.Receipt.Task.SessionID,
		ExpectedHeadDigestSHA256: c.ExpectedLedgerHeadDigestSHA256,
		EventType:                closureprotocol.LedgerEventResultTransitionRecorded,
		Payload:                  payload,
		PayloadMediaType:         "application/yaml",
		ProducerID:               recordingProducerID,
		ProducedAt:               producedAt,
	}); err != nil {
		t.Fatalf("append malicious transition: %v", err)
	}
	return taskDir, c.Receipt.TransitionID
}

func TestTwoStageRefsSwapped(t *testing.T) {
	taskDir, id := recordMutatedTransition(t, func(p *ledger.TaskEventPayload) {
		a := stageKey(closureprotocol.ResultPipelineStages[0])
		b := stageKey(closureprotocol.ResultPipelineStages[1])
		p.Artifacts[a], p.Artifacts[b] = p.Artifacts[b], p.Artifacts[a]
	})
	_, err := LoadRecordedTransition(taskDir, id)
	var re *Error
	if !errors.As(err, &re) || re.Code != CodeStageMismatch {
		t.Fatalf("want stage_mismatch, got %v", err)
	}
}

func TestProjectionRefsSwapped(t *testing.T) {
	taskDir, id := recordMutatedTransition(t, func(p *ledger.TaskEventPayload) {
		p.Artifacts[KeySession], p.Artifacts[KeyStatus] = p.Artifacts[KeyStatus], p.Artifacts[KeySession]
	})
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
	taskDir, id := recordMutatedTransition(t, func(p *ledger.TaskEventPayload) {
		p.TaskID = "task.other"
	})
	if _, err := LoadRecordedTransition(taskDir, id); err == nil {
		t.Fatal("altered event task must fail reload")
	}
}

func TestEventSessionAltered(t *testing.T) {
	taskDir, id := recordMutatedTransition(t, func(p *ledger.TaskEventPayload) {
		p.SessionID = "session.other"
	})
	if _, err := LoadRecordedTransition(taskDir, id); err == nil {
		t.Fatal("altered event session must fail reload")
	}
}

func TestEventResultBindingAltered(t *testing.T) {
	taskDir, id := recordMutatedTransition(t, func(p *ledger.TaskEventPayload) {
		rb := *p.ResultBinding
		rb.ResultRevision = "0000000000000000000000000000000000000000"
		p.ResultBinding = &rb
	})
	_, err := LoadRecordedTransition(taskDir, id)
	var re *Error
	if !errors.As(err, &re) || re.Code != CodeRecordedTransitionInvalid {
		t.Fatalf("want recorded_transition_invalid, got %v", err)
	}
}

// TestUpstreamPayloadMutatedAfterVerification proves the byte-stable snapshot: a
// mutation of an upstream payload after chain verification fails closed on reload.
// It never rewrites a committed entry through any ledger API; it corrupts a payload
// file on disk to simulate post-verification drift, and ReadVerifiedPayload catches
// it because the file's recomputed digest no longer matches the verified entry.
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
				if err := os.WriteFile(ve.PayloadPath, []byte("tampered: true\n"), 0o644); err != nil {
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
