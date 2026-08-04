// SPDX-License-Identifier: AGPL-3.0-only

package senseireport

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInitRepo(t *testing.T, root string) {
	t.Helper()
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
}

func gitCommitAll(t *testing.T, root, message string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("add", ".")
	run("commit", "-m", message)
}

func TestContentDigestChangesOnTrackedFileEdit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInitRepo(t, root)
	gitCommitAll(t, root, "initial")

	before, err := ContentDigest(root, nil, nil)
	if err != nil {
		t.Fatalf("ContentDigest: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n\nvar x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := ContentDigest(root, nil, nil)
	if err != nil {
		t.Fatalf("ContentDigest: %v", err)
	}
	if before == after {
		t.Fatalf("digest did not change after editing a tracked file")
	}
}

func TestContentDigestIgnoresExcludedReportOutputs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInitRepo(t, root)
	gitCommitAll(t, root, "initial")

	before, err := ContentDigest(root, []string{".sensei/"}, []string{"SENSEI.md"})
	if err != nil {
		t.Fatalf("ContentDigest: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "SENSEI.md"), []byte("# report\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".sensei", "report.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	after, err := ContentDigest(root, []string{".sensei/"}, []string{"SENSEI.md"})
	if err != nil {
		t.Fatalf("ContentDigest: %v", err)
	}
	if before != after {
		t.Fatalf("digest changed after writing excluded report outputs: before=%s after=%s", before, after)
	}
}

func TestContentDigestStableAcrossRepeatedCalls(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInitRepo(t, root)
	gitCommitAll(t, root, "initial")

	first, err := ContentDigest(root, nil, nil)
	if err != nil {
		t.Fatalf("ContentDigest: %v", err)
	}
	second, err := ContentDigest(root, nil, nil)
	if err != nil {
		t.Fatalf("ContentDigest: %v", err)
	}
	if first != second {
		t.Fatalf("digest not stable across repeated calls: %s != %s", first, second)
	}
}
