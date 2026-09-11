// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

// Reproductions of the four defects reported at head 0f153fbf. Expected to FAIL
// there; three of them were introduced or left by the previous repair round.

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/taskcontrol"
)

// N1 (3981430697). A replay refreshes ReceiptDigestSHA256 in memory, then
// updateActiveControlPointer writes that digest into active.yaml while
// control/latest.yaml still holds the old bytes. verifySession then compares the
// pointer against the persisted artifact and reports a digest mismatch, leaving
// the task stale immediately after a SUCCESSFUL replay.
//
// The decisive shape is sequential, per the repair contract: replay, then a
// fresh public call must still verify.
func TestN1_ReplayMustLeaveTheTaskVerifiable(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	decision := recordAdmissionDecision(t, taskDir, time.Now().UTC())
	rebindActivePointer(t, repo, taskDir)
	// This fixture exists to establish the active-pointer replay scenario
	// DETERMINISTICALLY. Skipping on an unexpected error or disposition would let
	// a regression that bypasses the replay path entirely keep CI green while the
	// protected branch is never exercised -- so both are failures.
	if _, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, Active: true}); err != nil {
		t.Fatalf("first advance-task failed: %v", err)
	}
	recordCapabilityConsumption(t, taskDir, decision, time.Now().UTC().Add(time.Minute))
	rebindActivePointer(t, repo, taskDir)

	res, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, Active: true})
	if err != nil {
		t.Fatalf("second advance-task failed: %v", err)
	}
	if res.Disposition != AdvanceReplay {
		t.Fatalf("the fixture no longer reaches replay (disposition=%q); the pointer comparison and the subsequent verification were never exercised", res.Disposition)
	}

	// The pointer must name bytes that exist on disk.
	persisted, err := LoadTaskControl(filepath.Join(taskDir, "control", "latest.yaml"))
	if err != nil {
		t.Fatalf("load persisted control: %v", err)
	}
	ptr, err := LoadActivePointer(repo)
	if err != nil {
		t.Fatalf("load pointer: %v", err)
	}
	if ptr.LastTaskControlDigestSHA256 != persisted.ReceiptDigestSHA256 {
		t.Fatalf("N1: after a successful replay the active pointer names %s…, but control/latest.yaml holds %s… — the pointer identifies bytes that were never stored",
			shortOr(ptr.LastTaskControlDigestSHA256), shortOr(persisted.ReceiptDigestSHA256))
	}
	// And the task must still verify through a fresh public call.
	st, err := Status(StatusOptions{RepoRoot: repo, TaskDir: taskDir, Verify: true})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Verified {
		t.Fatalf("N1: the task does not verify immediately after a successful replay: %v", st.VerifyErrors)
	}
}

// N3 (3981430712). A receipt carrying the right task, session, decision digest
// and capability, but naming an operation the decision never admitted, is
// accepted as an ordinary spend. ConsumeCapability refuses to CREATE such a
// receipt (capability.go:47-55); the reader must refuse to BELIEVE one.
func TestN3_ConsumptionMustSpendOnlyAdmittedOperations(t *testing.T) {
	// Only the unknown-id case is driveable end to end: the seeded decision has
	// no non-admitted verdict to name. The declared-but-refused case is covered
	// deterministically by TestConsumptionMembershipSubsetRule rather than left
	// as a permanent skip -- a check that always skips is not a check.
	repo, taskDir := enrolledPreparedTask(t)
	decision := recordAdmissionDecision(t, taskDir, time.Now().UTC())
	rebindActivePointer(t, repo, taskDir)
	rec, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		t.Fatalf("recorded authority: %v", err)
	}
	var admitted []string
	for _, v := range decision.OperationVerdicts {
		if v.Verdict == admission.AdmissionVerdictAdmitted {
			admitted = append(admitted, v.OperationID)
		}
	}
	c, err := admission.ConsumeCapability(decision, rec.Base.Task, rec.Actor, admitted, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	c.ConsumedOperationIDs = []string{"op.never.declared"} // correct in every other respect
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if _, err := admission.RecordAdmissionConsumed(taskLedgerStore(taskDir), head, c, time.Now().UTC()); err != nil {
		t.Fatalf("record: %v", err)
	}
	rebuildProjections(t, taskDir)
	if _, err := governanceDisposition(taskDir, time.Now().UTC(), nil); err == nil {
		t.Fatal("N3: a receipt spending an operation the decision does not admit was accepted as an ordinary spend")
	}
}

