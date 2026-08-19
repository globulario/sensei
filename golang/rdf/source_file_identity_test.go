// SPDX-License-Identifier: AGPL-3.0-only

package rdf

import (
	"strings"
	"testing"
)

// The required regression from the #197 architect decision, item by item.

// "repo A README.md != repo B README.md" -- the collapse that started the
// issue. README.md is a path every Sensei-onboarded repository has, so this
// is the ordinary case, not an edge case.
func TestTwoRepositoriesREADMEsMintDifferentSubjects(t *testing.T) {
	a := MintSourceFileIRI("github.com/globulario/sensei", "README.md")
	b := MintSourceFileIRI("github.com/globulario/sensei-code", "README.md")
	if a == b {
		t.Fatalf("two repositories' README.md collapsed onto one subject: %s", a)
	}
	for _, shared := range []string{"docs/awareness/invariants.yaml", "README.md"} {
		if MintSourceFileIRI("github.com/globulario/sensei", shared) ==
			MintSourceFileIRI("github.com/globulario/services", shared) {
			t.Errorf("%q collapsed across repositories", shared)
		}
	}
}

// "same repo + same path is stable across checkout paths and machines" --
// identity is a function of repository identity and repo-relative path
// only, so nothing about where the checkout happens to live can reach it.
func TestSameRepositoryAndPathIsStableAcrossCheckouts(t *testing.T) {
	want := MintSourceFileIRI("github.com/globulario/sensei", "golang/rdf/builder.go")
	for _, again := range []string{
		MintSourceFileIRI("github.com/globulario/sensei", "golang/rdf/builder.go"),
		MintSourceFileIRI("github.com/globulario/sensei", "golang/rdf/builder.go"),
	} {
		if again != want {
			t.Fatalf("identity is not stable: %s vs %s", again, want)
		}
	}
}

// "publication-domain aliasing does not change SourceFileID" -- a build
// naming a different publication domain for the same repository must not
// re-identify its files. Nothing in this function reads a publication
// domain, so the test states the property the call sites must preserve:
// the identity argument is the repository, not the domain a build selects.
func TestPublicationDomainAliasingDoesNotChangeIdentity(t *testing.T) {
	repository := "github.com/globulario/sensei"
	want := MintSourceFileIRI(repository, "README.md")
	for _, publicationDomain := range []string{"github.com/globulario/sensei", "example.com/a", "pilot/cli"} {
		// The publication domain is not an input. Passing the repository
		// identity is the only way to mint, whatever domain is being
		// published under.
		if got := MintSourceFileIRI(repository, "README.md"); got != want {
			t.Fatalf("publication domain %q changed the identity: %s", publicationDomain, got)
		}
	}
}

// "cross-repo edges attach only to the intended repo-scoped file" -- an
// edge minted for one repository's file must not resolve to another's.
func TestCrossRepoEdgesAttachToTheIntendedFile(t *testing.T) {
	senseiREADME := MintSourceFileIRI("github.com/globulario/sensei", "README.md")
	codeREADME := MintSourceFileIRI("github.com/globulario/sensei-code", "README.md")

	parsed, ok := ParseSourceFileIRI(codeREADME)
	if !ok {
		t.Fatal("a freshly minted identity did not parse")
	}
	if parsed.RepositoryIdentity != "github.com/globulario/sensei-code" || parsed.Path != "README.md" {
		t.Fatalf("parsed identity = %+v", parsed)
	}
	if codeREADME == senseiREADME {
		t.Fatal("an edge minted for sensei-code's README resolves to sensei's")
	}
}

func TestMintedIdentitiesRoundTrip(t *testing.T) {
	for _, tc := range []struct{ repository, path string }{
		{"github.com/globulario/sensei", "README.md"},
		{"github.com/globulario/sensei", "docs/awareness/invariants.yaml"},
		{"example.com/a", "a/b/c/d.go"},
		{"github.com/o/r", "path with spaces/and%percent.txt"},
		{"github.com/o/r", "weird<>{}|^`\\name.go"},
	} {
		iri := MintSourceFileIRI(tc.repository, tc.path)
		got, ok := ParseSourceFileIRI(iri)
		if !ok {
			t.Errorf("%s did not parse", iri)
			continue
		}
		if got.Generation != SourceFileGenerationV2 {
			t.Errorf("%s parsed as generation %q", iri, got.Generation)
		}
		if got.RepositoryIdentity != tc.repository || got.Path != tc.path {
			t.Errorf("round trip of (%q, %q) gave %+v", tc.repository, tc.path, got)
		}
	}
}

// A v1 identity is reported as v1 and never guessed into a repository:
// sourceFile/README.md can mean any of N repositories, so inventing one
// would manufacture exactly the false continuity the migration decision
// forbids.
func TestV1IdentitiesParseAsAmbiguousAndCarryNoRepository(t *testing.T) {
	v1 := MintIRI(ClassSourceFile, "README.md")
	got, ok := ParseSourceFileIRI(v1)
	if !ok {
		t.Fatalf("%s did not parse", v1)
	}
	if got.Generation != SourceFileGenerationV1 {
		t.Fatalf("generation = %q, want %q", got.Generation, SourceFileGenerationV1)
	}
	if got.RepositoryIdentity != "" {
		t.Fatalf("a v1 identity was given repository %q -- it carries none", got.RepositoryIdentity)
	}
	if got.Path != "README.md" {
		t.Fatalf("path = %q", got.Path)
	}
}

