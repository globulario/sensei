// SPDX-License-Identifier: AGPL-3.0-only

package knowledgeadmission

import (
	"os"
	"path/filepath"
	"testing"
)

func writeAdmissionCorpusFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, "docs", "awareness", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func digestFor(t *testing.T, root string, ids ...string) string {
	t.Helper()
	got, err := AdmissionCorpusDigest(root, ids)
	if err != nil {
		t.Fatalf("AdmissionCorpusDigest: %v", err)
	}
	return got
}

func TestAdmissionCorpusDigestIgnoresPathAndFormatting(t *testing.T) {
	const id = "invariant.path.independent"

	rootA := t.TempDir()
	writeAdmissionCorpusFile(t, rootA, "invariants.yaml", `invariants:
  - id: invariant.path.independent
    title: probe
    severity: high
`)

	rootB := t.TempDir()
	writeAdmissionCorpusFile(t, rootB, "candidates/deep/rule.yml", `# same authored declaration, different spelling and location
invariants:
  - severity: high
    id: invariant.path.independent
    title: "probe"
`)

	if a, b := digestFor(t, rootA, id), digestFor(t, rootB, id); a != b {
		t.Fatalf("path/formatting changed corpus digest:\nA %s\nB %s", a, b)
	}
}

func TestAdmissionCorpusDigestRecognizesClassDiscriminatedSingleEntity(t *testing.T) {
	const id = "implementation_pattern.example"

	rootA := t.TempDir()
	writeAdmissionCorpusFile(t, rootA, "architecture/patterns/ip_example.yaml", `id: implementation_pattern.example
class: ImplementationPattern
label: Example pattern
status: active
when_to_use:
  - task one
  - task two
`)

	rootB := t.TempDir()
	writeAdmissionCorpusFile(t, rootB, "candidates/moved/example.yml", `# same declaration, moved and reformatted
when_to_use:
  - task one
  - task two
status: active
label: "Example pattern"
class: ImplementationPattern
id: implementation_pattern.example
`)

	a := digestFor(t, rootA, id)
	b := digestFor(t, rootB, id)
	if a != b {
		t.Fatalf("single-entity path/formatting changed corpus digest:\nA %s\nB %s", a, b)
	}

	writeAdmissionCorpusFile(t, rootB, "candidates/moved/example.yml", `id: implementation_pattern.example
class: ImplementationPattern
label: Changed pattern
status: active
when_to_use:
  - task one
  - task two
`)
	if changed := digestFor(t, rootB, id); changed == a {
		t.Fatalf("single-entity governed mutation did not change corpus digest: %s", changed)
	}
}

func TestAdmissionCorpusDigestSupportsImporterSingleEntityClasses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		class string
		id    string
	}{
		{name: "implementation pattern", class: "ImplementationPattern", id: "implementation_pattern.one"},
		{name: "design pattern", class: "DesignPattern", id: "pattern.one"},
		{name: "pattern misuse", class: "PatternMisuse", id: "pattern_misuse.one"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeAdmissionCorpusFile(t, root, "single.yaml", "id: "+tc.id+"\nclass: "+tc.class+"\nstatus: active\n")
			_ = digestFor(t, root, tc.id)
		})
	}
}

func TestAdmissionCorpusDigestTreatsAliasAsExpandedValue(t *testing.T) {
	const id = "invariant.alias.independent"

	rootAlias := t.TempDir()
	writeAdmissionCorpusFile(t, rootAlias, "invariants.yaml", `defaults: &details
  severity: high
invariants:
  - id: invariant.alias.independent
    details: *details
`)

	rootExpanded := t.TempDir()
	writeAdmissionCorpusFile(t, rootExpanded, "invariants.yaml", `invariants:
  - id: invariant.alias.independent
    details:
      severity: high
`)

	if a, b := digestFor(t, rootAlias, id), digestFor(t, rootExpanded, id); a != b {
		t.Fatalf("alias spelling changed corpus digest:\naliased  %s\nexpanded %s", a, b)
	}
}

func TestAdmissionCorpusDigestIgnoresUnadmittedCandidates(t *testing.T) {
	const governed = "invariant.governed"
	root := t.TempDir()
	writeAdmissionCorpusFile(t, root, "invariants.yaml", `invariants:
  - id: invariant.governed
    title: governed
    severity: high
`)
	before := digestFor(t, root, governed)

	writeAdmissionCorpusFile(t, root, "candidates/invariant_candidates.yaml", `invariants:
  - id: candidate.invariant.one
    title: candidate one
    severity: low
  - id: candidate.invariant.two
    title: candidate two
    severity: low
`)
	after := digestFor(t, root, governed)
	if before != after {
		t.Fatalf("unadmitted candidate generation changed corpus digest: %s -> %s", before, after)
	}
}

func TestAdmissionCorpusDigestChangesWhenGovernedKnowledgeChanges(t *testing.T) {
	const id = "invariant.governed"
	root := t.TempDir()
	writeAdmissionCorpusFile(t, root, "invariants.yaml", `invariants:
  - id: invariant.governed
    title: before
    severity: high
`)
	before := digestFor(t, root, id)

	writeAdmissionCorpusFile(t, root, "invariants.yaml", `invariants:
  - id: invariant.governed
    title: after
    severity: high
`)
	after := digestFor(t, root, id)
	if before == after {
		t.Fatalf("governed source mutation did not change corpus digest: %s", before)
	}
}

func TestAdmissionCorpusDigestRetainsDuplicateDeclarations(t *testing.T) {
	const id = "meta.same.identity"
	root := t.TempDir()
	body := `principles:
  - id: meta.same.identity
    statement: one stable identity
`
	writeAdmissionCorpusFile(t, root, "a.yaml", body)
	one := digestFor(t, root, id)

	// A second authored declaration of the same stable identity is not silently
	// collapsed. The live meta.* corpus contains this exact shape.
	writeAdmissionCorpusFile(t, root, "nested/b.yaml", body)
	two := digestFor(t, root, id)
	if one == two {
		t.Fatal("duplicate authored declaration was collapsed out of the corpus digest")
	}

	// File enumeration order and pathname still do not matter.
	rootReordered := t.TempDir()
	writeAdmissionCorpusFile(t, rootReordered, "z.yaml", body)
	writeAdmissionCorpusFile(t, rootReordered, "aa/deeper.yaml", body)
	if got := digestFor(t, rootReordered, id); got != two {
		t.Fatalf("same duplicate declaration multiset produced different digest: %s != %s", got, two)
	}
}

func TestAdmissionCorpusDigestExcludesGeneratedOutput(t *testing.T) {
	const id = "invariant.authored"
	root := t.TempDir()
	writeAdmissionCorpusFile(t, root, "invariants.yaml", `invariants:
  - id: invariant.authored
    title: authored
`)
	before := digestFor(t, root, id)

	writeAdmissionCorpusFile(t, root, "generated/report.yaml", `invariants:
  - id: invariant.authored
    title: generated copy that must not bind admission
`)
	after := digestFor(t, root, id)
	if before != after {
		t.Fatalf("generated output changed admission corpus digest: %s -> %s", before, after)
	}
}

func TestAdmissionCorpusDigestRequiresAuthoredDeclaration(t *testing.T) {
	root := t.TempDir()
	writeAdmissionCorpusFile(t, root, "generated/only.yaml", `invariants:
  - id: invariant.generated.only
    title: generated only
`)
	if _, err := AdmissionCorpusDigest(root, []string{"invariant.generated.only"}); err == nil {
		t.Fatal("generated-only admitted identity unexpectedly produced a corpus digest")
	}
}
