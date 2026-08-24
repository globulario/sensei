// SPDX-License-Identifier: AGPL-3.0-only

package derive

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// The forbidden collapse, blocked by the type system rather than by a rule.
//
// A StoredFact is a QUESTION. If holding one were enough to claim coverage,
// "I know what to ask here" would silently become "I know the answer", and a
// fabricated record would close a real gap.
//
// AnchorFor takes an Established, which only Derive returns. A caller holding a
// recipe has nothing to pass it. This test pins the signature, because the
// dangerous version of this feature is one overload away.
func TestCoverageCannotBeAnchoredByARecipe(t *testing.T) {
	fn := reflect.TypeOf(AnchorFor)
	if got := fn.In(0); got != reflect.TypeOf(Established{}) {
		t.Fatalf("AnchorFor takes %v; it must take an Established so a recipe cannot reach it", got)
	}
	for _, f := range []any{AnchorFor, AnchorFromRecipe} {
		ft := reflect.TypeOf(f)
		for i := 0; i < ft.NumIn(); i++ {
			if ft.In(i) == reflect.TypeOf(StoredFact{}) && ft.NumIn() < 3 {
				t.Fatal("a constructor takes a StoredFact without also taking a world to revalidate in")
			}
		}
	}
}

// The legitimate path, end to end.
func TestARecipeAnchorsCoverageOnlyAfterRevalidating(t *testing.T) {
	src := pinned(t, map[string]string{"internal/event/bus.go": busGo})
	receipt, _ := Derive(src, lockProp(), at("2026-08-23T12:00:00Z"))
	stored, err := Admit(receipt)
	if err != nil {
		t.Fatal(err)
	}

	r, anchor := AnchorFromRecipe(stored, src, at("2026-08-23T13:00:00Z"))
	if anchor == nil {
		t.Fatalf("a valid recipe did not anchor after revalidating: %s (%s)", r.Outcome, r.Detail)
	}
	if anchor.World() != src.Commit() {
		t.Fatalf("anchor world %s, source %s", anchor.World(), src.Commit())
	}
	// Coverage must not read stronger than the derivation behind it.
	if !strings.Contains(anchor.Scope(), "WHERE THIS DERIVATION CAN SEE IT") {
		t.Fatalf("coverage dropped the derivation envelope: %q", anchor.Scope())
	}
	if anchor.Receipt().Outcome != Derived {
		t.Fatal("an anchor carries a basis that did not derive")
	}
}

// Attack 1: a forged recipe. Anybody may write one; nobody may make it true.
func TestAForgedRecipeAnchorsNothing(t *testing.T) {
	forged := StoredFact{
		Proposition:  lockProp(),
		DerivationID: "derive.field_access_under_lock", DerivationVersion: "v1",
		FirstEstablished: Provenance{Repository: "fabricated", Commit: strings.Repeat("a", 40),
			Detail: "established, trust me"},
	}
	leaky := pinned(t, map[string]string{"internal/event/bus.go": busGoLeaky})
	r, anchor := AnchorFromRecipe(forged, leaky, at("2026-08-23T12:00:00Z"))
	if anchor != nil {
		t.Fatal("a forged recipe closed a coverage gap")
	}
	if r.Outcome != NotDerived {
		t.Fatalf("outcome %s", r.Outcome)
	}
}

// Attack 2: the B specimen. A purpose claim has no derivation, so it can never
// anchor anything, whatever anybody stores about it.
func TestThePurposeClaimAnchorsNothing(t *testing.T) {
	p := lockProp()
	p.Kind = "lock_purpose_serialize_map"
	recipe := StoredFact{Proposition: p, DerivationID: "whatever", DerivationVersion: "v1"}
	src := pinned(t, map[string]string{"internal/event/bus.go": busGo})
	r, anchor := AnchorFromRecipe(recipe, src, at("2026-08-23T12:00:00Z"))
	if anchor != nil {
		t.Fatal("a purpose claim anchored coverage")
	}
	if r.Outcome != Unknown {
		t.Fatalf("outcome %s, want UNKNOWN", r.Outcome)
	}
}

// Attack 3: a recipe that was genuinely established at the base cannot supply
// coverage for a candidate that broke the discipline.
func TestAValidRecipeCannotCoverACandidateThatBrokeIt(t *testing.T) {
	src := pinned(t, map[string]string{"internal/event/bus.go": busGo})
	dir := gitDirOf(src)
	receipt, _ := Derive(src, lockProp(), at("2026-08-23T12:00:00Z"))
	stored, err := Admit(receipt)
	if err != nil {
		t.Fatal(err)
	}
	// Genuinely anchors at the base.
	if _, a := AnchorFromRecipe(stored, src, at("2026-08-23T12:30:00Z")); a == nil {
		t.Fatal("the base world was not anchored")
	}

	candidate := commitInto(t, dir, map[string]string{"internal/event/bus.go": busGoLeaky})
	candidateSrc, err := NewGitSource(context.Background(), dir, "example.com/fixture", candidate)
	if err != nil {
		t.Fatal(err)
	}
	r, a := AnchorFromRecipe(stored, candidateSrc, at("2026-08-23T14:00:00Z"))
	if a != nil {
		t.Fatal("a fact established at the base anchored coverage for a candidate that broke it")
	}
	if r.Outcome != NotDerived {
		t.Fatalf("outcome %s", r.Outcome)
	}
}

// An Established from one world cannot be re-pointed at another by hand.
func TestAnAnchorCannotClaimAWorldItWasNotDerivedIn(t *testing.T) {
	src := pinned(t, map[string]string{"internal/event/bus.go": busGo})
	_, est := Derive(src, lockProp(), at("2026-08-23T12:00:00Z"))
	if est == nil {
		t.Fatal("fixture did not derive")
	}
	if _, err := AnchorFor(*est, strings.Repeat("b", 40)); err == nil {
		t.Fatal("a fact anchored coverage for a world it was never derived in")
	}
	if _, err := AnchorFor(*est, src.Commit()); err != nil {
		t.Fatalf("its own world was refused: %v", err)
	}
}

// CoverageAnchor is constructible only through this package, like Established.
func TestCoverageAnchorHasNoExportedFields(t *testing.T) {
	rt := reflect.TypeOf(CoverageAnchor{})
	for i := 0; i < rt.NumField(); i++ {
		if rt.Field(i).IsExported() {
			t.Fatalf("CoverageAnchor.%s is exported; the anchor must be obtainable only from a "+
				"successful derivation in the world being assessed", rt.Field(i).Name)
		}
	}
}
