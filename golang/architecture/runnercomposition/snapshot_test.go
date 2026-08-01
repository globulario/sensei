// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// initTestRepo creates a real git repository in a fresh temp directory,
// runs setup (which should git add/commit as needed), and returns the
// repository's absolute root path.
func initTestRepo(t *testing.T, setup func(root string)) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "test")
	setup(root)
	return root
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestExtractSnapshotPreservesRegularExecutableAndSymlink(t *testing.T) {
	root := initTestRepo(t, func(root string) {
		if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("a.txt", filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
		runGit(t, root, "add", "-A")
		runGit(t, root, "commit", "-q", "-m", "init")
	})
	rev := runGit(t, root, "rev-parse", "HEAD")

	snapshotDir, manifest, digest, cleanup, err := ExtractSnapshot(context.Background(), root, rev)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cleanup(); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()

	byPath := map[string]CandidateManifestEntry{}
	for _, e := range manifest {
		byPath[e.Path] = e
	}

	a, ok := byPath["a.txt"]
	if !ok || a.Mode != ModeRegular || string(a.Content) != "hello" {
		t.Errorf("a.txt = %+v, want regular %q", a, "hello")
	}
	run, ok := byPath["run.sh"]
	if !ok || run.Mode != ModeExecutable {
		t.Errorf("run.sh = %+v, want ModeExecutable", run)
	}
	link, ok := byPath["link"]
	if !ok || link.Mode != ModeSymlink || link.SymlinkTarget != "a.txt" {
		t.Errorf("link = %+v, want symlink to %q", link, "a.txt")
	}

	// The returned manifest must be BOUND to what was actually written to
	// disk, not merely a plausible-looking side value: re-derive the
	// manifest independently from the filesystem and confirm it produces
	// the identical digest.
	diskEntries, err := BuildManifest(snapshotDir)
	if err != nil {
		t.Fatal(err)
	}
	diskDigest, err := ManifestDigest(diskEntries)
	if err != nil {
		t.Fatal(err)
	}
	if diskDigest != digest {
		t.Errorf("digest returned by ExtractSnapshot (%q) does not match ManifestDigest(BuildManifest(snapshotDir)) (%q)", digest, diskDigest)
	}
	wantDigest, err := ManifestDigest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if wantDigest != digest {
		t.Errorf("digest returned by ExtractSnapshot (%q) does not equal ManifestDigest(manifest) (%q)", digest, wantDigest)
	}
}

// TestExtractSnapshotBypassesExportSubst is the core correctness proof:
// git archive would substitute $Format:%H$ with the real commit hash for a
// file marked export-subst; ExtractSnapshot must return the RAW,
// unsubstituted blob bytes, matching git show/cat-file exactly.
func TestExtractSnapshotBypassesExportSubst(t *testing.T) {
	root := initTestRepo(t, func(root string) {
		if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("*.txt export-subst\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("hash is $Format:%H$\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, root, "add", "-A")
		runGit(t, root, "commit", "-q", "-m", "init")
	})
	rev := runGit(t, root, "rev-parse", "HEAD")

	snapshotDir, _, _, cleanup, err := ExtractSnapshot(context.Background(), root, rev)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	got, err := os.ReadFile(filepath.Join(snapshotDir, "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := "hash is $Format:%H$\n"
	if string(got) != want {
		t.Errorf("f.txt content = %q, want raw unsubstituted %q -- ExtractSnapshot applied export-subst", got, want)
	}

	// Cross-check against git show, the canonical raw-blob accessor.
	rawFromGit := runGit(t, root, "show", rev+":f.txt")
	if strings.TrimRight(string(got), "\n") != rawFromGit {
		t.Errorf("extracted content %q does not match git show's raw blob %q", got, rawFromGit)
	}
}

// TestExtractSnapshotAndInitializeCandidateBufferProduceIdenticalManifests
// proves hard law 8: immediately after buffer initialization, the buffer's
// manifest (and therefore its ManifestDigest) is identical to the
// snapshot's -- no mutation has happened yet.
func TestExtractSnapshotAndInitializeCandidateBufferProduceIdenticalManifests(t *testing.T) {
	root := initTestRepo(t, func(root string) {
		if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "sub", "b.txt"), []byte("nested"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("top"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, root, "add", "-A")
		runGit(t, root, "commit", "-q", "-m", "init")
	})
	rev := runGit(t, root, "rev-parse", "HEAD")

	snapshotDir, _, snapshotDigest, snapshotCleanup, err := ExtractSnapshot(context.Background(), root, rev)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshotCleanup()

	bufferDir, bufferCleanup, err := InitializeCandidateBuffer(snapshotDir)
	if err != nil {
		t.Fatal(err)
	}
	defer bufferCleanup()

	bufferEntries, err := BuildManifest(bufferDir)
	if err != nil {
		t.Fatal(err)
	}
	bufferDigest, err := ManifestDigest(bufferEntries)
	if err != nil {
		t.Fatal(err)
	}
	if snapshotDigest != bufferDigest {
		t.Errorf("snapshot digest %q != freshly-initialized buffer digest %q", snapshotDigest, bufferDigest)
	}
}

func TestExtractSnapshotRejectsEmptyBaseRevision(t *testing.T) {
	root := initTestRepo(t, func(root string) {
		os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644)
		runGit(t, root, "add", "-A")
		runGit(t, root, "commit", "-q", "-m", "init")
	})
	if _, _, _, _, err := ExtractSnapshot(context.Background(), root, ""); err == nil {
		t.Error("expected an empty baseRevision to be rejected")
	}
}

// TestExtractSnapshotRejectsFlagLikeBaseRevision defends against
// baseRevision being misinterpreted as a git option rather than a
// positional revision.
func TestExtractSnapshotRejectsFlagLikeBaseRevision(t *testing.T) {
	root := initTestRepo(t, func(root string) {
		os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644)
		runGit(t, root, "add", "-A")
		runGit(t, root, "commit", "-q", "-m", "init")
	})
	if _, _, _, _, err := ExtractSnapshot(context.Background(), root, "--upload-pack=evil"); err == nil {
		t.Error("expected a flag-like baseRevision to be rejected")
	}
}

// TestExtractSnapshotRejectsSymbolicRevisions is the architect's
// structural-pinning blocker, proven directly: a branch name, "HEAD", an
// abbreviated SHA, and a relative expression are all valid Git tree-ish
// values that `git ls-tree`/`git archive` would happily resolve, but none
// of them is a structurally pinned commit identity -- a branch can move, an
// abbreviated SHA is ambiguous as history grows, and "HEAD"/relative
// expressions depend on ambient repository state at resolution time, not on
// what the caller actually pinned.
func TestExtractSnapshotRejectsSymbolicRevisions(t *testing.T) {
	root := initTestRepo(t, func(root string) {
		os.WriteFile(filepath.Join(root, "a.txt"), []byte("first"), 0o644)
		runGit(t, root, "add", "-A")
		runGit(t, root, "commit", "-q", "-m", "first")
		os.WriteFile(filepath.Join(root, "a.txt"), []byte("second"), 0o644)
		runGit(t, root, "add", "-A")
		runGit(t, root, "commit", "-q", "-m", "second")
	})
	full := runGit(t, root, "rev-parse", "HEAD")
	short := runGit(t, root, "rev-parse", "--short", "HEAD")

	for _, rev := range []string{"HEAD", "master", "main", short, "HEAD~1", full + "^{commit}"} {
		t.Run(rev, func(t *testing.T) {
			if _, _, _, _, err := ExtractSnapshot(context.Background(), root, rev); err == nil {
				t.Errorf("expected symbolic/relative/abbreviated revision %q to be rejected", rev)
			}
		})
	}
}

// TestExtractSnapshotRejectsNonCommitObjectID proves the structural-pinning
// check goes beyond shape: a full, syntactically valid 40-hex object ID
// that names a tree or a blob (not a commit) must still be rejected.
func TestExtractSnapshotRejectsNonCommitObjectID(t *testing.T) {
	root := initTestRepo(t, func(root string) {
		os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644)
		runGit(t, root, "add", "-A")
		runGit(t, root, "commit", "-q", "-m", "init")
	})
	commit := runGit(t, root, "rev-parse", "HEAD")
	treeOID := runGit(t, root, "rev-parse", commit+"^{tree}")
	blobOID := runGit(t, root, "rev-parse", commit+":a.txt")

	for _, oid := range []string{treeOID, blobOID} {
		t.Run(oid, func(t *testing.T) {
			if _, _, _, _, err := ExtractSnapshot(context.Background(), root, oid); err == nil {
				t.Errorf("expected non-commit object ID %q to be rejected", oid)
			}
		})
	}
}

