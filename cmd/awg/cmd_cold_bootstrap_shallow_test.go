// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os/exec"
	"testing"
)

func TestShallowHistoryNote(t *testing.T) {
	// A non-git directory: git errors, so no (misleading) shallow note.
	if note := shallowHistoryNote(t.TempDir()); note != "" {
		t.Errorf("non-git dir should yield no note, got %q", note)
	}

	// A normal (full) git repo: not shallow -> no note.
	full := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", full}, args...)...)
		cmd.Env = append(cmd.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable/failed (%v): %s", err, out)
		}
	}
	run("init")
	run("commit", "--allow-empty", "-m", "first")
	if note := shallowHistoryNote(full); note != "" {
		t.Errorf("full repo should yield no shallow note, got %q", note)
	}
}
