// SPDX-License-Identifier: AGPL-3.0-only

package subjectstate

import "testing"

type node struct{ id, iri, status, severity string }

func (n node) GetId() string       { return n.id }
func (n node) GetIri() string      { return n.iri }
func (n node) GetStatus() string   { return n.status }
func (n node) GetSeverity() string { return n.severity }

func live(id string) node { return node{id: id, iri: "urn:" + id, status: "active", severity: "high"} }
func retired(id string) node {
	return node{id: id, iri: "urn:" + id, status: "retired", severity: "critical"}
}

func primary(n Node) bool { return n.GetStatus() != "retired" }

// The three states, and the one that was collapsed.
//
// "Anchors existed and every one has been retired" is a DETERMINED NEGATIVE. It
// was folded into "no anchors at all", and the index fallback then resurrected
// the subject -- so a file whose governance was deliberately withdrawn came back
// as examined and covered.
func TestAWithdrawnSubjectIsDeterminedNotUnknown(t *testing.T) {
	s := Build(Raw{ClassInvariant: {retired("r1"), retired("r2")}}, primary)
	if got := s.Examination(); got != ExaminedWithdrawn {
		t.Fatalf("examination=%q for a retired-only subject", got)
	}
	if !s.Examination().Determined() {
		t.Error("a withdrawal was reported as missing information")
	}
	if s.Examination().MayConsultIndex() {
		t.Fatal("a determined withdrawal may be overturned by a secondary source")
	}
	if s.HasLiveAnchors() {
		t.Error("a retired anchor counted as live governance")
	}
	// Withdrawn knowledge is not discarded: it is guidance, not a binding.
	if len(s.Retired(ClassInvariant)) != 2 {
		t.Error("retired knowledge was dropped rather than reclassified")
	}
}

// Only the genuinely unknown state may ask a secondary source. That is the
// entire rule the index fallback kept breaking.
func TestOnlyTheUnknownStateMayConsultTheIndex(t *testing.T) {
	for _, c := range []struct {
		name string
		raw  Raw
		want Examination
		ask  bool
	}{
		{"live anchor", Raw{ClassContract: {live("c1")}}, ExaminedGoverned, false},
		{"retired only", Raw{ClassContract: {retired("c1")}}, ExaminedWithdrawn, false},
		{"nothing at all", Raw{}, ExaminedUnknown, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := Build(c.raw, primary)
			if got := s.Examination(); got != c.want {
				t.Fatalf("examination=%q want %q", got, c.want)
			}
			if got := s.Examination().MayConsultIndex(); got != c.ask {
				t.Fatalf("MayConsultIndex=%v want %v", got, c.ask)
			}
		})
	}
}

// Class-completeness. A subject anchored only by a live forbidden fix or
// contract IS anchored -- the exact case where coverage said "no governing rule
// applies" while the same anchor decided authority.
func TestEveryGovernedClassCounts(t *testing.T) {
	for _, c := range AllClasses() {
		s := Build(Raw{c: {live("x")}}, primary)
		if !s.HasLiveAnchors() {
			t.Errorf("a live %s did not count as an anchor", c)
		}
		if s.Examination() != ExaminedGoverned {
			t.Errorf("a live %s left the subject %q", c, s.Examination())
		}
		if len(s.AllLive()) != 1 {
			t.Errorf("a live %s is missing from the complete set", c)
		}
	}
	if n := len(AllClasses()); n != 5 {
		t.Fatalf("the closed class set has %d members; a decision written against 5 will silently miss the rest", n)
	}
}

// A narrower decision must NAME its classes. The keyword risk classifier is
// deliberately narrow, and that narrowing has to be visible at the call rather
// than implied by which fields someone remembered to merge.
func TestANarrowerDecisionMustNameItsClasses(t *testing.T) {
	s := Build(Raw{
		ClassInvariant: {live("i1")},
		ClassContract:  {live("c1")},
	}, primary)
	if len(s.AllLive()) != 2 {
		t.Fatalf("complete set has %d, want 2", len(s.AllLive()))
	}
	narrow := s.LiveIn(ClassInvariant)
	if len(narrow) != 1 || narrow[0].GetId() != "i1" {
		t.Fatalf("a named-class read returned %v", narrow)
	}
}

// The state is derived from the anchors and cannot be asserted against them.
// Three surfaces each computed this and disagreed; there is now nowhere to
// disagree.
func TestExaminationCannotContradictTheAnchors(t *testing.T) {
	s := Build(Raw{ClassInvariant: {live("i1"), retired("r1")}}, primary)
	if s.Examination() != ExaminedGoverned {
		t.Fatal("a subject with one live and one retired anchor is governed")
	}
	if s.CountLive() != 1 || s.CountRaw() != 2 {
		t.Fatalf("live=%d raw=%d; the two counts answer different questions and must not merge",
			s.CountLive(), s.CountRaw())
	}
}

// Accessors must not hand out the internal slice: a consumer that appends to a
// returned set would mutate the canonical state for every later reader.
func TestAConsumerCannotMutateTheCanonicalState(t *testing.T) {
	s := Build(Raw{ClassInvariant: {live("i1"), live("i2")}}, primary)

	// Appending is NOT the test. A returned slice whose length equals its
	// capacity reallocates on append, so an aliasing accessor passes that
	// check while still handing out the internal array. The first version of
	// this test did exactly that and the mutation survived.
	//
	// The property is that two reads do not share a backing array.
	a, b := s.Live(ClassInvariant), s.Live(ClassInvariant)
	if len(a) == 0 || len(b) == 0 {
		t.Fatal("fixture returned nothing")
	}
	if &a[0] == &b[0] {
		t.Fatal("the accessor hands out the internal slice; a consumer writing " +
			"through it would rewrite the canonical state for every later reader")
	}

	// And writing through one read must not be visible in another.
	a[0] = live("injected")
	if s.Live(ClassInvariant)[0].GetId() != "i1" {
		t.Fatal("a write through a returned slice reached the canonical state")
	}
}

// A nil predicate must not silently mean "everything is live". It is the
// fail-open shape: no lifecycle information read as all-clear.
func TestANilPredicateDoesNotMeanEverythingIsLive(t *testing.T) {
	s := Build(Raw{ClassInvariant: {live("i1"), retired("r1")}}, nil)
	if s.CountLive() != 0 {
		t.Fatalf("with no lifecycle predicate, %d anchors were treated as live", s.CountLive())
	}
	if s.Examination() != ExaminedWithdrawn {
		t.Fatalf("examination=%q; with no predicate nothing is established as live", s.Examination())
	}
}
