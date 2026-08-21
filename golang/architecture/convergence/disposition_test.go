// SPDX-License-Identifier: AGPL-3.0-only

package convergence

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/dispositionsemantics"
)

func awaitingEvidenceDialogue() architecture.DialogueDocument {
	return architecture.DialogueDocument{OpenQuestions: []architecture.OpenQuestion{{
		ID: "question.one", Priority: architecture.QuestionPriorityHigh,
		Status: architecture.QuestionStatusAwaitingEvidence,
	}}}
}

func dismissed(id string) map[string]dispositionsemantics.Decision {
	return map[string]dispositionsemantics.Decision{id: {State: dispositionsemantics.Dismissed, ReceiptDigestSHA256: "receipt"}}
}

// A dismissal never reaches the dialogue document's status field, so a wait
// class computed from q.Status alone advertises an evidence wait the architect
// already terminated.
func TestDismissedQuestionIsNotAnEvidenceWait(t *testing.T) {
	report := closure.Report{Verdict: closure.VerdictOpen}
	if got := WaitClasses(report, awaitingEvidenceDialogue(), emptyProbeDoc(), nil); !contains(got, WaitEvidence) {
		t.Fatalf("undisposed baseline must wait on evidence, got %v", got)
	}
	got := WaitClasses(report, awaitingEvidenceDialogue(), emptyProbeDoc(), dismissed("question.one"))
	if contains(got, WaitEvidence) {
		t.Fatalf("dismissed question still advertised an evidence wait: %v", got)
	}
}

func TestDismissedQuestionIsNotOfferedAsANextAction(t *testing.T) {
	report := closure.Report{Verdict: closure.VerdictOpen}
	base := NextActions(report, awaitingEvidenceDialogue(), emptyProbeDoc(), nil)
	if len(base) == 0 || base[0].Class != "provide_evidence" {
		t.Fatalf("undisposed baseline must offer provide_evidence, got %+v", base)
	}
	for _, a := range NextActions(report, awaitingEvidenceDialogue(), emptyProbeDoc(), dismissed("question.one")) {
		if a.Class == "provide_evidence" {
			t.Fatalf("dismissed question still offered as a next action: %+v", a)
		}
	}
}

// Deferral moves the wait to the architect; it does not remove it.
func TestDeferredQuestionWaitsOnTheArchitect(t *testing.T) {
	decisions := map[string]dispositionsemantics.Decision{"question.one": {State: dispositionsemantics.Deferred, ReceiptDigestSHA256: "receipt"}}
	got := WaitClasses(closure.Report{Verdict: closure.VerdictOpen}, awaitingEvidenceDialogue(), emptyProbeDoc(), decisions)
	if contains(got, WaitEvidence) {
		t.Fatalf("deferred question still waits on evidence: %v", got)
	}
	if !contains(got, WaitArchitect) {
		t.Fatalf("deferred question dropped its architect wait: %v", got)
	}
	actions := NextActions(closure.Report{Verdict: closure.VerdictOpen}, awaitingEvidenceDialogue(), emptyProbeDoc(), decisions)
	if len(actions) == 0 || actions[0].Class != "answer_question" {
		t.Fatalf("deferred question must be answerable by the architect, got %+v", actions)
	}
}

// Everything that decides nothing must leave the demand exactly where it was.
func TestOnlyADecidedDismissalSuppressesTheEvidenceWait(t *testing.T) {
	cases := map[string]dispositionsemantics.Decision{
		"contested dismissal": {State: dispositionsemantics.Dismissed, Contested: true},
		"answered":            {State: dispositionsemantics.Answered},
		"task_local":          {State: dispositionsemantics.TaskLocal},
		"unrecognised":        {State: dispositionsemantics.State("something_new")},
		"nothing recorded":    {},
	}
	for name, decision := range cases {
		got := WaitClasses(closure.Report{Verdict: closure.VerdictOpen}, awaitingEvidenceDialogue(), emptyProbeDoc(),
			map[string]dispositionsemantics.Decision{"question.one": decision})
		if !contains(got, WaitEvidence) {
			t.Fatalf("%s suppressed the evidence wait: %v", name, got)
		}
	}
}

// CHARACTERIZATION, not an endorsement. With the evidence wait gone and the
// blocker still open, the session classifies as stalled rather than waiting.
//
// Pinned deliberately: `stalled` is truthful about the machine — there is no
// known next action — but it sits in a vocabulary of pathologies beside
// `oscillating` and `budget_exhausted`, and a deliberate architect retirement
// is not the engine failing to progress. Whether the REASON should carry the
// governed decision is an open question (see the PR); the capability outcome is
// refusal either way, so nothing is admitted that was not admitted before.
func TestDismissalLeavesTheSessionStalledNotWaiting(t *testing.T) {
	policy := Policy{ID: PolicyStrictV1, NoEffectInputLimit: 3}
	if got := classifyStatus(closure.VerdictOpen, []string{WaitEvidence}, policy, false, 0, false); got != StatusWaiting {
		t.Fatalf("baseline status = %s, want %s", got, StatusWaiting)
	}
	if got := classifyStatus(closure.VerdictOpen, nil, policy, false, 0, false); got != StatusStalled {
		t.Fatalf("status after the last wait is dismissed = %s, want %s", got, StatusStalled)
	}
}

func contains(in []string, want string) bool {
	for _, v := range in {
		if v == want {
			return true
		}
	}
	return false
}
