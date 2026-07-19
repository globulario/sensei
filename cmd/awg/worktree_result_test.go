// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// verify-admission observes the working tree by default, so ordinary
// uncommitted agent edits are seen (staged and unstaged, tracked and untracked)
// without a commit, and the user's real Git index is never modified.

// worktreeRepo builds a repo whose base commit contains cliTarget and returns
// (repoRoot, baseRev, canonical base tree digest). No result commit is made.
func worktreeRepo(t *testing.T) (string, string, string) {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, cliTarget), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "base")
	baseRev := gitRev(t, repo)
	return repo, baseRev, canonicalTreeOf(t, repo, baseRev)
}

func prepareAdmittedTask(t *testing.T, repo, baseRev, baseTree string) string {
	t.Helper()
	dir := seedLedger(t, baseRev, baseTree, true)
	if code := runAdmitChangeV2(dir, head(t, dir), "yaml"); code != 0 {
		t.Fatal("admit failed")
	}
	if code := runConsumeAdmission([]string{"--task-dir", dir, "--expected-head", head(t, dir), "--format", "yaml"}); code != 0 {
		t.Fatal("consume failed")
	}
	return dir
}

func TestVerifyWorktreeUncommittedEditVerifies(t *testing.T) {
	repo, baseRev, baseTree := worktreeRepo(t)
	dir := prepareAdmittedTask(t, repo, baseRev, baseTree)
	// Edit the admitted file in the worktree, do NOT commit.
	if err := os.WriteFile(filepath.Join(repo, cliTarget), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runVerifyAdmissionV2(dir, repo, head(t, dir), "", "yaml"); code != 0 {
		t.Fatalf("worktree verify exit = %d, want 0 (uncommitted edit observed)", code)
	}
}

func TestVerifyWorktreeExtraUntrackedFileFails(t *testing.T) {
	repo, baseRev, baseTree := worktreeRepo(t)
	dir := prepareAdmittedTask(t, repo, baseRev, baseTree)
	if err := os.WriteFile(filepath.Join(repo, cliTarget), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Unrelated dirt: an untracked file outside the admitted envelope.
	if err := os.WriteFile(filepath.Join(repo, "STRAY.md"), []byte("stray\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runVerifyAdmissionV2(dir, repo, head(t, dir), "", "yaml"); code != 3 {
		t.Fatalf("worktree verify exit = %d, want 3 (extra untracked file out of envelope)", code)
	}
}

func TestVerifyWorktreeDefaultCannotHideEdits(t *testing.T) {
	repo, baseRev, baseTree := worktreeRepo(t)
	dir := prepareAdmittedTask(t, repo, baseRev, baseTree)
	if err := os.WriteFile(filepath.Join(repo, cliTarget), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Committed-HEAD mode sees nothing (HEAD did not move) and fails not_observed;
	// the default worktree mode observes the edit and verifies.
	if code := runVerifyAdmissionV2(dir, repo, head(t, dir), "HEAD", "yaml"); code != 3 {
		t.Fatalf("HEAD mode exit = %d, want 3 (empty committed change)", code)
	}
}

func TestVerifyWorktreeEmptyChangeFailsWhenMutationAdmitted(t *testing.T) {
	repo, baseRev, baseTree := worktreeRepo(t)
	dir := prepareAdmittedTask(t, repo, baseRev, baseTree)
	// No worktree edit at all: the admitted modify never happened.
	if code := runVerifyAdmissionV2(dir, repo, head(t, dir), "", "yaml"); code != 3 {
		t.Fatalf("empty worktree verify exit = %d, want 3 (admitted mutation not observed)", code)
	}
}

func TestObserveChangeSeesStagedAndUnstaged(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, s string) {
		if err := os.WriteFile(filepath.Join(repo, rel), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(cliTarget, "base\n")
	write("src/other.go", "base\n")
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "base")
	baseRev := gitRev(t, repo)

	write(cliTarget, "staged\n")
	gitRun(t, repo, "add", cliTarget)   // staged
	write("src/other.go", "unstaged\n") // unstaged, tracked
	// Untracked file (explicit policy: observed via add -A).
	write("src/new.go", "new\n")

	observed, err := observeChange(repo, baseRev, "", "actor", "authority")
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, f := range observed.Files {
		paths[f.Path] = f.ChangeType
	}
	if paths[cliTarget] != "modify" {
		t.Fatalf("staged change not observed: %+v", observed.Files)
	}
	if paths["src/other.go"] != "modify" {
		t.Fatalf("unstaged tracked change not observed: %+v", observed.Files)
	}
	if paths["src/new.go"] != "create" {
		t.Fatalf("untracked file not observed as create: %+v", observed.Files)
	}
}

func TestObserveChangeLeavesRealIndexUntouched(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, cliTarget), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "base")
	baseRev := gitRev(t, repo)
	// Stage a change so a real index exists, then snapshot it.
	if err := os.WriteFile(filepath.Join(repo, cliTarget), []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", cliTarget)
	indexPath := filepath.Join(repo, ".git", "index")
	before, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := observeChange(repo, baseRev, "", "actor", "authority"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("observeChange modified the real .git/index")
	}
}
