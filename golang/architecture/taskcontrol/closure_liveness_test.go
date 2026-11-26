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
