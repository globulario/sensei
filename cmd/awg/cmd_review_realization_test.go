// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestReview_RecordsDecisionAndExcludesFromSuggest(t *testing.T) {
	var f reviewsFile
	updated := recordReview(&f, "contract.http.api_cors_diagnostics",
		"contract.config_mutation_requires_valid_token", "rejected",
		"read/diagnostic surface; no config-mutation obligation")
	if updated {
		t.Error("first record should be a new entry, not an update")
	}
	if len(f.ContractRealizationReviews.Reviews) != 1 {
		t.Fatalf("expected 1 review, got %d", len(f.ContractRealizationReviews.Reviews))
	}

	// The decided pair must be excluded from future suggestion.
	decided := decidedPairs(renderReviews(&f))
	if !decided["contract.http.api_cors_diagnostics|contract.config_mutation_requires_valid_token"] {
		t.Error("rejected pair must appear in decidedPairs so suggest skips it")
	}
}

func TestReview_UpsertsSamePair(t *testing.T) {
	var f reviewsFile
	recordReview(&f, "i", "a", "needs_test", "no test yet")
	updated := recordReview(&f, "i", "a", "rejected", "decided it's wrong")
	if !updated {
		t.Error("second record of the same pair must update, not duplicate")
	}
	if n := len(f.ContractRealizationReviews.Reviews); n != 1 {
		t.Fatalf("expected 1 entry after upsert, got %d", n)
	}
	if f.ContractRealizationReviews.Reviews[0].Decision != "rejected" {
		t.Errorf("decision not updated: %q", f.ContractRealizationReviews.Reviews[0].Decision)
	}
}

func TestReview_DecisionEnumValidatedByCommand(t *testing.T) {
	// invalid decision must be refused (exit 2 from the command)
	if reviewDecisions["promoted"] {
		t.Error("'promoted' must NOT be a review decision — promotion is a separate command")
	}
	for _, d := range []string{"rejected", "needs_contract", "needs_test", "needs_failure_mode", "needs_human_decision"} {
		if !reviewDecisions[d] {
			t.Errorf("%q must be a valid review decision", d)
		}
	}
	if reviewDecisions["bogus"] {
		t.Error("unknown decision must be invalid")
	}
}

func TestReview_RenderIsCandidatesOnlyProcessState(t *testing.T) {
	var f reviewsFile
	recordReview(&f, "i", "a", "rejected", "why")
	out := string(renderReviews(&f))
	// reviews are process-state, never authority — must not assert realizesContract.
	if strings.Contains(out, "realizesContract") {
		t.Error("reviews file must not assert authority predicates")
	}
	if !strings.Contains(out, "decision: rejected") || !strings.Contains(out, "reason: why") {
		t.Errorf("review entry not rendered:\n%s", out)
	}
}
