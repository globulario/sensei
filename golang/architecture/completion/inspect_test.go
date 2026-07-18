// SPDX-License-Identifier: AGPL-3.0-only

package completion

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

func (w world) inspect(t *testing.T) TerminalStateAssessment {
	t.Helper()
	a, err := InspectTerminalState(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	return a
}

func (w world) recover(t *testing.T) RecoverResult {
	t.Helper()
	r, err := RecoverProjections(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	return r
}

func deleteProjections(t *testing.T, taskDir string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(taskDir, "projections")); err != nil {
		t.Fatal(err)
	}
}

func deleteReceiptArtifact(t *testing.T, taskDir string) {
	t.Helper()
	chain, err := ledger.NewStore(taskDir).VerifyChain()
	if err != nil {
		t.Fatal(err)
	}
	for i := len(chain.Entries) - 1; i >= 0; i-- {
		ve := chain.Entries[i]
		if ve.Entry.EventType != closureprotocol.LedgerEventCompleted {
			continue
		}
		data, _ := ledger.ReadVerifiedPayload(ve)
		payload, _ := ledger.ParseTaskEventPayload(data)
		ref := payload.Artifacts[completionArtifactKey]
		if err := os.Remove(filepath.Join(taskDir, filepath.FromSlash(ref.Path))); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("no completed event")
}

func seedOrphanReceipt(t *testing.T, taskDir string) {
	t.Helper()
	store := ledger.NewStore(taskDir)
	// A terminal-receipt-shaped blob with no completed event referencing it.
	if _, err := store.StoreArtifactBytes([]byte(`{"schema_version":"`+TerminalReceiptSchemaVersion+`"}`), "application/json"); err != nil {
		t.Fatal(err)
	}
}

// Proof 1: a successful completion is reconstructed as committed from durable state
// alone (a fresh Inspect call reads only disk — the restart proof).
func TestInspectCommittedAfterRestart(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("setup completion failed")
	}
	a := w.inspect(t)
	if a.State != TerminalCommitted {
		t.Fatalf("state = %s (%s), want committed", a.State, a.Detail)
	}
	if a.Committed == nil || a.Committed.GovernedDriftAfterCompletion {
		t.Fatal("committed facts must be present with no drift")
	}
}

// Proof 2: deleted projections are reconstructed as projection_stale_or_missing, and
// a bounded rebuild restores committed — appending no ledger event or receipt.
func TestInspectAndRecoverProjectionLoss(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("setup completion failed")
	}
	deleteProjections(t, w.TaskDir)
	if a := w.inspect(t); a.State != TerminalProjectionStaleOrMissing {
		t.Fatalf("state = %s, want projection_stale_or_missing", a.State)
	}
	entriesBefore := ledgerEntryCount(t, w.TaskDir)
	rec := w.recover(t)
	if rec.Outcome != RecoverProjectionsRebuilt || rec.After == nil || rec.After.State != TerminalCommitted {
		t.Fatalf("recover = %s, after = %v", rec.Outcome, rec.After)
	}
	if ledgerEntryCount(t, w.TaskDir) != entriesBefore {
		t.Fatal("recovery appended a ledger event")
	}
	if w.inspect(t).State != TerminalCommitted {
		t.Fatal("state not committed after rebuild")
	}
}

// Proof 3: a receipt persisted without a completed event is residue, not completion.
func TestInspectReceiptWithoutEvent(t *testing.T) {
	w := seedWorld(t)
	seedOrphanReceipt(t, w.TaskDir)
	a := w.inspect(t)
	if a.State != TerminalReceiptWithoutEvent {
		t.Fatalf("state = %s, want receipt_without_event", a.State)
	}
	// recovery must not bless residue.
	if rec := w.recover(t); rec.Outcome != RecoverNothingToRecover {
		t.Fatalf("recover = %s, want nothing_to_recover", rec.Outcome)
	}
	if hasLedgerEvent(t, w.TaskDir, "completed") {
		t.Fatal("residue must never become a completed event")
	}
}

// Proof 4: a completed event whose receipt artifact is missing is broken, not
// completion.
func TestInspectEventWithoutReceipt(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("setup completion failed")
	}
	deleteReceiptArtifact(t, w.TaskDir)
	if a := w.inspect(t); a.State != TerminalEventWithoutValidReceipt {
		t.Fatalf("state = %s, want event_without_valid_receipt", a.State)
	}
	if rec := w.recover(t); rec.Outcome != RecoverBrokenCompletion {
		t.Fatalf("recover = %s, want broken_completion", rec.Outcome)
	}
}