// TestExtractSnapshotNeverFallsBackToHEAD proves there is no default
// revision anywhere in ExtractSnapshot: an invalid revision must fail, not
// silently resolve to HEAD or the live working tree.
func TestExtractSnapshotNeverFallsBackToHEAD(t *testing.T) {
	root := initTestRepo(t, func(root string) {
		os.WriteFile(filepath.Join(root, "a.txt"), []byte("head content"), 0o644)
		runGit(t, root, "add", "-A")
		runGit(t, root, "commit", "-q", "-m", "init")
	})
	// Well-formed 40-hex but not an object that exists in this repository.
	nonexistent := strings.Repeat("a", 40)
	if _, _, _, _, err := ExtractSnapshot(context.Background(), root, nonexistent); err == nil {
		t.Error("expected a nonexistent revision to be rejected rather than silently falling back to HEAD")
	}
}

// TestExtractSnapshotRejectsSubmodule proves a submodule (gitlink) entry is
// rejected outright -- there is no representation for it in the closed
// regular/executable/symlink mode vocabulary. Uses the low-level
// update-index plumbing to add a gitlink entry without needing a second
// real repository to submodule.
func TestExtractSnapshotRejectsSubmodule(t *testing.T) {
	root := initTestRepo(t, func(root string) {
		os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644)
		runGit(t, root, "add", "-A")
		fakeSHA := "0123456789abcdef0123456789abcdef01234567"
		runGit(t, root, "update-index", "--add", "--cacheinfo", "160000,"+fakeSHA+",submod")
		runGit(t, root, "commit", "-q", "-m", "init with gitlink")
	})
	rev := runGit(t, root, "rev-parse", "HEAD")

	if _, _, _, _, err := ExtractSnapshot(context.Background(), root, rev); err == nil {
		t.Error("expected a submodule (gitlink) entry to be rejected")
	}
}

