package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A candidate is not a canonical claim, so it is never among the identities a
// domain is REQUIRED to project.
//
// Requiring it made every foreign repository fail closure on onboarding:
// `sensei import` writes extracted candidates under a top-level `invariants:`
// key -- a governed class -- and correctly refuses to publish them, so the build
// demanded publication of exactly what the design forbids publishing. Observed
// on golang/sync: 24 required, 0 projected, not authoritative, no governed run
// able to start.

func closureCorpus(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const candidateOnly = `# extracted, never promoted
invariants:
  - id: candidate.invariant.authority.cur
    statement: cur appears to be mutated through a constrained writer set.
    status: candidate
`

const canonicalOnly = `invariants:
  - id: real.invariant.one
    statement: something governed.
    status: active
`

//  1. Candidate-only import: the candidate is declared, is not required to
//     project, and closure stays provable.
func TestACandidateIsDeclaredButNeverRequiredToProject(t *testing.T) {
	expected, excluded, err := expectedIdentities(closureCorpus(t, "invariant_candidates.yaml", candidateOnly))
	if err != nil {
		t.Fatal(err)
	}
	if len(expected) != 0 {
		t.Fatalf("a candidate was required to project: %v", expected)
	}
	if len(excluded) != 1 || excluded[0] != "candidate.invariant.authority.cur" {
		t.Fatalf("the candidate was not recorded as declared-non-authority: %v", excluded)
	}
}

//  2. A canonical governed identity that does not project still fails closure.
//     The fix must not become "closure no longer notices absence".
func TestACanonicalIdentityIsStillRequiredToProject(t *testing.T) {
	expected, excluded, err := expectedIdentities(closureCorpus(t, "invariants.yaml", canonicalOnly))
	if err != nil {
		t.Fatal(err)
	}
	if len(expected) != 1 {
		t.Fatalf("a canonical identity stopped being required: expected=%v excluded=%v", expected, excluded)
	}
	if _, ok := expected["real.invariant.one"]; !ok {
		t.Fatalf("wrong identity required: %v", expected)
	}
}

// 3. Mixed: the candidate is excused, the canonical one is not.
func TestOnlyTheCandidateIsExcusedInAMixedFile(t *testing.T) {
	expected, excluded, err := expectedIdentities(closureCorpus(t, "invariants.yaml", `invariants:
  - id: candidate.invariant.authority.cur
    statement: inferred.
    status: candidate
  - id: real.invariant.one
    statement: governed.
    status: active
  - id: no.status.declared
    statement: says nothing about its status.
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := expected["candidate.invariant.authority.cur"]; ok {
		t.Fatal("the candidate is required to project")
	}
	if _, ok := expected["real.invariant.one"]; !ok {
		t.Fatal("the canonical identity stopped being required")
	}
	// An entry that declares no status keeps its obligation. Reading the
	// NON-canonical set by membership fails closed; listing canonical statuses
	// and excusing everything else would let a typo quietly drop an obligation.
	if _, ok := expected["no.status.declared"]; !ok {
		t.Fatal("an identity with no declared status was excused; unknown must stay required")
	}
	if len(excluded) != 1 {
		t.Fatalf("expected exactly the candidate excluded, got %v", excluded)
	}
}

// 4. Cold start: a repository with zero canonical claims proves closure.
//
// PROVEN here means "everything claiming canonical authority is accounted for".
// It does NOT mean the repository is understood, and nothing may read this
// state as safety -- the graph is authoritative and architecturally EMPTY at the
// same time, which is precisely the state the investigator is meant to operate
// on.
func TestColdStartProvesClosureWithoutClaimingKnowledge(t *testing.T) {
	expected, _, err := expectedIdentities(closureCorpus(t, "invariant_candidates.yaml", candidateOnly))
	if err != nil {
		t.Fatal(err)
	}
	c := ComputeClosure(t.TempDir(), expected, nil, map[string]*Subject{})
	ok, reasons := c.Authoritative()
	if !ok {
		t.Fatalf("a repository with zero canonical claims failed closure: %v", reasons)
	}
	if len(c.ExpectedToProject) != 0 || len(c.Projected) != 0 {
		t.Fatalf("cold start should be 0/0, got %d/%d",
			len(c.Projected), len(c.ExpectedToProject))
	}
	// The trap this guards: "nothing missing" must never be reachable as
	// "nothing to know". Closure says the canonical set is accounted for and is
	// silent about coverage.
	if len(c.Missing) != 0 {
		t.Fatalf("0/0 reported missing identities: %v", c.Missing)
	}
}

// A `status:` nested inside a relation block is not the entry's own status.
func TestANestedStatusDoesNotExcuseAnEntry(t *testing.T) {
	expected, _, err := expectedIdentities(closureCorpus(t, "invariants.yaml", `invariants:
  - id: real.invariant.one
    statement: governed.
    status: active
    evidence:
      - id: some.evidence
        status: candidate
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := expected["real.invariant.one"]; !ok {
		t.Fatal("a nested candidate status excused the governed entry that contained it")
	}
}

// Field order inside an entry is not a fact about the entry, and a neighbour's
// formatting must never change an entry's obligation.
//
// The stream-based parser attached a `status:` that preceded its own `id:` to
// the PREVIOUS entry: the candidate stayed required, and the canonical
// neighbour was excused from closure -- a fail-open in the closure check
// itself, triggered by formatting.
func TestStatusBeforeIdBelongsToItsOwnEntry(t *testing.T) {
	expected, excluded, err := expectedIdentities(closureCorpus(t, "invariants.yaml", `invariants:
  - id: real.one
    status: active
  - status: candidate
    id: candidate.two
  - status: candidate
    statement: nothing
    id: candidate.three
  - id: real.four
`))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"real.one", "real.four"} {
		if _, ok := expected[id]; !ok {
			t.Fatalf("%s lost its obligation because of a neighbour's field order: expected=%v excluded=%v", id, expected, excluded)
		}
	}
	for _, id := range []string{"candidate.two", "candidate.three"} {
		if _, ok := expected[id]; ok {
			t.Fatalf("%s is required to project although it declares status: candidate", id)
		}
	}
	if len(excluded) != 2 {
		t.Fatalf("expected exactly the two candidates excluded, got %v", excluded)
	}
}

// A nested block's fields are not the entry's, whatever their order.
func TestNestedIdAndStatusAreNotTheEntrys(t *testing.T) {
	expected, _, err := expectedIdentities(closureCorpus(t, "invariants.yaml", `invariants:
  - id: real.one
    evidence:
      - status: candidate
        id: nested.thing
    status: active
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := expected["real.one"]; !ok {
		t.Fatal("a nested candidate excused the governed entry containing it")
	}
	if _, ok := expected["nested.thing"]; ok {
		t.Fatal("a nested id was read as a top-level identity")
	}
}