// Proof 5: tampered receipt bytes are an integrity failure.
func TestInspectTamperedReceipt(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("setup completion failed")
	}
	tamperCompletionReceipt(t, w.TaskDir)
	if a := w.inspect(t); a.State != TerminalIntegrityFailure {
		t.Fatalf("state = %s, want integrity_failure", a.State)
	}
}

// Proof 6: duplicate completed facts are contradictory history, never normalized.
func TestInspectDuplicateCompleted(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("setup completion failed")
	}
	seedCompletedEvent(t, w.TaskDir)
	if a := w.inspect(t); a.State != TerminalContradictoryHistory {
		t.Fatalf("state = %s, want contradictory_terminal_history", a.State)
	}
	rec := w.recover(t)
	if rec.Outcome != RecoverContradictory {
		t.Fatalf("recover = %s, want contradictory_terminal_history", rec.Outcome)
	}
	if countLedgerEvents(t, w.TaskDir, "completed") != 2 {
		t.Fatal("recovery must not normalize contradictory history")
	}
}

// Proof 7: completed plus revoked is contradictory history.
func TestInspectCompletedPlusRevoked(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("setup completion failed")
	}
	w.appendRevoked(t)
	if a := w.inspect(t); a.State != TerminalContradictoryHistory {
		t.Fatalf("state = %s, want contradictory_terminal_history", a.State)
	}
}

// Proof 8: a completion bound to a result other than the current one is wrong-bound.
func TestInspectWrongResultBinding(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("setup completion failed")
	}
	rb := currentResultBinding(t, w.TaskDir)
	newer := rb
	newer.ResultTreeDigestSHA256 = "7777777777777777777777777777777777777777777777777777777777777777"
	w.appendResultTransition(t, newer)
	if a := w.inspect(t); a.State != TerminalWrongBinding {
		t.Fatalf("state = %s, want wrong_task_or_result_binding", a.State)
	}
}

// Proof 9: governed drift after completion is reported distinctly from corruption —
// the completion remains committed and the historical receipt is unaltered.
func TestInspectGovernedDriftAfterCompletion(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("setup completion failed")
	}
	recordedDigest := w.inspect(t).Committed.ReceiptDigestSHA256
	changeGoverned(t, w.Repo)
	a := w.inspect(t)
	if a.State != TerminalCommitted {
		t.Fatalf("state = %s, want committed (drift is not corruption)", a.State)
	}
	if a.Committed == nil || !a.Committed.GovernedDriftAfterCompletion {
		t.Fatal("governed drift must be reported distinctly")
	}
	if a.Committed.ReceiptDigestSHA256 != recordedDigest {
		t.Fatal("drift must not alter the historical receipt")
	}
}

// Proof 10: repeated inspection is deterministic; inspection and already-current
// recovery write no authoritative truth.
func TestInspectDeterministicAndSideEffectFree(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("setup completion failed")
	}
	before := treeDigest(t, w.Repo)
	a1 := w.inspect(t)
	a2 := w.inspect(t)
	if a1.DigestSHA256 != a2.DigestSHA256 {
		t.Fatal("inspection is not deterministic")
	}
	if treeDigest(t, w.Repo) != before {
		t.Fatal("inspection mutated the repository")
	}
	entriesBefore := ledgerEntryCount(t, w.TaskDir)
	if rec := w.recover(t); rec.Outcome != RecoverAlreadyCurrent {
		t.Fatalf("recover on a current completion = %s, want already_current", rec.Outcome)
	}
	if ledgerEntryCount(t, w.TaskDir) != entriesBefore {
		t.Fatal("recovery appended a ledger event on an already-current task")
	}
	if treeDigest(t, w.Repo) != before {
		t.Fatal("already-current recovery mutated the repository")
	}
}

// A not-completed task is reconstructed as not_completed with nothing to recover.
func TestInspectNotCompleted(t *testing.T) {
	w := seedWorld(t)
	if a := w.inspect(t); a.State != TerminalNotCompleted {
		t.Fatalf("state = %s, want not_completed", a.State)
	}
	if rec := w.recover(t); rec.Outcome != RecoverNothingToRecover {
		t.Fatalf("recover = %s, want nothing_to_recover", rec.Outcome)
	}
}
