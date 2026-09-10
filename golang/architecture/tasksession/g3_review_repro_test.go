// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

// Reproductions of the five review findings on PR #351 at head 0a43c29c.
// These are EXPECTED TO FAIL at that head; each documents one defect.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/taskcontrol"
)

// F1 (P1). AdvanceTask's replay early return at control.go:253-258 serves the
// cached control state without recomputing permission, so a spent capability
// can still be reported as admitted.
func TestF1_AdvanceTaskReplayMustNotServeASpentGrant(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	decision := recordAdmissionDecision(t, taskDir, time.Now().UTC())
	rebindActivePointer(t, repo, taskDir)

	first, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, TaskDir: taskDir})
	if err != nil {
		t.Skipf("advance-task unavailable in fixture: %v", err)
	}
	if first.Control.Permission.Modify != admission.CapabilityAdmitted {
		t.Skipf("fixture did not reach a granted cached state (modify=%q)", first.Control.Permission.Modify)
	}
	recordCapabilityConsumption(t, taskDir, decision, time.Now().UTC().Add(time.Minute))
	rebindActivePointer(t, repo, taskDir)

	second, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, TaskDir: taskDir})
	if err != nil {
		t.Skipf("second advance-task failed: %v", err)
	}
	if second.Control.Permission.Modify == admission.CapabilityAdmitted {
		t.Fatalf("F1: advance-task disposition=%v reported modify=admitted after admission_consumed; "+
			"the replay early return served control/latest.yaml without recomputing permission",
			second.Disposition)
	}
}

// F2 (P1). foldGovernance decodes capability_consumption and never reads a
// field of it (governance.go:162-167), so a receipt bound to a FOREIGN decision
// is projected as an ordinary spend -- withholding the legitimate current grant
// and hiding an integrity failure behind a normal-looking "waiting".
func TestF2_ForeignConsumptionMustNotReadAsAnOrdinarySpend(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	decision := recordAdmissionDecision(t, taskDir, time.Now().UTC())
	rebindActivePointer(t, repo, taskDir)

	if gov, err := governanceDisposition(taskDir, time.Now().UTC(), nil); err != nil || !gov.GrantModify {
		t.Skipf("fixture did not reach a granted state (err=%v)", err)
	}

	// A structurally valid consumption bound to a DIFFERENT decision.
	rec, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		t.Fatalf("recorded authority: %v", err)
	}
	ops := make([]string, 0, len(decision.OperationVerdicts))
	for _, v := range decision.OperationVerdicts {
		ops = append(ops, v.OperationID)
	}
	foreign, err := admission.ConsumeCapability(decision, rec.Base.Task, rec.Actor, ops, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	foreign.DecisionDigestSHA256 = strings.Repeat("f", 64) // belongs to no decision here
	foreign.CapabilityID = "capability.forged.0000"

	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if _, err := admission.RecordAdmissionConsumed(taskLedgerStore(taskDir), head, foreign, time.Now().UTC()); err != nil {
		t.Fatalf("record foreign consumption: %v", err)
	}
	rebuildProjections(t, taskDir)

	gov, err := governanceDisposition(taskDir, time.Now().UTC(), nil)
	if err != nil {
		return // a governance-integrity error is the correct outcome
	}
	t.Fatalf("F2: a consumption carrying decision digest %s… and capability %q was accepted as an ordinary spend "+
		"(phase=%s status=%s GrantModify=%v); it must be a governance-integrity error, not a silent waiting",
		foreign.DecisionDigestSHA256[:8], foreign.CapabilityID, gov.Phase, gov.Status, gov.GrantModify)
}

// F3 (P1). Prepare publishes PrepareResult.Modify and persists
// Session.MutationCapability straight from the file decision
// (session.go:1479,1541), bypassing the sole owner.
func TestF3_PrepareAndStatusMustNotPublishARawSessionPermission(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	recordAdmissionDecision(t, taskDir, time.Now().UTC())
	rebindActivePointer(t, repo, taskDir)

	sess, _, err := loadSessionForControl(filepath.Join(taskDir, "session.yaml"))
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	decision, err := loadCurrentAdmissionDecision(taskDir)
	if err != nil {
		t.Fatalf("load decision: %v", err)
	}
	perm, err := resolveMutationPermission(taskDir, decision, time.Now().UTC())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// What is PUBLISHED as current permission must equal the owner's answer.
	published := resultFromSession(repo, taskDir, sess, "")
	if published.Modify != perm.Capability {
		t.Fatalf("F3: PrepareResult.Modify publishes %q as current permission while the sole owner resolves %q",
			published.Modify, perm.Capability)
	}
	// The session's own recorded value is HISTORICAL and must be preserved, not
	// rewritten to look current.
	if sess.MutationCapability != "waiting" {
		t.Fatalf("F3: the historical session value was rewritten to %q; a prepare-time receipt is evidence of what was true then",
			sess.MutationCapability)
	}
}

