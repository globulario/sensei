// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/completion"
	"github.com/globulario/sensei/golang/architecture/tasksession"
)

// A task-directory resolution failure is surfaced as an explicit, typed `unavailable`
// completion envelope — never silently omitted, never a fabricated terminal state.
func TestCompletionProjectionEnvelopeResolutionFailureIsVisible(t *testing.T) {
	dir := t.TempDir() // no .sensei active-task pointer here
	env := completionProjectionEnvelope(tasksession.StatusOptions{RepoRoot: dir, Active: true})
	if env.Availability != completion.CompletionUnavailable {
		t.Fatalf("availability = %s, want unavailable", env.Availability)
	}
	if env.UnavailableClass != "task_directory_unresolved" {
		t.Fatalf("class = %q, want task_directory_unresolved", env.UnavailableClass)
	}
	if env.Projection != nil {
		t.Fatal("a resolution failure must not fabricate a projection/terminal state")
	}
	if env.DigestSHA256 == "" {
		t.Fatal("the unavailable envelope must still carry a deterministic identity")
	}
}
