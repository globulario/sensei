// SPDX-License-Identifier: Apache-2.0

package architecture

import "testing"

func TestClaimIDIsDeterministic(t *testing.T) {
	a := StableClaimID(validClaim())
	b := StableClaimID(validClaim())
	if a != b {
		t.Fatalf("ids differ: %s != %s", a, b)
	}
}

func TestClaimIDIgnoresEpistemicStatus(t *testing.T) {
	a := validClaim()
	b := validClaim()
	b.EpistemicStatus = StatusContested
	if StableClaimID(a) != StableClaimID(b) {
		t.Fatal("epistemic status changed claim id")
	}
}

func TestClaimIDIgnoresConfidence(t *testing.T) {
	a := validClaim()
	b := validClaim()
	b.Confidence = 0.1
	if StableClaimID(a) != StableClaimID(b) {
		t.Fatal("confidence changed claim id")
	}
}

func TestClaimIDIgnoresValidationTime(t *testing.T) {
	a := validClaim()
	b := validClaim()
	b.LastValidatedAt = "2026-07-13T00:00:00Z"
	if StableClaimID(a) != StableClaimID(b) {
		t.Fatal("validation time changed claim id")
	}
}

func TestNormalizeClaimsSortsAndDeduplicatesLists(t *testing.T) {
	c := validClaim()
	c.Scope.Files = []string{"b.go", "a.go", "a.go", `dir\c.go`}
	c.Scope.Symbols = []string{"Z", "A", "A"}
	out, err := NormalizeClaims([]Claim{c})
	if err != nil {
		t.Fatal(err)
	}
	if got := out[0].Scope.Files; len(got) != 3 || got[0] != "a.go" || got[2] != "dir/c.go" {
		t.Fatalf("files not canonical: %#v", got)
	}
	if got := out[0].Scope.Symbols; len(got) != 2 || got[0] != "A" || got[1] != "Z" {
		t.Fatalf("symbols not canonical: %#v", got)
	}
}

func TestNormalizeClaimsDeduplicatesIdenticalClaim(t *testing.T) {
	c := validClaim()
	out, err := NormalizeClaims([]Claim{c, c})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("len=%d, want 1", len(out))
	}
}

func TestNormalizeClaimsRejectsIDDivergence(t *testing.T) {
	a := validClaim()
	b := validClaim()
	a.ID = "claim.collision"
	b.ID = "claim.collision"
	b.Statement.Object = "other_state"
	if _, err := NormalizeClaims([]Claim{a, b}); err == nil {
		t.Fatal("expected id collision error")
	}
}

func TestValidateClaimRejectsMissingStatement(t *testing.T) {
	c := validClaim()
	c.Statement.Subject = ""
	if err := ValidateClaim(c); err == nil {
		t.Fatal("expected missing statement error")
	}
}

func TestValidateClaimRejectsArbitraryPredicate(t *testing.T) {
	c := validClaim()
	c.Statement.Predicate = "owns state; <bad>"
	if err := ValidateClaim(c); err == nil {
		t.Fatal("expected arbitrary predicate error")
	}
}

func TestValidateClaimRejectsUnknownPlane(t *testing.T) {
	c := validClaim()
	c.ArchitecturalPlane = "runtime"
	if err := ValidateClaim(c); err == nil {
		t.Fatal("expected unknown plane error")
	}
}

func TestValidateClaimRejectsUnknownOrigin(t *testing.T) {
	c := validClaim()
	c.AssertionOrigin = "invented"
	if err := ValidateClaim(c); err == nil {
		t.Fatal("expected unknown origin error")
	}
}

func TestValidateClaimRejectsUnknownEpistemicStatus(t *testing.T) {
	c := validClaim()
	c.EpistemicStatus = "active"
	if err := ValidateClaim(c); err == nil {
		t.Fatal("expected unknown status error")
	}
}

func TestValidateClaimRejectsNonCandidatePromotion(t *testing.T) {
	c := validClaim()
	c.PromotionStatus = "active"
	if err := ValidateClaim(c); err == nil {
		t.Fatal("expected non-candidate promotion error")
	}
}

func TestValidateClaimRequiresHumanReview(t *testing.T) {
	c := validClaim()
	c.HumanReviewRequired = false
	if err := ValidateClaim(c); err == nil {
		t.Fatal("expected human review error")
	}
}

func TestValidateClaimRejectsSelfDependency(t *testing.T) {
	c := validClaim()
	c.ID = "claim.self"
	c.DependsOnClaims = []string{"claim.self"}
	if err := ValidateClaim(c); err == nil {
		t.Fatal("expected self dependency error")
	}
}

func TestValidateClaimRejectsAbsoluteOrEscapingPath(t *testing.T) {
	for _, path := range []string{"/abs.go", "../x.go", "a/../x.go"} {
		c := validClaim()
		c.Scope.Files = []string{path}
		if err := ValidateClaim(c); err == nil {
			t.Fatalf("expected invalid path %q", path)
		}
	}
}

func validClaim() Claim {
	return Claim{
		ID:          "claim.valid",
		Label:       "Valid claim",
		Description: "A non-authoritative proposition.",
		Statement: ClaimStatement{
			Subject:   "repository.Publish",
			Predicate: "mutates_state",
			Object:    "package_identity",
		},
		Scope: ClaimScope{
			Repository: "github.com/example/project",
			Domain:     "repo",
			Files:      []string{"repository.go"},
			Symbols:    []string{"repository.Publish"},
		},
		ArchitecturalPlane:     PlaneObserved,
		AssertionOrigin:        OriginDerived,
		EpistemicStatus:        StatusSupported,
		InferenceRule:          "rule.direct_observation_projection",
		PremiseFacts:           []string{"fact.123"},
		InvalidationConditions: []string{"source digest changes"},
		Confidence:             0.55,
		Freshness:              "current",
		HumanReviewRequired:    true,
		PromotionStatus:        PromotionCandidate,
	}
}
