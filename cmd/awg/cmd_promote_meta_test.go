// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A meta-principle candidate must route to the portable pack under the
// invariants list, not the product canonical invariants.yaml.
func TestResolvePromoteTarget_MetaPrinciple(t *testing.T) {
	target, err := resolvePromoteTarget("", map[string]interface{}{"class": "meta_principle"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if target != metaPrincipleTarget {
		t.Fatalf("target = %q, want %q", target, metaPrincipleTarget)
	}
	if k := promoteTargetToListKey[target]; k != "invariants" {
		t.Errorf("list key = %q, want invariants", k)
	}
	if c := promoteTargetToClass[target]; c != "meta_principle" {
		t.Errorf("class = %q, want meta_principle", c)
	}
	// An explicit subpath target round-trips too.
	if got, err := resolvePromoteTarget(metaPrincipleTarget, nil); err != nil || got != metaPrincipleTarget {
		t.Errorf("explicit subpath: got %q, err %v", got, err)
	}
}

func TestArchitectureClaimCannotBePromotionTarget(t *testing.T) {
	if _, ok := promoteClassToTarget["architecture_claim"]; ok {
		t.Fatal("architecture_claim must not be a promotable candidate class")
	}
	if _, ok := promoteTargetToClass["architecture_claims.yaml"]; ok {
		t.Fatal("architecture_claims.yaml must not be a promotion target")
	}
	if _, err := resolvePromoteTarget("", map[string]interface{}{"class": "architecture_claim"}); err == nil {
		t.Fatal("architecture_claim promotion target unexpectedly resolved")
	}
}

func TestOpenQuestionCannotBePromotionTarget(t *testing.T) {
	if _, ok := promoteClassToTarget["open_question"]; ok {
		t.Fatal("open_question must not be a promotable candidate class")
	}
	if _, ok := promoteTargetToClass["open_questions.yaml"]; ok {
		t.Fatal("open_questions.yaml must not be a promotion target")
	}
	if _, err := resolvePromoteTarget("", map[string]interface{}{"class": "open_question"}); err == nil {
		t.Fatal("open_question promotion target unexpectedly resolved")
	}
}

func TestArchitectAnswerCannotBePromotionTarget(t *testing.T) {
	if _, ok := promoteClassToTarget["architect_answer"]; ok {
		t.Fatal("architect_answer must not be a promotable candidate class")
	}
	if _, ok := promoteTargetToClass["architect_answers.yaml"]; ok {
		t.Fatal("architect_answers.yaml must not be a promotion target")
	}
	if _, err := resolvePromoteTarget("", map[string]interface{}{"class": "architect_answer"}); err == nil {
		t.Fatal("architect_answer promotion target unexpectedly resolved")
	}
}

func TestEvidenceProbeCannotBePromotionTarget(t *testing.T) {
	if _, ok := promoteClassToTarget["evidence_probe"]; ok {
		t.Fatal("evidence_probe must not be a promotable candidate class")
	}
	if _, ok := promoteTargetToClass["evidence_probes.yaml"]; ok {
		t.Fatal("evidence_probes.yaml must not be a promotion target")
	}
	if _, err := resolvePromoteTarget("", map[string]interface{}{"class": "evidence_probe"}); err == nil {
		t.Fatal("evidence_probe promotion target unexpectedly resolved")
	}
}

// Only a meta.* id is dual-typed MetaPrinciple by the importer, so a
// meta_principle candidate with any other id must be rejected before it can
// land in the portable pack as a disguised invariant.
func TestValidateCandidate_MetaPrincipleRequiresMetaPrefix(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "generic"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, metaPrincipleTarget), []byte("invariants: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mk := func(id string) map[string]interface{} {
		return map[string]interface{}{
			"id": id, "class": "meta_principle", "status": "candidate",
			"confidence": "high", "evidence": "e", "discovered_from": "d",
		}
	}
	if err := validateCandidateEntry(mk("meta.some_principle"), metaPrincipleTarget, dir); err != nil {
		t.Errorf("meta.* id should validate: %v", err)
	}
	if err := validateCandidateEntry(mk("notmeta.principle"), metaPrincipleTarget, dir); err == nil {
		t.Errorf("non-meta id with class meta_principle should be rejected")
	}
}
