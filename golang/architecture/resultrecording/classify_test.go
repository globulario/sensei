// SPDX-License-Identifier: Apache-2.0

package resultrecording

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/proofrequirements"
)

func TestClassifyReadyEntersProving(t *testing.T) {
	ns, err := ClassifyNextState(proofrequirements.Document{
		ExtractionCompleteness: proofrequirements.ExtractionComplete,
		ProvingDisposition:     proofrequirements.ProvingReady,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ns.TaskPhase != closureprotocol.PhaseProving || ns.OperationalStatus != StatusReadyForProving {
		t.Fatalf("ready did not enter proving: %+v", ns)
	}
	if ns.NextAction != NextActionCompleteProof {
		t.Fatalf("next action = %s", ns.NextAction)
	}
}

func TestClassifyBlockedArchitect(t *testing.T) {
	ns, err := ClassifyNextState(proofrequirements.Document{
		ExtractionCompleteness: proofrequirements.ExtractionComplete,
		ProvingDisposition:     proofrequirements.ProvingBlocked,
		ArchitectQuestions:     []proofrequirements.Requirement{{Class: "ArchitectQuestion", ID: "q.1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ns.TaskPhase != closureprotocol.PhaseScopeVerified || ns.OperationalStatus != StatusWaitingArchitect {
		t.Fatalf("architect-blocked misclassified: %+v", ns)
	}
	if len(ns.WaitingOn) != 1 || ns.WaitingOn[0] != "q.1" {
		t.Fatalf("waiting reasons not retained: %v", ns.WaitingOn)
	}
}

// A blocked result whose dominant reason is a governed closure blocker projects
// waiting_mechanical_repair, but every waiting reason is retained.
func TestClassifyBlockedRetainsAllReasons(t *testing.T) {
	ns, err := ClassifyNextState(proofrequirements.Document{
		ExtractionCompleteness: proofrequirements.ExtractionComplete,
		ProvingDisposition:     proofrequirements.ProvingBlocked,
		ArchitectQuestions:     []proofrequirements.Requirement{{Class: "ArchitectQuestion", ID: "q.1"}},
		ClosureBlockers:        []proofrequirements.Requirement{{Class: "ClosureBlocker", ID: "b.1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ns.OperationalStatus != StatusWaitingMechanicalRepair {
		t.Fatalf("dominant status = %s, want mechanical", ns.OperationalStatus)
	}
	if len(ns.WaitingOn) != 2 {
		t.Fatalf("secondary blockers dropped: %v", ns.WaitingOn)
	}
}

func TestClassifyBlockedNoReasonUnprojectable(t *testing.T) {
	_, err := ClassifyNextState(proofrequirements.Document{
		ExtractionCompleteness: proofrequirements.ExtractionComplete,
		ProvingDisposition:     proofrequirements.ProvingBlocked,
	})
	if e, ok := err.(*Error); !ok || e.Code != CodeBlockedUnprojectable {
		t.Fatalf("want blocked_disposition_unprojectable, got %v", err)
	}
}

func TestClassifyIncompleteRejected(t *testing.T) {
	_, err := ClassifyNextState(proofrequirements.Document{
		ExtractionCompleteness: proofrequirements.ExtractionIncomplete,
	})
	if err == nil {
		t.Fatal("incomplete extraction must be unprojectable")
	}
}
