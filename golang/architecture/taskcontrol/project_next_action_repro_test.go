// SPDX-License-Identifier: AGPL-3.0-only

package taskcontrol

// F4 (P1) from the PR #351 review, reproduced at the selector.
//
// selectNextAction reads the mutation-permission scalar. Once that scalar was
// narrowed to mean "may consume a NEW capability", both of its branches became
// wrong: "admitted" now means a capability is available to CONSUME, not an edit
// to perform, and "waiting" after consumption means the capability is spent,
// not that another admission should be requested.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/lifecycleaction"
	"gopkg.in/yaml.v3"
)

func cleanState(modify string) TaskControlState {
	var s TaskControlState
	s.TaskID = "task.f4"
	s.Permission = PermissionSummary{Inspect: "admitted", Modify: modify}
	// No primary question and no active root blockers, so the selector actually
	// reaches the permission branch. In tasksession's own fixture an open
	// architect question wins first, which is why this defect is invisible there.
	return s
}

// F4a. A recorded typed decision whose capability is UNCONSUMED resolves to
// "admitted". The selector turns that into "perform only the admitted edit",
// instructing a mutation before `sensei consume-admission` has ever run --
// which architectural-closure-v1 requires first.
func TestF4a_AdmittedMustNotSelectPerformEditBeforeConsumption(t *testing.T) {
	got := selectNextAction(cleanState("admitted"), true, GovernedMutationDisposition{Governed: true, CapabilityAvailable: true})
	if got.Kind == ActionPerformAdmittedEdit {
		t.Fatalf("F4a: modify=%q selected %q; an available capability must be CONSUMED before any edit is performed",
			"admitted", got.Kind)
	}
}

// F4b. After consumption the owner resolves "waiting" -- meaning no NEW
// capability. The selector reads that as "request mutation admission", asking
// for an admission that was just spent.
func TestF4b_SpentCapabilityMustNotSelectRequestAdmission(t *testing.T) {
	got := selectNextAction(cleanState("waiting"), true, GovernedMutationDisposition{Governed: true, CapabilityConsumed: true})
	if got.Kind == ActionRequestMutation {
		t.Fatalf("F4b: modify=%q selected %q; after a capability is spent the next action is not another admission request",
			"waiting", got.Kind)
	}
}

