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
