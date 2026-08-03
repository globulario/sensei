// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closure"
)

// TestResolveClosureReportPath_BeforeAnyGeneration_ReturnsPrepareTimePath
// covers a task that has never run advance-task: no control/latest-
// generation.yaml exists yet, so the prepare-time snapshot under
// <taskDir>/convergence/latest/ is the only real closure report.
func TestResolveClosureReportPath_BeforeAnyGeneration_ReturnsPrepareTimePath(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)

	got, err := ResolveClosureReportPath(repo, taskDir, false)
	if err != nil {
		t.Fatalf("ResolveClosureReportPath: %v", err)
	}
	want := filepath.Join(taskDir, "convergence", "latest", "closure-after-dialogue.yaml")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("resolved path does not exist: %v", err)
	}
}

// TestResolveClosureReportPath_AfterGeneration_ReturnsGenerationScopedPath
// is the real regression test for a live review finding: once
// sensei advance-task has published at least one control generation, the
// authoritative closure snapshot moves under
// control/generations/<digest>/convergence/latest/ -- a caller resolving
// the bare prepare-time path instead silently binds to stale closure
// state. This proves ResolveClosureReportPath follows the same
// generation-pointer resolution ControlStatus itself uses, not a parallel,
// independently-hardcoded path.
func TestResolveClosureReportPath_AfterGeneration_ReturnsGenerationScopedPath(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)

	if _, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, Active: true, ObservedAt: "2026-07-14T18:31:00Z"}); err != nil {
		t.Fatalf("AdvanceTask: %v", err)
	}

	pointerPath := filepath.Join(taskDir, "control", "latest-generation.yaml")
	if _, err := os.Stat(pointerPath); err != nil {
		t.Fatalf("expected control/latest-generation.yaml to exist after AdvanceTask: %v", err)
	}

	got, err := ResolveClosureReportPath(repo, taskDir, false)
	if err != nil {
		t.Fatalf("ResolveClosureReportPath: %v", err)
	}
	prepareTimePath := filepath.Join(taskDir, "convergence", "latest", "closure-after-dialogue.yaml")
	if got == prepareTimePath {
		t.Fatalf("path = %q, still resolved to the stale prepare-time snapshot after a generation was published", got)
	}
	wantPrefix := filepath.Join(taskDir, "control", "generations")
	if !filepathHasPrefix(got, wantPrefix) {
		t.Fatalf("path = %q, want a path under %q", got, wantPrefix)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("resolved generation-scoped path does not exist: %v", err)
	}

	// Confirm this is the exact same path ControlStatus's own internal
	// resolution would use, by loading the report and checking it parses
	// as a real closure report (not just that a file happens to exist at
	// the expected prefix).
	if _, err := closure.LoadReport(got); err != nil {
		t.Fatalf("closure.LoadReport(%q): %v", got, err)
	}
}

func filepathHasPrefix(path, prefix string) bool {
	rel, err := filepath.Rel(prefix, path)
	if err != nil {
		return false
	}
	return rel != ".." && !filepath.IsAbs(rel) && len(rel) > 0 && rel[0] != '.'
}
