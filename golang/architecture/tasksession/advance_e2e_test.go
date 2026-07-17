// SPDX-License-Identifier: Apache-2.0

package tasksession

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/resultrecording"
	"github.com/globulario/sensei/golang/architecture/resulttestkit"
)

// These repository-level E2E scenarios drive a real admitted + scope-verified task
// (built by resulttestkit from a temporary Git repository, committed base/result
// revisions, governed sources, generated artifacts, and a complete ledger) through
// the single orchestration owner AdvanceResultTransition. The seed lives in
// resulttestkit (imported only by tests, so it never ships) and is shared with the
// CLI E2E — one harness, not three copies.

func e2eAdvance(t *testing.T, r resulttestkit.Result) AdvanceResult {
	t.Helper()
	res, err := AdvanceResultTransition(context.Background(), AdvanceResultRequest{
		RepositoryRoot: r.Repo, TaskDirectory: r.TaskDir, RepositoryDomain: resulttestkit.Domain, ResultRevision: r.ResultRev,
		Now: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	return res
}

func e2eSeed(t *testing.T, opts resulttestkit.Options) resulttestkit.Result {
	t.Helper()
	r, err := resulttestkit.Seed(t.TempDir(), opts)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return r
}

// e2eAllowedEvents is the set of phase-<=7 events this slice may produce; any other
// (evidence, proof, certification, completion, revocation, migration, Phase-8) is a
// boundary escape.
var e2eAllowedEvents = map[closureprotocol.LedgerEventType]bool{
	closureprotocol.LedgerEventTaskPrepared:             true,
	closureprotocol.LedgerEventAuthorityResolved:        true,
	closureprotocol.LedgerEventAdmissionDecided:         true,
	closureprotocol.LedgerEventAdmissionConsumed:        true,
	closureprotocol.LedgerEventChangeObserved:           true,
	closureprotocol.LedgerEventScopeVerified:            true,
	closureprotocol.LedgerEventResultTransitionRecorded: true,
}

func e2eLedgerEvents(t *testing.T, taskDir string) []closureprotocol.LedgerEventType {
	t.Helper()
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(func(et closureprotocol.LedgerEventType, mt string, data []byte) error {
		return ledger.ValidateTaskEventPayload(et, data)
	}))
	chain, err := store.VerifyChain()
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}
	var out []closureprotocol.LedgerEventType
	for _, e := range chain.Entries {
		out = append(out, e.Entry.EventType)
	}
	return out
}

func e2eCountTransitions(events []closureprotocol.LedgerEventType) int {
	n := 0
	for _, e := range events {
		if e == closureprotocol.LedgerEventResultTransitionRecorded {
			n++
		}
	}
	return n
}

