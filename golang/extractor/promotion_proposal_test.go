// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import "testing"

func TestPromotionProposalGeneratedFromRepeatedOutcome(t *testing.T) {
	signals := []OutcomeSignal{
		{OutcomeID: "outcome.a", Theme: "repository.installability_stuck", Status: "success", SourcePath: "docs/awareness/outcomes/a.yaml"},
		{OutcomeID: "outcome.b", Theme: "repository.installability_stuck", Status: "success", SourcePath: "docs/awareness/outcomes/b.yaml"},
		// A lone theme with a single outcome — not enough support, no proposal.
		{OutcomeID: "outcome.c", Theme: "rare.one_off", Status: "success", SourcePath: "docs/awareness/outcomes/c.yaml"},
	}
	props := GeneratePromotionProposals(signals, CandidateInvariant)
	if len(props) != 1 {
		t.Fatalf("expected 1 proposal from the repeated theme, got %d: %v", len(props), props)
	}
	if props[0].Theme != "repository.installability_stuck" {
		t.Errorf("proposal theme = %q", props[0].Theme)
	}
	if len(props[0].SupportingOutcomes) != 2 {
		t.Errorf("expected 2 supporting outcomes, got %d", len(props[0].SupportingOutcomes))
	}
}

func TestPromotionDoesNotAutoActivate(t *testing.T) {
	signals := []OutcomeSignal{
		{OutcomeID: "outcome.a", Theme: "t", SourcePath: "a.yaml"},
		{OutcomeID: "outcome.b", Theme: "t", SourcePath: "b.yaml"},
	}
	for _, p := range GeneratePromotionProposals(signals, CandidateInvariant) {
		if p.Status != proposalStatusCandidate {
			t.Errorf("generated proposal must be a candidate, got status %q", p.Status)
		}
	}
}

// A fully-supported, well-formed proposal is eligible.
func goodProposal() PromotionProposal {
	return PromotionProposal{
		CandidateID:        "candidate.good",
		CandidateClass:     CandidateInvariant,
		Status:             proposalStatusCandidate,
		SupportingOutcomes: []string{"outcome.a", "outcome.b"},
		AuthorityDomain:    "authority.repository_artifact_metadata",
		ActivationTrigger:  "editing the repository publish path",
		RequiredTests:      []string{"test:TestRepositoryInstallability"},
		Confidence:         "high",
		SourcePaths:        []string{"docs/awareness/outcomes/a.yaml"},
	}
}

func TestPromotionEligibility_GoodProposalPasses(t *testing.T) {
	ok, blockers := EvaluatePromotionEligibility(goodProposal(), false)
	if !ok {
		t.Errorf("good proposal should be eligible, blockers: %v", blockers)
	}
}

func TestPromotionBlockedByContradiction(t *testing.T) {
	ok, blockers := EvaluatePromotionEligibility(goodProposal(), true)
	if ok {
		t.Error("a proposal must not be eligible when an active contradiction exists")
	}
	if !containsSubstr(blockers, "contradiction") {
		t.Errorf("expected a contradiction blocker, got %v", blockers)
	}
}

func TestPromotionRequiresTestsOrReason(t *testing.T) {
	p := goodProposal()
	p.RequiredTests = nil
	p.Reason = ""
	ok, blockers := EvaluatePromotionEligibility(p, false)
	if ok {
		t.Error("a proposal with no tests and no reason must not be eligible")
	}
	if !containsSubstr(blockers, "tests or an explicit reason") {
		t.Errorf("expected a tests-or-reason blocker, got %v", blockers)
	}

	// Supplying an explicit reason satisfies the gate.
	p.Reason = "not unit-testable; verified by the convergence reconciler in production"
	if ok, blockers := EvaluatePromotionEligibility(p, false); !ok {
		t.Errorf("an explicit reason should satisfy the tests gate, blockers: %v", blockers)
	}
}

func containsSubstr(list []string, sub string) bool {
	for _, s := range list {
		if len(sub) == 0 {
			return true
		}
		if indexOf(s, sub) >= 0 {
			return true
		}
	}
	return false
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
