// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/identity"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

func enrolledPreparedTask(t *testing.T) (string, string) {
	t.Helper()
	repo, graph := authorityRepo(t)
	if _, err := identity.Enroll(identity.EnrollOptions{Root: identity.Root(repo), Now: time.Now().UTC()}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	res := prepareEdit(t, repo, graph)
	return repo, filepath.Join(repo, filepath.FromSlash(res.TaskDir))
}

// taskLedgerStore opens a task ledger with the standard payload validator.
func taskLedgerStore(taskDir string) *ledger.Store {
	return ledger.NewStore(taskDir, ledger.WithPayloadValidator(func(et closureprotocol.LedgerEventType, mt string, data []byte) error {
		return ledger.ValidateTaskEventPayload(et, data)
	}))
}

func rebuildProjections(t *testing.T, taskDir string) {
	t.Helper()
	if _, err := ledger.RebuildProjections(taskDir, func(et closureprotocol.LedgerEventType, mt string, data []byte) error {
		return ledger.ValidateTaskEventPayload(et, data)
	}); err != nil {
		t.Fatalf("rebuild projections: %v", err)
	}
}

// rebindActivePointer refreshes the active task pointer to the current ledger
// head, standing in for the re-bind the CLI performs after appending admission
// events, so binding verification stays fresh across manual appends.
func rebindActivePointer(t *testing.T, repo, taskDir string) {
	t.Helper()
	report, err := taskLedgerStore(taskDir).Verify()
	if err != nil {
		t.Fatalf("verify ledger: %v", err)
	}
	ptr, err := LoadActivePointer(repo)
	if err != nil {
		t.Fatalf("load active pointer: %v", err)
	}
	ptr.LedgerHeadDigestSHA256 = report.HeadDigestSHA256
	ptr.LedgerSequence = report.EntryCount
	if err := WriteActivePointer(repo, ptr); err != nil {
		t.Fatalf("write active pointer: %v", err)
	}
}

// recordAdmissionDecision appends a real typed admission_decided event exactly as
// the admit-change writer does: it rebuilds the request from the recorded
// authority, decides admission, records it, and rebuilds projections. It is the
// sole way a task reaches the admitted state — no reader mints one.
func recordAdmissionDecision(t *testing.T, taskDir string, decidedAt time.Time) closureprotocol.AdmissionDecision {
	t.Helper()
	rec, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		t.Fatalf("load recorded authority: %v", err)
	}
	req := closureprotocol.AdmissionRequest{
		ActorBinding:                    rec.Actor,
		BaseBinding:                     rec.Base,
		ChangePlan:                      rec.ChangePlan,
		AuthorityResolutionDigestSHA256: rec.Resolution.AuthorityResolutionDigestSHA256,
		PolicyID:                        strings.TrimSpace(rec.Base.Policies.Admission),
	}
	policy := admission.AdmissionV2Policy{
		PolicyID:           strings.TrimSpace(rec.Base.Policies.Admission),
		CompletionPolicyID: strings.TrimSpace(rec.Base.Policies.Completion),
		ValidityWindow:     24 * time.Hour,
	}
	decision, err := admission.DecideAdmission(req, rec.Resolution, policy, decidedAt.UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("decide admission: %v", err)
	}
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if _, err := admission.RecordAdmissionDecided(taskLedgerStore(taskDir), head, decision, rec.Base.Task, decidedAt.UTC()); err != nil {
		t.Fatalf("record admission_decided: %v", err)
	}
	rebuildProjections(t, taskDir)
	return decision
}

// recordCapabilityConsumption spends the single-use capability of a recorded
// decision, standing in for the consume step of verify-admission.
func recordCapabilityConsumption(t *testing.T, taskDir string, decision closureprotocol.AdmissionDecision, at time.Time) {
	t.Helper()
	rec, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		t.Fatalf("load recorded authority: %v", err)
	}
	ops := make([]string, 0, len(decision.OperationVerdicts))
	for _, v := range decision.OperationVerdicts {
		ops = append(ops, v.OperationID)
	}
	consumption, err := admission.ConsumeCapability(decision, rec.Base.Task, rec.Actor, ops, at.UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("consume capability: %v", err)
	}
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if _, err := admission.RecordAdmissionConsumed(taskLedgerStore(taskDir), head, consumption, at.UTC()); err != nil {
		t.Fatalf("record admission_consumed: %v", err)
	}
	rebuildProjections(t, taskDir)
}

