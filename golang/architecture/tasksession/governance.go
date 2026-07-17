// SPDX-License-Identifier: Apache-2.0

package tasksession

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// GovernanceError is a typed governance-integrity failure: recorded history that
// is present but unreadable, malformed, or drifted from its recorded digest. It is
// never absence, so it must never be treated as "no record" and must never grant
// or suggest mutation.
type GovernanceError struct {
	Code   string
	Detail string
}

func (e *GovernanceError) Error() string { return e.Code + ": " + e.Detail }

// Stable governance-integrity error codes.
const (
	GovernanceCodeChainUnverifiable = "tasksession.governance_chain_unverifiable"
	GovernanceCodeRecordUnreadable  = "tasksession.governance_record_unreadable"
	GovernanceCodeArtifactDrifted   = "tasksession.governance_artifact_drifted"
)

func governanceValidator(et closureprotocol.LedgerEventType, _ string, data []byte) error {
	return ledger.ValidateTaskEventPayload(et, data)
}

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

// governanceDisposition folds the recorded admission-v2 receipts into the furthest
// legal task phase from ONE verified-chain snapshot, failing closed. Every event
// payload is read through ledger.ReadVerifiedPayload and every referenced artifact
// is revalidated against its recorded digest before decoding, so the reducer can
// never mix on-disk worlds nor trust drifted bytes. Only genuine ABSENCE of an
// event may move the task to an earlier phase; any integrity or read error is a
// typed GovernanceError that never grants or suggests mutation. It never calls
// DecideAdmission — reading a task can never mint, refresh, or extend a capability;
// a recorded decision expires against its own CapabilityExpiry.
//
// afterSnapshot is an injected hook (nil in production) fired once, immediately
// after the single verified-chain snapshot is taken and before any record is
// decoded, so a test can append to the on-disk ledger and prove the reduction
// reads only the frozen snapshot. It is passed per call, never a process global.
func governanceDisposition(taskDir string, now time.Time, afterSnapshot func(taskDir string)) (governanceState, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(governanceValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return governanceState{}, &GovernanceError{Code: GovernanceCodeChainUnverifiable, Detail: err.Error()}
	}
	if afterSnapshot != nil {
		afterSnapshot(taskDir)
	}
	// Index the single snapshot: the latest verified entry per event type. Every
	// decode below reads only from this frozen snapshot and the immutable,
	// content-addressed artifacts its entries reference.
	latest := map[closureprotocol.LedgerEventType]ledger.VerifiedEntry{}
	for _, ve := range chain.Entries {
		latest[ve.Entry.EventType] = ve
	}

	// authority_resolved ABSENT → the task never engaged typed governance.
	authVE, ok := latest[closureprotocol.LedgerEventAuthorityResolved]
	if !ok {
		return governanceState{Phase: closureprotocol.PhaseWaitingGovernance, Status: StatusWaitingGovernance}, nil
	}
	var rec admission.RecordedAuthority
	if err := decodeGovernedArtifact(taskDir, authVE, "authority_resolution", &rec.Resolution); err != nil {
		return governanceState{}, err
	}
	if err := decodeGovernedArtifact(taskDir, authVE, "actor_binding", &rec.Actor); err != nil {
		return governanceState{}, err
	}
	if err := decodeGovernedArtifact(taskDir, authVE, "change_plan", &rec.ChangePlan); err != nil {
		return governanceState{}, err
	}
	if err := decodeGovernedArtifact(taskDir, authVE, "base_binding", &rec.Base); err != nil {
		return governanceState{}, err
	}

	// scope_verified is the non-mutable terminal. Present-but-unreadable is a HARD
	// error — never "no terminal", so a corrupt verification cannot reopen the task.
	if scopeVE, ok := latest[closureprotocol.LedgerEventScopeVerified]; ok {
		var v admission.ScopeVerification
		if err := decodeGovernedArtifact(taskDir, scopeVE, "scope_verification", &v); err != nil {
			return governanceState{}, err
		}
		if admission.ScopeVerified(v) {
			return governanceState{Phase: closureprotocol.PhaseScopeVerified, Status: StatusScopeVerified, Resolved: true, Terminal: true}, nil
		}
		return governanceState{Phase: closureprotocol.PhaseWaitingMechanicalRepair, Status: StatusWaitingMechanical, Resolved: true}, nil
	}

	decVE, ok := latest[closureprotocol.LedgerEventAdmissionDecided]
	if !ok {
		// Authority resolved, no typed decision yet: the next legal action is
		// admit-change; no mutation is granted here.
		return governanceState{Phase: closureprotocol.PhaseReadyForAdmission, Status: StatusReadyForAdmission, Resolved: true}, nil
	}
	var dec closureprotocol.AdmissionDecision
	if err := decodeGovernedArtifact(taskDir, decVE, "admission_decision", &dec); err != nil {
		return governanceState{}, err
	}
	if !recordedDecisionBinds(dec, rec, now) {
		return governanceState{Phase: closureprotocol.PhaseRefused, Status: StatusRefused, Resolved: true}, nil
	}

	// change_observed present: the mutation is observed but scope is not yet
	// verified, so mutation is closed and the next action is scope verification.
	if _, ok := latest[closureprotocol.LedgerEventChangeObserved]; ok {
		return governanceState{Phase: closureprotocol.PhaseMutationObserved, Status: StatusMutationObserved, Resolved: true}, nil
	}

	// A recorded consumption spends the single-use capability. Present-but-
	// unreadable is a HARD error: it must NEVER be mistaken for "unconsumed" and
	// resurrect a mutation grant.
	if consVE, ok := latest[closureprotocol.LedgerEventAdmissionConsumed]; ok {
		var c closureprotocol.CapabilityConsumption
		if err := decodeGovernedArtifact(taskDir, consVE, "capability_consumption", &c); err != nil {
			return governanceState{}, err
		}
		return governanceState{Phase: closureprotocol.PhaseAdmitted, Status: StatusAdmitted, Resolved: true}, nil
	}
	return governanceState{
		Phase:       closureprotocol.PhaseAdmitted,
		Status:      StatusReadyForMutation,
		ModifyPaths: changePlanTargets(rec.ChangePlan),
		GrantModify: true,
		Resolved:    true,
	}, nil
}

