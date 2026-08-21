// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/extractbudget"
)

// scopedFixture is a repository with a Go file the caller wants scanned and a
// generated-artifact directory the caller excluded.
func scopedFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                            "module example.com/fixture\n\ngo 1.25.0\n",
		"kept.go":                           "package fixture\n\ntype Kept struct {\n\tField string `json:\"field\"`\n}\n",
		"docs/awareness/generated/gen.yaml": "generated: true\n",
	}
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func scopedBinding() architecture.ClaimDocumentBinding {
	return architecture.ClaimDocumentBinding{
		RepositoryDomain:  "example.test/fixture",
		Revision:          "0123456789012345678901234567890123456789",
		RevisionStatus:    architecture.RevisionResolved,
		GraphDigestStatus: architecture.GraphDigestNotRequested,
	}
}

// A bounded extraction's receipt claims the scope was enforced. A fact whose
// location is recorded only in Scope.Files — with no evidence anchor — was
// never checked against the scope at all, because the filter reads only
// Evidence.SourceFile.
//
// Reachable today: the generated-artifact fact deliberately carries no anchor
// (a directory is not a source file), so it names an excluded path in its scope
// and survives a run that excluded it.
func TestExcludedPathsAreFilteredEvenWithoutAnEvidenceAnchor(t *testing.T) {
	root := scopedFixture(t)
	doc, err := Extract(root, Options{
		CapturedAt: "2026-01-01T00:00:00Z",
		Repository: scopedBinding(),
		Budget:     extractbudget.Budget{ExcludePaths: []string{"docs/"}}.Normalize(),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	var escaped []string
	for _, o := range doc.Observations {
		for _, f := range o.Scope.Files {
			if len(f) >= 5 && f[:5] == "docs/" {
				escaped = append(escaped, o.Extractor+": "+o.Subject+" scoped to "+f+" (anchor="+o.Evidence.SourceFile+")")
			}
		}
	}
	if len(escaped) > 0 {
		t.Fatalf("an excluded path survived a bounded extraction:\n  %v", escaped)
	}
}

// The baseline: the scope filter must not simply drop everything. A file inside
// the scope still produces observations, so a passing test above cannot pass by
// refusing the whole repository.
func TestIncludedPathsStillProduceObservations(t *testing.T) {
	root := scopedFixture(t)
	doc, err := Extract(root, Options{
		CapturedAt: "2026-01-01T00:00:00Z",
		Repository: scopedBinding(),
		Budget:     extractbudget.Budget{ExcludePaths: []string{"docs/"}}.Normalize(),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, o := range doc.Observations {
		if o.Evidence.SourceFile == "kept.go" {
			return
		}
	}
	t.Fatal("the in-scope file produced no observation: the fixture no longer exercises the filter")
}

// A fact spanning an in-scope and an out-of-scope file describes something the
// caller asked about. Scoping bounds what was searched; it does not redact what
// was found.
func TestFactSpanningScopeBoundaryIsKept(t *testing.T) {
	budget := extractbudget.Budget{ExcludePaths: []string{"docs/"}}.Normalize()
	f := architecture.Fact{Scope: architecture.Scope{Files: []string{"docs/generated", "kept.go"}}}
	if outOfScope(f, budget) {
		t.Fatal("a fact naming an in-scope file was dropped")
	}
}

// A fact recording no location at all is repository-level: there is nothing to
// check it against, and inventing a scope for it would drop observations the
// caller never excluded.
func TestFactWithNoRecordedLocationIsKept(t *testing.T) {
	budget := extractbudget.Budget{ExcludePaths: []string{"docs/"}}.Normalize()
	if outOfScope(architecture.Fact{}, budget) {
		t.Fatal("a fact with no recorded location was dropped")
	}
	blank := architecture.Fact{Scope: architecture.Scope{Files: []string{"", "  "}}}
	if outOfScope(blank, budget) {
		t.Fatal("blank scope entries were treated as an out-of-scope location")
	}
}

// The anchor still decides on its own when it is present and in scope, so this
// change cannot narrow what a previously-passing anchored fact does.
func TestAnchoredFactStillDecidedByItsAnchor(t *testing.T) {
	budget := extractbudget.Budget{ExcludePaths: []string{"docs/"}}.Normalize()
	in := architecture.Fact{Evidence: architecture.Evidence{SourceFile: "kept.go"}}
	if outOfScope(in, budget) {
		t.Fatal("an in-scope anchored fact was dropped")
	}
	out := architecture.Fact{Evidence: architecture.Evidence{SourceFile: "docs/generated/x.yaml"}}
	if !outOfScope(out, budget) {
		t.Fatal("an out-of-scope anchored fact survived")
	}
}

// With no scopes active the filter never runs, but the predicate must still be
// safe if called: an unbounded budget excludes nothing.
func TestUnboundedBudgetDropsNothing(t *testing.T) {
	var unbounded extractbudget.Budget
	f := architecture.Fact{Scope: architecture.Scope{Files: []string{"docs/generated"}}}
	if outOfScope(f, unbounded) {
		t.Fatal("an unbounded budget dropped a fact")
	}
}
