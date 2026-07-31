// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestBuildManifestReadsRegularFilesAndExecutables(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := BuildManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(entries), entries)
	}
	byPath := map[string]CandidateManifestEntry{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	a, ok := byPath["a.txt"]
	if !ok || a.Mode != ModeRegular || string(a.Content) != "hello" {
		t.Errorf("a.txt entry = %+v, want regular content %q", a, "hello")
	}
	run, ok := byPath["run.sh"]
	if !ok || run.Mode != ModeExecutable {
		t.Errorf("run.sh entry = %+v, want ModeExecutable", run)
	}
}

func TestBuildManifestNestedDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "b", "c.txt"), []byte("deep"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := BuildManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Path != "a/b/c.txt" {
		t.Fatalf("entries = %+v, want exactly one entry at a/b/c.txt", entries)
	}
}

// TestBuildManifestOmitsDirectoriesAsEntries proves an empty directory
// produces no manifest entry of its own -- directories are implicit,
// reconstructed from their children's paths.
func TestBuildManifestOmitsDirectoriesAsEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := BuildManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Path != "f.txt" {
		t.Fatalf("entries = %+v, want exactly one entry at f.txt (empty/ must not appear)", entries)
	}
}

// TestBuildManifestReadsSymlinkAsOpaqueEntryWithoutFollowing proves a
// symlink becomes its own manifest entry with the raw target string,
// without BuildManifest ever dereferencing it -- even a target that does
// not exist must not cause an error.
func TestBuildManifestReadsSymlinkAsOpaqueEntryWithoutFollowing(t *testing.T) {
	root := t.TempDir()
	target := "/nonexistent/path/never/followed"
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	entries, err := BuildManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want exactly one symlink entry", entries)
	}
	e := entries[0]
	if e.Mode != ModeSymlink || e.SymlinkTarget != target {
		t.Errorf("symlink entry = %+v, want target %q", e, target)
	}
}

// TestBuildManifestDoesNotDescendIntoSymlinkedDirectory proves a symlink to
// a directory is recorded as its own opaque symlink entry, and BuildManifest
// never walks INTO it -- the real directory it points at is only visited
// via its own real path, not duplicated via the symlink's path.
func TestBuildManifestDoesNotDescendIntoSymlinkedDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "realdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "realdir", "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("realdir", filepath.Join(root, "linkdir")); err != nil {
		t.Fatal(err)
	}

	entries, err := BuildManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = e.Path
	}
	sort.Strings(paths)

	want := []string{"linkdir", "realdir/f.txt"}
	if len(paths) != len(want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("paths = %v, want %v", paths, want)
			break
		}
	}
}

func TestBuildManifestRejectsSymlinkRoot(t *testing.T) {
	parent := t.TempDir()
	real := t.TempDir()
	link := filepath.Join(parent, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildManifest(link); err == nil {
		t.Error("expected a symlink root to be rejected")
	}
}

func TestBuildManifestRejectsMissingRoot(t *testing.T) {
	if _, err := BuildManifest(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Error("expected a missing root to be rejected")
	}
}

func TestBuildManifestRejectsFileRoot(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildManifest(file); err == nil {
		t.Error("expected a regular-file root to be rejected")
	}
}

func TestBuildManifestResultIsUsableByManifestDigest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "z.txt"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := BuildManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ManifestDigest(entries); err != nil {
		t.Errorf("ManifestDigest rejected BuildManifest's own output: %v", err)
	}
}

func TestBuildManifestDeterministic(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	e1, err := BuildManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	d1, err := ManifestDigest(e1)
	if err != nil {
		t.Fatal(err)
	}
	e2, err := BuildManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := ManifestDigest(e2)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Errorf("BuildManifest is not deterministic: %q != %q", d1, d2)
	}
}
