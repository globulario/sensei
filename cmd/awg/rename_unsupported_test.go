// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// A Git-detected rename must be observed with both endpoints preserved and never
// silently decomposed into a delete plus a create. v1 cannot verify a rename, so
// verify-admission fails closed with no successful scope_verified receipt.

func TestObserveChangePreservesRenameEndpoints(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "owner.go"), []byte("package owner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "base")
	baseRev := gitRev(t, repo)
	if err := os.MkdirAll(filepath.Join(repo, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "mv", "owner.go", "internal/owner.go")
	gitRun(t, repo, "commit", "-q", "-m", "rename")
	resultRev := gitRev(t, repo)

	files, err := admissionGitChangedFiles(repo, baseRev, resultRev)
	if err != nil {
		t.Fatal(err)
	}
	// Exactly one entry, a rename — NOT a delete plus a create (no translation).
	if len(files) != 1 {
		t.Fatalf("rename must yield exactly one observed file, got %+v", files)
	}
	f := files[0]
	if f.ChangeType != "rename" {
		t.Fatalf("expected change type rename, got %q", f.ChangeType)
	}
	if f.FromPath != "owner.go" || f.ToPath != "internal/owner.go" {
		t.Fatalf("rename endpoints not preserved: from=%q to=%q", f.FromPath, f.ToPath)
	}
}

func TestVerifyAdmissionV2RenameFailsClosed(t *testing.T) {
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
	baseTree := canonicalTreeOf(t, repo, baseRev)

	// The admitted operation is a modify of cliTarget; the agent instead renames
	// it, so the observed change is a Git rename outside what v1 can verify.
	gitRun(t, repo, "mv", cliTarget, "src/renamed.go")
	gitRun(t, repo, "commit", "-q", "-m", "rename")
	resultRev := gitRev(t, repo)

	dir := seedLedger(t, baseRev, baseTree, true)
	if code := runAdmitChangeV2(dir, head(t, dir), "yaml"); code != 0 {
		t.Fatal("admit failed")
	}
	if code := runConsumeAdmission([]string{"--task-dir", dir, "--expected-head", head(t, dir), "--format", "yaml"}); code != 0 {
		t.Fatal("consume failed")
	}
	if code := runVerifyAdmissionV2(dir, repo, head(t, dir), resultRev, "yaml"); code != 3 {
		t.Fatalf("verify-admission exit = %d, want 3 (rename unsupported)", code)
	}
	// No successful scope verification: if a receipt was recorded it must be
	// invalid, carrying the rename violation — never a verified scope.
	if v, err := admission.LoadRecordedScopeVerification(dir); err == nil {
		if admission.ScopeVerified(v) {
			t.Fatal("a rename must never produce a successful scope_verified receipt")
		}
		found := false
		for _, viol := range v.Violations {
			if viol.Code == "scope.operation.rename_unsupported" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected scope.operation.rename_unsupported violation, got %+v", v.Violations)
		}
	}
	// And certainly no certification or completion.
	assertNoEvents(t, dir, closureprotocol.LedgerEventCertified, closureprotocol.LedgerEventCompleted)
}