// N4 (3981430720). For a task on the file protocol with a positive decision,
// Modify is downgraded while Next still instructs the edit, so Prepare's output
// simultaneously says "no consumable capability" and "perform the edit"; and the
// file protocol's established Modify value changed despite the stated boundary.
func TestN4_PrepareReturnsACoherentPermissionAndAction(t *testing.T) {
	repo, taskDir := notEnrolledPreparedTask(t)
	if gov, err := governanceDisposition(taskDir, time.Now().UTC(), nil); err != nil || gov.Resolved {
		t.Fatalf("fixture must deterministically be on the file protocol, but resolved=%v err=%v", gov.Resolved, err)
	}
	sess, _, err := loadSessionForControl(filepath.Join(taskDir, "session.yaml"))
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	// The reviewer's case: a FILE-PROTOCOL task whose file decision admitted
	// mutation. That is the session resultFromSession receives in production
	// when admission admits and no typed authority was ever resolved.
	sess.MutationCapability = admission.CapabilityAdmitted
	sess.NextActions = []NextAction{{Action: NextPerformEdit, Reference: sess.TaskID}}

	res := resultFromSession(repo, taskDir, sess, "")

	grants := res.Modify == admission.CapabilityAdmitted || res.Modify == admission.CapabilityAdmittedWithConditions
	instructsEdit := res.Next.Action == NextPerformEdit
	if instructsEdit && !grants {
		t.Fatalf("N4a: Prepare publishes Modify=%q while Next=%q instructs the edit — the two describe different evaluations",
			res.Modify, res.Next.Action)
	}
	if res.Modify != sess.MutationCapability {
		t.Fatalf("N4b: a file-protocol task published Modify=%q where the file protocol established %q; the compatibility boundary was crossed",
			res.Modify, sess.MutationCapability)
	}
}

// notEnrolledPreparedTask prepares a task whose chain has NO typed authority --
// the file protocol.
func notEnrolledPreparedTask(t *testing.T) (string, string) {
	t.Helper()
	repo, graph := authorityRepo(t)
	res := prepareEdit(t, repo, graph)
	return repo, filepath.Join(repo, filepath.FromSlash(res.TaskDir))
}

var _ = taskcontrol.ActionCompleteTask

// consumptionBinds is pure, so the three membership cases are tested directly --
// including the one the seeded fixture cannot produce, which would otherwise
// SKIP, and a skipped check is not a check.
func TestConsumptionMembershipSubsetRule(t *testing.T) {
	task := closureprotocol.TaskBinding{ID: "task.m", SessionID: "session.m"}
	rec := admission.RecordedAuthority{}
	rec.Base.Task = task
	dec := closureprotocol.AdmissionDecision{
		CapabilityID: "cap.m",
		OperationVerdicts: []closureprotocol.OperationAdmissionVerdict{
			{OperationID: "op.a", Verdict: admission.AdmissionVerdictAdmitted},
			{OperationID: "op.b", Verdict: admission.AdmissionVerdictAdmitted},
			{OperationID: "op.refused", Verdict: "refused"},
		},
	}
	digest, err := closureprotocol.SemanticDigest(dec)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	// Canonically VALID in every field, so each negative case isolates exactly
	// the membership relation. Without the actor these receipts were refused by
	// the canonical validator, and the negative cases passed for the wrong
	// reason -- proving nothing about membership at all.
	receipt := func(ops ...string) closureprotocol.CapabilityConsumption {
		return closureprotocol.CapabilityConsumption{
			CapabilityID: "cap.m", Task: task, ConsumedOperationIDs: ops,
			ConsumedAt: time.Now().UTC().Format(time.RFC3339), DecisionDigestSHA256: digest,
			OneUseStatus: closureprotocol.ReceiptValid,
			ConsumerActor: closureprotocol.ActorBinding{
				PrincipalID: "principal.m", ActorKind: closureprotocol.ActorAgent,
			},
		}
	}
	if err := closureprotocol.ValidateCapabilityConsumption(receipt("op.a")); err != nil {
		t.Fatalf("the baseline receipt must be canonically valid or these cases prove nothing about membership: %v", err)
	}

	t.Run("valid non-empty subset is accepted", func(t *testing.T) {
		if err := consumptionBinds(receipt("op.a"), dec, rec); err != nil {
			t.Fatalf("a proper non-empty admitted subset was rejected: %v", err)
		}
		if err := consumptionBinds(receipt("op.a", "op.b"), dec, rec); err != nil {
			t.Fatalf("the full admitted set was rejected: %v", err)
		}
	})
	t.Run("unknown operation id is refused", func(t *testing.T) {
		if err := consumptionBinds(receipt("op.never.declared"), dec, rec); err == nil {
			t.Fatal("a receipt spending an operation the decision never declared was accepted")
		}
	})
	t.Run("declared but not admitted is refused", func(t *testing.T) {
		if err := consumptionBinds(receipt("op.refused"), dec, rec); err == nil {
			t.Fatal("a receipt spending a DECLARED but non-admitted operation was accepted")
		}
	})
	t.Run("coverage is not required", func(t *testing.T) {
		// op.b unspent: subset validity, not whole-capability consumption.
		if err := consumptionBinds(receipt("op.a"), dec, rec); err != nil {
			t.Fatalf("spending a subset was treated as incomplete: %v", err)
		}
	})
}

