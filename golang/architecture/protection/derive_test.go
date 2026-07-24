// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"testing"
)

// contract §3.5 / §12 "successful scan with no supported signals yields
// EMPTY, not COMPLETE safety language" — an empty repository must never
// read as "safe," it must read as an explicit, distinct EMPTY state.
func TestDerive_EmptyRepoIsExplicitlyEmpty(t *testing.T) {
	root := t.TempDir()
	cov, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	if cov.Status != CoverageEmpty {
		t.Fatalf("expected EMPTY for a repository with zero protection signals, got %s", cov.Status)
	}
	if len(cov.ProtectedPaths) != 0 {
		t.Fatalf("expected zero protected paths, got %d", len(cov.ProtectedPaths))
	}
}

// contract §3.1 / bootstrap-gap regression: an empty manual `files:` list
// plus real governed/structural signals must never collapse to zero
// effective protection or a low-risk conclusion.
func TestDerive_EmptyManualListNeverHidesRealProtection(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ManualRegistryFile, "files: []\n")
	writeFile(t, root, "docs/awareness/invariants.yaml", testInvariantsYAML)

	cov, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cov.ProtectedPaths) == 0 {
		t.Fatal("an empty manual registry must not suppress governed-relation-derived protection")
	}
	fc, _ := ClassifyFile(cov, "src/core/engine.go")
	if !fc.Protected {
		t.Fatal("src/core/engine.go must be protected via governed relation despite an empty manual list")
	}
	if cov.Status == CoverageEmpty {
		t.Fatal("coverage with real derived protection must not report EMPTY")
	}
}

// contract §12 "unrelated sibling or transitively adjacent files are not
// automatically swept in" — the core non-goal this whole contract exists to
// prevent: blanket protection.
func TestDerive_UnrelatedFileNeverSweptIn(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/awareness/invariants.yaml", testInvariantsYAML)
	writeFile(t, root, ManualRegistryFile, "files: []\n")
	writeFile(t, root, "README.md", "# fixture\n")

	cov, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	fc, ok := ClassifyFile(cov, "README.md")
	if !ok {
		t.Fatal("classification of a normal path must succeed")
	}
	if fc.Protected {
		t.Fatalf("an unrelated README must not be swept into protection, got reasons=%+v", fc.Reasons)
	}
}

// contract §12 "coverage reasons and path ordering are deterministic across
// shuffled input order" — two derivations of identical logical content
// (built via different map/slice insertion order) must produce byte-for-byte
// identical GenerationIdentity and path ordering.
func TestDerive_DeterministicAcrossInputOrder(t *testing.T) {
	buildA := func() string {
		root := t.TempDir()
		writeFile(t, root, "docs/awareness/invariants.yaml", testInvariantsYAML)
		writeFile(t, root, "docs/awareness/failure_modes.yaml", testFailureModesYAML)
		writeFile(t, root, ManualRegistryFile, "files:\n  - src/auth/\n  - src/core/\n")
		cov, err := Derive(root)
		if err != nil {
			t.Fatal(err)
		}
		return cov.GenerationIdentity
	}
	buildB := func() string {
		root := t.TempDir()
		// Same logical content, different file/write order.
		writeFile(t, root, ManualRegistryFile, "files:\n  - src/core/\n  - src/auth/\n")
		writeFile(t, root, "docs/awareness/failure_modes.yaml", testFailureModesYAML)
		writeFile(t, root, "docs/awareness/invariants.yaml", testInvariantsYAML)
		cov, err := Derive(root)
		if err != nil {
			t.Fatal(err)
		}
		return cov.GenerationIdentity
	}
	idA := buildA()
	idB := buildB()
	if idA != idB {
		t.Fatalf("generation identity must be deterministic regardless of input order: %s != %s", idA, idB)
	}
}

// contract §3.7 outside-repository / malformed-path security: classifying a
// path that escapes the repository must fail typed, never silently report
// "not protected" (which would read as safe).
func TestClassifyFile_EscapingPathFailsTyped(t *testing.T) {
	root := t.TempDir()
	cov, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	_, ok := ClassifyFile(cov, "../../etc/passwd")
	if ok {
		t.Fatal("an escaping path must fail classification, not report protected=false as if safe")
	}
}

// contract §10-adjacent / §6 "protection reasons are surfaced" — a path
// protected only by a provisional (candidate) reason must be classified as
// Provisional=true; a path with any definite reason must not be.
func TestClassifyFile_ProvisionalVsDefinite(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/awareness/candidates/authority_surface_candidates.yaml", testAuthorityCandidatesYAML)
	writeFile(t, root, "docs/awareness/invariants.yaml", testInvariantsYAML)

	cov, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}

	provisionalOnly, _ := ClassifyFile(cov, "src/lifecycle/start.go")
	if !provisionalOnly.Protected || !provisionalOnly.Provisional {
		t.Fatalf("candidate-only path must be protected+provisional, got %+v", provisionalOnly)
	}

	definite, _ := ClassifyFile(cov, "src/core/engine.go")
	if !definite.Protected || definite.Provisional {
		t.Fatalf("a directly-governed path must be protected and NOT provisional, got %+v", definite)
	}
}
