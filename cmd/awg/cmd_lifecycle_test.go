// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

// Regression for the self-coherence defect this lane fixed: `awg lifecycle`'s
// tier tally had no `declaration` case, so the 7 architectural-declaration-backed
// principles (a VALID listed tier, accepted by TestMetaPrincipleCoverage) were
// mis-reported as UNCLASSIFIED. declaration must be classified, never UNCLASSIFIED.
func TestTallyCoverageTiers_DeclarationIsClassifiedNotUnclassified(t *testing.T) {
	got := tallyCoverageTiers([]coveragePrinciple{
		{Principle: "meta.a", Tier: "behavioral"},
		{Principle: "meta.b", Tier: "declaration"},
		{Principle: "meta.c", Tier: "planned", IntendedTier: "behavioral"},
		{Principle: "meta.d", Tier: "review_only"},
	})
	if len(got.Declaration) != 1 || got.Declaration[0] != "meta.b" {
		t.Fatalf("declaration not classified: Declaration=%v", got.Declaration)
	}
	if len(got.Other) != 0 {
		t.Fatalf("declaration is a valid LISTED tier and must NOT be UNCLASSIFIED; got Other=%v", got.Other)
	}
	if len(got.Behavioral) != 1 || len(got.Planned) != 1 || len(got.ReviewOnly) != 1 {
		t.Fatalf("tier counts wrong: behavioral=%v planned=%v review_only=%v",
			got.Behavioral, got.Planned, got.ReviewOnly)
	}
	if len(got.Conflicts) != 0 {
		t.Fatalf("no duplicates expected; got Conflicts=%v", got.Conflicts)
	}
}

// A genuinely unknown tier MUST still surface as UNCLASSIFIED (the check that
// catches a real coverage typo — we fixed the false positive without blinding it).
func TestTallyCoverageTiers_UnknownTierStaysUnclassified(t *testing.T) {
	got := tallyCoverageTiers([]coveragePrinciple{{Principle: "meta.x", Tier: "bogus"}})
	if len(got.Other) != 1 {
		t.Fatalf("unknown tier must be UNCLASSIFIED; got Other=%v", got.Other)
	}
}

// A principle listed more than once is a self-coherence conflict (directive #7),
// counted once, with the conflict reported.
func TestTallyCoverageTiers_DuplicateIsConflictCountedOnce(t *testing.T) {
	got := tallyCoverageTiers([]coveragePrinciple{
		{Principle: "meta.dup", Tier: "behavioral"},
		{Principle: "meta.dup", Tier: "review_only"},
	})
	if len(got.Conflicts) != 1 {
		t.Fatalf("duplicate principle must be flagged as a conflict; got Conflicts=%v", got.Conflicts)
	}
	if len(got.Behavioral) != 1 || len(got.ReviewOnly) != 0 {
		t.Fatalf("duplicate must be counted once (first wins); behavioral=%v review_only=%v",
			got.Behavioral, got.ReviewOnly)
	}
}
