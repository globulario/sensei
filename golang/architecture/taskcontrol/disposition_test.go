// SPDX-License-Identifier: AGPL-3.0-only

package taskcontrol

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

// exhaustedFixture is the state the issue describes: every static probe has run
// and returned nothing, so the question is active and unanswerable.
func exhaustedFixture() Inputs {
	in := controlFixture()
	in.Probes.Probes = nil
	return in
}

func TestUndisposedQuestionStillDemandsEvidence(t *testing.T) {
	state, err := Project(exhaustedFixture())
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Questions[0].ResolutionClass; got != ClassActiveUnresolved {
		t.Fatalf("class=%s, want %s", got, ClassActiveUnresolved)
	}
	if state.NextAction.Kind != ActionProvideExternalEvidence {
		t.Fatalf("next=%s, want %s", state.NextAction.Kind, ActionProvideExternalEvidence)
	}
}

// An architect who dismissed the question through the governed path decided
// that no evidence will be sought. Demanding it anyway leaves the task on an
// action nobody intends to take.
func TestDismissedQuestionStopsDemandingEvidence(t *testing.T) {
	in := exhaustedFixture()
	in.Dispositions = map[string]QuestionDisposition{
		"question.one": {Disposition: "dismissed", ReceiptDigestSHA256: "receipt-digest"},
	}
	state, err := Project(in)
	if err != nil {
		t.Fatal(err)
	}
	q := state.Questions[0]
	if q.ResolutionClass != ClassGovernedDisposed {
		t.Fatalf("class=%s, want %s", q.ResolutionClass, ClassGovernedDisposed)
	}
	if q.RequiredActor != "none" || q.BlockingEffect != "non_blocking" {
		t.Fatalf("dismissed question still demands an actor: actor=%s effect=%s", q.RequiredActor, q.BlockingEffect)
	}
	if state.NextAction.Kind == ActionProvideExternalEvidence || state.NextAction.Kind == ActionAnswerArchitectQuestion {
		t.Fatalf("next action still asks for the dismissed question: %+v", state.NextAction)
	}
	if len(q.AnswerabilityBasis) == 0 || q.AnswerabilityBasis[0] != "dismissed by governed disposition receipt receipt-digest" {
		t.Fatalf("suppression does not name the receipt that caused it: %v", q.AnswerabilityBasis)
	}
}

// The claim is still unsupported. Only the question was disposed of, so the
// work continues rather than reporting itself finished.
func TestDismissedQuestionLeavesTheBlockerStanding(t *testing.T) {
	in := exhaustedFixture()
	in.Dispositions = map[string]QuestionDisposition{"question.one": {Disposition: "dismissed"}}
	state, err := Project(in)
	if err != nil {
		t.Fatal(err)
	}
	if state.Summary.ActiveRootBlockers == 0 {
		t.Fatal("dismissing a question silently cleared the blocker it was about")
	}
	if state.NextAction.Kind != ActionAdvanceConvergence {
		t.Fatalf("next=%s, want %s", state.NextAction.Kind, ActionAdvanceConvergence)
	}
}

// Two conflicting receipts decide nothing.
func TestContestedDispositionDoesNotSuppressTheQuestion(t *testing.T) {
	in := exhaustedFixture()
	in.Dispositions = map[string]QuestionDisposition{
		"question.one": {Disposition: "dismissed", Contested: true},
	}
	state, err := Project(in)
	if err != nil {
		t.Fatal(err)
	}
	if state.Questions[0].ResolutionClass != ClassActiveUnresolved {
		t.Fatalf("a contested disposition suppressed the question: %s", state.Questions[0].ResolutionClass)
	}
}

// Deferred is a decision to wait for the architect, not to stop asking.
func TestDeferredQuestionWaitsForTheArchitect(t *testing.T) {
	in := exhaustedFixture()
	in.Dispositions = map[string]QuestionDisposition{"question.one": {Disposition: "deferred"}}
	state, err := Project(in)
	if err != nil {
		t.Fatal(err)
	}
	if state.Questions[0].ResolutionClass != ClassArchitectJudgementRequired || state.Questions[0].RequiredActor != "architect" {
		t.Fatalf("deferred question: class=%s actor=%s", state.Questions[0].ResolutionClass, state.Questions[0].RequiredActor)
	}
	if state.NextAction.Kind != ActionAnswerArchitectQuestion {
		t.Fatalf("next=%s, want %s", state.NextAction.Kind, ActionAnswerArchitectQuestion)
	}
}

// An `answered` receipt asserts an answer exists, and the dialogue document is
// the authority on whether it does. A ledger that disagrees is a divergence to
// surface, never a reason to stop asking.
func TestAnsweredDispositionDoesNotSuppressAnUnansweredQuestion(t *testing.T) {
	for _, d := range []string{"answered", "task_local", "", "something_new"} {
		in := exhaustedFixture()
		in.Dispositions = map[string]QuestionDisposition{"question.one": {Disposition: d}}
		state, err := Project(in)
		if err != nil {
			t.Fatal(err)
		}
		if state.Questions[0].ResolutionClass != ClassActiveUnresolved {
			t.Fatalf("disposition %q suppressed an unanswered question: %s", d, state.Questions[0].ResolutionClass)
		}
	}
}

// A question the dialogue already closed keeps its own lifecycle answer; the
// ledger must not reclassify it.
func TestResolvedQuestionIsUnaffectedByADisposition(t *testing.T) {
	in := exhaustedFixture()
	in.Dialogue.OpenQuestions[0].Status = architecture.QuestionStatusResolved
	in.Dispositions = map[string]QuestionDisposition{"question.one": {Disposition: "dismissed"}}
	state, err := Project(in)
	if err != nil {
		t.Fatal(err)
	}
	if state.Questions[0].ResolutionClass != ClassMechanicallyAnswerable {
		t.Fatalf("class=%s, want %s", state.Questions[0].ResolutionClass, ClassMechanicallyAnswerable)
	}
}
