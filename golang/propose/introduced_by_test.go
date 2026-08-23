// SPDX-License-Identifier: AGPL-3.0-only

package propose

import (
	"strings"
	"testing"
)

func scar(attr ...Attribution) Request {
	return Request{
		Kind: "failure_mode", Title: "a governed failure",
		RelatedInvariants: []string{"inv.example"},
		Evidence:          []string{"observed once"},
		Repo:              "github.com/globulario/sensei",
		IntroducedBy:      attr,
	}
}

// The persisted identity is repository plus commit, never the SHA alone. The
// CLI may take only the SHA because the repository is already known; what is
// recorded carries both, because the same short hash occurs in more than one
// repository.
func TestAttributionCarriesRepositoryAndCommit(t *testing.T) {
	r := scar(Attribution{Commit: "7FBD15A4"})
	Normalize(&r)
	if len(r.IntroducedBy) != 1 {
		t.Fatalf("attribution lost: %#v", r.IntroducedBy)
	}
	got := r.IntroducedBy[0]
	if got.Commit != "7fbd15a4" {
		t.Fatalf("commit %q was not normalised to lowercase", got.Commit)
	}
	if got.Repo != "github.com/globulario/sensei" {
		t.Fatalf("repository was not defaulted from the request context: %q", got.Repo)
	}
	if errs := Validate(r); len(errs) != 0 {
		t.Fatalf("a well-formed attribution was rejected: %v", errs)
	}
}

// A failure may carry more than one, because real failures have compound
// ancestry: one change introduces unsafe state, another removes the check that
// compensated for it.
func TestAFailureMayNameMoreThanOneIntroducingChange(t *testing.T) {
	r := scar(Attribution{Commit: "aaaaaaa"}, Attribution{Commit: "bbbbbbb"})
	Normalize(&r)
	if len(r.IntroducedBy) != 2 {
		t.Fatalf("compound ancestry was collapsed: %#v", r.IntroducedBy)
	}
	if errs := Validate(r); len(errs) != 0 {
		t.Fatalf("two attributions were rejected: %v", errs)
	}
	// The same change twice is one attribution, not two.
	dup := scar(Attribution{Commit: "aaaaaaa"}, Attribution{Commit: "AAAAAAA"})
	Normalize(&dup)
	if len(dup.IntroducedBy) != 1 {
		t.Fatalf("a duplicate attribution was kept: %#v", dup.IntroducedBy)
	}
}

// Only an immutable identity may be recorded. A branch, a tag and HEAD all
// move, and an attribution that silently repoints later is worse than none,
// because it still reads as a record of what happened.
func TestOnlyAnImmutableCommitIdentityIsAccepted(t *testing.T) {
	for _, bad := range []string{"HEAD", "main", "v1.2.0", "HEAD~3", "abc", "not-a-sha", "zzzzzzz", ""} {
		r := scar(Attribution{Commit: bad})
		Normalize(&r)
		if len(r.IntroducedBy) == 0 && bad == "" {
			continue // an empty attribution is dropped rather than reported
		}
		errs := Validate(r)
		if len(errs) == 0 {
			t.Fatalf("%q was accepted as a change identity", bad)
		}
		if !strings.Contains(strings.Join(errs, " "), "commit SHA") {
			t.Fatalf("%q was rejected without saying why: %v", bad, errs)
		}
	}
}

// The relation asserts that a change introduced a FAILURE. What it would mean
// on an invariant or a required_test has not been decided, and inventing a
// meaning would put edges in the graph nobody defined.
func TestIntroducedByIsOnlyAcceptedOnAFailure(t *testing.T) {
	for _, kind := range []string{"invariant", "required_test", "forbidden_fix", "decision", "contract_unknown"} {
		r := Request{
			Kind: kind, Title: "t", ID: "x", Description: "d",
			RelatedInvariants: []string{"inv.example"},
			RelatedFailures:   []string{"failure.example"},
			Evidence:          []string{"e"}, SourceFiles: []string{"a.go"},
			RequiredTests:    []string{"a_test.go:TestX"},
			ProposedContract: "c", Repo: "github.com/globulario/sensei",
			IntroducedBy: []Attribution{{Commit: "7fbd15a4"}},
		}
		Normalize(&r)
		errs := Validate(r)
		if !strings.Contains(strings.Join(errs, " "), "only accepted on failure_mode") {
			t.Fatalf("%s accepted introduced_by: %v", kind, errs)
		}
	}
}

// Absence means nobody recorded an attribution. It is not evidence that a
// change was correct, and nothing may treat it as one — so a scar without the
// relation stays perfectly valid and gains no field asserting the opposite.
func TestAbsenceIsNotAClaimAboutCorrectness(t *testing.T) {
	r := scar()
	Normalize(&r)
	if errs := Validate(r); len(errs) != 0 {
		t.Fatalf("a scar with no attribution was rejected: %v", errs)
	}
	c, err := RenderCandidate(r)
	if err != nil {
		t.Fatal(err)
	}
	body := string(c.Content)
	if strings.Contains(body, "introduced_by") {
		t.Fatalf("an unattributed scar rendered an introduced_by field:\n%s", body)
	}
	for _, invented := range []string{"no_regression", "correct", "clean", "unattributed"} {
		if strings.Contains(body, invented) {
			t.Fatalf("the renderer invented a claim about absence (%q):\n%s", invented, body)
		}
	}
}

// The attribution survives into the reviewed artifact as structured fields, not
// as prose folded into evidence or description.
func TestAttributionIsPersistedStructurally(t *testing.T) {
	r := scar(Attribution{Commit: "7fbd15a4"})
	Normalize(&r)
	c, err := RenderCandidate(r)
	if err != nil {
		t.Fatal(err)
	}
	body := string(c.Content)
	if !strings.Contains(body, "introduced_by:") || !strings.Contains(body, "commit: 7fbd15a4") {
		t.Fatalf("attribution is not structurally persisted:\n%s", body)
	}
	if !strings.Contains(body, "repo: github.com/globulario/sensei") {
		t.Fatalf("attribution lost its repository:\n%s", body)
	}
	for _, ev := range r.Evidence {
		if strings.Contains(ev, "7fbd15a4") {
			t.Fatal("the attribution leaked into evidence prose")
		}
	}
}
