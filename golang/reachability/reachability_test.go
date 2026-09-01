// SPDX-License-Identifier: AGPL-3.0-only

package reachability

import (
	"strings"
	"testing"
)

// The defect this package exists for: a briefing reported "authoritative
// (current)" while the serving graph was eleven days and 159 corpus changes
// behind, because "current" was measured against the published artifact's own
// marker -- and an artifact always matches itself.
func TestAdmittedButUnpublishedKnowledgeIsStaleNotCurrent(t *testing.T) {
	a := Assess(Inputs{
		PublishedCommit: "96f19456f5fb",
		CorpusCommit:    "52b4ae5cdeadbeef",
		CommitsAhead:    159,
		Contains:        true,
		AheadKnown:      true,
	})
	if a.State != StateStale {
		t.Fatalf("state=%q for a graph 159 corpus changes behind", a.State)
	}
	if a.Reachable() {
		t.Fatal("a stale graph reported its admitted knowledge as reachable")
	}
	if !strings.Contains(a.Line(), "159") {
		t.Errorf("the distance is not in the report: %q", a.Line())
	}
	// The whole point: this must not read as "there is no such rule".
	if a.AssertsAbsence() {
		t.Fatal("a reachability state claimed absence of law")
	}
	if !strings.Contains(a.Line(), "unpublished rather than absent") {
		t.Errorf("the report does not separate unpublished from absent: %q", a.Line())
	}
}

// Unknown is a MEMBER, not a fallback. Each of these could be silently reported
// as current by a permissive implementation, and each would hide the same
// defect the package was written for.
func TestEveryUnanswerableQuestionIsUnknownNeverCurrent(t *testing.T) {
	for _, c := range []struct {
		name string
		in   Inputs
	}{
		{"graph states no revision", Inputs{CorpusCommit: "abc", Contains: true, AheadKnown: true}},
		{"corpus revision unresolved", Inputs{PublishedCommit: "abc", Contains: true, AheadKnown: true}},
		{"published revision not in this history", Inputs{PublishedCommit: "abc", CorpusCommit: "def", AheadKnown: true}},
		{"nothing at all", Inputs{}},
		// The distance could not be measured. Before AheadKnown existed this
		// read as a measured zero and therefore as CURRENT -- a fail-open in
		// the package written to stop one.
		{"distance unmeasured", Inputs{PublishedCommit: "aaaa", CorpusCommit: "bbbb", Contains: true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := Assess(c.in)
			if a.State != StateUnknown {
				t.Fatalf("state=%q, want unknown", a.State)
			}
			if a.Reachable() {
				t.Fatal("an unanswerable reachability question reported as reachable")
			}
			if !strings.Contains(a.Line(), "not as absent") {
				t.Errorf("unknown does not warn against reading it as absence: %q", a.Line())
			}
		})
	}
}

// A graph built from a revision this checkout does not contain cannot be
// ordered against it. Reporting `stale` would invent an ordering; reporting
// `current` would invent agreement.
func TestAnUnorderableGraphIsNotCalledStale(t *testing.T) {
	a := Assess(Inputs{PublishedCommit: "aaaaaaaa", CorpusCommit: "bbbbbbbb", CommitsAhead: 40, Contains: false, AheadKnown: true})
	if a.State == StateStale {
		t.Fatal("an unorderable pair was reported as stale, inventing an ordering")
	}
	if a.State != StateUnknown {
		t.Fatalf("state=%q, want unknown", a.State)
	}
}

// The graph reports an abbreviated commit and a caller resolves a full one.
// Equality would call an up-to-date graph stale on every single call.
func TestAbbreviatedAndFullRevisionsAreTheSameRevision(t *testing.T) {
	a := Assess(Inputs{
		PublishedCommit: "52b4ae5c",
		CorpusCommit:    "52b4ae5cf1e2d3c4b5a697887766554433221100",
		CommitsAhead:    0,
		Contains:        true,
		// deliberately NOT AheadKnown: this case must be decided by revision
		// identity alone, so the assertion exercises sameRevision rather than
		// passing through a measured-zero shortcut.
	})
	if a.State != StateCurrent {
		t.Fatalf("state=%q — an abbreviated revision was treated as a different one", a.State)
	}
	// And a genuinely different revision must not match on a short prefix.
	b := Assess(Inputs{PublishedCommit: "52b4ae5c", CorpusCommit: "52b4ae5d0000", CommitsAhead: 3, Contains: true, AheadKnown: true})
	if b.State != StateStale {
		t.Fatalf("state=%q — two different revisions compared equal", b.State)
	}
}

// Code movement is not knowledge movement. Counting every commit would cry wolf
// until the signal was ignored, which is how a real staleness signal dies.
func TestOnlyCorpusPathsAreMeasured(t *testing.T) {
	if len(CorpusPaths) == 0 {
		t.Fatal("no corpus paths — every commit would count as knowledge movement")
	}
	for _, p := range CorpusPaths {
		if !strings.Contains(p, "awareness") {
			t.Errorf("corpus path %q is not authored knowledge", p)
		}
	}
}
