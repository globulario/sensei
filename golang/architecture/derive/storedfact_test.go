// SPDX-License-Identifier: AGPL-3.0-only

package derive

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Only a successful derivation may become durable. UNKNOWN and NOT_DERIVED are
// not weaker forms of a fact.
func TestOnlyADerivedReceiptMayBeAdmitted(t *testing.T) {
	src := pinned(t, map[string]string{"internal/event/bus.go": busGo})
	good, _ := Derive(src, lockProp(), at("2026-08-23T12:00:00Z"))
	if _, err := Admit(good); err != nil {
		t.Fatalf("a derived receipt was refused: %v", err)
	}

	leaky := pinned(t, map[string]string{"internal/event/bus.go": busGoLeaky})
	refuted, _ := Derive(leaky, lockProp(), at("2026-08-23T12:00:00Z"))
	if _, err := Admit(refuted); err == nil {
		t.Fatal("a NOT_DERIVED receipt was admitted")
	}

	p := lockProp()
	p.Kind = "lock_purpose_serialize_map"
	unknown, _ := Derive(src, p, at("2026-08-23T12:00:00Z"))
	if _, err := Admit(unknown); err == nil {
		t.Fatal("an UNKNOWN receipt was admitted; the B specimen must create no durable fact")
	}
}

// Storage remembers what to CHECK, not what is TRUE.
//
// A forged record is therefore harmless as a truth claim: anybody can write the
// bytes, nobody can make the derivation succeed by writing them. The worst a
// fabricated entry achieves is wasting one derivation.
func TestAForgedStoredFactEstablishesNothing(t *testing.T) {
	// Hand-written, never produced by Admit, asserting something false about a
	// field that is genuinely unprotected.
	forged := StoredFact{
		Proposition: Proposition{Kind: KindFieldAccessUnderLock,
			Dir: "internal/event", Type: "Bus", Field: "subs", Lock: "mu"},
		DerivationID: "derive.field_access_under_lock", DerivationVersion: "v1",
		FirstEstablished: Provenance{Repository: "fabricated", Commit: strings.Repeat("a", 40),
			Detail: "I checked this myself and it definitely holds"},
	}
	leaky := pinned(t, map[string]string{"internal/event/bus.go": busGoLeaky})
	receipt, est := forged.Revalidate(leaky, at("2026-08-23T12:00:00Z"))
	if est != nil {
		t.Fatal("a forged record produced an Established")
	}
	if receipt.Outcome != NotDerived {
		t.Fatalf("outcome %s: %s", receipt.Outcome, receipt.Detail)
	}
	// The record's own prose has no effect on the answer.
	if strings.Contains(receipt.Detail, "definitely holds") {
		t.Fatal("the stored record's assertion reached the result")
	}
}

// Supersession needs no engine: a recipe whose re-derivation stops succeeding
// stops producing a fact, because no truth was cached to invalidate.
func TestAStoredFactStopsProducingAFactWhenTheWorldMovesOn(t *testing.T) {
	src := pinned(t, map[string]string{"internal/event/bus.go": busGo})
	dir := gitDirOf(src)
	receipt, _ := Derive(src, lockProp(), at("2026-08-23T12:00:00Z"))
	stored, err := Admit(receipt)
	if err != nil {
		t.Fatal(err)
	}

	// Same world: revalidates.
	if _, est := stored.Revalidate(src, at("2026-08-23T13:00:00Z")); est == nil {
		t.Fatal("a stored fact did not revalidate in the world it was established in")
	}

	// The world moves: the access leaves the lock.
	candidate := commitInto(t, dir, map[string]string{"internal/event/bus.go": busGoLeaky})
	candidateSrc, err := NewGitSource(context.Background(), dir, "example.com/fixture", candidate)
	if err != nil {
		t.Fatal(err)
	}
	r2, est2 := stored.Revalidate(candidateSrc, at("2026-08-23T14:00:00Z"))
	if est2 != nil {
		t.Fatal("a stored fact still produced an Established after the code it described changed")
	}
	if r2.Outcome != NotDerived {
		t.Fatalf("outcome %s", r2.Outcome)
	}
	// And the record itself is unchanged and still useful: it still names the
	// question worth asking here.
	if stored.Proposition.Field != "subs" {
		t.Fatal("the recipe mutated")
	}
}

// The ratchet: what survives is which proposition is worth checking here — the
// judgment-bearing half an investigation produced. Recomputing the answer is
// parsing a package.
func TestWhatSurvivesIsTheQuestionNotTheAnswer(t *testing.T) {
	src := pinned(t, map[string]string{"internal/event/bus.go": busGo})
	receipt, _ := Derive(src, lockProp(), at("2026-08-23T12:00:00Z"))
	stored, err := Admit(receipt)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	var round StoredFact
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(round.Proposition, stored.Proposition) || round.DerivationID != stored.DerivationID {
		t.Fatalf("the recipe did not survive storage: %+v", round)
	}
	// The envelope travels too, so a reader meets the limits with the record.
	if len(round.FirstEstablished.CompletenessScope) == 0 {
		t.Fatal("the completeness scope did not survive serialization — §8f is exactly this boundary")
	}
	// Re-running it in the same world reaches the same answer.
	if _, est := round.Revalidate(src, at("2026-08-23T15:00:00Z")); est == nil {
		t.Fatal("a round-tripped recipe did not revalidate")
	}
}

// A derived fact is descriptive. It ends "Sensei knows nothing here"; it does
// not oblige any future implementation to keep the mutex.
func TestADerivedFactIsNeverNormative(t *testing.T) {
	src := pinned(t, map[string]string{"internal/event/bus.go": busGo})
	receipt, _ := Derive(src, lockProp(), at("2026-08-23T12:00:00Z"))
	stored, err := Admit(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Normative() {
		t.Fatal("a derived fact reported itself normative; replacing the mutex with ownership, " +
			"a channel or an atomic may be entirely correct, and a description has no standing to forbid it")
	}
	if strings.Contains(strings.ToLower(stored.String()), "must") {
		t.Fatalf("the rendering reads as an obligation: %q", stored.String())
	}
}
