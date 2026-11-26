// SPDX-License-Identifier: AGPL-3.0-only

package taskcontrol

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closure"
)

// R-CLOSURE-01: Sensei must not present its own obstruction as a
// participant's missing input. A tooling defect is reported first; a stale
// extraction is refreshed before any participant is asked a question the
// refresh may change or retire; only then does the architect's question lead.
func TestSenseiOwnedObstructionsLeadTheNextAction(t *testing.T) {
	question := &ClassifiedQuestion{ID: "question.direction", QuestionText: "What current intended architecture must this task preserve?"}
	refresh := ClassifiedBlocker{ID: "blocker.evidence.stale", Consequence: closure.ActionRefreshExtraction, Disposition: ClassActiveUnresolved}
	defect := ClassifiedBlocker{ID: "blocker.agent.defect", Consequence: closure.ActionReportToolingDefect, Disposition: ClassActiveUnresolved}

	state := TaskControlState{TaskID: "task.x", PrimaryQuestion: question, Blockers: []ClassifiedBlocker{refresh}}
	if got := selectNextAction(state, true, GovernedMutationDisposition{}); got.Kind != ActionRefreshExtraction || got.TargetID != refresh.ID {
		t.Fatalf("stale extraction must be refreshed before the architect is asked: %+v", got)
	}
	state.Blockers = append(state.Blockers, defect)
	if got := selectNextAction(state, true, GovernedMutationDisposition{}); got.Kind != ActionReportToolingDefect || got.TargetID != defect.ID {
		t.Fatalf("a tooling defect must be reported as Sensei's: %+v", got)
	}
	state.Blockers = []ClassifiedBlocker{{ID: refresh.ID, Consequence: refresh.Consequence, Disposition: ClassDuplicate}}
	if got := selectNextAction(state, true, GovernedMutationDisposition{}); got.Kind != ActionAnswerArchitectQuestion {
		t.Fatalf("a duplicate refresh blocker must not displace the architect question: %+v", got)
	}
}

// A load-bearing root blocker that no question covers is delivered to its
// responsible party by name. advance_convergence would re-emit it unchanged
// (observed in Sensei's self-admission of #387: define_authority blockers had
// no question template and the next action was advance_convergence).
func TestUndeliveredObligationIsNamedForItsResponsibleParty(t *testing.T) {
	authority := ClassifiedBlocker{ID: "blocker.authority.x", Consequence: "define_authority", Statement: "applicable authority records disagree",
		LoadBearing: true, Disposition: ClassActiveUnresolved}
	state := TaskControlState{TaskID: "task.x", Blockers: []ClassifiedBlocker{authority},
		Groups: []BlockerGroup{{ID: "group.1", RootBlockerID: authority.ID}}, Summary: Summary{ActiveRootBlockers: 1}}
	got := selectNextAction(state, true, GovernedMutationDisposition{})
	if got.Kind != ActionArchitectDecisionRequired || got.TargetID != authority.ID {
		t.Fatalf("an architect-owned obligation was not delivered: %+v", got)
	}
	state.Blockers[0] = ClassifiedBlocker{ID: "blocker.agent.y", Consequence: "add_test", Statement: "no required test", LoadBearing: true, Disposition: ClassActiveUnresolved}
	state.Groups[0].RootBlockerID = "blocker.agent.y"
	if got := selectNextAction(state, true, GovernedMutationDisposition{}); got.Kind != ActionImplementorActionRequired {
		t.Fatalf("an implementor-owned obligation was not delivered: %+v", got)
	}
	state.Blockers[0].QuestionIDs = []string{"question.z"}
	if got := selectNextAction(state, true, GovernedMutationDisposition{}); got.Kind == ActionImplementorActionRequired {
		t.Fatalf("a blocker already covered by a question was re-delivered: %+v", got)
	}
}
