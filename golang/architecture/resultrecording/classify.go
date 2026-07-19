// SPDX-License-Identifier: Apache-2.0

package resultrecording

import (
	"sort"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/proofrequirements"
)

// Operational status strings projected onto the ledger event.
const (
	StatusReadyForProving         = "ready_for_proving"
	StatusWaitingArchitect        = "waiting_architect"
	StatusWaitingGovernance       = "waiting_governance"
	StatusWaitingMechanicalRepair = "waiting_mechanical_repair"

	NextActionCompleteProof    = "complete_required_proof"
	NextActionResolveQuestion  = "resolve_architect_question"
	NextActionGovernanceReview = "governance_review"
	NextActionMechanicalRepair = "perform_mechanical_repair"
)

// NextState is the honest projected task disposition of a recorded transition. It
// is derived purely from the proof-requirement document; it never claims proof
// has been discharged and never asserts correctness.
type NextState struct {
	TaskPhase         closureprotocol.TaskPhase
	OperationalStatus string
	WaitingOn         []string
	NextAction        string
	ReasonIDs         []string
}

// ClassifyNextState maps a complete proof-requirement document to the next task
// state. A ready result opens the proving phase (it does NOT claim proof); a
// blocked result keeps the protocol phase at scope_verified and projects one
// honest waiting state, retaining every blocker. Pending tests, evidence, and
// proof slots are ordinary proving work — never a blocked state.
func ClassifyNextState(proof proofrequirements.Document) (NextState, error) {
	if proof.ExtractionCompleteness != proofrequirements.ExtractionComplete {
		return NextState{}, recErr(CodeBlockedUnprojectable, "extraction is not complete (%q)", proof.ExtractionCompleteness)
	}
	switch proof.ProvingDisposition {
	case proofrequirements.ProvingReady:
		return NextState{
			TaskPhase:         closureprotocol.PhaseProving,
			OperationalStatus: StatusReadyForProving,
			NextAction:        NextActionCompleteProof,
		}, nil
	case proofrequirements.ProvingBlocked:
		return classifyBlocked(proof)
	default:
		return NextState{}, recErr(CodeBlockedUnprojectable, "unprojectable proving disposition %q", proof.ProvingDisposition)
	}
}

// classifyBlocked partitions the represented blockers into waiting classes,
// retaining every reason and choosing one deterministic primary next action.
func classifyBlocked(proof proofrequirements.Document) (NextState, error) {
	var architect, governance, mechanical []string

	for _, q := range proof.ArchitectQuestions {
		switch q.Class {
		case "ArchitectQuestion":
			architect = append(architect, q.ID)
		case "UnaccountedBlocker", "DuplicateAccounting", "UnsupportedCritical":
			governance = append(governance, q.ID)
		default:
			governance = append(governance, q.ID)
		}
	}
	for _, ch := range proof.RequirementChanges {
		if ch.Disposition == "governance_review_required" {
			governance = append(governance, ch.ID)
		}
	}
	// Governed closure blockers that demand a mutation/mechanical correction.
	for _, b := range proof.ClosureBlockers {
		mechanical = append(mechanical, b.ID)
	}

	all := append(append(append([]string(nil), architect...), governance...), mechanical...)
	all = sortedUniqueStrings(all)
	if len(all) == 0 {
		return NextState{}, recErr(CodeBlockedUnprojectable, "proving is blocked with no represented reason")
	}

	// Deterministic dominance: mechanical > governance > architect (most concrete
	// blocking wins the primary next action); all reasons are retained.
	ns := NextState{TaskPhase: closureprotocol.PhaseScopeVerified, WaitingOn: all, ReasonIDs: all}
	switch {
	case len(mechanical) > 0:
		ns.OperationalStatus = StatusWaitingMechanicalRepair
		ns.NextAction = NextActionMechanicalRepair
	case len(governance) > 0:
		ns.OperationalStatus = StatusWaitingGovernance
		ns.NextAction = NextActionGovernanceReview
	default:
		ns.OperationalStatus = StatusWaitingArchitect
		ns.NextAction = NextActionResolveQuestion
	}
	return ns, nil
}

func sortedUniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
