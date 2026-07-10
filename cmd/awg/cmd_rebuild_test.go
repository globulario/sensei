// SPDX-License-Identifier: AGPL-3.0-only

package main

import "testing"

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
