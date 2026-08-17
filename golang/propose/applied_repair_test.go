// SPDX-License-Identifier: AGPL-3.0-only

package propose

import (
	"strings"
	"testing"
)

func completeAppliedRepair() Request {
	return Request{
		Kind:             "applied_repair",
		Title:            "Verify the composed document parses before the rename",
		Description:      "Added a parse check before the atomic rename.",
		RelatedFailures:  []string{"failure.governed_append_corrupted_a_scaffolded_marker"},
		RequiredTests:    []string{"golang/architecture/governedmutation/apply_test.go:TestFirstAppendStaysValid"},
		SourceFiles:      []string{"golang/architecture/governedmutation/apply.go"},
		SurvivalEvidence: []string{"the guard caught a bug in its own change"},
	}
}

func TestCompleteAppliedRepairIsAccepted(t *testing.T) {
	r := completeAppliedRepair()
	Normalize(&r)
	if errs := Validate(r); len(errs) != 0 {
		t.Fatalf("a complete applied_repair was rejected: %v", errs)
	}
}

// Each requirement exists to keep the record from degrading into a changelog
// entry. Dropping any one must be refused, with a reason that says why.
func TestEachAppliedRepairRequirementIsEnforced(t *testing.T) {
	cases := map[string]struct {
		mutate func(*Request)
		want   string
	}{
		"no failure":           {func(r *Request) { r.RelatedFailures = nil }, "anecdote"},
		"no test":              {func(r *Request) { r.RequiredTests = nil }, "unproven"},
		"no source file":       {func(r *Request) { r.SourceFiles = nil }, "context-bound"},
		"no description":       {func(r *Request) { r.Description = "" }, "what the repair actually did"},
		"no survival evidence": {func(r *Request) { r.SurvivalEvidence = nil }, "HELD"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := completeAppliedRepair()
			tc.mutate(&r)
			Normalize(&r)
			errs := Validate(r)
			if len(errs) == 0 {
				t.Fatalf("%s was accepted", name)
			}
			joined := strings.Join(errs, "; ")
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("refusal does not explain itself (want %q): %s", tc.want, joined)
			}
		})
	}
}

// survival_evidence is a DISTINCT field from evidence. "we applied this" and "it
// held" are different claims, and only the second makes the record advice rather
// than a changelog entry — so evidence must not satisfy the survival requirement.
func TestEvidenceDoesNotSatisfySurvivalEvidence(t *testing.T) {
	r := completeAppliedRepair()
	r.SurvivalEvidence = nil
	r.Evidence = []string{"we applied the change and the build passed"}
	Normalize(&r)

	errs := Validate(r)
	if len(errs) == 0 {
		t.Fatal("generic evidence was accepted in place of survival evidence")
	}
}

// The kind must be advertised, or agents cannot discover it.
func TestAppliedRepairIsAdvertisedAsAKind(t *testing.T) {
	var found bool
	for _, k := range Kinds() {
		if k == "applied_repair" {
			found = true
		}
	}
	if !found {
		t.Fatalf("applied_repair missing from Kinds(): %v", Kinds())
	}
	// And the "kind is required" message must name it, since that message is
	// what an agent sees when it guesses wrong.
	if errs := Validate(Request{}); len(errs) == 0 || !strings.Contains(errs[0], "applied_repair") {
		t.Fatalf("the kind-required message does not mention applied_repair: %v", errs)
	}
}

// It rides the same contract-first rule as every other kind: related_failure
// satisfies the general requirement, so the two rules do not contradict.
func TestAppliedRepairSatisfiesContractFirstViaRelatedFailure(t *testing.T) {
	r := completeAppliedRepair()
	r.Contract = ""
	r.RelatedInvariants = nil
	Normalize(&r)

	for _, e := range Validate(r) {
		if strings.Contains(e, "contract-first") {
			t.Fatalf("related_failure should satisfy contract-first: %s", e)
		}
	}
}

// The survival evidence must survive normalization into the candidate document —
// a required field that is silently dropped on the way to disk would make the
// requirement theatre.
func TestSurvivalEvidenceReachesTheCandidate(t *testing.T) {
	r := completeAppliedRepair()
	r.SurvivalEvidence = []string{"  held across two releases  ", "", "second line"}
	Normalize(&r)
	if len(r.SurvivalEvidence) != 2 || r.SurvivalEvidence[0] != "held across two releases" {
		t.Fatalf("normalization mangled survival evidence: %#v", r.SurvivalEvidence)
	}

	cand, err := RenderCandidate(r)
	if err != nil {
		t.Fatalf("RenderCandidate: %v", err)
	}
	body := string(cand.Content)
	for _, want := range []string{"survival_evidence", "held across two releases", "second line"} {
		if !strings.Contains(body, want) {
			t.Fatalf("candidate is missing %q:\n%s", want, body)
		}
	}
}
