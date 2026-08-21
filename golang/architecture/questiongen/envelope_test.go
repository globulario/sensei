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

// The case the fix would otherwise miss: a task that ALREADY carries the wide
// question. Coverage alone leaves it standing, still naming files the task
// never admitted.
//
// It is narrowed IN PLACE, because it is the same question:
// StableOpenQuestionID derives identity from repository, domain, dimension,
// text, claims, template, nodes and blockers — not from the file scope. So a
// re-scoped question has the same ID, and superseding it would invent a second
// question with the identity of the first.
func TestAWideExistingQuestionIsNarrowedInPlace(t *testing.T) {
	claim := architecture.Claim{
		ID: "claim.wide", ArchitecturalPlane: architecture.PlaneObserved,
		AssertionOrigin: architecture.OriginDerived, PremiseFacts: []string{"fact.wide"},
		EpistemicStatus: architecture.StatusUnknown,
		Statement:       architecture.ClaimStatement{Subject: "cluster/state", Predicate: "has_observed_writer_set", Object: "many"},
		Scope:           architecture.ClaimScope{Files: []string{"internal/doctor/doctor.go", "internal/architect/debt.go"}},
	}
	blocker := closure.Blocker{
		ID: "blocker.evidence.abcdef012345", Dimension: closure.DimensionEvidence,
		Severity: architecture.QuestionPriorityHigh, Code: "closure.evidence.claim_unknown",
		Summary: "Claim has no evidence.", ClaimIDs: []string{claim.ID},
		Files: []string{"internal/doctor/doctor.go"}, RequiredNextAction: "add_evidence",
	}

	// The earlier run had no envelope, so the question took the full union.
	wide := questionGenContext()
	wide.Claims.Claims = []architecture.Claim{claim}
	wide.Closure.Blockers = []closure.Blocker{blocker}
	wide.Closure.ScopeReceipt.Files = nil
	first, err := Generate(wide, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Dialogue.OpenQuestions) != 1 {
		t.Fatalf("setup: questions=%d, want 1", len(first.Dialogue.OpenQuestions))
	}
	original := first.Dialogue.OpenQuestions[0]
	if len(original.Scope.Files) < 2 {
		t.Fatalf("setup: the wide question named %v", original.Scope.Files)
	}

	// The task now runs with an envelope admitting one of those files.
	bounded := questionGenContext()
	bounded.Claims.Claims = []architecture.Claim{claim}
	bounded.Closure.Blockers = []closure.Blocker{blocker}
	bounded.Closure.ScopeReceipt.Files = []string{"internal/doctor/doctor.go"}
	bounded.Existing = &first.Dialogue

	res, err := Generate(bounded, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Dialogue.OpenQuestions) != 1 {
		t.Fatalf("questions=%d, want the same one narrowed rather than a second", len(res.Dialogue.OpenQuestions))
	}
	q := res.Dialogue.OpenQuestions[0]
	if q.ID != original.ID {
		t.Fatalf("the question's identity changed from %s to %s; narrowing is not a new question", original.ID, q.ID)
	}
	for _, f := range q.Scope.Files {
		if f != "internal/doctor/doctor.go" {
			t.Errorf("the question still names %q, which this task never admitted", f)
		}
	}
	var said bool
	for _, r := range q.ReasonsOpen {
		if strings.Contains(r, "outside it") {
			said = true
		}
	}
	if !said {
		t.Fatalf("nothing records what the narrowing removed: %v", q.ReasonsOpen)
	}
}

// A question already inside the envelope is left exactly as it is: churning a
// question per convergence iteration would be its own defect.
func TestAQuestionAlreadyInsideTheEnvelopeIsNotRewrittenEachRun(t *testing.T) {
	ctx := questionGenContext()
	claim := architecture.Claim{
		ID: "claim.narrow", ArchitecturalPlane: architecture.PlaneObserved,
		AssertionOrigin: architecture.OriginDerived, PremiseFacts: []string{"fact.narrow"},
		EpistemicStatus: architecture.StatusUnknown,
		Statement:       architecture.ClaimStatement{Subject: "cluster/state", Predicate: "has_observed_writer_set", Object: "one"},
		Scope:           architecture.ClaimScope{Files: []string{"config.go"}},
	}
	blocker := closure.Blocker{
		ID: "blocker.evidence.abcdef012345", Dimension: closure.DimensionEvidence,
		Severity: architecture.QuestionPriorityHigh, Code: "closure.evidence.claim_unknown",
		Summary: "Claim has no evidence.", ClaimIDs: []string{claim.ID},
		Files: []string{"config.go"}, RequiredNextAction: "add_evidence",
	}
	ctx.Claims.Claims = []architecture.Claim{claim}
	ctx.Closure.Blockers = []closure.Blocker{blocker}
	ctx.Closure.ScopeReceipt.Files = []string{"config.go"}

	first, err := Generate(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx.Existing = &first.Dialogue
	second, err := Generate(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Dialogue.OpenQuestions) != len(first.Dialogue.OpenQuestions) {
		t.Fatalf("questions grew from %d to %d on an unchanged run", len(first.Dialogue.OpenQuestions), len(second.Dialogue.OpenQuestions))
	}
	for _, q := range second.Dialogue.OpenQuestions {
		if len(q.ReasonsOpen) != len(first.Dialogue.OpenQuestions[0].ReasonsOpen) {
			t.Fatalf("an in-envelope question was rewritten on an unchanged run: %v", q.ReasonsOpen)
		}
	}
}