// A governance-integrity failure fails the call closed: no state is produced at
// all, so it never reaches the selector.
func TestGovernanceErrorProducesNoStateAtAll(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	decision := recordAdmissionDecision(t, taskDir, time.Now().UTC())
	rebindActivePointer(t, repo, taskDir)
	rec, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		t.Fatalf("recorded authority: %v", err)
	}
	c, err := admission.ConsumeCapability(decision, rec.Base.Task, rec.Actor, []string{}, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		// no admitted ops to spend; build the mismatch directly
		c = closureprotocol.CapabilityConsumption{
			CapabilityID: "cap.foreign", Task: rec.Base.Task,
			ConsumedOperationIDs: []string{"op.foreign"},
			ConsumedAt:           time.Now().UTC().Format(time.RFC3339),
			DecisionDigestSHA256: strings.Repeat("a", 64), OneUseStatus: closureprotocol.ReceiptValid,
		}
	}
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if _, err := admission.RecordAdmissionConsumed(taskLedgerStore(taskDir), head, c, time.Now().UTC()); err != nil {
		t.Fatalf("record: %v", err)
	}
	rebuildProjections(t, taskDir)

	if _, _, err := ControlStatus(repo, taskDir, false); err == nil {
		t.Fatal("a governance-integrity failure produced a control state instead of failing the call closed")
	}
}

// Contract 3, typed half. The file-protocol case (N4) cannot see this: it never
// enters the ledger-derived branch, so a mutation removing the governed action
// survives it. Equivalent prepare scenarios on BOTH protocols, asserting Modify
// and Next TOGETHER -- a test of Modify alone misses this defect again.
func TestTypedPrepareReturnsACoherentPermissionAndAction(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	decision := recordAdmissionDecision(t, taskDir, time.Now().UTC())
	rebindActivePointer(t, repo, taskDir)
	sess, _, err := loadSessionForControl(filepath.Join(taskDir, "session.yaml"))
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	// The stale session still says waiting and still instructs the edit.
	sess.NextActions = []NextAction{{Action: NextPerformEdit, Reference: sess.TaskID}}

	granted := resultFromSession(repo, taskDir, sess, "")
	if granted.Modify != admission.CapabilityAdmitted {
		t.Fatalf("typed task with an unconsumed capability published Modify=%q, want %q", granted.Modify, admission.CapabilityAdmitted)
	}
	if granted.Next.Action != NextConsumeCapability {
		t.Fatalf("typed task published Modify=%q beside Next=%q; an available capability is CONSUMED before any edit, and the pair must come from one evaluation",
			granted.Modify, granted.Next.Action)
	}

	recordCapabilityConsumption(t, taskDir, decision, time.Now().UTC().Add(time.Minute))
	rebindActivePointer(t, repo, taskDir)
	spent := resultFromSession(repo, taskDir, sess, "")
	if spent.Modify == admission.CapabilityAdmitted {
		t.Fatalf("typed task still publishes Modify=%q after consumption", spent.Modify)
	}
	if spent.Next.Action == NextPerformEdit {
		t.Fatalf("after consumption the published Next is %q -- an unconditional mutation instruction", spent.Next.Action)
	}
	if spent.Next.Action != NextVerifyAdmission {
		t.Fatalf("after consumption Next=%q, want %q (reconcile and record)", spent.Next.Action, NextVerifyAdmission)
	}
}

