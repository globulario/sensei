// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

// All eight lifecycle states, through all THREE adapters. Before the owner
// existed, two of the three agreed on two of the eight; scope_verified drew
// three different answers.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/lifecycleaction"
	"github.com/globulario/sensei/golang/architecture/taskcontrol"
	"gopkg.in/yaml.v3"
)

func eightStates() []struct {
	name string
	gov  governanceState
	want lifecycleaction.Action
} {
	return []struct {
		name string
		gov  governanceState
		want lifecycleaction.Action
	}{
		{"waiting_governance", governanceState{Phase: "waiting_governance", Status: StatusWaitingGovernance}, lifecycleaction.ResolveAuthority},
		{"ready_for_admission", governanceState{Phase: "ready_for_admission", Status: StatusReadyForAdmission, Resolved: true}, lifecycleaction.DecideAdmission},
		{"refused", governanceState{Phase: "refused", Status: StatusRefused, Resolved: true}, lifecycleaction.None},
		{"waiting_mechanical_repair", governanceState{Phase: "waiting_mechanical_repair", Status: StatusWaitingMechanical, Resolved: true}, lifecycleaction.MechanicalRepair},
		{"capability available", governanceState{Phase: "admitted", Status: StatusReadyForMutation, Resolved: true, GrantModify: true}, lifecycleaction.ConsumeCapability},
		{"capability consumed", governanceState{Phase: "admitted", Status: StatusAdmitted, Resolved: true}, lifecycleaction.VerifyAdmission},
		{"mutation_observed", governanceState{Phase: "mutation_observed", Status: StatusMutationObserved, Resolved: true}, lifecycleaction.VerifyScope},
		{"scope_verified", governanceState{Phase: "scope_verified", Status: StatusScopeVerified, Resolved: true, Terminal: true}, lifecycleaction.RecordResultTransition},
	}
}

