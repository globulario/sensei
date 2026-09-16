// SPDX-License-Identifier: AGPL-3.0-only

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
	"github.com/globulario/sensei/golang/architecture/lifecycleaction"
	"github.com/globulario/sensei/golang/architecture/taskcontrol"
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
	// GovernanceCodeConsumptionUnbound is a consumption receipt that decodes but
	// does not belong to this task's current decision. It is an integrity
	// failure, never an ordinary spend: reading it as "consumed" would withhold
	// the legitimate current grant while looking exactly like normal waiting.
	GovernanceCodeConsumptionUnbound = "tasksession.governance_consumption_unbound"
	// GovernanceCodeScopeUnbound is a scope_verification receipt that decodes but
	// does not bind the records a scope verification is ABOUT: the decision it
	// names, the consumption that spent its capability, and the observed change
	// it claims to have verified -- or one that precedes them in the chain.
	GovernanceCodeScopeUnbound = "tasksession.governance_scope_unbound"
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
	return foldGovernance(chain, taskDir, now)
}

// foldGovernance folds the admission-v2 governance from an ALREADY-verified chain
// snapshot. It reads only that snapshot and the immutable, content-addressed
// artifacts its entries reference, so a caller (loadCurrentState) can fold
// governance from the SAME snapshot it used for head/sequence/projection — one
// authoritative world, no second verification. Fail-closed rules are unchanged:
// only absence moves to an earlier phase; any integrity/read error is a typed
// GovernanceError that never grants or suggests mutation.
func foldGovernance(chain ledger.VerifiedChain, taskDir string, now time.Time) (governanceState, error) {
	// Index the single snapshot: the latest verified entry per event type.
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

	// The decision is decoded BEFORE the terminal, because the terminal is about
	// it. Reading the terminal first is what let a scope_verified receipt answer
	// for a task whose decision had never been examined.
	decVE, hasDecision := latest[closureprotocol.LedgerEventAdmissionDecided]
	var dec closureprotocol.AdmissionDecision
	if hasDecision {
		if err := decodeGovernedArtifact(taskDir, decVE, "admission_decision", &dec); err != nil {
			return governanceState{}, err
		}
	}

	// scope_verified is the non-mutable terminal. Present-but-unreadable is a HARD
	// error — never "no terminal", so a corrupt verification cannot reopen the task.
	//
	// A TERMINAL MUST GUARD ITS OWN ENTRY. This branch used to return before any
	// predecessor was examined, so an appended or imported scope_verified event
	// reached the terminal with no admitted decision, no spent capability and no
	// observed change behind it -- and every status and control reader then
	// advertised the result transition for a mutation that never happened.
	if scopeVE, ok := latest[closureprotocol.LedgerEventScopeVerified]; ok {
		var v admission.ScopeVerification
		if err := decodeGovernedArtifact(taskDir, scopeVE, "scope_verification", &v); err != nil {
			return governanceState{}, err
		}
		if err := scopeVerificationBinds(taskDir, latest, authVE, decVE, scopeVE, v, dec, hasDecision, rec); err != nil {
			return governanceState{}, err
		}
		if admission.ScopeVerified(v) {
			return governanceState{Phase: closureprotocol.PhaseScopeVerified, Status: StatusScopeVerified, Resolved: true, Terminal: true}, nil
		}
		return governanceState{Phase: closureprotocol.PhaseWaitingMechanicalRepair, Status: StatusWaitingMechanical, Resolved: true}, nil
	}

	if !hasDecision {
		// Authority resolved, no typed decision yet: the next legal action is
		// admit-change; no mutation is granted here.
		return governanceState{Phase: closureprotocol.PhaseReadyForAdmission, Status: StatusReadyForAdmission, Resolved: true}, nil
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
		// Decoding is not binding. A receipt that parses but belongs to another
		// decision, capability or task is an INTEGRITY FAILURE, not a spend --
		// and the difference is invisible downstream, because both would project
		// the same ordinary "waiting". Validate before believing it.
		if err := consumptionBinds(c, dec, rec); err != nil {
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
	if !decisionBindsRecord(dec, rec) {
		return false
	}
	// TEMPORAL half, deliberately separated. A capability that has run out is not
	// a grant NOW; it does not stop the decision from being the one this task
	// recorded, which is what a later terminal needs to check long after the
	// window has legitimately closed.
	if expiry := strings.TrimSpace(dec.CapabilityExpiry); expiry != "" {
		exp, err := time.Parse(time.RFC3339, expiry)
		if err != nil || !now.Before(exp) {
			return false
		}
	}
	return true
}

// decisionBindsRecord is the TIME-INDEPENDENT half: this decision is the one
// THIS recorded authority asked for, and it adjudicated exactly the plan that
// authority recorded.
func decisionBindsRecord(dec closureprotocol.AdmissionDecision, rec admission.RecordedAuthority) bool {
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
	if !verdictsCoverPlan(dec, rec.ChangePlan) {
		return false
	}
	return admission.AllAdmitted(dec)
}

// verdictsCoverPlan requires the decision's verdicts to correspond EXACTLY to
// the recorded change plan: one verdict per planned operation, no extras, no
// duplicates, nothing unadjudicated.
//
// WHY THE DIGEST IS NOT ENOUGH. request_digest_sha256 binds the decision to the
// request, but OperationVerdicts is a SEPARATE field that digest does not cover.
// A decision carrying a correct (copied) request digest and a verdict list
// replaced by one unrelated admitted operation therefore satisfied AllAdmitted,
// which only iterates the verdicts it is handed -- and the grant that followed
// took its scope from rec.ChangePlan, exposing every target in the plan for
// operations the decision never adjudicated.
//
// The producer already guarantees this correspondence: DecideAdmission emits one
// verdict per plan operation, in plan order (decision_v2.go). Only an appended
// or imported artifact reaches the reader without it.
func verdictsCoverPlan(dec closureprotocol.AdmissionDecision, plan closureprotocol.ChangePlan) bool {
	planned := make(map[string]bool, len(plan.Operations))
	for _, op := range plan.Operations {
		id := strings.TrimSpace(op.OperationID)
		if id == "" || planned[id] {
			// An unidentifiable or duplicated operation cannot be adjudicated,
			// so no verdict set can correspond to this plan.
			return false
		}
		planned[id] = true
	}
	if len(planned) == 0 {
		return false
	}
	adjudicated := make(map[string]bool, len(dec.OperationVerdicts))
	for _, v := range dec.OperationVerdicts {
		id := strings.TrimSpace(v.OperationID)
		if !planned[id] || adjudicated[id] {
			return false
		}
		adjudicated[id] = true
	}
	return len(adjudicated) == len(planned)
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
	}
	// The lifecycle owner decides; this adapter presents. Previously this switch
	// covered two states and left the rest showing whatever the historical
	// session said -- including a stale "perform admitted edit".
	switch a := lifecycleaction.Select(lifecycleDisposition(disp, disp.Resolved)); a {
	case lifecycleaction.ConsumeCapability:
		res.Next = NextAction{Action: NextConsumeCapability, Summary: "run consume-admission to spend the single-use capability for this exact operation set before applying the mutation"}
	case lifecycleaction.VerifyAdmission:
		res.Next = NextAction{Action: NextVerifyAdmission, Summary: "run verify-admission to reconcile and record the consumed operation"}
	case lifecycleaction.VerifyScope:
		res.Next = NextAction{Action: NextVerifyAdmission, Summary: "run verify-admission to verify the scope of the observed change"}
	case lifecycleaction.RecordResultTransition:
		res.Next = NextAction{Action: NextRebuildResult, Summary: "record the result transition, rebuilding and binding the result architecture"}
	case lifecycleaction.MechanicalRepair:
		res.Next = NextAction{Action: NextMechanicalRepair, Summary: "perform the mechanical repair the scope verification requires"}
	case lifecycleaction.DecideAdmission:
		res.Next = NextAction{Action: NextDecideAdmission, Summary: "decide admission for the exact scope"}
	case lifecycleaction.ResolveAuthority:
		res.Next = NextAction{Action: NextResolveAuthority, Summary: "resolve typed authority for this task"}
	case lifecycleaction.None:
		res.Next = NextAction{Action: NextNoLegalAdvance, Summary: "admission refused; no legal advance from this state"}
	case lifecycleaction.Unavailable:
		// Not this owner's task: preserve the established compatibility
		// behaviour, which is whatever this surface already decided.
		return
	default:
		// Blocked. Never preserve the historical action here -- that is how a
		// stale "perform admitted edit" survives a state nothing should act on.
		res.Next = NextAction{Action: NextNoLegalAdvance, Summary: "the governed lifecycle state is inconsistent or unrecognised; no action is safe until it is resolved"}
	}
}

// scopeVerificationBinds checks a decoded scope verification against the records
// a scope verification IS ABOUT, and against their order in the chain.
//
// The PRODUCER cannot mint one without them: admission.VerifyScope takes a
// ScopeExpectation carrying the decision AND the consumption, plus an observed
// change set, and stamps both digests into the receipt it returns. The reader
// accepted any receipt whose Status was valid with an empty violations list --
// admission.ScopeVerified looks at nothing else -- so an appended or imported
// receipt asserted a verified terminal on its own say-so.
//
// Every failure is a GovernanceError, never an earlier phase: a receipt that
// does not bind says the record is untrustworthy, not that the task is younger
// than it claims. Fail-closed rules elsewhere in this fold move to an earlier
// phase only on ABSENCE.
//
// The predecessor set is the one resultrecording already requires to
// reconstruct a result transition (load.go): authority_resolved,
// admission_decided, admission_consumed, change_observed -- in that order.
func scopeVerificationBinds(
	taskDir string,
	latest map[closureprotocol.LedgerEventType]ledger.VerifiedEntry,
	authVE, decVE, scopeVE ledger.VerifiedEntry,
	v admission.ScopeVerification,
	dec closureprotocol.AdmissionDecision,
	hasDecision bool,
	rec admission.RecordedAuthority,
) error {
	fail := func(detail string) error {
		return &GovernanceError{Code: GovernanceCodeScopeUnbound, Detail: detail}
	}
	if !hasDecision {
		return fail("scope_verified is recorded with no admission_decided; nothing was ever admitted for this verification to be about")
	}
	// TIME-INDEPENDENT binding only. The capability has usually expired by the
	// time scope is verified, and that is not evidence against the record.
	if !decisionBindsRecord(dec, rec) {
		return fail("scope_verified names a decision that does not bind this task's recorded authority and change plan")
	}
	decDigest, err := closureprotocol.SemanticDigest(dec)
	if err != nil {
		return fail("the current typed decision has no computable semantic digest: " + err.Error())
	}
	if got := strings.TrimSpace(v.DecisionDigestSHA256); got != decDigest {
		return fail(fmt.Sprintf("scope_verification verifies decision %s, but the current typed decision is %s", short12(got), short12(decDigest)))
	}
	if wantCap := strings.TrimSpace(dec.CapabilityID); wantCap != "" && strings.TrimSpace(v.CapabilityID) != wantCap {
		return fail(fmt.Sprintf("scope_verification names capability %q, but the current decision issued %q", v.CapabilityID, wantCap))
	}

	consVE, ok := latest[closureprotocol.LedgerEventAdmissionConsumed]
	if !ok {
		return fail("scope_verified is recorded with no admission_consumed; a scope cannot be verified for a capability that was never spent")
	}
	var c closureprotocol.CapabilityConsumption
	if err := decodeGovernedArtifact(taskDir, consVE, "capability_consumption", &c); err != nil {
		return err
	}
	if err := consumptionBinds(c, dec, rec); err != nil {
		return err
	}

	obsVE, ok := latest[closureprotocol.LedgerEventChangeObserved]
	if !ok {
		return fail("scope_verified is recorded with no change_observed; there is no observed change for it to have verified")
	}
	var observed admission.ObservedChangeSet
	if err := decodeGovernedArtifact(taskDir, obsVE, "observed_change_set", &observed); err != nil {
		return err
	}
	obsDigest, err := admission.ObservedChangeSetDigest(observed)
	if err != nil {
		return fail("the recorded observed change set has no computable digest: " + err.Error())
	}
	if got := strings.TrimSpace(v.ObservedChangeSetDigestSHA256); got != obsDigest {
		return fail(fmt.Sprintf("scope_verification verifies observed change %s, but this task's recorded observation is %s", short12(got), short12(obsDigest)))
	}

	// ORDER. Each record must have existed when the next one was made. A chain
	// carrying all four in the wrong order describes a verification of something
	// that had not happened yet.
	for _, step := range []struct {
		earlier, later ledger.VerifiedEntry
		detail         string
	}{
		{authVE, decVE, "admission_decided precedes the authority_resolved it rests on"},
		{decVE, consVE, "admission_consumed precedes the admission_decided it spends"},
		{consVE, obsVE, "change_observed precedes the admission_consumed that authorised it"},
		{obsVE, scopeVE, "scope_verified precedes the change_observed it claims to verify"},
	} {
		if step.earlier.Entry.Sequence >= step.later.Entry.Sequence {
			return fail("out-of-order governance chain: " + step.detail)
		}
	}
	return nil
}

// consumptionBinds checks a decoded consumption against the contract it claims
// to satisfy and against the records it claims to bind: the current typed
// decision, and this task and session.
//
// Every failure here is a GovernanceError rather than a disposition, because a
// mismatched receipt says the record is untrustworthy -- not that a capability
// was legitimately spent. Callers must be able to tell "validly consumed" from
// "governance cannot be verified"; collapsing them onto waiting is the defect
// this exists to prevent.
func consumptionBinds(c closureprotocol.CapabilityConsumption, dec closureprotocol.AdmissionDecision, rec admission.RecordedAuthority) error {
	fail := func(detail string) error {
		return &GovernanceError{Code: GovernanceCodeConsumptionUnbound, Detail: detail}
	}
	// CONTRACT: the canonical validator, not a hand-rolled subset of it.
	//
	// Re-listing "the fields that make a consumption a consumption" duplicated
	// part of closureprotocol.ValidateCapabilityConsumption and silently omitted
	// the rest -- an invalid ConsumerActor and a non-valid OneUseStatus both
	// passed, and RecordAdmissionConsumed only serializes its input, so nothing
	// else would have caught them. The canonical validator owns the contract;
	// this function owns only the CROSS-RECORD relations it cannot see.
	if err := closureprotocol.ValidateCapabilityConsumption(c); err != nil {
		return fail("capability_consumption fails its own contract: " + err.Error())
	}
	// Relation: it must bind THIS decision, by the same digest ConsumeCapability
	// stamps (closureprotocol.SemanticDigest of the decision).
	want, digestErr := closureprotocol.SemanticDigest(dec)
	if digestErr != nil {
		return fail("the current typed decision has no computable semantic digest: " + digestErr.Error())
	}
	if got := strings.TrimSpace(c.DecisionDigestSHA256); got != want {
		return fail(fmt.Sprintf("capability_consumption binds decision %s, but the current typed decision is %s", short12(got), short12(want)))
	}
	// Relation: it must spend THIS decision's capability.
	if wantCap := strings.TrimSpace(dec.CapabilityID); wantCap != "" && strings.TrimSpace(c.CapabilityID) != wantCap {
		return fail(fmt.Sprintf("capability_consumption spends capability %q, but the current decision issued %q", c.CapabilityID, wantCap))
	}
	// Relation: the spend must fall inside the capability's OWN validity window.
	//
	// NOT a now() comparison. recordedDecisionBinds already refuses a decision
	// whose capability has expired as of the READER'S clock; this is the
	// different question of whether the receipt claims a spend that happened
	// after the capability it spends had already expired. A reader still inside
	// the window accepted such a receipt as a valid spend, which withheld the
	// legitimate grant and selected verification instead -- so the defect is
	// invisible exactly while the capability still looks live.
	//
	// admission.ConsumeCapability refuses to CREATE one (capability.go:37-44).
	// ValidateCapabilityConsumption checks only that consumed_at PARSES; the
	// relation between the two records is not its to see. A receipt appended or
	// imported by any other route reaches here unexamined.
	if expiry := strings.TrimSpace(dec.CapabilityExpiry); expiry != "" {
		exp, err := time.Parse(time.RFC3339, expiry)
		if err != nil {
			return fail("the current decision's capability_expiry is not RFC3339: " + err.Error())
		}
		spent, err := time.Parse(time.RFC3339, strings.TrimSpace(c.ConsumedAt))
		if err != nil {
			return fail("capability_consumption consumed_at is not RFC3339: " + err.Error())
		}
		if spent.After(exp) {
			return fail(fmt.Sprintf("capability_consumption records a spend at %s, after the capability it spends expired at %s", c.ConsumedAt, expiry))
		}
	}
	// Relation: it must bind THIS task and session.
	if c.Task.ID != rec.Base.Task.ID {
		return fail(fmt.Sprintf("capability_consumption binds task %q, but this task is %q", c.Task.ID, rec.Base.Task.ID))
	}
	if strings.TrimSpace(rec.Base.Task.SessionID) != "" && c.Task.SessionID != rec.Base.Task.SessionID {
		return fail(fmt.Sprintf("capability_consumption binds session %q, but this task's session is %q", c.Task.SessionID, rec.Base.Task.SessionID))
	}
	// Relation: it may spend ONLY operations this decision admitted.
	//
	// admission.ConsumeCapability already refuses to CREATE such a receipt
	// (capability.go:47-55, via admittedOperationSet). A receipt appended or
	// imported by any other route bypasses that producer, and without this the
	// reader believes it.
	//
	// SUBSET, not coverage: a consumption spends a NON-EMPTY subset of the
	// admitted operations and need not spend all of them. Only operation
	// IDENTITY is checked; matching targets to actual changes needs ChangePlan
	// resolution and belongs to the application bridge.
	//
	// EMPTINESS IS CHECKED EXPLICITLY. The membership loop below runs zero times
	// for an empty set, so "every consumed operation was admitted" is vacuously
	// true of a receipt that spends nothing -- and ValidateCapabilityConsumption
	// does not look at operation IDs at all. A receipt claiming a capability was
	// spent on no operation is not a spend.
	consumed := closureprotocol.NormalizeSet(c.ConsumedOperationIDs)
	if len(consumed) == 0 {
		return fail("capability_consumption spends no operation; a receipt that spends nothing is not a consumption")
	}
	admitted := map[string]bool{}
	for _, v := range dec.OperationVerdicts {
		if v.Verdict == admission.AdmissionVerdictAdmitted {
			admitted[v.OperationID] = true
		}
	}
	// NO len(admitted) > 0 GUARD. Skipping the check when the decision admits
	// nothing would accept any operation against a decision that admitted none.
	for _, op := range consumed {
		if !admitted[op] {
			return fail(fmt.Sprintf("capability_consumption spends operation %q, which the current decision does not admit", op))
		}
	}
	return nil
}

func short12(s string) string {
	if len(s) >= 12 {
		return s[:12]
	}
	if s == "" {
		return "(none)"
	}
	return s
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

// lifecycleDisposition converts this package's governance fold into the value
// the lifecycle owner decides from. It is the ONLY conversion: three readers
// each deriving their own action from governanceState is the defect this
// removes.
//
// typedProtocol is supplied BY THE CALLER, per the caller-context ruling, and is
// never inferred here:
//
//   - An explicitly typed entry point, whose own contract establishes the
//     protocol, may assert true before authority exists -- that is how
//     typed-but-awaiting-authority reaches ResolveAuthority.
//   - A MIXED surface (Status, control status) must pass gov.Resolved. Status
//     consults this fold for file-protocol sessions too, so calling it proves
//     nothing about the protocol; only verified typed authority does.
//   - A mixed surface with no typed authority therefore yields Unavailable, and
//     the adapter preserves its established compatibility behaviour. THE
//     AMBIGUITY IS RECORDED, NOT RESOLVED: a legacy file-protocol task and a
//     typed task awaiting authority are indistinguishable without durable
//     protocol identity, and an action-selection repair must not introduce a
//     protocol migration by guessing between them.
//   - Unreadable or invalid authority never reaches here: foldGovernance returns
//     a GovernanceError and the caller propagates it.
func lifecycleDisposition(gov governanceState, typedProtocol bool) lifecycleaction.Disposition {
	return lifecycleaction.Disposition{
		TypedProtocol:     typedProtocol,
		AuthorityResolved: gov.Resolved,
		Phase:             gov.Phase,
		Status:            gov.Status,
		// BOTH, not either. foldGovernance sets GrantModify and
		// StatusReadyForMutation at exactly ONE site, together, so production
		// cannot produce one without the other. An OR would let a status label
		// override a false grant.
		CapabilityAvailable: gov.GrantModify && gov.Status == StatusReadyForMutation,
		// The combination production cannot produce. A fixture setting only one
		// of them is incomplete -- not evidence that the combination is legal --
		// and the owner refuses rather than choosing which field to believe.
		GrantInconsistent: gov.GrantModify != (gov.Status == StatusReadyForMutation),
		// NOT masked by gov.Resolved. Anding authority into the observation
		// erased the downstream fact before Select could reject it: a chain
		// reporting StatusAdmitted with authority absent yielded zero positions
		// and selected resolve_authority, discarding the consumption entirely.
		// The conversion reports what the fold observed; Select decides whether
		// it is coherent with authority.
		CapabilityConsumed:       gov.Status == StatusAdmitted,
		ScopeVerified:            gov.Terminal,
		ChangeObserved:           gov.Status == StatusMutationObserved,
		MechanicalRepairRequired: gov.Status == StatusWaitingMechanical,
		ReadyForAdmission:        gov.Status == StatusReadyForAdmission,
		Refused:                  gov.Status == StatusRefused,
	}
}

// MutationPermission is what every public projection of a task's mutation
// permission must be derived from. It exists because three surfaces used to
// answer the question independently -- AdvanceTask from the ledger reducer,
// projectControlStatusAndClosure and BuildTaskBriefing from the admission
// decision FILE -- and disagreed with each other on a healthy binding in both
// directions: reporting a grant the ledger withheld, and withholding one the
// ledger had granted.
//
// WHAT permission.modify MEANS, exactly: permission to CONSUME A NEW mutation
// capability. It is not permission to apply an operation whose capability was
// already consumed. After consumption the answer is "no new capability", which
// is not the same statement as "the application you already made was
// unauthorized", and the two must not be collapsed onto Refused.
type MutationPermission struct {
	// Capability is the projected permission.modify value.
	Capability string
	// Scope is the exact modify envelope the capability covers. It is non-empty
	// only while a fresh grant is available.
	Scope []string
	// LedgerDerived reports that a typed governed chain decided this, rather
	// than the file protocol. It is what the protocol boundary turns on.
	LedgerDerived bool
	// GovernedMutation is this same fold expressed for taskcontrol's selector, so
	// permission and next action can never be computed from different folds.
	GovernedMutation taskcontrol.GovernedMutationDisposition
	// Disposition is the governance fold this permission was derived from. It is
	// carried so a caller that also needs the disposition reuses THIS fold rather
	// than folding a second time -- one authoritative world per call, which is
	// the same property foldGovernance's own snapshot comment protects.
	Disposition governanceState
}

// resolveMutationPermission is the SOLE owner of the mutation-permission
// predicate. Every public projection routes through it; none re-derives it.
//
// THE PROTOCOL BOUNDARY. A task whose chain has resolved typed authority is
// governed by the ledger, and the decision file may not add to what the reducer
// grants. A task that has not is on the file protocol, whose established
// behaviour is preserved -- with the one rule AdvanceTask already applied: the
// legacy path may not hand out a positive mutation capability on its own.
//
// FAIL CLOSED. A governance-integrity error is never "no governance"; it grants
// nothing. Absence of a readable governed record and absence of governance are
// different facts and only the second is a legitimate file-protocol task.
func resolveMutationPermission(taskDir string, decision admission.Decision, now time.Time) (MutationPermission, error) {
	gov, err := governanceDisposition(taskDir, now, nil)
	if err != nil {
		return MutationPermission{Capability: admission.CapabilityWaiting, LedgerDerived: true}, err
	}
	if gov.Resolved {
		// The single-use grant is projected only while a typed decision binds and
		// its capability is unconsumed. After consumption or scope verification
		// the reducer withholds it, and no read may reopen it.
		if gov.GrantModify {
			return MutationPermission{
				Capability:       admission.CapabilityAdmitted,
				Scope:            append([]string{}, gov.ModifyPaths...),
				LedgerDerived:    true,
				Disposition:      gov,
				GovernedMutation: taskcontrol.GovernedMutationDisposition{Governed: true, CapabilityAvailable: true, Disposition: lifecycleDisposition(gov, gov.Resolved)},
			}, nil
		}
		// No NEW capability. Deliberately not Refused: a consumed capability is
		// spent, not repudiated.
		return MutationPermission{
			Capability:       admission.CapabilityWaiting,
			LedgerDerived:    true,
			Disposition:      gov,
			GovernedMutation: taskcontrol.GovernedMutationDisposition{Governed: true, CapabilityConsumed: gov.Status == StatusAdmitted, Disposition: lifecycleDisposition(gov, gov.Resolved)},
		}, nil
	}
	capability := decision.MutationCapability
	if capability == admission.CapabilityAdmitted || capability == admission.CapabilityAdmittedWithConditions {
		capability = admission.CapabilityWaiting
	}
	return MutationPermission{
		Capability:  capability,
		Scope:       append([]string{}, decision.Envelope.ModifyPaths...),
		Disposition: gov,
	}, nil
}