// F4 (P1) is reproduced in package taskcontrol, at the selector itself
// (see project_next_action_repro_test.go). It cannot be reached through this
// fixture: enrolledPreparedTask leaves an open architect question, and
// selectNextAction returns answer_architect_question before it ever reads the
// permission scalar. That masking is worth recording -- the defect is real in
// code and invisible from this direction.

// F5 (P1). The cache overlay refreshes Permission only; NextAction and
// ReceiptDigestSHA256 still describe the superseded state.
func TestF5_CacheOverlayMustNotLeaveActionAndDigestStale(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	decision := recordAdmissionDecision(t, taskDir, time.Now().UTC())
	rebindActivePointer(t, repo, taskDir)
	if _, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, TaskDir: taskDir}); err != nil {
		t.Skipf("advance-task unavailable: %v", err)
	}
	recordCapabilityConsumption(t, taskDir, decision, time.Now().UTC().Add(time.Minute))
	rebindActivePointer(t, repo, taskDir)

	st, _, err := ControlStatus(repo, taskDir, false)
	if err != nil {
		t.Fatalf("ControlStatus: %v", err)
	}
	if st.Permission.Modify != admission.CapabilityAdmitted &&
		st.NextAction.Kind == taskcontrol.ActionPerformAdmittedEdit {
		t.Fatalf("F5a: permission refreshed to %q but the cached action is still %q -- directing a mutation with no capability",
			st.Permission.Modify, st.NextAction.Kind)
	}
	if want := taskcontrol.StateDigest(st); want != st.ReceiptDigestSHA256 {
		t.Fatalf("F5b: receipt digest %s… does not equal StateDigest %s… of the state actually returned",
			shortOr(st.ReceiptDigestSHA256), shortOr(want))
	}
}

func shortOr(s string) string {
	if len(s) >= 8 {
		return s[:8]
	}
	return "(empty)"
}

var _ = context.Background
var _ = ledger.NewStore
var _ = closureprotocol.PhaseAdmitted

// Constraint 5: a correct selector with incorrect wiring must not survive. This
// asserts tasksession actually passes the governance disposition through, by
// checking the ACTION the public projection returns -- not the selector in
// isolation.
func TestGovernanceDispositionReachesTheSelector(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	decision := recordAdmissionDecision(t, taskDir, time.Now().UTC())
	rebindActivePointer(t, repo, taskDir)

	st, _, err := ControlStatus(repo, taskDir, false)
	if err != nil {
		t.Fatalf("ControlStatus: %v", err)
	}
	if st.Permission.Modify != admission.CapabilityAdmitted {
		t.Fatalf("fixture did not reach an available capability: modify=%q", st.Permission.Modify)
	}
	if st.NextAction.Kind == taskcontrol.ActionPerformAdmittedEdit {
		t.Fatal("the disposition did not reach the selector: an available capability still selects perform_admitted_edit")
	}

	recordCapabilityConsumption(t, taskDir, decision, time.Now().UTC().Add(time.Minute))
	rebindActivePointer(t, repo, taskDir)
	spent, _, err := ControlStatus(repo, taskDir, false)
	if err != nil {
		t.Fatalf("ControlStatus after consumption: %v", err)
	}
	if spent.NextAction.Kind == taskcontrol.ActionRequestMutation {
		t.Fatal("the disposition did not reach the selector: a spent capability still requests another admission")
	}
}

// Constraint 1: action and digest must describe the SAME evaluation as the
// permission, on the cache path.
func TestCachePathReturnsOneCoherentEvaluation(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	decision := recordAdmissionDecision(t, taskDir, time.Now().UTC())
	rebindActivePointer(t, repo, taskDir)
	if _, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, TaskDir: taskDir}); err != nil {
		t.Skipf("advance-task unavailable: %v", err)
	}
	recordCapabilityConsumption(t, taskDir, decision, time.Now().UTC().Add(time.Minute))
	rebindActivePointer(t, repo, taskDir)

	st, _, err := ControlStatus(repo, taskDir, false)
	if err != nil {
		t.Fatalf("ControlStatus: %v", err)
	}
	if want := taskcontrol.StateDigest(st); want != st.ReceiptDigestSHA256 {
		t.Fatalf("digest %s… identifies a state that was not returned (StateDigest %s…)", shortOr(st.ReceiptDigestSHA256), shortOr(want))
	}
	if st.NextAction.Kind == taskcontrol.ActionRequestMutation {
		t.Fatalf("the refreshed cache kept an action describing the superseded permission: %q", st.NextAction.Kind)
	}
}
