// SPDX-License-Identifier: AGPL-3.0-only

package protection

import "testing"

func TestGovernedSourceFiles_ExcludesGeneratedAndCandidates(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/awareness/invariants.yaml", "invariants: []\n")
	writeFile(t, root, "docs/awareness/generated/awareness_graph_code_symbols.yaml", "x: 1\n")
	writeFile(t, root, "docs/awareness/candidates/invariant_candidates.yaml", "x: 1\n")

	files, err := GovernedSourceFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"docs/awareness/invariants.yaml": true}
	got := map[string]bool{}
	for _, f := range files {
		got[f] = true
	}
	for f := range got {
		if isUnderAny(f, generatedDir, candidatesDir) {
			t.Fatalf("generated/candidates file leaked into governed sources: %s", f)
		}
	}
	if !got["docs/awareness/invariants.yaml"] {
		t.Fatalf("expected authored invariants.yaml in governed sources, got %v (want superset of %v)", got, want)
	}
}

func TestGovernedSourceReasons_ManualRegistryProtectedOnceItExists(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ManualRegistryFile, "files: []\n")
	reasons, err := GovernedSourceReasons(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reasons[ManualRegistryFile]) == 0 {
		t.Fatal("the manual registry's own definition must be unconditionally protected once it exists, regardless of its files: content")
	}
}

func TestGovernedSourceReasons_AbsentManualRegistryIsNotProtected(t *testing.T) {
	root := t.TempDir()
	// Nothing under docs/awareness/ at all — a truly pristine repository.
	reasons, err := GovernedSourceReasons(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reasons[ManualRegistryFile]) != 0 {
		t.Fatal("a manual registry that was never scaffolded must not appear as a phantom protected path")
	}
}
