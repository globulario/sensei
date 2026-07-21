// SPDX-License-Identifier: AGPL-3.0-only

package architecture

import "testing"

func compactableClaim(object, fact string) Claim {
	return Claim{
		Label: "writer proposition", Statement: ClaimStatement{Subject: "gin.writer", Predicate: "preserves", Object: object},
		Scope:              ClaimScope{Repository: "example.com/gin", Repo: "example.com/gin", Files: []string{"writer.go"}},
		ArchitecturalPlane: PlaneObserved, AssertionOrigin: OriginDerived, EpistemicStatus: StatusSupported,
		InferenceRule: "rule.test.v1", PremiseFacts: []string{fact}, Confidence: .8,
		HumanReviewRequired: true, PromotionStatus: PromotionCandidate,
	}
}

func TestClaimCompactionMergesPremiseFactsForIdenticalPropositionAndScope(t *testing.T) {
	claims, err := CompactClaims([]Claim{compactableClaim("monotonic", "fact.a"), compactableClaim("monotonic", "fact.b")})
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || len(claims[0].PremiseFacts) != 2 {
		t.Fatalf("compacted claims=%+v", claims)
	}
}

func TestClaimCompactionPreservesDistinctObjects(t *testing.T) {
	claims, err := CompactClaims([]Claim{compactableClaim("monotonic", "fact.a"), compactableClaim("resettable", "fact.b")})
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 2 {
		t.Fatalf("claims=%d, want 2", len(claims))
	}
}

func TestClaimCompactionMarksConflictingEvidenceContested(t *testing.T) {
	a := compactableClaim("monotonic", "fact.a")
	a.SupportingEvidence = []string{"evidence:support"}
	b := compactableClaim("monotonic", "fact.b")
	b.EpistemicStatus = StatusRefuted
	b.RefutingEvidence = []string{"evidence:refute"}
	claims, err := CompactClaims([]Claim{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || claims[0].EpistemicStatus != StatusContested {
		t.Fatalf("claims=%+v", claims)
	}
}

func TestClaimCompactionIsDeterministicAndDoesNotTruncate(t *testing.T) {
	input := []Claim{
		compactableClaim("one", "fact.1"), compactableClaim("two", "fact.2"), compactableClaim("three", "fact.3"),
	}
	first, err := CompactClaims(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CompactClaims([]Claim{input[2], input[0], input[1]})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("compaction truncated claims: %d %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("nondeterministic IDs: %v != %v", first, second)
		}
	}
}
