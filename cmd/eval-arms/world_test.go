// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

func initRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "eval@example.test"},
		{"config", "user.name", "eval"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "first"}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return root
}

// A clean checkout is identified by the commit it actually is.
func TestWorldBindingCleanTreeBindsRevision(t *testing.T) {
	root := initRepo(t)
	binding, err := worldBinding(root, "example.test/clean")
	if err != nil {
		t.Fatalf("worldBinding: %v", err)
	}
	if binding.RevisionStatus != architecture.RevisionResolved || binding.Revision == "" {
		t.Fatalf("clean tree must bind its revision, got status=%s revision=%q", binding.RevisionStatus, binding.Revision)
	}
	if binding.TreeDigestSHA256 != "" {
		t.Fatalf("clean tree must not need a tree digest, got %q", binding.TreeDigestSHA256)
	}
}

// A dirty checkout was not the commit HEAD names, so it must not claim to be:
// the measurement binds to the tree that was read (#216).
func TestWorldBindingDirtyTreeRefusesRevisionAndBindsTreeDigest(t *testing.T) {
	root := initRepo(t)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binding, err := worldBinding(root, "example.test/dirty")
	if err != nil {
		t.Fatalf("worldBinding: %v", err)
	}
	if binding.Revision != "" {
		t.Fatalf("dirty tree must not claim a revision, got %q", binding.Revision)
	}
	if binding.RevisionStatus != architecture.RevisionUnavailable {
		t.Fatalf("dirty tree revision status = %s, want unavailable", binding.RevisionStatus)
	}
	if binding.TreeDigestSHA256 == "" {
		t.Fatal("dirty tree must be identified by its tree digest")
	}
}

// The digest must follow the content, or two different working trees would
// report the same identity.
func TestWorldBindingTreeDigestFollowsContent(t *testing.T) {
	root := initRepo(t)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := worldBinding(root, "example.test/dirty")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := worldBinding(root, "example.test/dirty")
	if err != nil {
		t.Fatal(err)
	}
	if first.TreeDigestSHA256 == second.TreeDigestSHA256 {
		t.Fatal("two different working trees produced the same tree digest")
	}
}

// Computing a dirty tree's identity must not disturb the repository it reads.
func TestWorldBindingLeavesRepositoryIndexAlone(t *testing.T) {
	root := initRepo(t)
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worldBinding(root, "example.test/dirty"); err != nil {
		t.Fatal(err)
	}
	after, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("worldBinding staged the caller's work: %q -> %q", before, after)
	}
}

// A report naming this machine's checkout is not comparable with the same
// world measured elsewhere.
func TestRelocateRootRemovesCheckoutLocation(t *testing.T) {
	got := relocateRoot("/home/somebody/checkout", "read /home/somebody/checkout/docs/generated: is a directory")
	if want := "read <repo>/docs/generated: is a directory"; got != want {
		t.Fatalf("relocateRoot = %q, want %q", got, want)
	}
	if got := relocateRoot("/home/somebody/checkout", "scanned /home/somebody/checkout"); got != "scanned <repo>" {
		t.Fatalf("bare root not relocated: %q", got)
	}
}

func TestParseWorldSpecRejectsIncompleteSpec(t *testing.T) {
	if _, _, _, err := parseWorldSpec("name=domain"); err == nil {
		t.Fatal("a spec without a path must be refused")
	}
	if _, _, _, err := parseWorldSpec("=domain=/path"); err == nil {
		t.Fatal("a spec without a name must be refused")
	}
	name, domain, path, err := parseWorldSpec("world2=github.com/x/y=/tmp/z")
	if err != nil || name != "world2" || domain != "github.com/x/y" || path != "/tmp/z" {
		t.Fatalf("parseWorldSpec = %q %q %q %v", name, domain, path, err)
	}
}