// TestGovernanceReadyForAdmissionBeforeDecision proves the reducer grants no
// mutation while only authority is resolved: a prepared task carries an
// authority_resolved receipt but no typed decision, so it stops at
// ready_for_admission (regression #3). Reading it repeatedly never mints a
// decision or a grant (regression #1).
func TestGovernanceReadyForAdmissionBeforeDecision(t *testing.T) {
	_, taskDir := enrolledPreparedTask(t)
	for i := 0; i < 3; i++ {
		disp := mustDisposition(t, taskDir, time.Now().UTC())
		if !disp.Resolved || disp.Status != StatusReadyForAdmission {
			t.Fatalf("read %d disposition = %+v, want resolved ready_for_admission", i, disp)
		}
		if disp.GrantModify || len(disp.ModifyPaths) != 0 {
			t.Fatalf("read %d granted mutation without a decision: %+v", i, disp)
		}
	}
	if hasEventType(t, taskDir, closureprotocol.LedgerEventAdmissionDecided) {
		t.Fatal("reading a task minted an admission_decided event")
	}
}

// TestGovernanceReadyForMutationAfterDecision proves a recorded typed decision —
// and only a recorded decision — grants the single-use mutation.
func TestGovernanceReadyForMutationAfterDecision(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	recordAdmissionDecision(t, taskDir, time.Now().UTC())
	rebindActivePointer(t, repo, taskDir)

	disp := mustDisposition(t, taskDir, time.Now().UTC())
	if !disp.Resolved || disp.Status != StatusReadyForMutation || !disp.GrantModify {
		t.Fatalf("disposition = %+v, want resolved ready_for_mutation with grant", disp)
	}
	if !hasLimitation(disp.ModifyPaths, "gin.go") {
		t.Fatalf("modify envelope = %v, want gin.go", disp.ModifyPaths)
	}
	st, err := Status(StatusOptions{RepoRoot: repo, Active: true, Verify: true})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Status != StatusReadyForMutation {
		t.Fatalf("reported status = %q, want ready_for_mutation", st.Status)
	}
}

// TestGovernanceRecordedDecisionExpires proves a recorded capability expires
// against its own CapabilityExpiry and that reading it — before or after expiry —
// never moves that expiry forward (regression #2).
func TestGovernanceRecordedDecisionExpires(t *testing.T) {
	_, taskDir := enrolledPreparedTask(t)
	decidedAt := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	decision := recordAdmissionDecision(t, taskDir, decidedAt)
	wantExpiry := decision.CapabilityExpiry
	if wantExpiry == "" {
		t.Fatal("decision has no capability expiry")
	}

	// Within validity: grants, and does not extend the recorded expiry.
	if disp := mustDisposition(t, taskDir, decidedAt.Add(time.Hour)); disp.Status != StatusReadyForMutation {
		t.Fatalf("within-window disposition = %q, want ready_for_mutation", disp.Status)
	}
	if got, _ := admission.LoadRecordedDecision(taskDir); got.CapabilityExpiry != wantExpiry {
		t.Fatalf("read extended expiry: %q -> %q", wantExpiry, got.CapabilityExpiry)
	}

	// Past validity: refused, and still does not rewrite the recorded expiry.
	if disp := mustDisposition(t, taskDir, decidedAt.Add(25*time.Hour)); disp.Status != StatusRefused {
		t.Fatalf("expired disposition = %q, want refused", disp.Status)
	}
	if got, _ := admission.LoadRecordedDecision(taskDir); got.CapabilityExpiry != wantExpiry {
		t.Fatalf("expired read rewrote expiry: %q -> %q", wantExpiry, got.CapabilityExpiry)
	}
}

// TestGovernanceConsumedCapabilityStaysSpent proves a consumed single-use
// capability never reappears as an available mutation grant through a read
// (regression #4). The task remains admitted awaiting its observed change, but no
// modify permission is projected.
func TestGovernanceConsumedCapabilityStaysSpent(t *testing.T) {
	_, taskDir := enrolledPreparedTask(t)
	decision := recordAdmissionDecision(t, taskDir, time.Now().UTC())
	recordCapabilityConsumption(t, taskDir, decision, time.Now().UTC())

	for i := 0; i < 3; i++ {
		disp := mustDisposition(t, taskDir, time.Now().UTC())
		if disp.Status != StatusAdmitted {
			t.Fatalf("read %d disposition = %q, want admitted", i, disp.Status)
		}
		if disp.GrantModify || len(disp.ModifyPaths) != 0 {
			t.Fatalf("read %d re-granted a consumed capability: %+v", i, disp)
		}
	}
}

