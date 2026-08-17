// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A repository that has never created a task session must be ANSWERED, not
// errored. The reported harm was concrete: an architect asked for task status,
// received `open .../.sensei/tasks: no such file or directory`, and carried that
// errno into its architectural claims as evidence of an unavailable governed
// surface. The truthful reading is that there is nothing to report.
func TestMissingTasksDirectoryIsTypedAbsenceNotAnError(t *testing.T) {
	repo := t.TempDir() // no .sensei at all

	_, _, err := ControlStatus(repo, "", true)
	if err == nil {
		t.Fatal("expected the typed no-task-session signal")
	}
	if !errors.Is(err, ErrNoTaskSession) {
		t.Fatalf("err = %v, want ErrNoTaskSession", err)
	}
	// The whole point: no filesystem path or errno leaks to the caller.
	if strings.Contains(err.Error(), "no such file") || strings.Contains(err.Error(), repo) {
		t.Fatalf("the raw filesystem failure reached the caller: %v", err)
	}
}

// Status (the default, non-compact surface) must agree with ControlStatus rather
// than one answering and the other erroring.
func TestStatusAgreesWithControlStatusOnAbsence(t *testing.T) {
	repo := t.TempDir()

	_, err := Status(StatusOptions{RepoRoot: repo, Active: true})
	if !errors.Is(err, ErrNoTaskSession) {
		t.Fatalf("Status err = %v, want ErrNoTaskSession", err)
	}
}

// An EMPTY tasks directory is the same answer as an absent one: nothing to
// report on.
func TestEmptyTasksDirectoryIsTheSameAnswerAsAbsent(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".sensei", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, _, err := ControlStatus(repo, "", true)
	if !errors.Is(err, ErrNoTaskSession) {
		t.Fatalf("err = %v, want ErrNoTaskSession", err)
	}
}

// The distinction that must survive: a directory that EXISTS and cannot be read
// is a real error. We were unable to look, which is not the same as having
// looked and found nothing. Collapsing the two would let a permissions fault
// masquerade as "this repo has no tasks".
func TestUnreadableTasksDirectoryStaysAnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions do not apply on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions, so this cannot be provoked")
	}
	repo := t.TempDir()
	tasks := filepath.Join(repo, ".sensei", "tasks")
	if err := os.MkdirAll(tasks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tasks, 0o000); err != nil {
		t.Skipf("cannot remove read permission: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(tasks, 0o755) })

	_, _, err := ControlStatus(repo, "", true)
	if err == nil {
		t.Fatal("an unreadable tasks directory must not be reported as an empty task set")
	}
	if errors.Is(err, ErrNoTaskSession) {
		t.Fatalf("an unreadable directory was reported as absence: %v", err)
	}
}
