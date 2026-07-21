// SPDX-License-Identifier: AGPL-3.0-only

package architecture

import "testing"

func TestClaimDocumentRejectsDirectDependencyCycle(t *testing.T) {
	doc := twoClaimDocument()
	doc.Claims[0].DependsOnClaims = []string{doc.Claims[1].ID}
	doc.Claims[1].DependsOnClaims = []string{doc.Claims[0].ID}
	if err := ValidateClaimDocument(doc); err == nil {
		t.Fatal("expected dependency cycle rejection")
	}
}

func TestClaimDocumentRejectsTransitiveDependencyCycle(t *testing.T) {
	doc := threeClaimDocument()
	doc.Claims[0].DependsOnClaims = []string{doc.Claims[1].ID}
	doc.Claims[1].DependsOnClaims = []string{doc.Claims[2].ID}
	doc.Claims[2].DependsOnClaims = []string{doc.Claims[0].ID}
	if err := ValidateClaimDocument(doc); err == nil {
		t.Fatal("expected transitive dependency cycle rejection")
	}
}

func TestClaimDocumentRejectsDirectSupersessionCycle(t *testing.T) {
	doc := twoClaimDocument()
	doc.Claims[0].EpistemicStatus = StatusSuperseded
	doc.Claims[0].SupersededBy = doc.Claims[1].ID
	doc.Claims[1].EpistemicStatus = StatusSuperseded
	doc.Claims[1].SupersededBy = doc.Claims[0].ID
	if err := ValidateClaimDocument(doc); err == nil {
		t.Fatal("expected supersession cycle rejection")
	}
}

func TestClaimDocumentRejectsTransitiveSupersessionCycle(t *testing.T) {
	doc := threeClaimDocument()
	for i := range doc.Claims {
		doc.Claims[i].EpistemicStatus = StatusSuperseded
	}
	doc.Claims[0].SupersededBy = doc.Claims[1].ID
	doc.Claims[1].SupersededBy = doc.Claims[2].ID
	doc.Claims[2].SupersededBy = doc.Claims[0].ID
	if err := ValidateClaimDocument(doc); err == nil {
		t.Fatal("expected transitive supersession cycle rejection")
	}
}

func TestAcyclicDependencyChainIsValid(t *testing.T) {
	doc := threeClaimDocument()
	doc.Claims[1].DependsOnClaims = []string{doc.Claims[0].ID}
	doc.Claims[2].DependsOnClaims = []string{doc.Claims[1].ID}
	if err := ValidateClaimDocument(doc); err != nil {
		t.Fatalf("acyclic dependencies rejected: %v", err)
	}
}

func TestAcyclicSupersessionChainIsValid(t *testing.T) {
	doc := threeClaimDocument()
	doc.Claims[0].EpistemicStatus = StatusSuperseded
	doc.Claims[0].SupersededBy = doc.Claims[1].ID
	doc.Claims[1].EpistemicStatus = StatusSuperseded
	doc.Claims[1].SupersededBy = doc.Claims[2].ID
	if err := ValidateClaimDocument(doc); err != nil {
		t.Fatalf("acyclic supersession rejected: %v", err)
	}
}

func twoClaimDocument() ClaimDocument {
	doc := validClaimDocument()
	secondFact := doc.FactReceipts[0]
	secondFact.Fact.ID = "fact.456"
	secondFact.Fact.Subject = "repository.Verify"
	secondFact.Fact.Object = "package_identity"
	second := doc.Claims[0]
	second.ID = "claim.second"
	second.Statement.Subject = "repository.Verify"
	second.PremiseFacts = []string{secondFact.Fact.ID}
	doc.FactReceipts = append(doc.FactReceipts, secondFact)
	doc.Claims = append(doc.Claims, second)
	return doc
}

func threeClaimDocument() ClaimDocument {
	doc := twoClaimDocument()
	thirdFact := doc.FactReceipts[0]
	thirdFact.Fact.ID = "fact.789"
	thirdFact.Fact.Subject = "repository.Audit"
	third := doc.Claims[0]
	third.ID = "claim.third"
	third.Statement.Subject = "repository.Audit"
	third.PremiseFacts = []string{thirdFact.Fact.ID}
	doc.FactReceipts = append(doc.FactReceipts, thirdFact)
	doc.Claims = append(doc.Claims, third)
	return doc
}
