// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/lifecycleaction"
	"github.com/globulario/sensei/golang/architecture/taskcontrol"
)

// G1 (review body, governance.go:246). A decision carrying the CORRECT request
// digest but a verdict list adjudicating none of the planned operations must not
// bind. AllAdmitted iterates only the verdicts it is handed, and the grant that
// follows takes its scope from the recorded ChangePlan -- so every target in the
// plan is exposed for operations the decision never adjudicated.
//
// Original witness: a decision carrying the CORRECT request digest but a verdict list
// that adjudicates none of the planned operations must not bind.
func TestVerdictsMustCoverTheRecordedPlan(t *testing.T) {
	var rec admission.RecordedAuthority
	rec.Base.Task = closureprotocol.TaskBinding{ID: "task.w1", SessionID: "session.w1"}
	rec.ChangePlan = closureprotocol.ChangePlan{PlanID: "plan.w1", Operations: []closureprotocol.ChangeOperation{
		{OperationID: "op.a", Target: "a.go"},
		{OperationID: "op.b", Target: "b.go"},
	}}
	req := closureprotocol.AdmissionRequest{
		ActorBinding:                    rec.Actor,
		BaseBinding:                     rec.Base,
		ChangePlan:                      rec.ChangePlan,
		AuthorityResolutionDigestSHA256: rec.Resolution.AuthorityResolutionDigestSHA256,
	}
	digest, err := closureprotocol.SemanticDigest(req)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	dec := closureprotocol.AdmissionDecision{
		RequestDigestSHA256: digest,
		CapabilityID:        "cap.w1",
		OperationVerdicts: []closureprotocol.OperationAdmissionVerdict{
			{OperationID: "op.unrelated", Verdict: admission.AdmissionVerdictAdmitted},
		},
	}
	if recordedDecisionBinds(dec, rec, time.Now().UTC()) {
		t.Fatal("G1: a decision adjudicating none of the planned operations bound, and the grant exposes every plan target")
	}
}

// G2 (review body, governance.go:136-140). A TERMINAL MUST GUARD ITS OWN ENTRY.
// An appended or imported scope_verified with no admitted decision, no spent
// capability and no observed change behind it must never produce the terminal;
// status and control readers would advertise the result transition for a
// mutation that never happened.
//
// Original witness: a scope_verified terminal with no admitted decision, no spent
// capability and no observed change behind it must not produce a terminal.
func TestScopeVerifiedRequiresItsPredecessors(t *testing.T) {
	_, taskDir := enrolledPreparedTask(t)
	rec, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		t.Fatalf("load recorded authority: %v", err)
	}
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	orphan := admission.ScopeVerification{
		CapabilityID:         "capability.orphan",
		DecisionDigestSHA256: "decision",
		BaseTreeDigestSHA256: "base",
		VerifiedOperationIDs: []string{"op.0"},
		Status:               closureprotocol.ReceiptValid,
		VerifiedAt:           time.Unix(0, 0).UTC().Format(time.RFC3339),
	}
	if _, err := admission.RecordScopeVerified(taskLedgerStore(taskDir), head, rec.Base.Task, orphan, time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("record scope_verified: %v", err)
	}
	gov, err := governanceDisposition(taskDir, time.Now().UTC(), nil)
	if err == nil && gov.Terminal {
		t.Fatalf("G2: an orphan scope_verified produced the terminal %+v with no decision, consumption or observation behind it", gov)
	}
}

// G3 (inline 3991072883, session.go). A consumed receipt cannot distinguish
// consume-before-apply from apply-before-crash, which is why the owner defines
// that state as reconciliation. On the supported same-task replay path (Prepare
// retains an existing ledger and calls resultFromSession) an instruction to
// apply applies the change twice.
//
// Original witness: the consumed state must not be published as "apply the mutation".
func TestConsumedStateIsReconciliationNotAnotherApply(t *testing.T) {
	perm := MutationPermission{
		GovernedMutation: taskcontrol.GovernedMutationDisposition{
			Governed: true, CapabilityConsumed: true,
			Disposition: lifecycleaction.Disposition{
				TypedProtocol: true, AuthorityResolved: true,
				Phase: closureprotocol.PhaseAdmitted, Status: "admitted", CapabilityConsumed: true,
			},
		},
	}
	got := governedNextAction(perm, Session{TaskID: "task.w3"})
	if got.Summary == "apply the admitted mutation, then run verify-admission to record the observed change and verify scope" {
		t.Fatalf("G3: the consumed state published %q, asserting an application that may already have happened", got.Summary)
	}
}

