// SPDX-License-Identifier: AGPL-3.0-only

package coverage

import (
	"os"
	"path/filepath"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

// TestPhase81bGovernedContractPresent is the govern-first gate for Slice 8.1b
// (governed promotion). It proves the atomicity/projection/journal laws and the
// dedicated promotion authority are authored in the canonical source YAML —
// every new invariant, failure mode, forbidden fix, and authority record is
// present and schema-valid — and that the promotion authority surfaces are
// high-risk covered. The contract exists before any promotion implementation.
func TestPhase81bGovernedContractPresent(t *testing.T) {
	root := repoRootForHighRisk(t)

	wantInvariants := []string{
		"closure.promotion_journal_is_not_governed_truth",
		"closure.source_commit_without_graph_verification_is_incomplete",
		"closure.graph_verification_binds_committed_manifest",
		"closure.promotion_retry_resumes_same_attempt",
		"closure.contradictory_canonical_id_never_overwritten",
		"closure.promotion_receipt_only_after_projection_verified",
		"closure.repo_graph_and_combined_seed_identities_distinct",
		"closure.promotion_journal_separate_from_task_lifecycle",
		"closure.offline_provenance_query_matches_graph_owner",
	}
	wantFailureModes := []string{
		"closure.promotion_journal_mistaken_for_governed_truth",
		"closure.incomplete_source_commit_reported_governed",
		"closure.graph_verified_against_wrong_source_world",
		"closure.duplicate_attempt_or_record_on_retry",
		"closure.contradictory_canonical_id_overwritten",
		"closure.receipt_authoritative_before_projection_verified",
		"closure.repo_graph_confused_with_combined_seed",
		"closure.promotion_event_leaks_into_task_projection",
		"closure.offline_query_diverges_from_graph_owner",
	}
	wantForbiddenFixes := []string{
		"phase8_promotion_writes_combined_seed",
		"phase8_rollback_by_deletion_as_recovery",
		"phase8_release_lock_before_graph_verified",
		"phase8_promotion_events_as_task_events",
		"phase8_trust_caller_governed_status_or_graph_digest",
		"phase8_overwrite_contradictory_governed_record",
		"phase8_bespoke_provenance_semantics",
		"phase8_temporary_compile_reported_as_promoted",
	}

	awareness := filepath.Join(root, "docs", "awareness")
	assertGovernedIDs(t, filepath.Join(awareness, "invariants.yaml"), "invariants", wantInvariants)
	assertGovernedIDs(t, filepath.Join(awareness, "failure_modes.yaml"), "failure_modes", wantFailureModes)
	assertGovernedIDs(t, filepath.Join(awareness, "forbidden_fixes.yaml"), "forbidden_fixes", wantForbiddenFixes)

	// The dedicated promotion authority (op/mechanism/domain/grant) must be authored.
	assertGovernedIDs(t, filepath.Join(awareness, "authority_domains.yaml"), "authority_domains",
		[]string{"authority.sensei_governed_promotion"})
	assertGovernedIDs(t, filepath.Join(awareness, "authority_grants.yaml"), "authority_grants",
		[]string{"grant.sensei.governed_promotion"})
	assertGovernedIDs(t, filepath.Join(awareness, "mutation_paths.yaml"), "mutation_paths",
		[]string{"mutation_path.governed_promotion"})

	// The promotion authority surfaces must require a briefing before an edit — the
	// future promotion owner and repository-graph owner (under the already-covered
	// golang/architecture/ prefix) and the governed authority sources.
	hrData, err := os.ReadFile(filepath.Join(awareness, "high_risk_files.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var hr struct {
		Files []string `yaml:"files"`
	}
	if err := yaml.Unmarshal(hrData, &hr); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"golang/architecture/questionpromotion/promote.go", // future promotion owner
		"golang/architecture/governedmutation/owner.go",    // future governed-mutation owner
		"docs/awareness/authority_grants.yaml",
		"docs/awareness/authority_domains.yaml",
		"docs/awareness/mutation_paths.yaml",
	} {
		if !requiresBriefing(hr.Files, p) {
			t.Errorf("Phase-8.1b authority surface %q is not high-risk covered", p)
		}
	}
}
