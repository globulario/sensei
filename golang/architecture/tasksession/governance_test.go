// SPDX-License-Identifier: Apache-2.0

package tasksession

import (
	"path/filepath"
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

func TestGovernanceReadyForMutationWhenEnrolled(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	disp := governanceDisposition(taskDir, time.Now().UTC())
	if !disp.Resolved || disp.Status != StatusReadyForMutation {
		t.Fatalf("disposition = %+v, want resolved ready_for_mutation", disp)
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

func TestGovernanceWaitsWhenNotEnrolled(t *testing.T) {
	repo, graph := authorityRepo(t) // no enrollment
	res := prepareEdit(t, repo, graph)
	taskDir := filepath.Join(repo, filepath.FromSlash(res.TaskDir))

	disp := governanceDisposition(taskDir, time.Now().UTC())
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
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(func(et closureprotocol.LedgerEventType, mt string, data []byte) error {
		return ledger.ValidateTaskEventPayload(et, data)
	}))
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
	if _, err := ledger.RebuildProjections(taskDir, func(et closureprotocol.LedgerEventType, mt string, data []byte) error {
		return ledger.ValidateTaskEventPayload(et, data)
	}); err != nil {
		t.Fatalf("rebuild projections: %v", err)
	}
}

func hasEventType(t *testing.T, taskDir string, want closureprotocol.LedgerEventType) bool {
	t.Helper()
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(func(et closureprotocol.LedgerEventType, mt string, data []byte) error {
		return ledger.ValidateTaskEventPayload(et, data)
	}))
	chain, err := store.VerifyChain()
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}
	for _, e := range chain.Entries {
		if e.Entry.EventType == want {
			return true
		}
	}
	return false
}

func TestGovernanceScopeVerifiedStaysUncertified(t *testing.T) {
	_, taskDir := enrolledPreparedTask(t)
	recordScopeVerification(t, taskDir, true)

	disp := governanceDisposition(taskDir, time.Now().UTC())
	if disp.Status != StatusReadyForMutation {
		t.Fatalf("scope-verified disposition = %q, want ready_for_mutation", disp.Status)
	}
	// Closure invariant: advancing through scope verification never certifies
	// correctness or completes the task — Phase 6 remains the sole certifier.
	if hasEventType(t, taskDir, closureprotocol.LedgerEventCertified) || hasEventType(t, taskDir, closureprotocol.LedgerEventCompleted) {
		t.Fatal("admission v2 emitted a certified/completed event")
	}
}

func TestGovernanceWaitingMechanicalOnScopeViolation(t *testing.T) {
	_, taskDir := enrolledPreparedTask(t)
	recordScopeVerification(t, taskDir, false)

	disp := governanceDisposition(taskDir, time.Now().UTC())
	if disp.Status != StatusWaitingMechanical {
		t.Fatalf("out-of-envelope disposition = %q, want waiting_mechanical_repair", disp.Status)
	}
}
