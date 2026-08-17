// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"os"
	"path/filepath"
	"testing"
)

func auditRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".sensei", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func taskDir(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, ".sensei", "tasks", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The state #151 reported: a task directory tracked with no session. It must be
// classified and PRESERVED, never repaired away.
func TestAuditClassifiesATaskDirectoryWithNoSession(t *testing.T) {
	root := auditRepo(t)
	dir := taskDir(t, root, "task.sourcefile-binding-repair.be7fd8ad2927")
	// A receipt but no session — exactly the shape that was tracked.
	if err := os.MkdirAll(filepath.Join(dir, "receipts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "receipts", "r.yaml"), []byte("k: v\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := AuditTasks(root)
	if err != nil {
		t.Fatalf("AuditTasks: %v", err)
	}
	if !report.Present {
		t.Fatal("tasks directory exists but was reported absent")
	}
	if report.InvalidCount != 1 || report.ValidCount != 0 {
		t.Fatalf("valid=%d invalid=%d, want 0/1", report.ValidCount, report.InvalidCount)
	}
	if got := report.Entries[0].Disposition; got != AuditInvalidOrUnreadable {
		t.Fatalf("disposition = %q, want %q", got, AuditInvalidOrUnreadable)
	}
	if report.Entries[0].Detail == "" {
		t.Error("a malformed entry must say why it could not be described")
	}

	// READ-ONLY is the contract: everything must still be there afterwards.
	if _, err := os.Stat(filepath.Join(dir, "receipts", "r.yaml")); err != nil {
		t.Fatalf("the audit disturbed a preserved directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "session.yaml")); !os.IsNotExist(err) {
		t.Fatal("the audit reconstructed a session that never existed")
	}
}

// An absent tasks directory is answered, not treated as a fault — the same
// distinction task_status now makes.
func TestAuditReportsAnAbsentTasksDirectoryAsAbsent(t *testing.T) {
	report, err := AuditTasks(t.TempDir())
	if err != nil {
		t.Fatalf("an absent tasks directory must not be an error: %v", err)
	}
	if report.Present {
		t.Fatal("Present should be false")
	}
	if len(report.Entries) != 0 || report.InvalidCount != 0 {
		t.Fatalf("expected an empty inventory, got %+v", report)
	}
}

// Non-task entries are ignored rather than miscounted as malformed tasks.
func TestAuditIgnoresNonTaskEntries(t *testing.T) {
	root := auditRepo(t)
	if err := os.WriteFile(filepath.Join(root, ".sensei", "tasks", "active.yaml"), []byte("x: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".sensei", "tasks", "notatask"), 0o755); err != nil {
		t.Fatal(err)
	}

	report, err := AuditTasks(root)
	if err != nil {
		t.Fatalf("AuditTasks: %v", err)
	}
	if len(report.Entries) != 0 {
		t.Fatalf("expected no task entries, got %+v", report.Entries)
	}
}

// A present-but-unusable active pointer is reported, not silently dropped: "no
// active task" and "the pointer is broken" are different facts.
func TestAuditReportsAnUnusableActivePointer(t *testing.T) {
	root := auditRepo(t)
	if err := os.WriteFile(filepath.Join(root, ".sensei", "tasks", "active.yaml"), []byte("{{ not yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := AuditTasks(root)
	if err != nil {
		t.Fatalf("AuditTasks: %v", err)
	}
	if report.ActiveDetail == "" {
		t.Fatal("a broken active pointer must be reported")
	}
}

// Ordering is deterministic so the report can be diffed across runs.
func TestAuditOrderingIsDeterministic(t *testing.T) {
	root := auditRepo(t)
	for _, n := range []string{"task.zeta.3", "task.alpha.1", "task.mid.2"} {
		taskDir(t, root, n)
	}
	report, err := AuditTasks(root)
	if err != nil {
		t.Fatalf("AuditTasks: %v", err)
	}
	if len(report.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(report.Entries))
	}
	for i := 1; i < len(report.Entries); i++ {
		if report.Entries[i-1].Dir >= report.Entries[i].Dir {
			t.Fatalf("entries are not sorted: %q then %q", report.Entries[i-1].Dir, report.Entries[i].Dir)
		}
	}
}
