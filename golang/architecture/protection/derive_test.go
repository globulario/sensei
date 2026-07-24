// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"os"
	"path/filepath"
	"strings"
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
	fc, _ := ClassifyFile(root, cov, "src/core/engine.go")
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
	fc, ok := ClassifyFile(root, cov, "README.md")
	if !ok {
		t.Fatal("classification of a normal path must succeed")
	}
	if fc.Protected {
		t.Fatalf("an unrelated README must not be swept into protection, got reasons=%+v", fc.Reasons)
	}
}

// contract §12 "coverage reasons and path ordering are deterministic across
// shuffled input order" — two independently-derived repositories with
// byte-IDENTICAL source content must produce byte-for-byte identical
// GenerationIdentity and path ordering, regardless of incidental internal
// nondeterminism (Go map iteration order while assembling ProtectedPaths,
// distinct absolute temp-dir paths, file-write order on disk).
//
// This is deliberately NOT "differently-ordered-but-semantically-equivalent
// YAML hashes the same" — GenerationIdentity binds raw source-file content
// (contract §3 correction), so a real byte-level edit — including reordering
// a list — MUST change identity. That is proven separately by
// TestSemanticDigest_ChangesOnEveryRequiredCase.
func TestDerive_DeterministicAcrossInputOrder(t *testing.T) {
	build := func() (ProtectionCoverage, string) {
		root := t.TempDir()
		writeFile(t, root, "docs/awareness/invariants.yaml", testInvariantsYAML)
		writeFile(t, root, "docs/awareness/failure_modes.yaml", testFailureModesYAML)
		writeFile(t, root, ManualRegistryFile, "files:\n  - src/auth/\n  - src/core/\n")
		cov, err := Derive(root)
		if err != nil {
			t.Fatal(err)
		}
		return cov, root
	}
	covA, _ := build()
	covB, _ := build()
	if covA.GenerationIdentity != covB.GenerationIdentity {
		t.Fatalf("generation identity must be deterministic across independent derivations of identical content: %s != %s",
			covA.GenerationIdentity, covB.GenerationIdentity)
	}
	if len(covA.ProtectedPaths) != len(covB.ProtectedPaths) {
		t.Fatal("protected path count must be deterministic")
	}
	for i := range covA.ProtectedPaths {
		if covA.ProtectedPaths[i].Path != covB.ProtectedPaths[i].Path {
			t.Fatalf("protected path ordering must be deterministic: index %d: %q != %q",
				i, covA.ProtectedPaths[i].Path, covB.ProtectedPaths[i].Path)
		}
	}
}

// contract §3 correction: GenerationIdentity must change when an invariant
// ID changes, a reason kind changes, source content changes, a candidate
// becomes non-provisional, or a different rule protects the same file — the
// exact five cases named in review. The prior (path-keys-only) digest left
// all five unchanged.
func TestSemanticDigest_ChangesOnEveryRequiredCase(t *testing.T) {
	baseline := func(invariantsYAML string) string {
		root := t.TempDir()
		writeFile(t, root, "docs/awareness/invariants.yaml", invariantsYAML)
		cov, err := Derive(root)
		if err != nil {
			t.Fatal(err)
		}
		return cov.GenerationIdentity
	}
	const original = `
invariants:
  - id: fixture.rule.one
    title: Original title
    severity: high
    protects:
      files:
        - src/target.go
`
	baseID := baseline(original)

	cases := map[string]string{
		"invariant ID changes": `
invariants:
  - id: fixture.rule.RENAMED
    title: Original title
    severity: high
    protects:
      files:
        - src/target.go
`,
		"a different rule protects the same file": `
invariants:
  - id: fixture.rule.one
    title: Original title
    severity: high
    protects:
      files:
        - src/target.go
  - id: fixture.rule.two
    title: A second rule
    severity: high
    protects:
      files:
        - src/target.go
`,
		"source content changes (unrelated field, same protects.files)": `
invariants:
  - id: fixture.rule.one
    title: A DIFFERENT title that does not affect protects.files
    severity: critical
    protects:
      files:
        - src/target.go
`,
	}
	for name, yaml := range cases {
		if got := baseline(yaml); got == baseID {
			t.Errorf("%s: expected GenerationIdentity to change, stayed %s", name, got)
		}
	}
}

// A candidate becoming non-provisional (i.e. the SAME file gaining a direct
// governed reason) must also change identity — proven directly since it
// changes AllProvisional on the affected ProtectedPath.
func TestSemanticDigest_ChangesWhenCandidateBecomesNonProvisional(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/awareness/candidates/authority_surface_candidates.yaml", testAuthorityCandidatesYAML)
	cov1, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "docs/awareness/invariants.yaml", `
invariants:
  - id: fixture.now.governs.the.candidate.file
    title: Now directly governs the same file the candidate pointed at
    severity: high
    protects:
      files:
        - src/lifecycle/start.go
`)
	cov2, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	if cov1.GenerationIdentity == cov2.GenerationIdentity {
		t.Fatal("expected GenerationIdentity to change when a candidate-only path becomes definitely governed")
	}
	fc, _ := ClassifyFile(root, cov2, "src/lifecycle/start.go")
	if fc.Provisional {
		t.Fatal("the path must no longer be provisional once a direct governed reason exists")
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
	_, ok := ClassifyFile(root, cov, "../../etc/passwd")
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

	provisionalOnly, _ := ClassifyFile(root, cov, "src/lifecycle/start.go")
	if !provisionalOnly.Protected || !provisionalOnly.Provisional {
		t.Fatalf("candidate-only path must be protected+provisional, got %+v", provisionalOnly)
	}

	definite, _ := ClassifyFile(root, cov, "src/core/engine.go")
	if !definite.Protected || definite.Provisional {
		t.Fatalf("a directly-governed path must be protected and NOT provisional, got %+v", definite)
	}
}

