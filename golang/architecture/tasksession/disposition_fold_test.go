// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

// Silence must never suppress a demand: with no readable ledger, every question
// stays exactly as open as it was.
func TestGovernedDispositionsFailClosedWithoutALedger(t *testing.T) {
	dialogue := architecture.DialogueDocument{OpenQuestions: []architecture.OpenQuestion{{ID: "question.one"}}}
	if got := governedDispositions(t.TempDir(), dialogue); got != nil {
		t.Fatalf("an unreadable ledger produced dispositions: %+v", got)
	}
	if got := governedDispositions("/nonexistent/task/dir", dialogue); got != nil {
		t.Fatalf("a missing task directory produced dispositions: %+v", got)
	}
}

func TestGovernedDispositionsSkipsTheLedgerWithoutQuestions(t *testing.T) {
	if got := governedDispositions(t.TempDir(), architecture.DialogueDocument{}); got != nil {
		t.Fatalf("no questions, yet dispositions were folded: %+v", got)
	}
}
