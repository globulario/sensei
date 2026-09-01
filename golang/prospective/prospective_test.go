// SPDX-License-Identifier: AGPL-3.0-only

package prospective

import "testing"

// The constitutional line: resemblance may not become authority, and no
// accumulation of it does.
func TestResemblanceNeverBecomesAuthority(t *testing.T) {
	if BasisResemblance.AuthorityEligible() {
		t.Fatal("resemblance was made authority-eligible")
	}

	// A caller must not be able to CLAIM a basis it did not earn. With an
	// exported field, Candidate{Basis: BasisEstablished} -- or a deserialized
	// one -- would pass AuthorityEligibleOnly with no governed relationship
	// behind it, and the constitutional line would be a struct literal away
	// from being forged.
	forged := Candidate{ID: "forged", Class: "invariant"}
	if forged.AuthorityEligible() {
		t.Fatal("a candidate constructed outside Retrieve claimed authority eligibility")
	}
	if forged.Basis() == BasisEstablished {
		t.Fatal("the zero value of a candidate is an established relationship")
	}
	if !BasisEstablished.AuthorityEligible() {
		t.Fatal("an established relationship is not authority-eligible; the package would return nothing usable")
	}

	// A hundred sibling matches are still guidance. There is no threshold at
	// which enough resemblance becomes a relationship.
	var many []Candidate
	for i := 0; i < 100; i++ {
		many = append(many, Candidate{ID: string(rune('a'+i%26)) + "x", basis: BasisResemblance, Signal: SignalSameDirectory})
	}
	if got := AuthorityEligibleOnly(many); len(got) != 0 {
		t.Fatalf("%d resemblance candidates produced %d authority-eligible ones", len(many), len(got))
	}
}

// A shared authority domain is a relationship the graph already holds. A
// sibling in the same directory is not.
func TestTheTwoBasesAreDistinguishedByRelationshipNotSimilarity(t *testing.T) {
	anchors := []Anchor{
		{ID: "inv.related", Class: "invariant", Files: []string{"golang/other/thing.go"}, Domains: []string{"authority.workflow"}},
		{ID: "inv.sibling", Class: "invariant", Files: []string{"golang/server/neighbour.go"}},
	}
	got := Retrieve(Subject{Files: []string{"golang/server/subject.go"}}, anchors, []string{"authority.workflow"})
	if len(got) != 2 {
		t.Fatalf("expected both candidates, got %+v", got)
	}
	byID := map[string]Candidate{}
	for _, c := range got {
		byID[c.ID] = c
	}
	if b := byID["inv.related"].Basis(); b != BasisEstablished {
		t.Errorf("a shared authority domain produced basis %q", b)
	}
	if b := byID["inv.sibling"].Basis(); b != BasisResemblance {
		t.Errorf("a directory sibling produced basis %q — proximity is not a relationship", b)
	}
	if eligible := AuthorityEligibleOnly(got); len(eligible) != 1 || eligible[0].ID != "inv.related" {
		t.Errorf("authority-eligible set is wrong: %+v", eligible)
	}
	// Ordering must not smuggle authority in: established first is a reading
	// convenience, and the basis still decides.
	if got[0].Basis() != BasisEstablished {
		t.Error("established candidates are not surfaced first")
	}
}

// Prospective retrieval must not return what the caller already had. Counting
// direct anchors would inflate recall with answers the direct lookup supplied.
func TestADirectAnchorIsNotAProspectiveCandidate(t *testing.T) {
	anchors := []Anchor{
		{ID: "inv.direct", Class: "invariant", Files: []string{"golang/server/subject.go"}, Domains: []string{"authority.workflow"}},
	}
	got := Retrieve(Subject{Files: []string{"golang/server/subject.go"}}, anchors, []string{"authority.workflow"})
	for _, c := range got {
		if c.ID == "inv.direct" {
			t.Fatal("a direct anchor was returned as a prospective candidate, inflating recall with what the caller already knew")
		}
	}
}

// Every candidate must say why it surfaced, so a reader can judge the reach
// instead of trusting a number. There is deliberately no score to trust.
func TestEveryCandidateStatesItsRelationship(t *testing.T) {
	anchors := []Anchor{
		{ID: "a", Class: "invariant", Files: []string{"golang/server/n.go"}},
		{ID: "b", Class: "invariant", Files: []string{"x/y.go"}, Domains: []string{"d1"}},
	}
	for _, c := range Retrieve(Subject{Files: []string{"golang/server/s.go"}}, anchors, []string{"d1"}) {
		if c.Why == "" || c.Signal == "" {
			t.Errorf("candidate %q surfaced without stating why: %+v", c.ID, c)
		}
	}
}
