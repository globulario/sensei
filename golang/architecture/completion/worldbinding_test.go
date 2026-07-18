// SPDX-License-Identifier: AGPL-3.0-only

package completion

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRepositoryTaskBinding(t *testing.T) {
	root := t.TempDir()
	under := filepath.Join(root, ".sensei", "tasks", "task.abc")
	if err := os.MkdirAll(under, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateRepositoryTaskBinding(root, under); err != nil {
		t.Fatalf("a task dir under the root must be accepted: %v", err)
	}
	if err := validateRepositoryTaskBinding(root, root); err != nil {
		t.Fatalf("the root itself is within the root: %v", err)
	}

	other := t.TempDir()
	otherTask := filepath.Join(other, ".sensei", "tasks", "task.xyz")
	if err := os.MkdirAll(otherTask, 0o755); err != nil {
		t.Fatal(err)
	}
	if validateRepositoryTaskBinding(root, otherTask) == nil {
		t.Fatal("a task dir from another repository must be rejected")
	}

	// Symlink-aware: a symlink INSIDE the root pointing into another repository must not
	// smuggle that other world past the check.
	link := filepath.Join(root, "sneaky-task")
	if err := os.Symlink(otherTask, link); err == nil {
		if validateRepositoryTaskBinding(root, link) == nil {
			t.Fatal("a symlinked task dir escaping the root must be rejected")
		}
	}

	if validateRepositoryTaskBinding("", under) == nil || validateRepositoryTaskBinding(root, "") == nil {
		t.Fatal("empty root or task must be rejected")
	}
}

// Every completion owner entrypoint rejects a repository paired with another repository's
// task directory BEFORE any lock, read, authority resolution, or projection rebuild — the
// mismatch can never acquire a lock under one world and mutate another.
func TestOwnerEntrypointsRejectCrossRepositoryComposition(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	taskB := filepath.Join(rootB, ".sensei", "tasks", "task.b")
	if err := os.MkdirAll(taskB, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	cr, err := CompleteTask(ctx, CompleteRequest{
		RepositoryRoot:                 rootA,
		TaskDirectory:                  taskB,
		IdentityRoot:                   filepath.Join(rootA, ".sensei", "identity"),
		ExpectedLedgerHeadDigestSHA256: "deadbeef",
	})
	if err != nil {
		t.Fatalf("CompleteTask returned a hard error: %v", err)
	}
	if cr.Outcome != OutcomeInputInvalid {
		t.Fatalf("CompleteTask cross-repo must be input_invalid, got %s", cr.Outcome)
	}

	rr, err := RecoverProjections(ctx, Request{RepositoryRoot: rootA, TaskDirectory: taskB})
	if err != nil {
		t.Fatalf("RecoverProjections returned a hard error: %v", err)
	}
	if rr.Outcome != RecoverInputInvalid {
		t.Fatalf("RecoverProjections cross-repo must be input_invalid, got %s", rr.Outcome)
	}

	if _, err := InspectTerminalState(ctx, Request{RepositoryRoot: rootA, TaskDirectory: taskB}); err == nil {
		t.Fatal("InspectTerminalState cross-repo must return an error")
	}
	if _, err := AssessReadiness(ctx, Request{RepositoryRoot: rootA, TaskDirectory: taskB}); err == nil {
		t.Fatal("AssessReadiness cross-repo must return an error")
	}
}
