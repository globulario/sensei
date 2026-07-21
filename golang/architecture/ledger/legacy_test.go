// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestImportLegacyTaskCreatesLimitedLedgerWithoutCertification(t *testing.T) {
	taskDir := t.TempDir()
	writeLegacyFile(t, taskDir, "session.yaml", "architecture_task_session:\n  schema_version: \"1\"\n  task_id: task.legacy\n")
	writeLegacyFile(t, taskDir, "task-request.yaml", "architecture_task_request:\n  schema_version: \"1\"\n")
	writeLegacyFile(t, taskDir, "closure-request.yaml", "architecture_closure_request:\n  schema_version: \"1\"\n")
	writeLegacyFile(t, taskDir, "control/latest.yaml", "task_control:\n  status: ready_for_mutation\n")
	writeLegacyFile(t, taskDir, "receipts/task-status.yaml", "task_status:\n  status: ready_for_mutation\n")

	res, err := ImportLegacyTask(taskDir, ImportOptions{
		ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.TaskID != "task.legacy" || res.SessionID == "" {
		t.Fatalf("unexpected import result: %+v", res)
	}
	report, err := VerifyTaskLedger(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.EntryCount != 1 {
		t.Fatalf("unexpected ledger report: %+v", report)
	}
	data, err := os.ReadFile(filepath.Join(taskDir, "artifacts", "sha256", filepath.Base(res.Head.EntryPath)))
	if err == nil && len(data) > 0 {
		t.Fatal("expected head path to point into ledger, not artifacts")
	}
	projected, err := os.ReadFile(filepath.Join(taskDir, "session.yaml"))
	if err != nil || len(projected) == 0 {
		t.Fatalf("missing rebuilt session projection: %v", err)
	}
	for _, limitation := range []string{"legacy_task_session", "certification_unavailable_legacy", "terminal_completion_unavailable"} {
		if !contains(res.Limitations, limitation) {
			t.Fatalf("missing limitation %s", limitation)
		}
	}
}

func writeLegacyFile(t *testing.T, taskDir, rel, content string) {
	t.Helper()
	path := filepath.Join(taskDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
