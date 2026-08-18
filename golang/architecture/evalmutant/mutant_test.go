// SPDX-License-Identifier: AGPL-3.0-only

package evalmutant

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func materialize(t *testing.T, m Mutant) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "tree")
	if err := Materialize(root, m); err != nil {
		t.Fatalf("materialize %s: %v", m.Defect, err)
	}
	return root
}

// THE test for a mutant suite. Every defect must be present in its own mutant
// and absent from the clean baseline.
//
// Without both halves the suite is decorative: a mutant that does not contain
// its defect means an evaluator "misses" nothing, and a baseline that already
// contains one means an evaluator "detects" something nobody introduced. Either
// way the resulting score describes the instrument rather than the subject —
// and nothing in a careful report would reveal it.
func TestEveryDefectIsPresentInItsMutantAndAbsentFromTheBaseline(t *testing.T) {
	baselineRoot := materialize(t, Baseline())

	for _, d := range Defects() {
		t.Run(string(d), func(t *testing.T) {
			witness, err := WitnessFor(d)
			if err != nil {
				t.Fatal(err)
			}

			present, detail, err := witness(baselineRoot)
			if err != nil {
				t.Fatalf("witness on the baseline: %v", err)
			}
			if present {
				t.Fatalf("the clean baseline already contains %s (%s); detection of it would be free", d, detail)
			}

			m, err := Build(d)
			if err != nil {
				t.Fatal(err)
			}
			mutantRoot := materialize(t, m)
			present, detail, err = witness(mutantRoot)
			if err != nil {
				t.Fatalf("witness on the mutant: %v", err)
			}
			if !present {
				t.Fatalf("%s does not actually contain its defect; this mutant would measure nothing", d)
			}
			if strings.TrimSpace(detail) == "" {
				t.Errorf("%s witness reports presence with no detail; a grader could not tell what was found", d)
			}
		})
	}
}

// A defect must not leak into its neighbours. If building one mutant also
// introduced another's defect, a detection could be credited to the wrong
// class and the per-class scores would be meaningless.
func TestEachMutantCarriesExactlyOneDefect(t *testing.T) {
	for _, d := range Defects() {
		t.Run(string(d), func(t *testing.T) {
			m, err := Build(d)
			if err != nil {
				t.Fatal(err)
			}
			root := materialize(t, m)

			for _, other := range Defects() {
				if other == d {
					continue
				}
				witness, err := WitnessFor(other)
				if err != nil {
					t.Fatal(err)
				}
				present, detail, err := witness(root)
				if err != nil {
					t.Fatalf("witness %s: %v", other, err)
				}
				if present {
					t.Errorf("the %s mutant also contains %s (%s); a detection could be credited to the wrong class", d, other, detail)
				}
			}
		})
	}
}

// Bytes must be stable, or a suite committed today measures something else in
// six months and the comparison silently stops meaning anything.
func TestMutantsAreDeterministic(t *testing.T) {
	for _, d := range Defects() {
		first, err := Build(d)
		if err != nil {
			t.Fatal(err)
		}
		second, err := Build(d)
		if err != nil {
			t.Fatal(err)
		}
		if len(first.Files) != len(second.Files) {
			t.Fatalf("%s: file count differs between builds", d)
		}
		for path, body := range first.Files {
			if second.Files[path] != body {
				t.Fatalf("%s: %s differs between builds", d, path)
			}
		}
		if first.Statement != second.Statement || first.CommitMessage != second.CommitMessage {
			t.Fatalf("%s: reference statement or commit message is not stable", d)
		}
	}
}

// Every mutant must differ from the baseline. A mutator that changed nothing
// would produce a tree any evaluator passes for free.
func TestEveryMutantDiffersFromTheBaseline(t *testing.T) {
	base := Baseline()
	for _, d := range Defects() {
		m, err := Build(d)
		if err != nil {
			t.Fatal(err)
		}
		if len(m.TouchedPaths) == 0 {
			t.Errorf("%s touched no path", d)
		}
		same := len(m.Files) == len(base.Files)
		if same {
			for p, body := range base.Files {
				if m.Files[p] != body {
					same = false
					break
				}
			}
		}
		if same {
			t.Errorf("%s is byte-identical to the baseline", d)
		}
	}
}

// The reference statement is what a grader compares a detection against, so it
// has to say what is wrong in terms a human can judge — not merely name the
// defect class back.
func TestEveryMutantStatesWhatIsWrong(t *testing.T) {
	for _, d := range Defects() {
		m, err := Build(d)
		if err != nil {
			t.Fatal(err)
		}
		if len(strings.Fields(m.Statement)) < 8 {
			t.Errorf("%s statement is too thin to grade against: %q", d, m.Statement)
		}
		if strings.TrimSpace(m.CommitMessage) == "" {
			t.Errorf("%s carries no commit message", d)
		}
	}
}

// The baseline must compile-shape as a coherent tree: the suite's value depends
// on the control being ordinary, not subtly broken in ways that could be
// mistaken for a defect.
func TestBaselineIsCoherent(t *testing.T) {
	base := Baseline()
	for _, required := range []string{
		"go.mod", "docs/ARCHITECTURE.md", "api/api.go", "service/service.go",
		"storage/storage.go", "billing/billing.go", "billing/surface.go",
		"shipping/shipping.go", "docs/claims.md", "evidence/rate_limit_test_report.txt",
		"docs/generated/inventory.md", "docs/obligations.md",
	} {
		if _, ok := base.Files[required]; !ok {
			t.Errorf("baseline is missing %s", required)
		}
	}
	// The documented direction must actually hold in the control.
	if strings.Contains(base.Files["storage/storage.go"], "example.com/eval/service") {
		t.Error("the baseline already inverts the dependency direction")
	}
	// And the published-surface rule must hold too.
	if !strings.Contains(base.Files["shipping/shipping.go"], "billing.Quote(") {
		t.Error("the baseline does not use billing's published surface")
	}
}

// Materialize must refuse a dirty directory: a mutant blended with unrelated
// content is no longer the bounded defect it claims to be.
func TestMaterializeRefusesANonEmptyDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tree")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Materialize(root, Baseline()); err == nil {
		t.Fatal("a non-empty directory was accepted")
	}
}

func TestUnknownDefectIsRefused(t *testing.T) {
	if _, err := Build("not_a_defect"); err == nil {
		t.Fatal("an unknown defect was built")
	}
	if _, err := WitnessFor("not_a_defect"); err == nil {
		t.Fatal("an unknown defect returned a witness")
	}
}

// The suite must cover exactly the classes issue #131 enumerates — no more, so
// it measures what was asked for, and no fewer, so a gap is not mistaken for a
// clean result.
func TestSuiteCoversTheEnumeratedDefectClasses(t *testing.T) {
	want := map[Defect]bool{
		DefectAuthoritySplit: true, DefectForbiddenDependencyDirection: true,
		DefectStaleDocumentation: true, DefectImplementationRepetition: true,
		DefectMissingEvidenceProvider: true, DefectMisleadingCommitMessage: true,
		DefectGeneratedFileContamination: true, DefectCrossDomainLeakage: true,
		DefectIncompleteProofDischarge: true,
	}
	got := map[Defect]bool{}
	for _, d := range Defects() {
		got[d] = true
		if !want[d] {
			t.Errorf("suite contains %s, which issue #131 does not enumerate", d)
		}
		if _, err := WitnessFor(d); err != nil {
			t.Errorf("%s has no witness", d)
		}
	}
	for d := range want {
		if !got[d] {
			t.Errorf("suite is missing the enumerated class %s", d)
		}
	}
}