// Scenario 1 — ready path: scope_verified → advance → exactly one recorded
// transition → independent reload/validation → phase proving.
func TestE2EReadyPathRecordsAndAdvancesToProving(t *testing.T) {
	r := e2eSeed(t, resulttestkit.Options{})
	res := e2eAdvance(t, r)

	if res.Outcome != OutcomeRecorded {
		t.Fatalf("outcome = %s, want recorded (refusal=%s %s)", res.Outcome, res.RefusalCode, res.RefusalDetail)
	}
	if res.TaskPhase != closureprotocol.PhaseProving {
		t.Fatalf("phase = %s, want proving", res.TaskPhase)
	}
	if res.TransitionEntryDigestSHA256 == "" || res.CurrentLedgerHeadSHA256 == "" {
		t.Fatal("must report transition entry identity and current head")
	}
	if e2eCountTransitions(e2eLedgerEvents(t, r.TaskDir)) != 1 {
		t.Fatal("exactly one transition event expected")
	}
	rt, err := resultrecording.LoadRecordedTransition(r.TaskDir, res.TransitionID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := resultrecording.ValidateRecordedTransition(rt); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// Scenario 2 — complete-but-blocked: a result whose direction requirement raises
// architect questions records exactly one transition, stays scope_verified, and
// retains its waiting reasons; no proving/certification/completion is claimed.
func TestE2ECompleteButBlockedStaysScopeVerified(t *testing.T) {
	r := e2eSeed(t, resulttestkit.Options{
		Direction:   "evolve",
		ResultFiles: map[string]string{"src/model.go": "package src\n\n// evolve\nfunc Publish() {}\n"},
	})
	res := e2eAdvance(t, r)

	if res.Outcome != OutcomeRecorded {
		t.Fatalf("outcome = %s, want recorded (refusal=%s %s)", res.Outcome, res.RefusalCode, res.RefusalDetail)
	}
	if res.TaskPhase != closureprotocol.PhaseScopeVerified {
		t.Fatalf("phase = %s, want scope_verified (blocked)", res.TaskPhase)
	}
	if len(res.WaitingReasons) == 0 {
		t.Fatal("a complete-but-blocked result must retain its waiting reasons")
	}
	if strings.Contains(res.OperationalStatus, "certified") || strings.Contains(res.OperationalStatus, "completed") {
		t.Fatalf("status %q must not claim certified/completed", res.OperationalStatus)
	}
	if e2eCountTransitions(e2eLedgerEvents(t, r.TaskDir)) != 1 {
		t.Fatal("exactly one transition event expected")
	}
}

// Scenario 3 — governed change: a committed result that changes a governed
// invariant (plus its regenerated artifacts) survives the complete orchestration
// with the EXACT governed record identity preserved through storage and reload.
func TestE2EGovernedChangeSurvivesOrchestration(t *testing.T) {
	const wantInvID = "https://globular.io/awareness#invariant/test.publish_mutates_state"
	const changedInvariants = `invariants:
  - id: test.publish_mutates_state
    title: Publish mutates package identity AND governs more
    severity: critical
    status: active
    protects:
      files:
        - src/model.go
    required_tests:
      - src/model_test.go:TestPublish
`
	r := e2eSeed(t, resulttestkit.Options{
		Regenerate: true,
		ResultFiles: map[string]string{
			"src/model.go":                   "package src\n\n// Publish is a no-op.\nfunc Publish() {}\n",
			"docs/awareness/invariants.yaml": changedInvariants,
		},
		AuthorizedSources:   []string{"src/model.go", "docs/awareness/invariants.yaml"},
		AuthorizedGenerated: []string{"golang/server/embeddata/awareness.nt", "golang/server/embeddata/awareness.result-manifest.tsv"},
	})
	res := e2eAdvance(t, r)
	if res.Outcome != OutcomeRecorded {
		t.Fatalf("outcome = %s (refusal=%s %s)", res.Outcome, res.RefusalCode, res.RefusalDetail)
	}

	rt, err := resultrecording.LoadRecordedTransition(r.TaskDir, res.TransitionID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := resultrecording.ValidateRecordedTransition(rt); err != nil {
		t.Fatalf("validate: %v", err)
	}
	for _, im := range rt.ImpactReport.Impacts {
		changed := closureprotocol.GovernedKnowledgeImpactChanged(im)
		if im.Category == "invariants" {
			if !changed || len(im.ChangedRecordIDs) != 1 || im.ChangedRecordIDs[0] != wantInvID {
				t.Fatalf("invariants changed-set = %v, want exactly [%s]", im.ChangedRecordIDs, wantInvID)
			}
			continue
		}
		if changed {
			t.Fatalf("unrelated category %q changed: %v", im.Category, im.ChangedRecordIDs)
		}
	}
}

// Scenario 4 — exact replay: a second advance appends no second event and reports
// the current ledger/projection state.
func TestE2EReplayIsIdempotent(t *testing.T) {
	r := e2eSeed(t, resulttestkit.Options{})
	first := e2eAdvance(t, r)
	second := e2eAdvance(t, r)

	if second.Outcome != OutcomeRecorded {
		t.Fatalf("replay outcome = %s", second.Outcome)
	}
	if second.TransitionDisposition != resultrecording.DispositionReplayed && second.TransitionDisposition != resultrecording.DispositionReconciled {
		t.Fatalf("replay disposition = %s, want replayed/reconciled", second.TransitionDisposition)
	}
	if second.TransitionEntryDigestSHA256 != first.TransitionEntryDigestSHA256 {
		t.Fatal("replay reported a different transition entry")
	}
	if e2eCountTransitions(e2eLedgerEvents(t, r.TaskDir)) != 1 {
		t.Fatal("replay must not append a second transition event")
	}
}

// Scenario 5 — concurrency / stale head: several advances race over one
// scope-verified task. Exactly one may record; the others reconcile (replay) or
// fail closed (refused/stale because the ledger moved during their preparation).
// No advance is a false success, no two disagree on the transition entry, and no
// second transition event is ever appended. (A mid-record stale expected head maps
// to OutcomeStale via resultrecording.RecordTransition, proven in Step 8; here we
// prove the orchestrator preserves single-recording under real concurrency.)
func TestE2EConcurrentAdvancesRecordExactlyOnce(t *testing.T) {
	r := e2eSeed(t, resulttestkit.Options{})
	const n = 4
	type outcome struct {
		res AdvanceResult
		err error
	}
	results := make(chan outcome, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := AdvanceResultTransition(context.Background(), AdvanceResultRequest{
				RepositoryRoot: r.Repo, TaskDirectory: r.TaskDir, RepositoryDomain: resulttestkit.Domain, ResultRevision: r.ResultRev,
				Now: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
			})
			results <- outcome{res, err}
		}()
	}
	wg.Wait()
	close(results)

	recorded := 0
	var entry string
	for o := range results {
		if o.err != nil {
			t.Fatalf("concurrent advance returned a hard error: %v", o.err)
		}
		switch o.res.Outcome {
		case OutcomeRecorded:
			if o.res.TransitionDisposition == resultrecording.DispositionRecorded {
				recorded++
			}
			if entry == "" {
				entry = o.res.TransitionEntryDigestSHA256
			} else if o.res.TransitionEntryDigestSHA256 != entry {
				t.Fatal("concurrent advances disagree on the transition entry identity")
			}
		case OutcomeRefused, OutcomeStale:
			if o.res.TransitionRecorded {
				t.Fatal("a refused/stale advance must record nothing")
			}
		default:
			t.Fatalf("unexpected concurrent outcome %s", o.res.Outcome)
		}
	}
	if recorded != 1 {
		t.Fatalf("exactly one advance may perform a fresh record; got %d", recorded)
	}
	if e2eCountTransitions(e2eLedgerEvents(t, r.TaskDir)) != 1 {
		t.Fatal("no second transition event may be appended under concurrency")
	}
}

// Scenario 9 — boundary proof: after a recorded transition, the ledger contains
// only phase-<=7 events (no evidence, proof, certification, completion, revocation,
// migration, or Phase-8 event), and the projected status never claims certified or
// completed. CorrectnessCertified stays false.
func TestE2EBoundaryProofNoLaterPhaseEvent(t *testing.T) {
	r := e2eSeed(t, resulttestkit.Options{})
	res := e2eAdvance(t, r)
	if res.Outcome != OutcomeRecorded {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	for _, e := range e2eLedgerEvents(t, r.TaskDir) {
		if !e2eAllowedEvents[e] {
			t.Fatalf("out-of-slice ledger event escaped: %s", e)
		}
	}
	if strings.Contains(res.OperationalStatus, "certified") || strings.Contains(res.OperationalStatus, "completed") {
		t.Fatalf("status %q claims certified/completed", res.OperationalStatus)
	}
	if res.TaskPhase == closureprotocol.PhaseCertified || res.TaskPhase == closureprotocol.PhaseCompleted {
		t.Fatalf("phase %s is a later-phase claim", res.TaskPhase)
	}
}
