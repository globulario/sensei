// SPDX-License-Identifier: AGPL-3.0-only

package architecture

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUncommittedSourceFilesReportsOnlyBytesTheRevisionDoesNotContain(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "committed.go", "package a\n")
	writeRepoFile(t, root, "modified.go", "package a\n")
	writeRepoFile(t, root, "unrelated.go", "package a\n")
	writeRepoFile(t, root, "pkg/nested.go", "package pkg\n")
	writeRepoFile(t, root, ".gitignore", "ignored.go\n")
	gitInRepo(t, root, "init")
	gitInRepo(t, root, "config", "user.email", "test@example.com")
	gitInRepo(t, root, "config", "user.name", "Test User")
	gitInRepo(t, root, "add", ".")
	gitInRepo(t, root, "commit", "-m", "initial")
	revision := strings.TrimSpace(gitInRepo(t, root, "rev-parse", "HEAD"))

	writeRepoFile(t, root, "modified.go", "package a\n\nfunc Added() {}\n")
	writeRepoFile(t, root, "untracked.go", "package a\n")
	writeRepoFile(t, root, "ignored.go", "package a\n")

	cited := []string{"committed.go", "modified.go", "untracked.go", "ignored.go", "pkg", "missing.go", ""}
	got, err := UncommittedSourceFiles(root, revision, cited)
	if err != nil {
		t.Fatalf("UncommittedSourceFiles: %v", err)
	}
	want := []string{"ignored.go", "modified.go", "untracked.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestUncommittedSourceFilesReportsNothingForACleanCheckout(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "committed.go", "package a\n")
	gitInRepo(t, root, "init")
	gitInRepo(t, root, "config", "user.email", "test@example.com")
	gitInRepo(t, root, "config", "user.name", "Test User")
	gitInRepo(t, root, "add", ".")
	gitInRepo(t, root, "commit", "-m", "initial")
	revision := strings.TrimSpace(gitInRepo(t, root, "rev-parse", "HEAD"))

	got, err := UncommittedSourceFiles(root, revision, []string{"committed.go"})
	if err != nil {
		t.Fatalf("UncommittedSourceFiles: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("clean checkout reported %v", got)
	}
}

func TestUncommittedSourceFilesRequiresARevisionToCompareAgainst(t *testing.T) {
	if _, err := UncommittedSourceFiles(t.TempDir(), "", []string{"a.go"}); err == nil {
		t.Fatal("expected an error when no revision is given")
	}
}

func TestUncommittedSourceFilesFailsRatherThanReportingCleanOutsideAGitRepository(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "a.go", "package a\n")
	if _, err := UncommittedSourceFiles(root, strings.Repeat("a", 40), []string{"a.go"}); err == nil {
		t.Fatal("expected an error when the revision cannot be listed")
	}
}

func writeRepoFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitInRepo(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