// observedFor builds a change set bound to this task's actor and authority,
// without recording it, so a test can compute its digest before deciding what
// order to write the records in.
func observedFor(t *testing.T, taskDir string) admission.ObservedChangeSet {
	t.Helper()
	rec, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		t.Fatalf("load recorded authority: %v", err)
	}
	actorDigest, err := closureprotocol.SemanticDigest(rec.Actor)
	if err != nil {
		t.Fatalf("actor digest: %v", err)
	}
	return admission.ObservedChangeSet{
		BaseTreeDigestSHA256:            "base.tree.test",
		ResultTreeDigestSHA256:          "result.tree.test",
		ActorBindingDigestSHA256:        actorDigest,
		AuthorityResolutionDigestSHA256: rec.Resolution.AuthorityResolutionDigestSHA256,
	}
}

func appendScopeVerified(t *testing.T, taskDir string, v admission.ScopeVerification) {
	t.Helper()
	rec, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		t.Fatalf("load recorded authority: %v", err)
	}
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if _, err := admission.RecordScopeVerified(taskLedgerStore(taskDir), head, rec.Base.Task, v, time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("record scope_verified: %v", err)
	}
}

func appendChangeObserved(t *testing.T, taskDir string, observed admission.ObservedChangeSet) {
	t.Helper()
	rec, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		t.Fatalf("load recorded authority: %v", err)
	}
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if _, err := admission.RecordChangeObserved(taskLedgerStore(taskDir), head, rec.Base.Task, observed, time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("record change_observed: %v", err)
	}
}

func mustNotBeTerminal(t *testing.T, taskDir, what string) {
	t.Helper()
	gov, err := governanceDisposition(taskDir, time.Now().UTC(), nil)
	if err == nil && gov.Terminal {
		t.Fatalf("%s produced the terminal %+v", what, gov)
	}
}

// COVERAGE, not merely membership. A decision must adjudicate EVERY planned
// operation. Two separate failures hide behind one another: a verdict list that
// names only some of the plan, and one whose cardinality matches the plan while
// naming an operation the plan never contained.
func TestVerdictCoverageIsExactBothWays(t *testing.T) {
	var rec admission.RecordedAuthority
	rec.Base.Task = closureprotocol.TaskBinding{ID: "task.cov", SessionID: "session.cov"}
	rec.ChangePlan = closureprotocol.ChangePlan{PlanID: "plan.cov", Operations: []closureprotocol.ChangeOperation{
		{OperationID: "op.a", Target: "a.go"},
		{OperationID: "op.b", Target: "b.go"},
	}}
	digest, err := closureprotocol.SemanticDigest(closureprotocol.AdmissionRequest{
		ActorBinding: rec.Actor, BaseBinding: rec.Base, ChangePlan: rec.ChangePlan,
		AuthorityResolutionDigestSHA256: rec.Resolution.AuthorityResolutionDigestSHA256,
	})
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	admitted := func(ids ...string) closureprotocol.AdmissionDecision {
		v := make([]closureprotocol.OperationAdmissionVerdict, 0, len(ids))
		for _, id := range ids {
			v = append(v, closureprotocol.OperationAdmissionVerdict{OperationID: id, Verdict: admission.AdmissionVerdictAdmitted})
		}
		return closureprotocol.AdmissionDecision{RequestDigestSHA256: digest, CapabilityID: "cap.cov", OperationVerdicts: v}
	}
	for _, tc := range []struct {
		name string
		ids  []string
	}{
		// A PROPER SUBSET. op.b was never adjudicated, yet the grant would expose
		// its target along with the rest of the plan.
		{"covers only part of the plan", []string{"op.a"}},
		// CARDINALITY IS NOT COVERAGE. Two verdicts for a two-operation plan, one
		// of which the plan never contained.
		{"right count, wrong operations", []string{"op.a", "op.unrelated"}},
		{"duplicated operation padding the count", []string{"op.a", "op.a"}},
		{"no verdicts at all", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if recordedDecisionBinds(admitted(tc.ids...), rec, time.Now().UTC()) {
				t.Fatalf("a decision adjudicating %v bound against a plan of op.a+op.b", tc.ids)
			}
		})
	}

	// POSITIVE CONTROL: exact correspondence still binds.
	if !recordedDecisionBinds(admitted("op.a", "op.b"), rec, time.Now().UTC()) {
		t.Fatal("a decision adjudicating exactly the planned operations was rejected")
	}
}