// The ratified vocabulary extension is COMPATIBLE but not inert: it widens the
// persisted NextAction vocabulary and changes StateDigest whenever it is
// selected. This proves serializers accept it, it survives a round trip, and the
// digest is sensitive to it -- so a stale digest cannot silently describe it.
func TestConsumeCapabilityIsAPersistableActionThatMovesTheDigest(t *testing.T) {
	base := cleanState("admitted")
	base.NextAction = NextAction{Kind: ActionPerformAdmittedEdit, TargetID: "task.f4"}
	consuming := cleanState("admitted")
	consuming.NextAction = NextAction{Kind: ActionConsumeCapability, TargetID: "task.f4"}

	if StateDigest(base) == StateDigest(consuming) {
		t.Fatal("StateDigest does not distinguish consume_capability from perform_admitted_edit; a stale digest could describe either")
	}

	for _, enc := range []struct {
		name      string
		marshal   func(any) ([]byte, error)
		unmarshal func([]byte, any) error
	}{
		{"json", json.Marshal, json.Unmarshal},
		{"yaml", yaml.Marshal, yaml.Unmarshal},
	} {
		t.Run(enc.name, func(t *testing.T) {
			raw, err := enc.marshal(consuming)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !strings.Contains(string(raw), "consume_capability") {
				t.Fatalf("%s serialization dropped the action: %s", enc.name, raw)
			}
			var back TaskControlState
			if err := enc.unmarshal(raw, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if back.NextAction.Kind != ActionConsumeCapability {
				t.Fatalf("%s round trip produced %q, want %q", enc.name, back.NextAction.Kind, ActionConsumeCapability)
			}
			if StateDigest(back) != StateDigest(consuming) {
				t.Fatalf("%s round trip changed StateDigest", enc.name)
			}
		})
	}
}

// N2 (3981430709). A governed task with typed authority resolved but no
// admission_decided has Governed=true and both capability flags false. The typed
// branch matches neither case and falls through to the ordinary selection, which
// with no blockers reaches ActionCompleteTask -- telling consumers a task
// awaiting admission is complete, and causing singleNonCompletedTask to filter
// it out of discovery entirely.
func TestN2_GovernedTaskAwaitingAdmissionMustNotSelectCompletion(t *testing.T) {
	// ready_for_admission. The original expectation here was
	// request_mutation_admission, written before the lifecycle owner existed;
	// the ratified mapping names this state's machine operation decide_admission.
	awaiting := lifecycleaction.Disposition{
		TypedProtocol: true, AuthorityResolved: true, Status: "ready_for_admission", ReadyForAdmission: true,
	}
	got := selectNextAction(cleanState("waiting"), true, GovernedMutationDisposition{Governed: true, Disposition: awaiting})
	if got.Kind == ActionCompleteTask {
		t.Fatalf("N2: a governed task awaiting admission selected %q; missing decision and false capability flags cannot establish completion", got.Kind)
	}
	if got.Kind != ActionDecideAdmission {
		t.Fatalf("N2: a governed task awaiting admission selected %q, want %q", got.Kind, ActionDecideAdmission)
	}
}

// An unrecognised governed state must fail closed -- never completion, never a
// mutation action, never an admission instruction invented to fill the gap.
func TestN2b_UnrecognisedGovernedStateFailsClosed(t *testing.T) {
	got := selectNextAction(cleanState("waiting"), true, GovernedMutationDisposition{Governed: true})
	for _, forbidden := range []string{ActionCompleteTask, ActionPerformAdmittedEdit, ActionConsumeCapability, ActionDecideAdmission} {
		if got.Kind == forbidden {
			t.Fatalf("an unrecognised governed state selected %q", got.Kind)
		}
	}
	if got.Kind != ActionNone {
		t.Fatalf("an unrecognised governed state selected %q, want %q", got.Kind, ActionNone)
	}
}

// A governance-integrity failure never reaches the selector: it fails the call
// closed with an error, so no state is produced at all. That is the stronger
// property, and it is asserted in tasksession where the error originates
// (TestGovernanceErrorProducesNoStateAtAll).

// The four ratified tokens widen the persisted NextAction vocabulary and move
// StateDigest when selected. Serialization, round trip and digest sensitivity
// are proven, not assumed.
func TestRatifiedLifecycleTokensPersistAndMoveTheDigest(t *testing.T) {
	for _, kind := range []string{
		ActionResolveAuthority, ActionDecideAdmission,
		ActionMechanicalRepair, ActionRecordResultTransition,
	} {
		t.Run(kind, func(t *testing.T) {
			base := cleanState("waiting")
			base.NextAction = NextAction{Kind: ActionNone, TargetID: "task.t"}
			with := cleanState("waiting")
			with.NextAction = NextAction{Kind: kind, TargetID: "task.t"}
			if StateDigest(base) == StateDigest(with) {
				t.Fatalf("StateDigest does not distinguish %q; a stale digest could describe it", kind)
			}
			for _, enc := range []struct {
				name string
				m    func(any) ([]byte, error)
				u    func([]byte, any) error
			}{{"json", json.Marshal, json.Unmarshal}, {"yaml", yaml.Marshal, yaml.Unmarshal}} {
				raw, err := enc.m(with)
				if err != nil {
					t.Fatalf("%s marshal: %v", enc.name, err)
				}
				if !strings.Contains(string(raw), kind) {
					t.Fatalf("%s serialization dropped %q", enc.name, kind)
				}
				var back TaskControlState
				if err := enc.u(raw, &back); err != nil {
					t.Fatalf("%s unmarshal: %v", enc.name, err)
				}
				if back.NextAction.Kind != kind {
					t.Fatalf("%s round trip produced %q, want %q", enc.name, back.NextAction.Kind, kind)
				}
				if StateDigest(back) != StateDigest(with) {
					t.Fatalf("%s round trip changed StateDigest for %q", enc.name, kind)
				}
			}
		})
	}
}
