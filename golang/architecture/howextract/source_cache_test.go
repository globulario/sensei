// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"os"
	"path/filepath"
	"testing"
)

// A file is read once per run, so one document cannot hold receipts describing
// two different versions of the same file.
//
// Before this, every observation re-read and re-hashed its own source: 233,392
// observations over ~1,558 files in a self-extraction, each file read about 300
// times. The cost was the visible half; the invisible half was that a tree
// changing mid-run produced receipts that disagreed with each other, with
// nothing in the document saying which version it described.
func TestSourceCacheReadsEachFileOncePerRun(t *testing.T) {
	root := t.TempDir()
	rel := "pkg/thing.go"
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package pkg\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cache := newSourceCache(root)
	first, err := cache.digest(rel)
	if err != nil {
		t.Fatal(err)
	}
	firstLine, err := cache.lines(rel, 1, 1)
	if err != nil {
		t.Fatal(err)
	}

	// The tree changes underneath the run.
	if err := os.WriteFile(path, []byte("package pkg\n\nfunc B() {}\nfunc C() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	second, err := cache.digest(rel)
	if err != nil {
		t.Fatal(err)
	}
	secondLine, err := cache.lines(rel, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("one run recorded two digests for one file: %s then %s", first[:12], second[:12])
	}
	if firstLine != secondLine {
		t.Fatalf("one run captured two contents for one file: %q then %q", firstLine, secondLine)
	}
}

// A file that cannot be read is reported as unreadable every time it is asked
// for, rather than becoming readable because the failure was cached as success.
func TestSourceCacheKeepsReportingAnUnreadableFile(t *testing.T) {
	cache := newSourceCache(t.TempDir())
	if _, err := cache.digest("missing.go"); err == nil {
		t.Fatal("a missing file returned a digest")
	}
	if _, err := cache.digest("missing.go"); err == nil {
		t.Fatal("a missing file returned a digest on the second ask")
	}
	if _, err := cache.lines("missing.go", 1, 1); err == nil {
		t.Fatal("a missing file returned captured lines")
	}
}

// The cached path must produce exactly what the uncached reader produced, or
// this is a behaviour change wearing a performance fix's clothes.
func TestSourceCacheMatchesTheDirectReader(t *testing.T) {
	root := t.TempDir()
	rel := "a.go"
	path := filepath.Join(root, rel)
	content := "package a\nfunc One() {}\nfunc Two() {}\nfunc Three() {}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := newSourceCache(root)
	for _, r := range [][2]int{{1, 1}, {2, 3}, {1, 4}, {4, 4}} {
		want, wantErr := readCapturedLines(path, r[0], r[1])
		got, gotErr := cache.lines(rel, r[0], r[1])
		if (wantErr == nil) != (gotErr == nil) {
			t.Fatalf("lines %v: err mismatch %v vs %v", r, wantErr, gotErr)
		}
		if got != want {
			t.Fatalf("lines %v: got %q want %q", r, got, want)
		}
	}
}
