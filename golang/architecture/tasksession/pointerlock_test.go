// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ptr(taskID string) ActivePointer {
	return ActivePointer{
		TaskID:                      taskID,
		RepositoryDomain:            "github.com/globulario/sensei",
		Revision:                    strings.Repeat("0", 40),
		GraphDigestSHA256:           strings.Repeat("a", 64),
		LedgerPath:                  ".sensei/tasks/" + taskID + "/ledger",
		SessionPath:                 ".sensei/tasks/" + taskID + "/session.yaml",
		SessionDigestSHA256:         strings.Repeat("b", 64),
		LastTaskControlDigestSHA256: strings.Repeat("c", 64),
	}
}

func activePath(repo string) string { return filepath.Join(repo, ".sensei", "tasks", "active.yaml") }

// TestAnotherTasksPointerSurvivesAWriteDuringTheIdentityWindow is the race the
// review named, driven deterministically rather than hoped for.
//
// Task A's pointer is checked, then task B activates in the window between the
// check and the unlink. Before the repair, A's clear deleted B's pointer: the
// identity check was real and its conclusion was stale by the time it was acted
// on. After the repair, a lock-respecting writer cannot act inside that window
// at all, which is what the hook now proves by being refused.
func TestAnotherTasksPointerSurvivesAWriteDuringTheIdentityWindow(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(activePath(repo)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteActivePointer(repo, ptr("task.defect.aaaa")); err != nil {
		t.Fatal(err)
	}

	var writeErr error
	var attempted bool
	afterPointerIdentityCheck = func() {
		attempted = true
		// A writer that respects the mechanism, arriving in the window.
		writeErr = WriteActivePointer(repo, ptr("task.defect.bbbb"))
	}
	t.Cleanup(func() { afterPointerIdentityCheck = nil })

	clearErr := ClearActivePointer(repo, "task.defect.aaaa")

	if !attempted {
		t.Fatal("the window hook never fired, so nothing was exercised")
	}
	if writeErr == nil {
		t.Fatal("a second writer completed inside the identity window; the check and the unlink are not serialized")
	}
	if !errors.Is(writeErr, ErrPointerLockHeld) {
		t.Fatalf("writer failed for the wrong reason: %v", writeErr)
	}
	if clearErr != nil {
		t.Fatalf("clear: %v", clearErr)
	}
	// A's pointer is gone and B's was never written, so nothing was deleted that
	// had not been checked.
	if _, err := os.Stat(activePath(repo)); !os.IsNotExist(err) {
		body, _ := os.ReadFile(activePath(repo))
		t.Fatalf("a pointer survives the clear: %s", body)
	}
}

// TestTheLockIsReleasedSoNormalSequencingStillWorks: the exclusion must be a
// window, not a wall.
func TestTheLockIsReleasedSoNormalSequencingStillWorks(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(activePath(repo)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteActivePointer(repo, ptr("task.defect.aaaa")); err != nil {
		t.Fatal(err)
	}
	if err := ClearActivePointer(repo, "task.defect.aaaa"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if err := WriteActivePointer(repo, ptr("task.defect.bbbb")); err != nil {
		t.Fatalf("a later writer was blocked by a lock nobody holds: %v", err)
	}
	loaded, err := LoadActivePointer(repo)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TaskID != "task.defect.bbbb" {
		t.Fatalf("pointer = %q, want task.defect.bbbb", loaded.TaskID)
	}
}