// Each predecessor is required SEPARATELY. A witness missing the first one never
// reaches the checks for the others.
func TestScopeVerifiedRequiresEachPredecessorSeparately(t *testing.T) {
	t.Run("no change_observed", func(t *testing.T) {
		_, taskDir := enrolledPreparedTask(t)
		now := time.Now().UTC()
		dec := recordAdmissionDecision(t, taskDir, now)
		recordCapabilityConsumption(t, taskDir, dec, now)
		decDigest, err := closureprotocol.SemanticDigest(dec)
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		obsDigest, err := admission.ObservedChangeSetDigest(observedFor(t, taskDir))
		if err != nil {
			t.Fatalf("observed digest: %v", err)
		}
		appendScopeVerified(t, taskDir, admission.ScopeVerification{
			CapabilityID: dec.CapabilityID, DecisionDigestSHA256: decDigest,
			ObservedChangeSetDigestSHA256: obsDigest, Status: closureprotocol.ReceiptValid,
			VerifiedAt: time.Unix(0, 0).UTC().Format(time.RFC3339),
		})
		mustNotBeTerminal(t, taskDir, "a scope verification with an admitted, spent capability but NO observed change")
	})

	t.Run("observed change recorded after the verification of it", func(t *testing.T) {
		_, taskDir := enrolledPreparedTask(t)
		now := time.Now().UTC()
		dec := recordAdmissionDecision(t, taskDir, now)
		recordCapabilityConsumption(t, taskDir, dec, now)
		observed := observedFor(t, taskDir)
		decDigest, err := closureprotocol.SemanticDigest(dec)
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		obsDigest, err := admission.ObservedChangeSetDigest(observed)
		if err != nil {
			t.Fatalf("observed digest: %v", err)
		}
		// Every record is present and every digest matches. Only the ORDER is
		// impossible: the verification precedes the observation it verifies.
		appendScopeVerified(t, taskDir, admission.ScopeVerification{
			CapabilityID: dec.CapabilityID, DecisionDigestSHA256: decDigest,
			ObservedChangeSetDigestSHA256: obsDigest, Status: closureprotocol.ReceiptValid,
			VerifiedAt: time.Unix(0, 0).UTC().Format(time.RFC3339),
		})
		appendChangeObserved(t, taskDir, observed)
		mustNotBeTerminal(t, taskDir, "a scope verification recorded BEFORE the change it verifies")
	})

	t.Run("verifies an observation this task never recorded", func(t *testing.T) {
		_, taskDir := enrolledPreparedTask(t)
		now := time.Now().UTC()
		dec := recordAdmissionDecision(t, taskDir, now)
		recordCapabilityConsumption(t, taskDir, dec, now)
		appendChangeObserved(t, taskDir, observedFor(t, taskDir))
		decDigest, err := closureprotocol.SemanticDigest(dec)
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		other := observedFor(t, taskDir)
		other.ResultTreeDigestSHA256 = "some.other.result.tree"
		otherDigest, err := admission.ObservedChangeSetDigest(other)
		if err != nil {
			t.Fatalf("observed digest: %v", err)
		}
		appendScopeVerified(t, taskDir, admission.ScopeVerification{
			CapabilityID: dec.CapabilityID, DecisionDigestSHA256: decDigest,
			ObservedChangeSetDigestSHA256: otherDigest, Status: closureprotocol.ReceiptValid,
			VerifiedAt: time.Unix(0, 0).UTC().Format(time.RFC3339),
		})
		mustNotBeTerminal(t, taskDir, "a scope verification naming an observed change set this task never recorded")
	})
}
