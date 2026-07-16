// SPDX-License-Identifier: Apache-2.0

package tasksession

import (
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// governanceState is the admission-v2 disposition folded read-only from a task's
// typed ledger receipts.
type governanceState struct {
	// Phase is the furthest legal admission-v2 phase the recorded receipts prove.
	Phase closureprotocol.TaskPhase
	// Status is the task-session status string projected from Phase.
	Status string
	// ModifyPaths is the exact change envelope. It is non-empty only while a
	// fresh mutation grant is available (admitted, capability not yet consumed).
	ModifyPaths []string
	// GrantModify reports whether mutation permission may be projected. It is
	// true only when an admission decision binds and its single-use capability
	// has not been consumed — never before admission, never after consumption,
	// never after scope verification.
	GrantModify bool
	// Resolved reports whether the task engaged typed governance (an
	// authority_resolved receipt exists). Un-engaged legacy tasks keep their
	// legacy disposition rather than being forced to waiting_governance.
	Resolved bool
	// Terminal reports the Phase-3 terminal state (scope_verified): mutation is
	// closed and the next legal action is result rebuild, owned by a later phase.
	// It is never re-entered as a mutation grant.
	Terminal bool
}

// governanceDisposition folds the recorded admission-v2 receipts into the
// furthest legal task phase. It is a pure ledger reducer: it reads
// authority_resolved -> admission_decided -> admission_consumed ->
// change_observed -> scope_verified and derives the disposition from what was
// recorded. It never calls DecideAdmission — deciding admission is a write-path
// transition (admit-change), not a projection input, so reading a task can never
// mint, refresh, or extend a capability. A recorded decision expires against its
// own CapabilityExpiry; reading it does not extend its validity.
func governanceDisposition(taskDir string, now time.Time) governanceState {
	rec, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		// No authority_resolved receipt: the task never engaged v2 governance.
		return governanceState{Phase: closureprotocol.PhaseWaitingGovernance, Status: StatusWaitingGovernance}
	}

	// Terminal: a scope verification was recorded post-mutation. Scope
	// verification is a non-mutable terminal — no further mutation is granted and
	// no admission is reopened. A failed verification drops to mechanical repair.
	if v, err := admission.LoadRecordedScopeVerification(taskDir); err == nil {
		if admission.ScopeVerified(v) {
			return governanceState{
				Phase:    closureprotocol.PhaseScopeVerified,
				Status:   StatusScopeVerified,
				Resolved: true,
				Terminal: true,
			}
		}
		return governanceState{
			Phase:    closureprotocol.PhaseWaitingMechanicalRepair,
			Status:   StatusWaitingMechanical,
			Resolved: true,
		}
	}

	dec, err := admission.LoadRecordedDecision(taskDir)
	if err != nil {
		// Authority resolved, but no typed admission decision recorded yet. The
		// next legal action is admit-change; no mutation is granted here.
		return governanceState{
			Phase:    closureprotocol.PhaseReadyForAdmission,
			Status:   StatusReadyForAdmission,
			Resolved: true,
		}
	}
	// Validate the recorded decision against the recorded authority WITHOUT
	// recomputing it: a decision whose request digest no longer binds, or whose
	// capability has expired, grants nothing. Reading never extends validity.
	if !recordedDecisionBinds(dec, rec, now) {
		return governanceState{
			Phase:    closureprotocol.PhaseRefused,
			Status:   StatusRefused,
			Resolved: true,
		}
	}

	// A change_observed receipt (recorded between consumption and verification)
	// — none is emitted operationally today, but fold it when present: the
	// mutation is observed but scope is not yet verified, so mutation is closed
	// and the next action is scope verification.
	if hasLedgerEvent(taskDir, closureprotocol.LedgerEventChangeObserved) {
		return governanceState{
			Phase:    closureprotocol.PhaseMutationObserved,
			Status:   StatusMutationObserved,
			Resolved: true,
		}
	}

	// Decision binds and no mutation has been observed yet. The single-use
	// capability governs the mutation grant: once consumed it is spent, so no
	// fresh modify permission is projected even though the task stays admitted
	// awaiting its observed-change receipt. A consumed capability can never
	// reappear as available through a read.
	if _, err := admission.LoadRecordedConsumption(taskDir); err == nil {
		return governanceState{
			Phase:    closureprotocol.PhaseAdmitted,
			Status:   StatusAdmitted,
			Resolved: true,
		}
	}
	return governanceState{
		Phase:       closureprotocol.PhaseAdmitted,
		Status:      StatusReadyForMutation,
		ModifyPaths: changePlanTargets(rec.ChangePlan),
		GrantModify: true,
		Resolved:    true,
	}
}