// TestExtractSnapshotLeavesNothingBehindOnFailure is the "honest
// partial-cleanup" proof: a run that fails partway (here, after listing the
// tree succeeds but the revision itself is bogus so listing fails
// immediately) must not leave a staging directory behind.
func TestExtractSnapshotLeavesNothingBehindOnFailure(t *testing.T) {
	root := initTestRepo(t, func(root string) {
		os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644)
		runGit(t, root, "add", "-A")
		runGit(t, root, "commit", "-q", "-m", "init")
	})

	before, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	nonexistent := strings.Repeat("a", 40)
	if _, _, _, _, err := ExtractSnapshot(context.Background(), root, nonexistent); err == nil {
		t.Fatal("expected an error")
	}

	after, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range after {
		if strings.HasPrefix(e.Name(), "runnercomposition-snapshot-") {
			found := false
			for _, b := range before {
				if b.Name() == e.Name() {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("ExtractSnapshot left a staging directory behind after failing: %s", e.Name())
			}
		}
	}
}

func TestInitializeCandidateBufferRejectsSymlinkRoot(t *testing.T) {
	real := t.TempDir()
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InitializeCandidateBuffer(link); err == nil {
		t.Error("expected a symlink snapshotDir to be rejected")
	}
}

func TestInitializeCandidateBufferRejectsRelativeRoot(t *testing.T) {
	if _, _, err := InitializeCandidateBuffer("relative/path"); err == nil {
		t.Error("expected a relative snapshotDir to be rejected")
	}
}