// decodeGovernedArtifact reads a verified entry's payload (revalidated against the
// entry digest) from the frozen snapshot, then reads the referenced artifact and
// recomputes its byte digest against the recorded ref before decoding. A present-
// but-corrupt record is a typed GovernanceError, never silent absence.
func decodeGovernedArtifact(taskDir string, ve ledger.VerifiedEntry, key string, out any) error {
	data, err := ledger.ReadVerifiedPayload(ve)
	if err != nil {
		return &GovernanceError{Code: GovernanceCodeRecordUnreadable, Detail: fmt.Sprintf("%s payload: %v", ve.Entry.EventType, err)}
	}
	payload, err := ledger.ParseTaskEventPayload(data)
	if err != nil {
		return &GovernanceError{Code: GovernanceCodeRecordUnreadable, Detail: fmt.Sprintf("%s payload parse: %v", ve.Entry.EventType, err)}
	}
	ref, ok := payload.Artifacts[key]
	if !ok {
		return &GovernanceError{Code: GovernanceCodeRecordUnreadable, Detail: fmt.Sprintf("%s event has no artifact %q", ve.Entry.EventType, key)}
	}
	raw, err := os.ReadFile(filepath.Join(taskDir, filepath.FromSlash(ref.Path)))
	if err != nil {
		return &GovernanceError{Code: GovernanceCodeArtifactDrifted, Detail: fmt.Sprintf("%s artifact %q: %v", ve.Entry.EventType, key, err)}
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != ref.DigestSHA256 {
		return &GovernanceError{Code: GovernanceCodeArtifactDrifted, Detail: fmt.Sprintf("%s artifact %q digest does not match its recorded ref", ve.Entry.EventType, key)}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &GovernanceError{Code: GovernanceCodeRecordUnreadable, Detail: fmt.Sprintf("%s artifact %q decode: %v", ve.Entry.EventType, key, err)}
	}
	return nil
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
		return
	}
	if !disp.Resolved {
		return
	}
	// Surface the single next legal command in the explicit admission-v2
	// workflow: at ready_for_mutation the capability is consumed BEFORE the
	// mutation, then the mutation is applied and verified. Consumption is never
	// hidden inside verification.
	switch disp.Status {
	case StatusReadyForMutation:
		res.Next = NextAction{Action: NextConsumeCapability, Summary: "run consume-admission to spend the single-use capability for this exact operation set before applying the mutation"}
	case StatusAdmitted:
		res.Next = NextAction{Action: NextVerifyAdmission, Summary: "apply the admitted mutation, then run verify-admission to record the observed change and verify scope"}
	}
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