// The single conversion must produce the ratified action for every state.
func TestLifecycleConversionCoversEveryState(t *testing.T) {
	for _, tc := range eightStates() {
		t.Run(tc.name, func(t *testing.T) {
			if got := lifecycleaction.Select(lifecycleDisposition(tc.gov, true)); got != tc.want {
				t.Fatalf("%s: owner selected %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// ADAPTER 1 -- applyGovernedDisposition (Status). No state may keep a stale
// session action, and none may instruct an edit.
func TestAdapter1StatusPublishesTheActualOperation(t *testing.T) {
	want := map[string]string{
		"ready_for_admission":       NextDecideAdmission,
		"refused":                   NextNoLegalAdvance,
		"waiting_mechanical_repair": NextMechanicalRepair,
		"capability available":      NextConsumeCapability,
		"capability consumed":       NextVerifyAdmission,
		"mutation_observed":         NextVerifyAdmission,
		"scope_verified":            NextRebuildResult,
	}
	for _, tc := range eightStates() {
		if tc.name == "waiting_governance" {
			continue // not this owner's task on a mixed surface
		}
		t.Run(tc.name, func(t *testing.T) {
			res := StatusResult{Status: "x", Next: NextAction{Action: NextPerformEdit}}
			applyGovernedDisposition(&res, tc.gov, "x")
			if res.Next.Action != want[tc.name] {
				t.Fatalf("%s published Action=%q, want %q -- an action that does not name the operation is not a faithful presentation",
					tc.name, res.Next.Action, want[tc.name])
			}
		})
	}
}

// Refused and blocked must publish an explicitly NON-ADVANCING action, not a
// plausible-looking instruction such as "advance one convergence iteration".
func TestRefusedAndBlockedPublishANonAdvancingAction(t *testing.T) {
	refused := StatusResult{Status: "x", Next: NextAction{Action: NextPerformEdit}}
	applyGovernedDisposition(&refused, governanceState{Phase: closureprotocol.PhaseRefused, Status: StatusRefused, Resolved: true}, "x")
	if refused.Next.Action != NextNoLegalAdvance {
		t.Fatalf("refused published %q, want %q", refused.Next.Action, NextNoLegalAdvance)
	}
	blocked := StatusResult{Status: "x", Next: NextAction{Action: NextPerformEdit}}
	applyGovernedDisposition(&blocked, governanceState{Status: "unexpected", Resolved: true}, "x")
	if blocked.Next.Action != NextNoLegalAdvance {
		t.Fatalf("an unrecognised typed state published %q, want %q", blocked.Next.Action, NextNoLegalAdvance)
	}
}

func TestAdapter1StatusNeverKeepsAStaleMutationInstruction(t *testing.T) {
	for _, tc := range eightStates() {
		if tc.name == "waiting_governance" {
			// Not governed by this owner on a mixed surface: the ruling PERMITS
			// preserving the established compatibility behaviour here, so a
			// surviving session action is correct rather than stale.
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			res := StatusResult{Status: "x", Next: NextAction{Action: NextPerformEdit}}
			applyGovernedDisposition(&res, tc.gov, "x")
			if res.Next.Action == NextPerformEdit {
				t.Fatalf("%s left the stale %q instruction in place", tc.name, NextPerformEdit)
			}
		})
	}
}

// ADAPTER 2 -- taskcontrol.selectNextAction. No state may reopen admission
// after mutation, and none may reach completion without positive evidence.
func TestAdapter2TaskControlCoversEveryState(t *testing.T) {
	want := map[string]string{
		"waiting_governance":        taskcontrol.ActionResolveAuthority,
		"ready_for_admission":       taskcontrol.ActionDecideAdmission,
		"refused":                   taskcontrol.ActionNone,
		"waiting_mechanical_repair": taskcontrol.ActionMechanicalRepair,
		"capability available":      taskcontrol.ActionConsumeCapability,
		"capability consumed":       taskcontrol.ActionVerifyAdmission,
		"mutation_observed":         taskcontrol.ActionVerifyAdmission,
		"scope_verified":            taskcontrol.ActionRecordResultTransition,
	}
	for _, tc := range eightStates() {
		t.Run(tc.name, func(t *testing.T) {
			var st taskcontrol.TaskControlState
			st.BindingHealth = "current"
			st.Permission = taskcontrol.PermissionSummary{Modify: "waiting"}
			gm := taskcontrol.GovernedMutationDisposition{Governed: true, Disposition: lifecycleDisposition(tc.gov, true)}
			got := taskcontrol.SelectNextActionFor(st, gm)
			if got.Kind != want[tc.name] {
				t.Fatalf("%s selected %q, want %q", tc.name, got.Kind, want[tc.name])
			}
			if got.Kind == taskcontrol.ActionCompleteTask {
				t.Fatalf("%s reached completion without positive completion evidence", tc.name)
			}
			if tc.name == "mutation_observed" || tc.name == "scope_verified" {
				if got.Kind == taskcontrol.ActionRequestMutation || got.Kind == taskcontrol.ActionDecideAdmission {
					t.Fatalf("%s reopened admission after mutation (%q)", tc.name, got.Kind)
				}
			}
		})
	}
}

// ADAPTER 3 -- dispositionNextAction (advance-result).
func TestAdapter3AdvanceResultCoversEveryState(t *testing.T) {
	want := map[string]string{
		"waiting_governance":        AdvanceNextResolveAuthority,
		"ready_for_admission":       AdvanceNextDecideAdmission,
		"refused":                   AdvanceNextNone,
		"waiting_mechanical_repair": AdvanceNextMechanicalRepair,
		"capability available":      AdvanceNextConsumeCapability,
		"capability consumed":       AdvanceNextVerifyAdmission,
		"mutation_observed":         AdvanceNextVerifyScope,
		"scope_verified":            AdvanceNextRecordTransition,
	}
	for _, tc := range eightStates() {
		t.Run(tc.name, func(t *testing.T) {
			got := dispositionNextAction(tc.gov)
			if got.Action != want[tc.name] {
				t.Fatalf("%s selected %q, want %q", tc.name, got.Action, want[tc.name])
			}
			if got.Action == AdvanceNextPerformMutation {
				t.Fatalf("%s instructed an unconditional mutation", tc.name)
			}
		})
	}
}

// The conversion must require BOTH grant signals. Production sets them at one
// site together; an OR would let a status label override a false grant.
func TestConversionRequiresBothGrantSignals(t *testing.T) {
	both := lifecycleDisposition(governanceState{Status: StatusReadyForMutation, Resolved: true, GrantModify: true}, true)
	if !both.CapabilityAvailable || both.GrantInconsistent {
		t.Fatalf("the production combination was not read as an available capability: %+v", both)
	}
	statusOnly := lifecycleDisposition(governanceState{Status: StatusReadyForMutation, Resolved: true}, true)
	if statusOnly.CapabilityAvailable {
		t.Fatal("a status label alone was read as an available capability, overriding a false grant")
	}
	if !statusOnly.GrantInconsistent {
		t.Fatal("a status/grant disagreement was not reported as inconsistent")
	}
	grantOnly := lifecycleDisposition(governanceState{Status: StatusAdmitted, Resolved: true, GrantModify: true}, true)
	if grantOnly.CapabilityAvailable || !grantOnly.GrantInconsistent {
		t.Fatal("a grant without its status was not reported as inconsistent")
	}
}

// The caller-context ruling, pinned. A MIXED surface may not claim typed
// protocol without verified typed authority; it must preserve the established
// compatibility behaviour and leave the ambiguity recorded rather than guessing
// between a legacy file-protocol task and a typed task awaiting authority.
func TestMixedSurfaceCannotClaimTypedWithoutAuthority(t *testing.T) {
	unresolved := governanceState{Phase: "waiting_governance", Status: StatusWaitingGovernance}

	mixed := lifecycleDisposition(unresolved, unresolved.Resolved)
	if mixed.TypedProtocol {
		t.Fatal("a mixed surface claimed typed protocol with no verified typed authority")
	}
	if got := lifecycleaction.Select(mixed); got != lifecycleaction.Unavailable {
		t.Fatalf("a mixed surface with no typed authority selected %q, want %q so the adapter keeps its compatibility behaviour", got, lifecycleaction.Unavailable)
	}

	// An explicitly typed entry point, whose contract establishes the protocol,
	// may assert it before authority exists.
	typed := lifecycleDisposition(unresolved, true)
	if got := lifecycleaction.Select(typed); got != lifecycleaction.ResolveAuthority {
		t.Fatalf("an explicitly typed entry point selected %q, want %q", got, lifecycleaction.ResolveAuthority)
	}
}

// Inconsistent grant inputs must be rejected BEFORE any advancing action can be
// selected -- including for a caller that asserts the typed protocol.
func TestInconsistentGrantIsRejectedBeforeAnyAdvance(t *testing.T) {
	d := lifecycleDisposition(governanceState{Phase: closureprotocol.PhaseAdmitted, Status: StatusReadyForMutation, Resolved: true}, true)
	got := lifecycleaction.Select(d)
	if got.Advances() {
		t.Fatalf("inconsistent grant inputs selected an advancing action (%q)", got)
	}
	if got != lifecycleaction.Blocked {
		t.Fatalf("inconsistent grant inputs selected %q, want %q", got, lifecycleaction.Blocked)
	}
}

// THE CRITICAL BOUNDARY. Unavailable and Blocked must not be treated alike.
//
// An adapter that checks only "does this action advance?" and otherwise keeps
// what it had will restore a historical instruction -- including a stale
// "perform admitted edit" -- for a typed state that is exactly the one nothing
// should be done in. Driven through the PUBLIC adapter with a historical
// perform-edit action in place, because the owner's own fail-closed test cannot
// see this.
func TestInconsistentTypedStateNeverRecoversAHistoricalMutationInstruction(t *testing.T) {
	// GrantModify unset beside StatusReadyForMutation: contradictory, and
	// production cannot produce it.
	inconsistent := governanceState{Phase: closureprotocol.PhaseAdmitted, Status: StatusReadyForMutation, Resolved: true}
	if got := lifecycleaction.Select(lifecycleDisposition(inconsistent, true)); got != lifecycleaction.Blocked {
		t.Fatalf("inconsistent typed state selected %q, want %q", got, lifecycleaction.Blocked)
	}
	res := StatusResult{Status: "x", Next: NextAction{Action: NextPerformEdit}}
	applyGovernedDisposition(&res, inconsistent, "x")
	if res.Next.Action == NextPerformEdit {
		t.Fatal("an inconsistent typed state recovered the historical perform-edit instruction")
	}
}

// The ruling's two cases, side by side through the PUBLIC adapter, each with a
// historical perform-edit in place:
//
//   - BLOCKED (typed, unsafe): must never recover the historical instruction.
//   - UNAVAILABLE (not this owner's task): MAY preserve it -- that is the
//     established compatibility behaviour, and the ambiguity is recorded rather
//     than resolved by guessing.
func TestBlockedNeverRecoversWhileUnavailablePreserves(t *testing.T) {
	blocked := StatusResult{Status: "x", Next: NextAction{Action: NextPerformEdit}}
	applyGovernedDisposition(&blocked, governanceState{Phase: closureprotocol.PhaseAdmitted, Status: StatusReadyForMutation, Resolved: true}, "x")
	if blocked.Next.Action == NextPerformEdit {
		t.Fatal("a BLOCKED typed state recovered the historical perform-edit instruction")
	}

	preserved := StatusResult{Status: "x", Next: NextAction{Action: NextPerformEdit}}
	applyGovernedDisposition(&preserved, governanceState{Phase: closureprotocol.PhaseWaitingGovernance, Status: StatusWaitingGovernance}, "x")
	if preserved.Next.Action != NextPerformEdit {
		t.Fatalf("a task this owner does not govern had its compatibility behaviour overridden: Next=%q", preserved.Next.Action)
	}
}

// Compatibility is preserved only where the ruling allows it: a surface this
// owner does not govern keeps its own decision.
func TestUnavailablePreservesCompatibilityWithoutAsserting(t *testing.T) {
	notOurs := lifecycleaction.Disposition{TypedProtocol: false, Status: StatusWaitingGovernance}
	if got := lifecycleaction.Select(notOurs); got != lifecycleaction.Unavailable {
		t.Fatalf("a task this owner does not govern selected %q, want %q", got, lifecycleaction.Unavailable)
	}
	if lifecycleaction.Unavailable == lifecycleaction.Blocked {
		t.Fatal("Unavailable and Blocked are the same value; the two cases cannot be distinguished")
	}
}

// The four added Next.Action values are plain strings on a public struct, so
// they must survive JSON and YAML unchanged. Audited at this revision: no
// non-test consumer compares Next.Action against any constant -- the only
// comparisons are the assignments themselves -- so nothing branches on a value
// these additions could surprise.
func TestAddedNextActionsRoundTripInJSONAndYAML(t *testing.T) {
	for _, action := range []string{
		NextResolveAuthority, NextDecideAdmission, NextMechanicalRepair, NextNoLegalAdvance,
	} {
		t.Run(action, func(t *testing.T) {
			in := NextAction{Action: action, Summary: "s"}
			jb, err := json.Marshal(in)
			if err != nil {
				t.Fatalf("json marshal: %v", err)
			}
			if !strings.Contains(string(jb), action) {
				t.Fatalf("json dropped %q: %s", action, jb)
			}
			var jback NextAction
			if err := json.Unmarshal(jb, &jback); err != nil || jback.Action != action {
				t.Fatalf("json round trip gave %q (err %v)", jback.Action, err)
			}
			yb, err := yaml.Marshal(in)
			if err != nil {
				t.Fatalf("yaml marshal: %v", err)
			}
			if !strings.Contains(string(yb), action) {
				t.Fatalf("yaml dropped %q: %s", action, yb)
			}
			var yback NextAction
			if err := yaml.Unmarshal(yb, &yback); err != nil || yback.Action != action {
				t.Fatalf("yaml round trip gave %q (err %v)", yback.Action, err)
			}
		})
	}
}

// The owner distinguishes CONSUMED (reconcile and record) from CHANGE OBSERVED
// (verify the scope). Both may invoke verify-admission, but the consumed case
// must not claim a change has already been observed. Action AND summary are
// asserted together, on all three adapters.
func TestConsumedAndObservedStayDistinctInEveryAdapter(t *testing.T) {
	consumed := governanceState{Phase: "admitted", Status: StatusAdmitted, Resolved: true}
	observed := governanceState{Phase: "mutation_observed", Status: StatusMutationObserved, Resolved: true}

	if a := lifecycleaction.Select(lifecycleDisposition(consumed, true)); a != lifecycleaction.VerifyAdmission {
		t.Fatalf("owner: consumed selected %q, want %q", a, lifecycleaction.VerifyAdmission)
	}
	if a := lifecycleaction.Select(lifecycleDisposition(observed, true)); a != lifecycleaction.VerifyScope {
		t.Fatalf("owner: observed selected %q, want %q", a, lifecycleaction.VerifyScope)
	}

	// advance-result: distinct action tokens.
	ca, oa := dispositionNextAction(consumed), dispositionNextAction(observed)
	if ca.Action == oa.Action {
		t.Fatalf("advance-result collapsed consumed and observed onto %q", ca.Action)
	}
	if ca.Action != AdvanceNextVerifyAdmission || oa.Action != AdvanceNextVerifyScope {
		t.Fatalf("advance-result: consumed=%q observed=%q", ca.Action, oa.Action)
	}
	// Assert the EXACT summary. "different from the other one" and "does not say
	// observed change" are both satisfied by a wrong instruction.
	const wantConsumedSummary = "run verify-admission to reconcile and record the consumed operation"
	const wantObservedSummary = "run verify-admission to verify the observed change against the admitted scope"
	if ca.Summary != wantConsumedSummary {
		t.Fatalf("advance-result consumed summary = %q, want %q", ca.Summary, wantConsumedSummary)
	}
	if oa.Summary != wantObservedSummary {
		t.Fatalf("advance-result observed summary = %q, want %q", oa.Summary, wantObservedSummary)
	}

	// Status: same command, different summary -- the distinction must survive.
	cs := StatusResult{Status: "x"}
	applyGovernedDisposition(&cs, consumed, "x")
	os_ := StatusResult{Status: "x"}
	applyGovernedDisposition(&os_, observed, "x")
	if cs.Next.Summary != wantConsumedSummary {
		t.Fatalf("Status consumed summary = %q, want %q", cs.Next.Summary, wantConsumedSummary)
	}
	// Status has its own wording for the same operation; assert it exactly too.
	const wantStatusObserved = "run verify-admission to verify the scope of the observed change"
	if os_.Next.Summary != wantStatusObserved {
		t.Fatalf("Status observed summary = %q, want %q", os_.Next.Summary, wantStatusObserved)
	}

	// taskcontrol. Both cases map to ActionVerifyAdmission, so the action-kind
	// table CANNOT detect swapped summaries here -- only asserting the action
	// and summary TOGETHER can. Without this the "every adapter" claim in this
	// test's name was false: taskcontrol was never called.
	tcState := func(gov governanceState) taskcontrol.NextAction {
		var st taskcontrol.TaskControlState
		st.BindingHealth = "current"
		st.Permission = taskcontrol.PermissionSummary{Modify: "waiting"}
		return taskcontrol.SelectNextActionFor(st, taskcontrol.GovernedMutationDisposition{
			Governed: true, Disposition: lifecycleDisposition(gov, true),
		})
	}
	tc, to := tcState(consumed), tcState(observed)
	if tc.Kind != taskcontrol.ActionVerifyAdmission || to.Kind != taskcontrol.ActionVerifyAdmission {
		t.Fatalf("taskcontrol kinds: consumed=%q observed=%q, want both %q", tc.Kind, to.Kind, taskcontrol.ActionVerifyAdmission)
	}
	if tc.Summary == to.Summary {
		t.Fatalf("taskcontrol collapsed consumed and observed onto one summary: %q", tc.Summary)
	}
	if tc.Summary != wantConsumedSummary {
		t.Fatalf("taskcontrol consumed summary = %q, want %q", tc.Summary, wantConsumedSummary)
	}
	if to.Summary != wantStatusObserved {
		t.Fatalf("taskcontrol observed summary = %q, want %q", to.Summary, wantStatusObserved)
	}
}

// Through the CONVERSION and then Select -- not a hand-built Disposition.
//
// The conversion must not mask a downstream observation with authority. Anding
// gov.Resolved into CapabilityConsumed erased the fact before Select saw it, so
// a chain reporting StatusAdmitted with authority absent selected
// resolve_authority and the consumption vanished.
func TestConversionDoesNotMaskDownstreamFactsWithAuthority(t *testing.T) {
	for _, tc := range []struct {
		name string
		gov  governanceState
	}{
		{"admitted/consumed with authority absent", governanceState{Phase: closureprotocol.PhaseAdmitted, Status: StatusAdmitted, Resolved: false}},
		{"mutation observed with authority absent", governanceState{Phase: closureprotocol.PhaseMutationObserved, Status: StatusMutationObserved, Resolved: false}},
		{"scope verified with authority absent", governanceState{Phase: closureprotocol.PhaseScopeVerified, Status: StatusScopeVerified, Resolved: false, Terminal: true}},
		{"refused with authority absent", governanceState{Phase: closureprotocol.PhaseRefused, Status: StatusRefused, Resolved: false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := lifecycleaction.Select(lifecycleDisposition(tc.gov, true))
			if got == lifecycleaction.ResolveAuthority {
				t.Fatalf("%s selected %q: the downstream fact was erased before Select could reject it", tc.name, got)
			}
			if got != lifecycleaction.Blocked {
				t.Fatalf("%s selected %q, want %q", tc.name, got, lifecycleaction.Blocked)
			}
		})
	}
}

// An empty consumption is not a spend. The membership loop runs zero times for
// an empty set, so "every consumed operation was admitted" is vacuously true --
// and ValidateCapabilityConsumption never looks at operation IDs. The explicit
// emptiness check is the only thing standing between a receipt that spends
// nothing and the capability reading as spent.
func TestEmptyConsumptionIsAnIntegrityFailure(t *testing.T) {
	task := closureprotocol.TaskBinding{ID: "task.e", SessionID: "session.e"}
	rec := admission.RecordedAuthority{}
	rec.Base.Task = task
	dec := closureprotocol.AdmissionDecision{
		CapabilityID: "cap.e",
		OperationVerdicts: []closureprotocol.OperationAdmissionVerdict{
			{OperationID: "op.a", Verdict: admission.AdmissionVerdictAdmitted},
			{OperationID: "op.b", Verdict: admission.AdmissionVerdictAdmitted},
		},
	}
	digest, err := closureprotocol.SemanticDigest(dec)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	receipt := func(ops []string) closureprotocol.CapabilityConsumption {
		return closureprotocol.CapabilityConsumption{
			CapabilityID: "cap.e", Task: task, ConsumedOperationIDs: ops,
			ConsumedAt: time.Now().UTC().Format(time.RFC3339), DecisionDigestSHA256: digest,
			OneUseStatus: closureprotocol.ReceiptValid,
			ConsumerActor: closureprotocol.ActorBinding{
				PrincipalID: "principal.e", ActorKind: closureprotocol.ActorAgent,
			},
		}
	}

	for _, tc := range []struct {
		name string
		ops  []string
	}{
		{"nil", nil},
		{"empty slice", []string{}},
		{"one empty string", []string{""}},
		{"whitespace only", []string{"   "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := consumptionBinds(receipt(tc.ops), dec, rec); err == nil {
				t.Fatalf("a receipt spending %v operations was accepted as a consumption", tc.ops)
			}
		})
	}

	// POSITIVE CONTROL: a valid, non-empty PROPER subset stays acceptable.
	if err := consumptionBinds(receipt([]string{"op.a"}), dec, rec); err != nil {
		t.Fatalf("a valid non-empty proper subset was rejected: %v", err)
	}
}

// HELPER CONTRACT, not a demonstrated public bypass: AllAdmitted appears to stop
// this reaching the helper through the normal fold. A non-empty consumption
// against a decision that admits NOTHING must still be refused -- the old
// len(admitted) > 0 guard skipped the check entirely in exactly that case.
func TestConsumptionAgainstADecisionThatAdmitsNothingIsRefused(t *testing.T) {
	task := closureprotocol.TaskBinding{ID: "task.z", SessionID: "session.z"}
	rec := admission.RecordedAuthority{}
	rec.Base.Task = task
	dec := closureprotocol.AdmissionDecision{
		CapabilityID: "cap.z",
		OperationVerdicts: []closureprotocol.OperationAdmissionVerdict{
			{OperationID: "op.refused", Verdict: "refused"},
		},
	}
	digest, err := closureprotocol.SemanticDigest(dec)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	c := closureprotocol.CapabilityConsumption{
		CapabilityID: "cap.z", Task: task, ConsumedOperationIDs: []string{"op.refused"},
		ConsumedAt: time.Now().UTC().Format(time.RFC3339), DecisionDigestSHA256: digest,
		OneUseStatus: closureprotocol.ReceiptValid,
		ConsumerActor: closureprotocol.ActorBinding{
			PrincipalID: "principal.z", ActorKind: closureprotocol.ActorAgent,
		},
	}
	if err := consumptionBinds(c, dec, rec); err == nil {
		t.Fatal("a consumption was accepted against a decision whose admitted set is empty")
	}
}

// F3 (review 3985530101). A consumption receipt recorded AFTER the capability it
// spends had expired is not a spend, whatever time the reader happens to run at.
//
// The producer refuses to mint one (admission/capability.go:37-44). The reader
// delegated the contract to ValidateCapabilityConsumption, which checks only that
// consumed_at PARSES -- the relation between the receipt and the decision beside
// it is not its to see -- so an appended or imported receipt bound anyway,
// withheld the legitimate grant and selected verification instead.
//
// THE READER'S CLOCK IS DELIBERATELY INSIDE THE WINDOW. recordedDecisionBinds
// already refuses an expired decision as of now(); a now-based check cannot see
// this, and the defect is invisible exactly while the capability still looks
// live.
func TestConsumptionRecordedAfterExpiryDoesNotBind(t *testing.T) {
	task := closureprotocol.TaskBinding{ID: "task.x", SessionID: "session.x"}
	rec := admission.RecordedAuthority{}
	rec.Base.Task = task
	expiry := time.Now().UTC().Add(1 * time.Hour)
	dec := closureprotocol.AdmissionDecision{
		CapabilityID:     "cap.x",
		CapabilityExpiry: expiry.Format(time.RFC3339),
		OperationVerdicts: []closureprotocol.OperationAdmissionVerdict{
			{OperationID: "op.a", Verdict: admission.AdmissionVerdictAdmitted},
		},
	}
	digest, err := closureprotocol.SemanticDigest(dec)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	actor := closureprotocol.ActorBinding{PrincipalID: "principal.x", ActorKind: closureprotocol.ActorAgent}
	receipt := func(consumedAt time.Time) closureprotocol.CapabilityConsumption {
		return closureprotocol.CapabilityConsumption{
			CapabilityID: "cap.x", Task: task, ConsumedOperationIDs: []string{"op.a"},
			ConsumedAt: consumedAt.Format(time.RFC3339), DecisionDigestSHA256: digest,
			OneUseStatus: closureprotocol.ReceiptValid, ConsumerActor: actor,
		}
	}

	// The receipt's own contract passes: this is a CROSS-RECORD relation, and
	// pinning that here keeps the repair from being "re-derive the validator".
	after := receipt(expiry.Add(1 * time.Minute))
	if err := closureprotocol.ValidateCapabilityConsumption(after); err != nil {
		t.Fatalf("the receipt fails its own contract, so this proves nothing about the relation: %v", err)
	}
	if err := consumptionBinds(after, dec, rec); err == nil {
		t.Fatal("a consumption recorded after the capability expired was accepted as a valid spend")
	}

	// SYMMETRY: the producer refuses the same pair. The two contracts must not
	// disagree about what a spend is.
	if _, err := admission.ConsumeCapability(dec, task, actor, []string{"op.a"}, after.ConsumedAt); err == nil {
		t.Fatal("the producer minted a receipt this reader must refuse; the asymmetry this repair closes has reopened")
	}

	// POSITIVE CONTROLS: inside the window and exactly at the boundary still bind.
	for _, tc := range []struct {
		name string
		at   time.Time
	}{
		{"well inside the window", expiry.Add(-30 * time.Minute)},
		{"exactly at expiry", expiry},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := consumptionBinds(receipt(tc.at), dec, rec); err != nil {
				t.Fatalf("a legitimate spend %s was rejected: %v", tc.name, err)
			}
		})
	}

	// An UNBOUNDED capability has no window to fall outside of.
	unbounded := dec
	unbounded.CapabilityExpiry = ""
	unboundedDigest, err := closureprotocol.SemanticDigest(unbounded)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	c := receipt(expiry.Add(24 * time.Hour))
	c.DecisionDigestSHA256 = unboundedDigest
	if err := consumptionBinds(c, unbounded, rec); err != nil {
		t.Fatalf("a spend against a capability with no expiry was rejected: %v", err)
	}
}