// recordedDecisionBinds validates a recorded admission decision against the
// recorded authority without recomputing it. The decision's request digest must
// match a request rebuilt from the recorded authority exactly as the admit-change
// writer built it, the capability must not have expired at now, and every
// operation must have been admitted.
func recordedDecisionBinds(dec closureprotocol.AdmissionDecision, rec admission.RecordedAuthority, now time.Time) bool {
	req := closureprotocol.AdmissionRequest{
		ActorBinding:                    rec.Actor,
		BaseBinding:                     rec.Base,
		ChangePlan:                      rec.ChangePlan,
		AuthorityResolutionDigestSHA256: rec.Resolution.AuthorityResolutionDigestSHA256,
		PolicyID:                        strings.TrimSpace(rec.Base.Policies.Admission),
	}
	want, err := closureprotocol.SemanticDigest(req)
	if err != nil || want != strings.TrimSpace(dec.RequestDigestSHA256) {
		return false
	}
	if expiry := strings.TrimSpace(dec.CapabilityExpiry); expiry != "" {
		exp, err := time.Parse(time.RFC3339, expiry)
		if err != nil || !now.Before(exp) {
			return false
		}
	}
	return admission.AllAdmitted(dec)
}

// reconcileGovernedStatus overlays the ledger-derived disposition onto a legacy
// projected status. When typed governance is resolved the ledger is
// authoritative; a task that has not resolved governance must not report a
// mutation grant, so a legacy ready_for_mutation is gated to waiting_governance.
func reconcileGovernedStatus(disp governanceState, legacyStatus string) string {
	if disp.Resolved {
		return disp.Status
	}
	if legacyStatus == StatusReadyForMutation {
		return StatusWaitingGovernance
	}
	return legacyStatus
}

// applyGovernedDisposition overlays the ledger-derived disposition onto a status
// result. At the scope-verified terminal it marks the phase terminal and points
// the next action at the deterministic result rebuild that a later phase owns;
// mutation is never reopened, and no certification or completion is projected.
func applyGovernedDisposition(res *StatusResult, disp governanceState, legacyStatus string) {
	res.Status = reconcileGovernedStatus(disp, legacyStatus)
	if disp.Terminal {
		res.Phase = string(closureprotocol.PhaseScopeVerified)
		res.Next = NextAction{Action: NextRebuildResult, Summary: "scope verified; rebuild and bind the result architecture"}
	}
}

// hasLedgerEvent reports whether the task ledger contains at least one event of
// the given type, read-only.
func hasLedgerEvent(taskDir string, want closureprotocol.LedgerEventType) bool {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(func(et closureprotocol.LedgerEventType, _ string, data []byte) error {
		return ledger.ValidateTaskEventPayload(et, data)
	}))
	chain, err := store.VerifyChain()
	if err != nil {
		return false
	}
	for _, e := range chain.Entries {
		if e.Entry.EventType == want {
			return true
		}
	}
	return false
}

func changePlanTargets(plan closureprotocol.ChangePlan) []string {
	out := make([]string, 0, len(plan.Operations))
	for _, op := range plan.Operations {
		if t := strings.TrimSpace(op.Target); t != "" {
			out = append(out, t)
		}
	}
	return out
}