// R-A. The canonical validator owns the receipt contract. A receipt with the
// right task, session, decision, capability and operations but an invalid actor
// or a non-valid one-use status must be refused -- these are exactly the fields
// a hand-rolled subset omitted.
func TestConsumptionUsesTheCanonicalValidator(t *testing.T) {
	task := closureprotocol.TaskBinding{ID: "task.v", SessionID: "session.v"}
	rec := admission.RecordedAuthority{}
	rec.Base.Task = task
	dec := closureprotocol.AdmissionDecision{
		CapabilityID: "cap.v",
		OperationVerdicts: []closureprotocol.OperationAdmissionVerdict{
			{OperationID: "op.a", Verdict: admission.AdmissionVerdictAdmitted},
		},
	}
	digest, err := closureprotocol.SemanticDigest(dec)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	base := closureprotocol.CapabilityConsumption{
		CapabilityID: "cap.v", Task: task, ConsumedOperationIDs: []string{"op.a"},
		ConsumedAt: time.Now().UTC().Format(time.RFC3339), DecisionDigestSHA256: digest,
		OneUseStatus: closureprotocol.ReceiptValid,
		ConsumerActor: closureprotocol.ActorBinding{
			PrincipalID: "principal.v", ActorKind: closureprotocol.ActorAgent,
		},
	}
	if err := closureprotocol.ValidateCapabilityConsumption(base); err != nil {
		t.Fatalf("the baseline receipt must be canonically valid, so the two negative cases isolate exactly one field each: %v", err)
	}
	if err := consumptionBinds(base, dec, rec); err != nil {
		t.Fatalf("a fully valid receipt was refused: %v", err)
	}
	t.Run("non-valid one-use status is refused", func(t *testing.T) {
		bad := base
		bad.OneUseStatus = closureprotocol.ReceiptRevoked
		if err := consumptionBinds(bad, dec, rec); err == nil {
			t.Fatal("a receipt whose one_use_status is not valid was accepted as a spend")
		}
	})
	t.Run("invalid consumer actor is refused", func(t *testing.T) {
		bad := base
		bad.ConsumerActor = closureprotocol.ActorBinding{PrincipalID: "  ", ActorKind: "not-a-kind"}
		if err := consumptionBinds(bad, dec, rec); err == nil {
			t.Fatal("a receipt with an invalid consumer actor was accepted as a spend")
		}
	})
}

// R-B. Status must PROPAGATE a governance-integrity failure, not substitute a
// waiting disposition and return an actionable state built from the historical
// session -- which could answer `sensei task-status` with a mutation
// instruction.
func TestStatusPropagatesGovernanceIntegrityFailure(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	recordAdmissionDecision(t, taskDir, time.Now().UTC())
	rebindActivePointer(t, repo, taskDir)
	rec, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		t.Fatalf("recorded authority: %v", err)
	}
	c := closureprotocol.CapabilityConsumption{
		CapabilityID: "cap.foreign", Task: rec.Base.Task,
		ConsumedOperationIDs: []string{"op.foreign"},
		ConsumedAt:           time.Now().UTC().Format(time.RFC3339),
		DecisionDigestSHA256: strings.Repeat("a", 64), OneUseStatus: closureprotocol.ReceiptValid,
	}
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if _, err := admission.RecordAdmissionConsumed(taskLedgerStore(taskDir), head, c, time.Now().UTC()); err != nil {
		t.Fatalf("record: %v", err)
	}
	rebuildProjections(t, taskDir)

	for _, verify := range []bool{false, true} {
		res, err := Status(StatusOptions{RepoRoot: repo, TaskDir: taskDir, Verify: verify})
		if err == nil {
			t.Fatalf("Status(Verify=%v) returned a state (phase=%q next=%q) for a governance-integrity failure; it must return no actionable state",
				verify, res.Phase, res.Next.Action)
		}
	}
}
