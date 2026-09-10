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

// TestAMismatchDecisionIsMadeUnderTheLock is the round-three finding.
//
// The caller used to read the pointer itself, decide "this names another task,
// nothing to do", and return -- all outside the lock. A writer arriving after
// that read could make the pointer name the task being abandoned, so the
// transition reported committed while its own pointer survived. The read, the
// match/mismatch decision and the optional unlink have to be one critical
// section, and the owner has to report what it did.
func TestAMismatchDecisionIsMadeUnderTheLock(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(activePath(repo)), 0o755); err != nil {
		t.Fatal(err)
	}
	// The pointer names task B while task A is being retired.
	if err := WriteActivePointer(repo, ptr("task.defect.bbbb")); err != nil {
		t.Fatal(err)
	}

	var writeErr error
	var fired bool
	afterPointerIdentityCheck = func() {
		fired = true
		// A lock-respecting writer trying to make the pointer name A, in the
		// window where the mismatch decision has just been taken.
		writeErr = WriteActivePointer(repo, ptr("task.defect.aaaa"))
	}
	t.Cleanup(func() { afterPointerIdentityCheck = nil })

	removed, err := RetireActivePointer(repo, "task.defect.aaaa")
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	if !fired {
		t.Fatal("the mismatch decision never reached the seam, so it is not inside the lock")
	}
	if writeErr == nil {
		t.Fatal("a writer completed inside the mismatch window; the decision is not under the lock")
	}
	if removed {
		t.Fatal("a pointer naming another task was reported as retired")
	}
	loaded, lerr := LoadActivePointer(repo)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if loaded.TaskID != "task.defect.bbbb" {
		t.Fatalf("pointer = %q, want the untouched task.defect.bbbb", loaded.TaskID)
	}
}

// TestRetireReportsWhatItDid: the owner, not the caller, says whether a pointer
// was removed -- the caller cannot know without re-reading, which is the race.
func TestRetireReportsWhatItDid(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(activePath(repo)), 0o755); err != nil {
		t.Fatal(err)
	}
	if removed, err := RetireActivePointer(repo, "task.defect.aaaa"); err != nil || removed {
		t.Fatalf("absent pointer: removed=%v err=%v, want false/nil", removed, err)
	}
	if err := WriteActivePointer(repo, ptr("task.defect.aaaa")); err != nil {
		t.Fatal(err)
	}
	if removed, err := RetireActivePointer(repo, "task.defect.aaaa"); err != nil || !removed {
		t.Fatalf("matching pointer: removed=%v err=%v, want true/nil", removed, err)
	}
	if removed, err := RetireActivePointer(repo, "task.defect.aaaa"); err != nil || removed {
		t.Fatalf("second retire: removed=%v err=%v, want false/nil", removed, err)
	}
}
