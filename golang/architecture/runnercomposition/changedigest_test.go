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
