// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func reportTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/example/report-fixture\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "sensei@example.test")
	run("config", "user.name", "Sensei Test")
	run("add", ".")
	run("commit", "-m", "initial")
	return root
}

func TestReportGenerate(t *testing.T) {
	root := reportTestRepo(t)
	if code := runReport([]string{"--repo", root}); code != 0 {
		t.Fatalf("runReport exit = %d", code)
	}
	mdPath := filepath.Join(root, "SENSEI.md")
	jsonPath := filepath.Join(root, "SENSEI.report.json")
	if _, err := os.Stat(mdPath); err != nil {
		t.Fatalf("SENSEI.md not written: %v", err)
	}
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("report.json not written: %v", err)
	}
}

func TestReportDeterminism(t *testing.T) {
	root := reportTestRepo(t)
	if code := runReport([]string{"--repo", root}); code != 0 {
		t.Fatalf("first runReport exit = %d", code)
	}
	mdPath := filepath.Join(root, "SENSEI.md")
	jsonPath := filepath.Join(root, "SENSEI.report.json")
	firstMD, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}

	if code := runReport([]string{"--repo", root}); code != 0 {
		t.Fatalf("second runReport exit = %d", code)
	}
	secondMD, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(firstMD) != string(secondMD) {
		t.Fatalf("SENSEI.md not deterministic across regeneration with no tree change:\n%s\n---\n%s", firstMD, secondMD)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("report.json not deterministic across regeneration with no tree change:\n%s\n---\n%s", firstJSON, secondJSON)
	}
}

func TestReportCheckPassesWhenCurrent(t *testing.T) {
	root := reportTestRepo(t)
	if code := runReport([]string{"--repo", root}); code != 0 {
		t.Fatalf("runReport exit = %d", code)
	}
	if code := runReport([]string{"--repo", root, "--check"}); code != 0 {
		t.Fatalf("runReport --check exit = %d, expected 0 (current)", code)
	}
}

// TestReportCheckPassesAfterCommittingReport proves --check survives the
// one state transition that is otherwise guaranteed to happen to every
// committed report: committing SENSEI.md/SENSEI.report.json necessarily
// advances HEAD past the commit the report itself recorded as
// evaluated_commit. --check must not treat that self-inflicted commit
// mismatch as staleness or a hand-edit -- only a real content-digest
// change (a tracked file edited after generation) may fail it.
func TestReportCheckPassesAfterCommittingReport(t *testing.T) {
	root := reportTestRepo(t)
	if code := runReport([]string{"--repo", root}); code != 0 {
		t.Fatalf("runReport exit = %d", code)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("add", "SENSEI.md", "SENSEI.report.json")
	run("commit", "-m", "add report")

	if code := runReport([]string{"--repo", root, "--check"}); code != 0 {
		t.Fatalf("runReport --check exit = %d, expected 0 (committing the report must not make it look stale)", code)
	}
}

func TestReportCheckMissing(t *testing.T) {
	root := reportTestRepo(t)
	if code := runReport([]string{"--repo", root, "--check"}); code != 1 {
		t.Fatalf("runReport --check exit = %d, expected 1 (missing)", code)
	}
}

func TestReportCheckStaleOnTrackedFileChange(t *testing.T) {
	root := reportTestRepo(t)
	if code := runReport([]string{"--repo", root}); code != 0 {
		t.Fatalf("runReport exit = %d", code)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() { println(1) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runReport([]string{"--repo", root, "--check"}); code != 1 {
		t.Fatalf("runReport --check exit = %d, expected 1 (stale)", code)
	}
}

func TestReportCheckFailsOnHandEditedFile(t *testing.T) {
	root := reportTestRepo(t)
	if code := runReport([]string{"--repo", root}); code != 0 {
		t.Fatalf("runReport exit = %d", code)
	}
	mdPath := filepath.Join(root, "SENSEI.md")
	data, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mdPath, append(data, []byte("\nhand-edited line\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runReport([]string{"--repo", root, "--check"}); code != 1 {
		t.Fatalf("runReport --check exit = %d, expected 1 (hand-edited)", code)
	}
}

func TestReportStdoutWritesNothing(t *testing.T) {
	root := reportTestRepo(t)
	if code := runReport([]string{"--repo", root, "--stdout"}); code != 0 {
		t.Fatalf("runReport --stdout exit = %d", code)
	}
	mdPath := filepath.Join(root, "SENSEI.md")
	jsonPath := filepath.Join(root, "SENSEI.report.json")
	if _, err := os.Stat(mdPath); !os.IsNotExist(err) {
		t.Fatalf("expected SENSEI.md not to be written by --stdout, stat err = %v", err)
	}
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatalf("expected report.json not to be written by --stdout, stat err = %v", err)
	}
}

func TestReportNoActiveTaskRendersHonestNote(t *testing.T) {
	root := reportTestRepo(t)
	if code := runReport([]string{"--repo", root}); code != 0 {
		t.Fatalf("runReport exit = %d", code)
	}
	data, err := os.ReadFile(filepath.Join(root, "SENSEI.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "no active task") {
		t.Fatalf("expected honest 'no active task' note, got:\n%s", data)
	}
}