func TestGovernanceWaitsWhenNotEnrolled(t *testing.T) {
	repo, graph := authorityRepo(t) // no enrollment
	res := prepareEdit(t, repo, graph)
	taskDir := filepath.Join(repo, filepath.FromSlash(res.TaskDir))

	disp := mustDisposition(t, taskDir, time.Now().UTC())
	if disp.Resolved {
		t.Fatalf("disposition should be unresolved without enrollment: %+v", disp)
	}
	if disp.Status != StatusWaitingGovernance {
		t.Fatalf("disposition status = %q, want waiting_governance", disp.Status)
	}
	// An un-governed task must never report a mutation grant.
	st, err := Status(StatusOptions{RepoRoot: repo, Active: true, Verify: true})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Status == StatusReadyForMutation {
		t.Fatal("un-enrolled task reported a mutation grant")
	}
}

// recordScopeVerification appends a scope_verified event to a prepared task
// ledger, standing in for the post-mutation verify-admission step.
func recordScopeVerification(t *testing.T, taskDir string, verified bool) {
	t.Helper()
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	store := taskLedgerStore(taskDir)
	v := admission.ScopeVerification{
		CapabilityID:                  "capability.test",
		DecisionDigestSHA256:          "decision",
		BaseTreeDigestSHA256:          "base",
		ResultTreeDigestSHA256:        "result",
		VerifiedOperationIDs:          []string{"op.0"},
		Status:                        closureprotocol.ReceiptValid,
		VerifiedAt:                    time.Unix(0, 0).UTC().Format(time.RFC3339),
		ScopeVerificationDigestSHA256: "scope",
	}
	if !verified {
		v.Status = closureprotocol.ReceiptInvalid
		v.Violations = []admission.ScopeViolation{{Code: "scope.file.out_of_envelope", Path: "extra.go", Detail: "out of envelope"}}
	}
	rec, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		t.Fatalf("load recorded authority: %v", err)
	}
	if _, err := admission.RecordScopeVerified(store, head, rec.Base.Task, v, time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("record scope_verified: %v", err)
	}
	rebuildProjections(t, taskDir)
}

func hasEventType(t *testing.T, taskDir string, want closureprotocol.LedgerEventType) bool {
	t.Helper()
	return ledgerEventIndex(t, taskDir, want) >= 0
}

// ledgerEventIndex returns the chain position of the first event of the given
// type, or -1 if absent. Chain order is append order, so it doubles as the
// event-ordering oracle.
func ledgerEventIndex(t *testing.T, taskDir string, want closureprotocol.LedgerEventType) int {
	t.Helper()
	chain, err := taskLedgerStore(taskDir).VerifyChain()
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}
	for i, e := range chain.Entries {
		if e.Entry.EventType == want {
			return i
		}
	}
	return -1
}

// TestLedgerOrdersAuthorityBeforeAdmission proves the typed authority resolution
// is ledgered before any admission decision and that preparation records no
// placeholder admission_decided event — the decision is produced later, by
// admit-change (regression #8, #9).
func TestLedgerOrdersAuthorityBeforeAdmission(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	authIdx := ledgerEventIndex(t, taskDir, closureprotocol.LedgerEventAuthorityResolved)
	if authIdx < 0 {
		t.Fatal("preparation recorded no authority_resolved event")
	}
	if ledgerEventIndex(t, taskDir, closureprotocol.LedgerEventAdmissionDecided) >= 0 {
		t.Fatal("preparation recorded a placeholder admission_decided event")
	}
	recordAdmissionDecision(t, taskDir, time.Now().UTC())
	rebindActivePointer(t, repo, taskDir)
	decIdx := ledgerEventIndex(t, taskDir, closureprotocol.LedgerEventAdmissionDecided)
	if decIdx < 0 {
		t.Fatal("admit-change recorded no admission_decided event")
	}
	if authIdx >= decIdx {
		t.Fatalf("authority_resolved (%d) is not ordered before admission_decided (%d)", authIdx, decIdx)
	}
}

