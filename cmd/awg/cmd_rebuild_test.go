// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluateSeedFreshness_ExternalOnlyDiffPasses(t *testing.T) {
	agOnly := nt(agLabel, agSev)
	committed := nt(agLabel, agSev)
	generated := nt(agLabel, agSev, svcLabel, svcSev)

	got := evaluateSeedFreshness(committed, generated, agOnly)
	if got.level != auditPASS {
		t.Fatalf("level = %v, want PASS", got.level)
	}
	if got.summary == "current" {
		t.Fatal("expected external/context drift summary, got strict current")
	}
}

func TestEvaluateSeedFreshness_OwnedDriftFails(t *testing.T) {
	agOld := `<https://globular.io/awareness#invariant/meta.storage_is_not_semantic_authority> <https://globular.io/awareness#severity> "medium" .`
	agOnly := nt(agLabel, agSev)
	committed := nt(agLabel, agOld)
	generated := nt(agLabel, agSev)

	got := evaluateSeedFreshness(committed, generated, agOnly)
	if got.level != auditFAIL {
		t.Fatalf("level = %v, want FAIL", got.level)
	}
	if len(got.details) == 0 {
		t.Fatal("expected owned drift details")
	}
}

func TestRunRebuild_DefaultSelfOnlyIgnoresServicesRepo(t *testing.T) {
	agRepo, svcRepo := setupSeedStatusRepos(t)

	if code := runRebuild([]string{"--ag-repo", agRepo, "--services-repo", svcRepo, "--no-runtime-reload"}); code != 0 {
		t.Fatalf("runRebuild code=%d, want 0", code)
	}

	seedPath := filepath.Join(agRepo, "golang", "server", "embeddata", "awareness.nt")
	seedBytes, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	if strings.Contains(string(seedBytes), "svc.test.one") {
		t.Fatalf("default rebuild leaked services triples into self-only seed:\n%s", string(seedBytes))
	}

	txPath := defaultTransactionPath(agRepo)
	txBytes, err := os.ReadFile(txPath)
	if err != nil {
		t.Fatalf("read transaction stamp: %v", err)
	}
	if !strings.Contains(string(txBytes), "repo\tservices\tmissing") {
		t.Fatalf("self-only transaction should mark services missing:\n%s", string(txBytes))
	}
}
