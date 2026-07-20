// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

func TestAttention_DeterministicIdentityAndOrder(t *testing.T) {
	reg := DefaultRegistry()
	b := satisfiedBundle(reg, rdf.ClassContract)
	b.Contradiction.Findings = []ContradictionObservation{{Identity: "contra.1", Relevant: true}}
	b.Dimensions["enforcement"] = DimensionObservation{Dimension: "enforcement", SourceOwner: "closure.enforcement", SourceSchema: "dim", SourceIdentity: "s.en", SourceAvailability: SourceAvailable, Outcome: OutcomeDefinitiveBlocker, BlockerIDs: []string{"b1"}}
	id, res := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	st1, err := BuildArtifactState(reg, id, res, b)
	if err != nil {
		t.Fatal(err)
	}
	st2, _ := BuildArtifactState(reg, id, res, b)
	if st1.DigestSHA256 != st2.DigestSHA256 {
		t.Fatal("artifact state (incl. attention) is not deterministic")
	}
	if len(st1.Attention) < 2 || st1.Attention[0].Severity != SeverityCritical {
		t.Fatalf("critical (contradiction) must sort first: %+v", st1.Attention)
	}
	for _, a := range st1.Attention {
		if err := validateAttentionItem(a); err != nil {
			t.Fatalf("attention id not canonical: %v", err)
		}
	}
}

func TestAttention_CriticalNeverDowngraded(t *testing.T) {
	for _, class := range []string{AttnContradictionPresent, AttnGraphAuthorityInvalid, AttnProvenanceIntegrity, AttnSeedVerification} {
		if sev, _ := severityForClass(class, false); sev != SeverityCritical {
			t.Fatalf("%s must be critical, got %q", class, sev)
		}
	}
	if sev, _ := severityForClass(AttnCoverageBlindSpot, true); sev != SeverityCritical {
		t.Fatalf("high-risk blind spot must be critical, got %q", sev)
	}
	if sev, _ := severityForClass(AttnCoverageBlindSpot, false); sev != SeverityWarning {
		t.Fatalf("ordinary blind spot must be warning, got %q", sev)
	}
}

func TestAttention_UnknownClassificationVisibleFailClosed(t *testing.T) {
	sev, basis := severityForClass("some_unmapped_condition", false)
	if sev != SeverityWarning || basis != "governed_mapping:unknown_classification" {
		t.Fatalf("unknown classification must fail closed to warning with a typed basis, got %q/%q", sev, basis)
	}
}

func TestAttention_DeterministicDedup(t *testing.T) {
	a, _ := newAttention("o", "s", "id1", "", AttnEnforcementMissing, "r", SeverityWarning, "b", []string{"x"}, true, nil, "architect", false)
	out := dedupSortAttention([]AttentionItem{a, a, a})
	if len(out) != 1 {
		t.Fatalf("duplicate attention items must dedup to 1, got %d", len(out))
	}
}

// Every frozen attention family has an executable typed construction path.
func TestAttention_AllFamiliesConstructable(t *testing.T) {
	aff := []string{"aw:x"}
	if a, ok, err := AttentionForGraphAuthority("auth", "", true, false, true, aff); err != nil || !ok || a.Severity != SeverityWarning {
		t.Fatalf("graph authority stale (unavailable) adapter: ok=%v sev=%q err=%v", ok, a.Severity, err)
	}
	if a, ok, err := AttentionForGraphAuthority("auth", "", true, true, false, aff); err != nil || !ok || a.Severity != SeverityCritical {
		t.Fatalf("graph authority integrity adapter must be critical: ok=%v sev=%q err=%v", ok, a.Severity, err)
	}
	must := func(a AttentionItem, err error) AttentionItem {
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	must(AttentionForContradiction(ContradictionSource{Owner: "extractor.contradiction", Schema: "contradiction"}, ContradictionObservation{Identity: "c1", Relevant: true}, aff))
	must(AttentionForDimensionBlocker("enforcement", DimensionObservation{SourceOwner: "closure.enforcement", SourceSchema: "dim", SourceIdentity: "s"}, aff))
	must(AttentionForOpenQuestion("q1", aff))
	must(AttentionForBlindSpot("coverage", "bs1", true, aff))
	must(AttentionForProvenanceIntegrity("questionpromotion", "p1", "d", aff))
	// Forbidden move preserves source severity + never downgrades critical.
	fm, limits, err := AttentionForForbiddenMove("editcheck", "forbidden_move", "fm1", "", SeverityCritical, aff, nil, "architect")
	if err != nil || fm.Severity != SeverityCritical || len(limits) != 0 {
		t.Fatalf("forbidden move must preserve critical: sev=%q limits=%v err=%v", fm.Severity, limits, err)
	}
	// Unknown source severity → warning + a typed limitation.
	fm2, limits2, _ := AttentionForForbiddenMove("editcheck", "forbidden_move", "fm2", "", AttentionSeverity("bogus"), aff, nil, "architect")
	if fm2.Severity != SeverityWarning || len(limits2) == 0 {
		t.Fatalf("unknown source severity → warning + limitation: sev=%q limits=%v", fm2.Severity, limits2)
	}
}

// Dimension attention preserves the underlying dimension source provenance (not synthetic).
func TestAttention_DimensionPreservesSourceProvenance(t *testing.T) {
	obs := DimensionObservation{SourceOwner: "closure.enforcement", SourceSchema: "dim-schema", SourceIdentity: "src.42", SourceDigest: "dig.42"}
	a, err := AttentionForDimensionBlocker("enforcement", obs, []string{"aw:x"})
	if err != nil {
		t.Fatal(err)
	}
	if a.SourceOwner != "closure.enforcement" || a.SourceSchema != "dim-schema" || a.SourceIdentity != "src.42" || a.SourceDigest != "dig.42" {
		t.Fatalf("dimension attention must preserve source provenance, got %+v", a)
	}
}
