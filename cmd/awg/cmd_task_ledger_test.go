// SPDX-License-Identifier: Apache-2.0

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskLedgerVerifyAndRebuildCLI(t *testing.T) {
	repo, graph := taskSessionTestRepo(t)
	code, _, stderr := captureTaskSessionCommand(t, func() int {
		return runPrepareChange([]string{
			"--repo", repo,
			"--repo-domain", "github.com/example/project",
			"--description", "Inspect route ownership.",
			"--mode", "inspect",
			"--task-class", "route_ownership",
			"--risk-class", "architecture_sensitive",
			"--direction", "preserve",
			"--graph-nt", graph,
			"--file", "read:gin.go",
		})
	})
	if code != 0 {
		t.Fatalf("prepare-change exit=%d stderr=%s", code, stderr)
	}
	code, stdout, stderr := captureTaskSessionCommand(t, func() int {
		return runTaskLedger([]string{"verify", "--repo", repo, "--active", "--format", "json"})
	})
	if code != 0 || !strings.Contains(stdout, `"valid": true`) {
		t.Fatalf("task-ledger verify exit=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	code, stdout, stderr = captureTaskSessionCommand(t, func() int {
		return runTaskLedger([]string{"rebuild-projections", "--repo", repo, "--active", "--format", "json"})
	})
	if code != 0 || !strings.Contains(stdout, `"files"`) {
		t.Fatalf("task-ledger rebuild exit=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if _, err := filepath.Abs(repo); err != nil {
		t.Fatal(err)
	}
}
