// SPDX-License-Identifier: AGPL-3.0-only

package questiongen

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
)

// #230 item 3: a question raised while assessing a three-file change was naming
// thirteen files, none of them the three. A change to doctor.go was being asked
// to account for internal/architect/debt.go.
func TestQuestionScopeIsBoundedToTheTaskEnvelope(t *testing.T) {
	ctx := questionGenContext()
	ctx.Closure.ScopeReceipt.Files = []string{"internal/doctor/doctor.go", "internal/doctor/probe.go"}
	claim := architecture.Claim{
		ID: "claim.wide", ArchitecturalPlane: architecture.PlaneObserved,
		AssertionOrigin: architecture.OriginDerived, PremiseFacts: []string{"fact.wide"},
		EpistemicStatus: architecture.StatusUnknown,
		Statement:       architecture.ClaimStatement{Subject: "cluster/state", Predicate: "has_observed_writer_set", Object: "many"},
		Scope: architecture.ClaimScope{Files: []string{
			"internal/doctor/doctor.go",
			"internal/architect/debt.go",
			"internal/assist/handoff.go",
			"internal/acceptance/governed_run_test.go",
		}},
	}
	ctx.Claims.Claims = []architecture.Claim{claim}
	ctx.Closure.Blockers = []closure.Blocker{{
		ID: "blocker.evidence.abcdef012345", Dimension: closure.DimensionEvidence,
		Severity: architecture.QuestionPriorityHigh, Code: "closure.evidence.claim_unknown",
		Summary: "Claim has no evidence.", ClaimIDs: []string{claim.ID},
		Files: []string{"internal/doctor/doctor.go"}, RequiredNextAction: "add_evidence",
	}}

	res, err := Generate(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Dialogue.OpenQuestions) != 1 {
		t.Fatalf("questions=%d, want 1", len(res.Dialogue.OpenQuestions))
	}
	q := res.Dialogue.OpenQuestions[0]

	for _, f := range q.Scope.Files {
		if f != "internal/doctor/doctor.go" && f != "internal/doctor/probe.go" {
			t.Errorf("the question names %q, which this task never admitted", f)
		}
	}
	if len(q.Scope.Files) == 0 {
		t.Fatal("the question names no files at all")
	}

	// The breadth must remain visible: a narrowed question that hides how wide
	// its claim reaches makes a wide claim look narrow.
	var said bool
	for _, r := range q.ReasonsOpen {
		if strings.Contains(r, "outside it") {
			said = true
		}
	}
	if !said {
		t.Fatalf("nothing records what the envelope bounded out: %v", q.ReasonsOpen)
	}
}

// An empty envelope binds nothing — a task-wide or repository-wide assessment
// has no narrower envelope, and inventing one would be worse than the breadth.
func TestAnEmptyEnvelopeBindsNothing(t *testing.T) {
	files := []string{"a.go", "b.go"}
	bound, dropped := boundToEnvelope(files, nil)
	if len(bound) != 2 || dropped != 0 {
		t.Fatalf("bound=%v dropped=%d, want the union unchanged", bound, dropped)
	}
}

// If every file is outside the envelope, the union stands: a question about no
// files at all says less than the honest breadth does.
func TestAQuestionEntirelyOutsideTheEnvelopeKeepsItsFiles(t *testing.T) {
	bound, dropped := boundToEnvelope([]string{"x.go", "y.go"}, []string{"unrelated.go"})
	if len(bound) != 2 {
		t.Fatalf("bound=%v, want both files kept rather than a question about nothing", bound)
	}
	if dropped != 2 {
		t.Fatalf("dropped=%d, want 2 recorded", dropped)
	}
}

func TestBoundToEnvelopeKeepsOnlyAdmittedFiles(t *testing.T) {
	bound, dropped := boundToEnvelope(
		[]string{"in/one.go", "out/two.go", "in/three.go"},
		[]string{"in/one.go", "in/three.go", "in/unused.go"},
	)
	if strings.Join(bound, ",") != "in/one.go,in/three.go" {
		t.Fatalf("bound=%v", bound)
	}
	if dropped != 1 {
		t.Fatalf("dropped=%d, want 1", dropped)
	}
}
