// SPDX-License-Identifier: AGPL-3.0-only

package factextract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A repository the extractor cannot see must say so, blocking.
//
// The observation surface is Go: both passes read .go files and nothing else.
// Run over a repository in another language, extraction returned an empty
// report whose only note was a non-blocking "directory prefix . does not
// contain main module" — which reads as "I looked and there was nothing to say"
// rather than "I cannot see this language at all". Measured on sqlite/sqlite:
// 315 C files, 0 facts, nothing blocking (#131 world 3).
func TestARepositoryOutsideTheObservationSurfaceSaysSo(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/main.c", "#include <stdio.h>\nint main(void){return 0;}\n")
	write("src/util.c", "int add(int a,int b){return a+b;}\n")
	write("README.md", "# a repository in a language this extractor cannot read\n")

	report, err := Extract(root, Options{IncludeTests: true, IncludeDocs: true, MinimumConfidence: "low"})
	if err != nil {
		t.Fatalf("extraction over an unreadable repository must report, not fail: %v", err)
	}
	if len(report.Facts) != 0 {
		t.Fatalf("facts=%d, want 0 from a repository with no Go", len(report.Facts))
	}

	var said bool
	for _, lim := range report.Limitations {
		if strings.Contains(lim.Reason, "observation surface") {
			if !lim.Blocking {
				t.Fatal("an unreadable repository was reported non-blocking; an empty result would read as a finding")
			}
			said = true
		}
	}
	if !said {
		t.Fatalf("nothing said the repository is outside the surface: %+v", report.Limitations)
	}
}

// A repository the extractor CAN see must not carry that limitation, or every
// ordinary run would claim to be blind.
func TestARepositoryInsideTheSurfaceIsNotReportedBlind(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/seen\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package seen\n\nfunc A() error { return nil }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Extract(root, Options{IncludeTests: true, MinimumConfidence: "low"})
	if err != nil {
		t.Fatal(err)
	}
	for _, lim := range report.Limitations {
		if strings.Contains(lim.Reason, "observation surface") {
			t.Fatalf("a Go repository was reported as outside the surface: %+v", lim)
		}
	}
}