// TestGovernanceScopeVerifiedIsNonMutableTerminal proves scope verification is a
// terminal Phase-3 state: it projects scope_verified (regression #5), never
// ready_for_mutation (regression #6), grants no mutation, points the next action
// at result rebuild, and never certifies or completes the task. Repeated reads
// never reopen the terminal.
func TestGovernanceScopeVerifiedIsNonMutableTerminal(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	decision := recordAdmissionDecision(t, taskDir, time.Now().UTC())
	recordCapabilityConsumption(t, taskDir, decision, time.Now().UTC())
	recordScopeVerification(t, taskDir, true)
	rebindActivePointer(t, repo, taskDir)

	disp := mustDisposition(t, taskDir, time.Now().UTC())
	if disp.Status != StatusScopeVerified || !disp.Terminal {
		t.Fatalf("disposition = %+v, want scope_verified terminal", disp)
	}
	if disp.Status == StatusReadyForMutation || disp.GrantModify || len(disp.ModifyPaths) != 0 {
		t.Fatal("scope verification reopened a mutation grant")
	}

	st, err := Status(StatusOptions{RepoRoot: repo, Active: true, Verify: true})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Status != StatusScopeVerified {
		t.Fatalf("reported status = %q, want scope_verified", st.Status)
	}
	if st.Phase != string(closureprotocol.PhaseScopeVerified) {
		t.Fatalf("reported phase = %q, want scope_verified", st.Phase)
	}
	// The only legal next action is the deterministic result rebuild a later
	// phase owns — never a new admission, mutation, certification, or completion.
	if st.Next.Action != NextRebuildResult {
		t.Fatalf("next action = %q, want %q", st.Next.Action, NextRebuildResult)
	}
	if hasEventType(t, taskDir, closureprotocol.LedgerEventCertified) || hasEventType(t, taskDir, closureprotocol.LedgerEventCompleted) {
		t.Fatal("admission v2 emitted a certified/completed event")
	}

	for i := 0; i < 3; i++ {
		if d := mustDisposition(t, taskDir, time.Now().UTC()); d.Status != StatusScopeVerified || d.GrantModify {
			t.Fatalf("re-read %d reopened terminal: %+v", i, d)
		}
	}
}

// TestAdvanceTaskAfterScopeVerificationIsStable proves repeated advance-task
// after scope verification keeps reporting the non-mutable terminal — same
// status and rebuild-and-bind next action — and never re-grants mutation or
// emits certification/completion (regression #7).
func TestAdvanceTaskAfterScopeVerificationIsStable(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	decision := recordAdmissionDecision(t, taskDir, time.Now().UTC())
	recordCapabilityConsumption(t, taskDir, decision, time.Now().UTC())
	recordScopeVerification(t, taskDir, true)
	rebindActivePointer(t, repo, taskDir)

	for i := 0; i < 2; i++ {
		if _, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, Active: true, ObservedAt: "2026-07-14T18:31:00Z"}); err != nil {
			t.Fatalf("advance %d: %v", i, err)
		}
		st, err := Status(StatusOptions{RepoRoot: repo, Active: true, Verify: true})
		if err != nil {
			t.Fatalf("status %d: %v", i, err)
		}
		if st.Status != StatusScopeVerified {
			t.Fatalf("advance %d status = %q, want scope_verified", i, st.Status)
		}
		if st.Next.Action != NextRebuildResult {
			t.Fatalf("advance %d next = %q, want %q", i, st.Next.Action, NextRebuildResult)
		}
	}
	if hasEventType(t, taskDir, closureprotocol.LedgerEventCertified) || hasEventType(t, taskDir, closureprotocol.LedgerEventCompleted) {
		t.Fatal("advance-task after scope verification emitted certified/completed")
	}
}

func TestGovernanceWaitingMechanicalOnScopeViolation(t *testing.T) {
	_, taskDir := enrolledPreparedTask(t)
	recordScopeVerification(t, taskDir, false)

	disp := mustDisposition(t, taskDir, time.Now().UTC())
	if disp.Status != StatusWaitingMechanical {
		t.Fatalf("out-of-envelope disposition = %q, want waiting_mechanical_repair", disp.Status)
	}
}

// mustDisposition folds governance and fails the test on an integrity error, so the
// existing disposition assertions stay concise under the strict (state, error) API.
func mustDisposition(t *testing.T, taskDir string, now time.Time) governanceState {
	t.Helper()
	disp, err := governanceDisposition(taskDir, now)
	if err != nil {
		t.Fatalf("governanceDisposition: %v", err)
	}
	return disp
}