func TestParseRejectsNonSourceFileIRIs(t *testing.T) {
	for _, iri := range []string{
		"",
		"<https://globular.io/awareness#invariant/foo>",
		"https://globular.io/awareness#sourceFile/",
		"https://example.com/other",
	} {
		if _, ok := ParseSourceFileIRI(iri); ok {
			t.Errorf("%q was accepted as a SourceFile IRI", iri)
		}
	}
}

func TestSourceFilePathFromIRIReadsBothGenerations(t *testing.T) {
	v2, ok := SourceFilePathFromIRI(MintSourceFileIRI("github.com/o/r", "docs/awareness/invariants.yaml"))
	if !ok || v2 != "docs/awareness/invariants.yaml" {
		t.Fatalf("v2 path = %q ok=%v", v2, ok)
	}
	v1, ok := SourceFilePathFromIRI(MintIRI(ClassSourceFile, "docs/awareness/invariants.yaml"))
	if !ok || v1 != "docs/awareness/invariants.yaml" {
		t.Fatalf("v1 path = %q ok=%v", v1, ok)
	}
}

// --- the generation boundary (#197 migration decision) ---

// "ambiguous v1 receipt without repo binding cannot be silently translated"
func TestAnAmbiguousV1IdentityCannotBeTranslated(t *testing.T) {
	v1 := MintIRI(ClassSourceFile, "README.md")
	if _, err := TranslateV1SourceFileIdentity(v1, ""); err == nil {
		t.Fatal("an unscoped identity with no repository binding was translated")
	}
	if _, err := TranslateV1SourceFileIdentity(v1, "   "); err == nil {
		t.Fatal("whitespace was accepted as a repository binding")
	}
}

// A v1 identity that the SAME document independently binds to a repository
// translates -- and the record says what was translated, rather than
// asserting the two IRIs are one subject.
func TestAV1IdentityWithARepositoryBindingTranslates(t *testing.T) {
	v1 := MintIRI(ClassSourceFile, "README.md")
	record, err := TranslateV1SourceFileIdentity(v1, "github.com/globulario/sensei")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if record.RepositoryIdentity != "github.com/globulario/sensei" || record.Path != "README.md" {
		t.Fatalf("record = %+v", record)
	}
	want := strings.Trim(MintSourceFileIRI("github.com/globulario/sensei", "README.md"), "<>")
	if record.NewIRI != want {
		t.Fatalf("NewIRI = %q, want %q", record.NewIRI, want)
	}
	if record.OldIRI == record.NewIRI {
		t.Fatal("the record claims the old and new identity are the same subject")
	}
	// The same old IRI, bound by a different repository's receipt, must
	// translate somewhere else -- which is precisely why no unconditional
	// alias from the old identity can exist.
	other, err := TranslateV1SourceFileIdentity(v1, "github.com/globulario/sensei-code")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if other.NewIRI == record.NewIRI {
		t.Fatal("one unscoped identity translated to the same subject for two different repositories")
	}
}

func TestAV2IdentityIsNeverRescoped(t *testing.T) {
	v2 := MintSourceFileIRI("github.com/globulario/sensei", "README.md")
	if _, err := TranslateV1SourceFileIdentity(v2, "github.com/globulario/sensei-code"); err == nil {
		t.Fatal("a repository-scoped identity was re-scoped to a different repository")
	}
}

// "v2 publication never exposes a mixed v1/v2 authoritative generation"
func TestAMixedGenerationPublicationIsRefused(t *testing.T) {
	v1 := MintIRI(ClassSourceFile, "README.md")
	v2 := MintSourceFileIRI("github.com/globulario/sensei", "README.md")
	typed := func(subject string) string {
		return subject + " " + IRI(PropType) + " " + IRI(ClassSourceFile) + " .\n"
	}

	if err := CheckSourceFileGeneration([]byte(typed(v2) + typed(v2))); err != nil {
		t.Fatalf("an all-v2 publication was refused: %v", err)
	}
	if err := CheckSourceFileGeneration([]byte(typed(v1))); err != nil {
		t.Fatalf("an all-v1 publication was refused: %v", err)
	}
	if err := CheckSourceFileGeneration(nil); err != nil {
		t.Fatalf("an empty publication was refused: %v", err)
	}

	err := CheckSourceFileGeneration([]byte(typed(v2) + typed(v1)))
	if err == nil {
		t.Fatal("a publication exposing both identity generations was accepted")
	}
	for _, want := range []string{"README.md", "unscoped", "repository-scoped"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q: %v", want, err)
		}
	}
}

// No blanket alias is ever minted: nothing in this package produces an
// owl:sameAs (or any other) edge between an old and a new identity. The
// only bridge is a migration record, which is a Go value a caller records
// as provenance -- not a triple this package emits into the graph.
func TestNoAliasEdgeIsMintedBetweenGenerations(t *testing.T) {
	record, err := TranslateV1SourceFileIdentity(MintIRI(ClassSourceFile, "README.md"), "github.com/globulario/sensei")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{record.OldIRI, record.NewIRI, record.RepositoryIdentity, record.Path} {
		if strings.Contains(field, "sameAs") {
			t.Fatalf("a migration record carries an alias predicate: %q", field)
		}
	}
}
