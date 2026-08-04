// SPDX-License-Identifier: AGPL-3.0-only

package senseireport

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCandidateFixture(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCandidatesMissingDirectory(t *testing.T) {
	root := t.TempDir()
	m, err := Candidates(root)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if m.CandidatesAwaitingReview != 0 {
		t.Fatalf("expected 0 candidates, got %d", m.CandidatesAwaitingReview)
	}
	if len(m.Limitations) == 0 {
		t.Fatalf("expected a limitation noting the missing directory")
	}
}

func TestCandidatesAllThreeShapes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "docs", "awareness", "candidates")

	// Shape 1: wrapper key + status-bearing list.
	writeCandidateFixture(t, dir, "authority_surface_candidates.yaml", `
authority_surface_candidates:
  candidates:
    - id: candidate.authority.one
      class: AuthoritySurface
      status: candidate
    - id: candidate.authority.two
      class: AuthoritySurface
      status: held
`)

	// Shape 2: flat single-entry file with status at the top level.
	writeCandidateFixture(t, dir, "skills/imported_skill.yaml", `
id: imported.skill.example
class: ImplementationPattern
status: candidate
`)

	// Shape 3: contract_unknown wrapper, no status field at all.
	writeCandidateFixture(t, dir, "contract_unknown_example.yaml", `
contract_unknown:
  - id: contract_unknown.example
    kind: contract_unknown
    title: something unresolved
`)

	m, err := Candidates(root)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if m.CandidatesAwaitingReview != 2 {
		t.Fatalf("expected 2 awaiting-review candidates (shape1 candidate + shape2), got %d: %+v", m.CandidatesAwaitingReview, m.Highlighted)
	}
	foundOne, foundTwo := false, false
	for _, c := range m.Highlighted {
		if c.ID == "candidate.authority.one" {
			foundOne = true
		}
		if c.ID == "imported.skill.example" {
			foundTwo = true
		}
	}
	if !foundOne || !foundTwo {
		t.Fatalf("expected both status:candidate entries highlighted, got %+v", m.Highlighted)
	}

	foundLimitation := false
	for _, l := range m.Limitations {
		if l == "no status-bearing entry found (excluded from count): docs/awareness/candidates/contract_unknown_example.yaml" {
			foundLimitation = true
		}
	}
	if !foundLimitation {
		t.Fatalf("expected the statusless contract_unknown file to be named in Limitations, got %+v", m.Limitations)
	}
}

func TestCandidatesMalformedYAMLNamedNotFatal(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "docs", "awareness", "candidates")
	writeCandidateFixture(t, dir, "broken.yaml", "not: [valid: yaml")

	m, err := Candidates(root)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if m.CandidatesAwaitingReview != 0 {
		t.Fatalf("expected 0 candidates from a malformed file, got %d", m.CandidatesAwaitingReview)
	}
	found := false
	for _, l := range m.Limitations {
		if l == "no status-bearing entry found (excluded from count): docs/awareness/candidates/broken.yaml (malformed YAML)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected malformed file named in Limitations, got %+v", m.Limitations)
	}
}
