// SPDX-License-Identifier: AGPL-3.0-only

package resultrecording

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	yaml "gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/resultpipeline"
)

func recordClean(t *testing.T) (string, RecordResult) {
	t.Helper()
	taskDir, c := cleanCandidate(t, recAt)
	res, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c})
	if err != nil {
		t.Fatal(err)
	}
	return taskDir, res
}

func TestTamperImpactReportRejected(t *testing.T) {
	taskDir, res := recordClean(t)
	p := filepath.Join(taskDir, filepath.FromSlash(res.ImpactReportRef.Path))
	if err := os.WriteFile(p, []byte(`{"schema_version":"governedimpact.report/v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRecordedTransition(taskDir, res.TransitionID); err == nil {
		t.Fatal("tampered impact report must fail reload")
	}
}

func TestTamperProjectionRejected(t *testing.T) {
	taskDir, res := recordClean(t)
	// Corrupt the content-addressed session projection artifact.
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(recordingPayloadValidator))
	chain, _ := store.VerifyChain()
	last := chain.Entries[len(chain.Entries)-1]
	payload, _ := loadEventPayload(last)
	ref := payload.Artifacts[KeySession]
	if err := os.WriteFile(filepath.Join(taskDir, filepath.FromSlash(ref.Path)), []byte(`{"schema_version":"resultrecording.projection/v1","kind":"session"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRecordedTransition(taskDir, res.TransitionID); err == nil {
		t.Fatal("tampered projection artifact must fail reload")
	}
}

func TestSwappedStageRefRejected(t *testing.T) {
	taskDir, res := recordClean(t)
	// Rewrite the event payload dropping a stage artifact key. The semantic payload
	// digest changes, so chain verification fails and the reload fails closed.
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(recordingPayloadValidator))
	chain, _ := store.VerifyChain()
	last := chain.Entries[len(chain.Entries)-1]
	payload, err := loadEventPayload(last)
	if err != nil {
		t.Fatal(err)
	}
	delete(payload.Artifacts, stageKey(closureprotocol.ResultPipelineStages[0])) // drop a stage ref
	out, err := yamlMarshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(last.PayloadPath, out, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRecordedTransition(taskDir, res.TransitionID); err == nil {
		t.Fatal("missing stage ref must fail reload")
	}
}

// TestReplayAfterLedgerAdvancedDoesNotRegress: after the ledger advances past the
// transition, an exact replay still reports the transition's own entry (not the new
// head) and re-projects the same next state without regression.
func TestReplayAfterLedgerAdvancedDoesNotRegress(t *testing.T) {
	taskDir, c := cleanCandidate(t, recAt)
	res, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c})
	if err != nil {
		t.Fatal(err)
	}
	txEntry := res.EntryDigestSHA256

	// Advance the ledger with an unrelated event (a divergent scope_verified).
	appendDivergentScope(t, taskDir, c)
	newHead, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	if newHead == txEntry {
		t.Fatal("ledger did not advance")
	}

	replay, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c})
	if err != nil {
		t.Fatalf("replay after advance: %v", err)
	}
	if replay.Disposition != DispositionReconciled {
		t.Fatalf("replay disposition = %s", replay.Disposition)
	}
	// The transition entry identity is still the transition's own entry.
	if replay.EntryDigestSHA256 != txEntry {
		t.Fatalf("replay reported entry %s, want the transition's own entry %s", replay.EntryDigestSHA256, txEntry)
	}
	// But the reported CURRENT head is the actual advanced head, not the transition.
	if replay.CurrentLedgerHeadSHA256 != newHead {
		t.Fatalf("replay current head = %s, want the actual advanced head %s", replay.CurrentLedgerHeadSHA256, newHead)
	}
	if replay.CurrentLedgerHeadSHA256 == txEntry {
		t.Fatal("replay misreported the transition entry as the current head")
	}
	// The reported projected state is the current rebuilt projection (unchanged by
	// the divergent event, which carried no projection), read from status.yaml.
	cur, err := currentProjectedState(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	if replay.TaskPhase != cur.TaskPhase || replay.OperationalStatus != cur.OperationalStatus || replay.NextAction != cur.NextAction {
		t.Fatalf("replay state %s/%s/%s != current projection %s/%s/%s",
			replay.TaskPhase, replay.OperationalStatus, replay.NextAction, cur.TaskPhase, cur.OperationalStatus, cur.NextAction)
	}
	if replay.TaskPhase != res.TaskPhase || replay.OperationalStatus != res.OperationalStatus {
		t.Fatal("replay regressed the projected task state")
	}
	if countTransitionEvents(t, taskDir) != 1 {
		t.Fatal("replay appended a second transition event")
	}
}

// appendDivergentScope advances the ledger with a benign, later scope_verified
// event so a replay must not regress to it.
func appendDivergentScope(t *testing.T, taskDir string, c resultpipeline.TransitionCandidate) {
	t.Helper()
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(func(et closureprotocol.LedgerEventType, mt string, data []byte) error {
		return ledger.ValidateTaskEventPayload(et, data)
	}))
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	div := admission.ScopeVerification{
		CapabilityID: "cap.later", DecisionDigestSHA256: "d", ActorBindingDigestSHA256: "a",
		AuthorityResolutionDigestSHA256: "au", BaseTreeDigestSHA256: "b", ResultTreeDigestSHA256: "r",
		ObservedChangeSetDigestSHA256: "o", VerifiedOperationIDs: []string{"op.later"},
		Status: closureprotocol.ReceiptValid, VerifiedAt: "2026-07-20T00:00:00Z",
	}
	if _, err := admission.RecordScopeVerified(store, head, c.Receipt.Task, div, e0); err != nil {
		t.Fatalf("advance ledger: %v", err)
	}
}

func yamlMarshal(v any) ([]byte, error) { return yaml.Marshal(v) }
