// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizePath_ValidCases(t *testing.T) {
	cases := map[string]string{
		"src/auth/login.go":   "src/auth/login.go",
		"./src/auth/login.go": "src/auth/login.go",
		"src\\auth\\login.go": "src/auth/login.go",
		"src/./auth/login.go": "src/auth/login.go",
	}
	for in, want := range cases {
		got, ok := NormalizePath(in)
		if !ok {
			t.Errorf("NormalizePath(%q): expected ok, got not-ok", in)
			continue
		}
		if got != want {
			t.Errorf("NormalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizePath_RejectsEscapes(t *testing.T) {
	bad := []string{
		"",
		"   ",
		"../etc/passwd",
		"../../x",
		"a/../../b",
		`C:\Windows\System32`,
		`\\host\share\file`,
		// A POSIX absolute path is not a repo-relative declaration.
		// NormalizePath has no repository root to resolve it against, so it
		// must reject rather than silently reinterpret "/etc/passwd" as
		// "etc/passwd" — a real absolute path goes through ResolveRepoPath.
		"/etc/passwd",
		"/src/auth/login.go",
	}
	for _, in := range bad {
		if _, ok := NormalizePath(in); ok {
			t.Errorf("NormalizePath(%q): expected reject, got accepted", in)
		}
	}
}

// contract §3.7: "avoid src/auth/ matching src/authorization/ unless
// explicitly intended" — the exact bug class this package must never
// reintroduce.
func TestInPathScope_SegmentBoundarySafe(t *testing.T) {
	if InPathScope("src/authorization/x.go", "src/auth") {
		t.Fatal("src/auth must not match src/authorization/x.go (segment-boundary violation)")
	}
	if !InPathScope("src/auth/login.go", "src/auth") {
		t.Fatal("src/auth must match src/auth/login.go")
	}
	if !InPathScope("src/auth", "src/auth") {
		t.Fatal("a prefix must match its own exact path")
	}
	if InPathScope("src/auth2/x.go", "src/auth/") {
		t.Fatal("src/auth/ must not match src/auth2/x.go")
	}
}

func TestInPathScope_TrailingSlashAgnostic(t *testing.T) {
	if !InPathScope("golang/server/main.go", "golang/server/") {
		t.Fatal("trailing slash on prefix must still match a descendant")
	}
	if !InPathScope("golang/server/main.go", "golang/server") {
		t.Fatal("no trailing slash on prefix must still match a descendant")
	}
}

// contract §2 correction: ResolveRepoPath must prove all five required
// cases — absolute-inside works, absolute-outside/traversal/symlink-escape
// fail, and Windows drive/UNC paths fail.

func TestResolveRepoPath_AbsolutePathInsideRepository(t *testing.T) {
	root := t.TempDir()
	full := filepath.Join(root, "src", "auth", "login.go")
	writeFile(t, root, "src/auth/login.go", "package auth\n")
	got, ok := ResolveRepoPath(root, full)
	if !ok {
		t.Fatal("an absolute path inside the repository must resolve")
	}
	if got != "src/auth/login.go" {
		t.Fatalf("got %q, want src/auth/login.go", got)
	}
}

func TestResolveRepoPath_RelativePathInsideRepository(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/auth/login.go", "package auth\n")
	got, ok := ResolveRepoPath(root, "src/auth/login.go")
	if !ok || got != "src/auth/login.go" {
		t.Fatalf("got (%q, %v), want (src/auth/login.go, true)", got, ok)
	}
}

// A path that does not exist on disk yet (an about-to-be-created file) must
// still resolve, via the nearest existing ancestor directory.
func TestResolveRepoPath_NotYetCreatedFileStillResolves(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "auth"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := ResolveRepoPath(root, "src/auth/new_file.go")
	if !ok || got != "src/auth/new_file.go" {
		t.Fatalf("got (%q, %v), want (src/auth/new_file.go, true)", got, ok)
	}
}

func TestResolveRepoPath_AbsolutePathOutsideRepositoryFails(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir() // a sibling temp dir — definitely outside root
	full := filepath.Join(outside, "secret.txt")
	writeFile(t, outside, "secret.txt", "x")
	if _, ok := ResolveRepoPath(root, full); ok {
		t.Fatal("an absolute path outside the repository must fail")
	}
	if _, ok := ResolveRepoPath(root, "/etc/passwd"); ok {
		t.Fatal("/etc/passwd must never resolve as repo-relative")
	}
}

func TestResolveRepoPath_TraversalFails(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.go", "package main\n")
	for _, p := range []string{"../etc/passwd", "../../etc/passwd", "src/../../etc/passwd"} {
		if _, ok := ResolveRepoPath(root, p); ok {
			t.Errorf("traversal path %q must fail to resolve", p)
		}
	}
}

func TestResolveRepoPath_SymlinkEscapeFails(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "secret.txt", "x")
	// A symlink INSIDE the repo that points OUTSIDE it.
	linkPath := filepath.Join(root, "escape_link")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Skipf("symlinks unavailable in this environment: %v", err)
	}
	if _, ok := ResolveRepoPath(root, "escape_link/secret.txt"); ok {
		t.Fatal("a symlink escaping the repository must fail to resolve")
	}
}

func TestResolveRepoPath_WindowsDriveAndUNCPathsFail(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{`C:\Windows\System32\config`, `\\host\share\file`} {
		if _, ok := ResolveRepoPath(root, p); ok {
			t.Errorf("Windows-style path %q must fail to resolve", p)
		}
	}
}

func TestResolveRepoPath_EmptyInputsFail(t *testing.T) {
	root := t.TempDir()
	if _, ok := ResolveRepoPath(root, ""); ok {
		t.Fatal("an empty path must fail")
	}
	if _, ok := ResolveRepoPath("", "src/main.go"); ok {
		t.Fatal("an empty repo root must fail")
	}
}