// TestInitializeCandidateBufferCleanupReportsSuccessAndRemovesDirectory
// proves the cleanup callback's error is real, not discarded: it returns
// nil on a normal removal, and the buffer directory is actually gone
// afterward.
func TestInitializeCandidateBufferCleanupReportsSuccessAndRemovesDirectory(t *testing.T) {
	snapshotDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(snapshotDir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	bufferDir, cleanup, err := InitializeCandidateBuffer(snapshotDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Errorf("cleanup() = %v, want nil", err)
	}
	if _, err := os.Stat(bufferDir); !os.IsNotExist(err) {
		t.Errorf("bufferDir %q still exists after cleanup()", bufferDir)
	}
}

// --- validateForMaterialization: unit tests against synthetic entries,
// since a well-formed git tree cannot itself produce these conflicts (this
// is deliberate defense in depth against a corrupted or adversarially
// crafted tree object, not a case reachable through normal git plumbing). ---

func TestValidateForMaterializationRejectsDuplicatePaths(t *testing.T) {
	entries := []revisionEntry{
		{path: "a.txt", mode: ModeRegular, oid: "oid1"},
		{path: "a.txt", mode: ModeRegular, oid: "oid2"},
	}
	if err := validateForMaterialization(entries); err == nil {
		t.Error("expected duplicate paths to be rejected")
	}
}

func TestValidateForMaterializationRejectsSymlinkParentConflict(t *testing.T) {
	entries := []revisionEntry{
		{path: "x", mode: ModeSymlink, oid: "oid1"},
		{path: "x/y", mode: ModeRegular, oid: "oid2"},
	}
	if err := validateForMaterialization(entries); err == nil {
		t.Error("expected a path nested under a symlink entry to be rejected")
	}
}

func TestValidateForMaterializationRejectsCaseInsensitiveCollision(t *testing.T) {
	entries := []revisionEntry{
		{path: "README.md", mode: ModeRegular, oid: "oid1"},
		{path: "readme.md", mode: ModeRegular, oid: "oid2"},
	}
	if err := validateForMaterialization(entries); err == nil {
		t.Error("expected a case-insensitive path collision to be rejected")
	}
}

func TestValidateForMaterializationRejectsUnicodeNormalizationCollision(t *testing.T) {
	// "é" (e + combining acute accent, NFD) normalizes to the same
	// NFC form as "é" (precomposed e-acute) -- two distinct byte
	// sequences that collide as the same filesystem entry on a
	// normalizing filesystem.
	precomposed := "caf\u00e9.txt" // U+00E9 LATIN SMALL LETTER E WITH ACUTE
	decomposed := "cafe\u0301.txt" // U+0065 U+0301 COMBINING ACUTE ACCENT
	entries := []revisionEntry{
		{path: precomposed, mode: ModeRegular, oid: "oid1"},
		{path: decomposed, mode: ModeRegular, oid: "oid2"},
	}
	if err := validateForMaterialization(entries); err == nil {
		t.Error("expected a Unicode NFC normalization collision to be rejected")
	}
}

func TestValidateForMaterializationAcceptsDistinctNonConflictingPaths(t *testing.T) {
	entries := []revisionEntry{
		{path: "a.txt", mode: ModeRegular, oid: "oid1"},
		{path: "dir/b.txt", mode: ModeRegular, oid: "oid2"},
		{path: "dir/sub/c.txt", mode: ModeExecutable, oid: "oid3"},
		{path: "link", mode: ModeSymlink, oid: "oid4"},
	}
	if err := validateForMaterialization(entries); err != nil {
		t.Errorf("expected non-conflicting paths to be accepted, got %v", err)
	}
}

// TestFetchBlobContentsAbortsPromptlyOnMissingObjectEvenUnderBackpressure
// reproduces the deadlock fetchBlobContents' abort helper exists to avoid:
// a batch large enough that the OS pipe buffer fills before git drains it
// (forcing the writer goroutine to block on a stdin write) combined with an
// error on the very first object read (a bogus, nonexistent oid placed
// first), so the read loop returns while the writer goroutine is still
// blocked. Without cancelling the process before Wait, this hangs forever;
// bounded in a goroutine + timeout so a regression fails the test cleanly
// instead of hanging the whole test binary.
func TestFetchBlobContentsAbortsPromptlyOnMissingObjectEvenUnderBackpressure(t *testing.T) {
	root := initTestRepo(t, func(root string) {
		os.WriteFile(filepath.Join(root, "seed.txt"), []byte("seed"), 0o644)
		runGit(t, root, "add", "-A")
		runGit(t, root, "commit", "-q", "-m", "init")
	})

	// Enough distinct blobs that their oids (40 hex chars + newline each)
	// exceed a typical 64KiB pipe buffer several times over.
	const n = 4000
	paths := make([]string, 0, n)
	filesDir := t.TempDir()
	for i := 0; i < n; i++ {
		p := filepath.Join(filesDir, fmt.Sprintf("f%d", i))
		if err := os.WriteFile(p, []byte(fmt.Sprintf("content-%d", i)), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	cmd := exec.Command("git", "hash-object", "-w", "--stdin-paths")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git hash-object: %v", err)
	}
	oids := strings.Fields(string(out))
	if len(oids) != n {
		t.Fatalf("got %d oids, want %d", len(oids), n)
	}

	nonexistent := strings.Repeat("f", 40)
	allOIDs := append([]string{nonexistent}, oids...)

	isolatedHome := t.TempDir()
	done := make(chan error, 1)
	go func() {
		_, err := fetchBlobContents(context.Background(), root, isolatedHome, allOIDs)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("expected an error for the nonexistent object")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("fetchBlobContents did not return within 15s -- likely deadlocked between the read loop's early return and the writer goroutine")
	}
}