// contract §6 correction: a malformed individual input must force coverage
// below COMPLETE — never let a dropped/unparseable entry hide behind an
// otherwise-clean-looking result.
func TestDerive_MalformedManualEntryForcesPartial(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/awareness/invariants.yaml", testInvariantsYAML)
	writeFile(t, root, ManualRegistryFile, "files:\n  - ../escapes/\n  - src/auth/\n")

	cov, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	if cov.Status == CoverageComplete {
		t.Fatalf("a malformed manual entry must not allow COMPLETE, got %s (gaps=%v)", cov.Status, cov.Gaps)
	}
	found := false
	for _, g := range cov.Gaps {
		if strings.HasPrefix(g, "manual_registry_malformed_entry") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a manual_registry_malformed_entry gap, got %v", cov.Gaps)
	}
}

// A malformed candidate file must likewise force coverage below COMPLETE.
func TestDerive_MalformedCandidateFileForcesPartial(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/awareness/invariants.yaml", testInvariantsYAML)
	writeFile(t, root, "docs/awareness/candidates/broken.yaml", "not: [valid: yaml:::")

	cov, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	if cov.Status == CoverageComplete {
		t.Fatalf("a malformed candidate file must not allow COMPLETE, got %s (gaps=%v)", cov.Status, cov.Gaps)
	}
}

// contract §3 correction (second review round): the digest must bind
// coverage status, gap codes, and each scanner's success/failure — a
// transient scanner failure must change identity even when the assembled
// ProtectedPaths and consulted source-file bytes happen to be byte-for-byte
// identical. Proven directly against the pure semanticDigest function so no
// real Derive() scenario needs to (impossibly) hold every other input fixed
// while only the outcome changes.
func TestSemanticDigest_ChangesWithScannerOutcomeAloneEvenWhenPathsAndFilesAreIdentical(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/awareness/invariants.yaml", testInvariantsYAML)
	cov, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	sourceFiles := []string{"docs/awareness/invariants.yaml"}

	clean := derivationOutcome{}
	degraded := derivationOutcome{GovernedErr: true}
	malformed := derivationOutcome{RelationMalformedCount: 1}

	digestClean := semanticDigest(root, cov, sourceFiles, clean)
	digestDegraded := semanticDigest(root, cov, sourceFiles, degraded)
	digestMalformed := semanticDigest(root, cov, sourceFiles, malformed)

	if digestClean == digestDegraded {
		t.Fatal("expected GenerationIdentity to change when a scanner's error outcome changes, even with identical protected paths and source content")
	}
	if digestClean == digestMalformed {
		t.Fatal("expected GenerationIdentity to change when a malformed-input count changes, even with identical protected paths and source content")
	}

	covStale := cov
	covStale.Status = CoveragePartial
	digestStatusChanged := semanticDigest(root, covStale, sourceFiles, clean)
	if digestClean == digestStatusChanged {
		t.Fatal("expected GenerationIdentity to change when coverage status changes alone")
	}
}

// contract §3 correction: raw error text (which can embed a checkout's
// absolute filesystem path) must never enter GenerationIdentity — only
// normalized, path-free outcome codes may. Proven by deriving the identical
// repository content, with the identical malformed/unreadable condition,
// from two DIFFERENT temp-dir roots (so any leaked absolute path would
// necessarily differ between them) and requiring identical identities.
func TestSemanticDigest_NeverLeaksAbsolutePathsFromMalformedInputErrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file permissions are not enforced")
	}
	build := func() ProtectionCoverage {
		root := t.TempDir()
		writeFile(t, root, "docs/awareness/invariants.yaml", testInvariantsYAML)
		writeFile(t, root, "docs/awareness/candidates/locked.yaml", "candidates: []\n")
		full := filepath.Join(root, "docs", "awareness", "candidates", "locked.yaml")
		if err := os.Chmod(full, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(full, 0o644) })
		cov, err := Derive(root)
		if err != nil {
			t.Fatal(err)
		}
		return cov
	}
	covA := build()
	covB := build()
	if covA.Status == CoverageComplete {
		t.Fatalf("setup: expected the unreadable candidate file to force below-COMPLETE coverage, got %s", covA.Status)
	}
	if covA.GenerationIdentity != covB.GenerationIdentity {
		t.Fatalf("GenerationIdentity must not depend on checkout-specific absolute paths embedded in raw error text: %s != %s",
			covA.GenerationIdentity, covB.GenerationIdentity)
	}
}
