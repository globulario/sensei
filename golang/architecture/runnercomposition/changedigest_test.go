// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGitChangeDigestRejectsEmptyDirs(t *testing.T) {
	dir := t.TempDir()
	if _, err := GitChangeDigest(context.Background(), "", dir); err == nil {
		t.Error("expected an empty oldDir to be rejected")
	}
	if _, err := GitChangeDigest(context.Background(), dir, ""); err == nil {
		t.Error("expected an empty newDir to be rejected")
	}
}

// TestGitChangeDigestWorksOutsideAnyGitRepository proves --no-index does
// what O3 needs: oldDir/newDir are plain ephemeral directories, never
// git-initialized, and GitChangeDigest still succeeds.
func TestGitChangeDigestWorksOutsideAnyGitRepository(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(oldDir, "a.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "a.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	digest, err := GitChangeDigest(context.Background(), oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	if digest == "" {
		t.Error("GitChangeDigest returned an empty digest")
	}
}

func TestGitChangeDigestIdenticalDirsProduceSameDigest(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(oldDir, "a.txt"), []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "a.txt"), []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}

	d1, err := GitChangeDigest(context.Background(), oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := GitChangeDigest(context.Background(), oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Errorf("GitChangeDigest is not deterministic for identical dirs: %q != %q", d1, d2)
	}
}

func TestGitChangeDigestDetectsContentChange(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "a.txt"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := t.TempDir()
	if err := os.WriteFile(filepath.Join(changed, "a.txt"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	unchangedDigest, err := GitChangeDigest(context.Background(), base, changed)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(changed, "a.txt"), []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}
	changedDigest, err := GitChangeDigest(context.Background(), base, changed)
	if err != nil {
		t.Fatal(err)
	}

	if unchangedDigest == changedDigest {
		t.Error("GitChangeDigest did not change when file content changed")
	}
}

func TestGitChangeDigestDetectsAddedFile(t *testing.T) {
	base := t.TempDir()
	changed := t.TempDir()
	before, err := GitChangeDigest(context.Background(), base, changed)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(changed, "new.txt"), []byte("added"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := GitChangeDigest(context.Background(), base, changed)
	if err != nil {
		t.Fatal(err)
	}

	if before == after {
		t.Error("GitChangeDigest did not change when a file was added")
	}
}

// TestGitChangeDigestPropagatesRealError proves a genuine git failure
// (a nonexistent path, which git diff --no-index reports as a hard error,
// not "no difference") is surfaced as an error, not silently treated as an
// empty/identical diff.
func TestGitChangeDigestPropagatesRealError(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist")
	if _, err := GitChangeDigest(context.Background(), missing, dir); err == nil {
		t.Error("expected a nonexistent oldDir to produce an error")
	}
}

// TestGitChangeDigestIsPathIndependent is the architect's first blocker,
// proven directly: the identical logical change (the same file content
// transition), computed from two COMPLETELY SEPARATE pairs of ephemeral
// directories (different t.TempDir() calls, so different random names,
// different depths, different parents), must produce the identical digest.
// A naive `git diff --no-index oldDir newDir` invocation would embed each
// pair's own random path in its output and fail this.
func TestGitChangeDigestIsPathIndependent(t *testing.T) {
	pairADigest := buildAndDiff(t, "before", "after")
	pairBDigest := buildAndDiff(t, "before", "after")
	if pairADigest != pairBDigest {
		t.Errorf("GitChangeDigest depends on ephemeral directory naming: %q != %q for the identical logical change", pairADigest, pairBDigest)
	}
}

func buildAndDiff(t *testing.T, oldContent, newContent string) string {
	t.Helper()
	oldDir := t.TempDir()
	newDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(oldDir, "a.txt"), []byte(oldContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "a.txt"), []byte(newContent), 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := GitChangeDigest(context.Background(), oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

// TestGitChangeDigestNeverMutatesInputDirectories proves oldDir/newDir are
// read-only from GitChangeDigest's perspective: their content is byte-for-
// byte identical before and after the call, and neither path is moved or
// removed.
func TestGitChangeDigestNeverMutatesInputDirectories(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(oldDir, "a.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "a.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := GitChangeDigest(context.Background(), oldDir, newDir); err != nil {
		t.Fatal(err)
	}

	oldContent, err := os.ReadFile(filepath.Join(oldDir, "a.txt"))
	if err != nil {
		t.Fatalf("oldDir was disturbed by GitChangeDigest: %v", err)
	}
	if string(oldContent) != "old" {
		t.Errorf("oldDir content = %q, want unchanged %q", oldContent, "old")
	}
	newContent, err := os.ReadFile(filepath.Join(newDir, "a.txt"))
	if err != nil {
		t.Fatalf("newDir was disturbed by GitChangeDigest: %v", err)
	}
	if string(newContent) != "new" {
		t.Errorf("newDir content = %q, want unchanged %q", newContent, "new")
	}
}

// TestGitChangeDigestIsConfigIndependent is the architect's second blocker,
// proven directly against a config value CONFIRMED to actually change git
// diff --no-index's output in this git version (verified by hand before
// writing this test -- see the commit message): core.autocrlf against
// CRLF-terminated content. A poisoned ambient environment -- HOME pointed
// at a directory whose global gitconfig sets core.autocrlf=true, plus
// GIT_CONFIG_GLOBAL pointed directly at that same poisoned config -- must
// not change GitChangeDigest's output at all, since it builds its own
// from-scratch environment (and explicitly forces core.autocrlf=false)
// rather than inheriting the calling process's.
func TestGitChangeDigestIsConfigIndependent(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(oldDir, "f.txt"), []byte("line1\r\nline2\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "f.txt"), []byte("line1\r\nline3\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	baseline, err := GitChangeDigest(context.Background(), oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}

	poisonedHome := t.TempDir()
	poisonedGlobalConfig := filepath.Join(poisonedHome, "poisoned-gitconfig")
	if err := os.WriteFile(poisonedGlobalConfig, []byte("[core]\n\tautocrlf = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origHome, hadHome := os.LookupEnv("HOME")
	origGlobal, hadGlobal := os.LookupEnv("GIT_CONFIG_GLOBAL")
	t.Cleanup(func() {
		if hadHome {
			os.Setenv("HOME", origHome)
		} else {
			os.Unsetenv("HOME")
		}
		if hadGlobal {
			os.Setenv("GIT_CONFIG_GLOBAL", origGlobal)
		} else {
			os.Unsetenv("GIT_CONFIG_GLOBAL")
		}
	})
	os.Setenv("HOME", poisonedHome)
	os.Setenv("GIT_CONFIG_GLOBAL", poisonedGlobalConfig)

	underPoison, err := GitChangeDigest(context.Background(), oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}

	if baseline != underPoison {
		t.Errorf("GitChangeDigest changed under a poisoned ambient HOME/GIT_CONFIG_GLOBAL (core.autocrlf=true): %q != %q", baseline, underPoison)
	}
}

// TestGitChangeDigestRejectsRelativePaths proves oldDir/newDir must be
// absolute -- a relative path's identity depends on the calling process's
// ambient current-working-directory state, which GitChangeDigest does not
// control.
func TestGitChangeDigestRejectsRelativePaths(t *testing.T) {
	dir := t.TempDir()
	if _, err := GitChangeDigest(context.Background(), "relative/old", dir); err == nil {
		t.Error("expected a relative oldDir to be rejected")
	}
	if _, err := GitChangeDigest(context.Background(), dir, "relative/new"); err == nil {
		t.Error("expected a relative newDir to be rejected")
	}
}

func TestGitChangeDigestRejectsFileRoots(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if _, err := GitChangeDigest(context.Background(), file, other); err == nil {
		t.Error("expected a regular-file oldDir to be rejected")
	}
	if _, err := GitChangeDigest(context.Background(), other, file); err == nil {
		t.Error("expected a regular-file newDir to be rejected")
	}
}

// TestGitChangeDigestRejectsSymlinkRoots is the architect's root-identity
// blocker, proven directly: os.Stat FOLLOWS a symlink root, so the prior
// validation would have let one pass; copyTree's filepath.WalkDir does NOT
// follow it, so it would have copied the symlink itself into staging rather
// than the real directory's content, and git diff --no-index would then
// have hashed the symlink's TARGET STRING rather than the candidate tree
// (see the negative control in the accompanying commit message for a direct
// demonstration).
func TestGitChangeDigestRejectsSymlinkRoots(t *testing.T) {
	real := t.TempDir()
	if err := os.WriteFile(filepath.Join(real, "f.txt"), []byte("real content"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()

	if _, err := GitChangeDigest(context.Background(), link, other); err == nil {
		t.Error("expected a symlink oldDir root to be rejected")
	}
	if _, err := GitChangeDigest(context.Background(), other, link); err == nil {
		t.Error("expected a symlink newDir root to be rejected")
	}
}
