// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

	snapshotDir, cleanup, err := ExtractSnapshot(context.Background(), root, rev)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	entries, err := BuildManifest(snapshotDir)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]CandidateManifestEntry{}
	for _, e := range entries {
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

	snapshotDir, cleanup, err := ExtractSnapshot(context.Background(), root, rev)
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

	snapshotDir, snapshotCleanup, err := ExtractSnapshot(context.Background(), root, rev)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshotCleanup()

	bufferDir, bufferCleanup, err := InitializeCandidateBuffer(snapshotDir)
	if err != nil {
		t.Fatal(err)
	}
	defer bufferCleanup()

	snapshotEntries, err := BuildManifest(snapshotDir)
	if err != nil {
		t.Fatal(err)
	}
	bufferEntries, err := BuildManifest(bufferDir)
	if err != nil {
		t.Fatal(err)
	}
	snapshotDigest, err := ManifestDigest(snapshotEntries)
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
	if _, _, err := ExtractSnapshot(context.Background(), root, ""); err == nil {
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
	if _, _, err := ExtractSnapshot(context.Background(), root, "--upload-pack=evil"); err == nil {
		t.Error("expected a flag-like baseRevision to be rejected")
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
	if _, _, err := ExtractSnapshot(context.Background(), root, "not-a-real-revision"); err == nil {
		t.Error("expected an invalid revision to be rejected rather than silently falling back to HEAD")
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

	if _, _, err := ExtractSnapshot(context.Background(), root, rev); err == nil {
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

	if _, _, err := ExtractSnapshot(context.Background(), root, "not-a-real-revision"); err == nil {
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
