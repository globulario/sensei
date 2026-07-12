// SPDX-License-Identifier: AGPL-3.0-only

package main

import "testing"

func TestExtractInvariantCandidates_FilterAndProof(t *testing.T) {
	tests := []bootstrapTest{
		// Rule-signaling → candidate invariants.
		{ID: "context_test.go:TestContextCopyShouldNotCancel"},   // negated modal
		{ID: "gin_integration_test.go:TestConcurrentHandleContext"}, // property
		{ID: "render_test.go:TestRenderHTMLDebugPanics"},         // panic contract
		{ID: "store_test.go:TestReloadIsIdempotent"},             // property
		// Example-based → NOT invariants (must be skipped).
		{ID: "context_file_test.go:TestContextFileNotFound"}, // "not" after noun
		{ID: "fs_test.go:TestOnlyFilesFSOpen"},               // "only" is a type name
		{ID: "math_test.go:TestAddReturnsSum"},               // plain example
		{ID: "routes_test.go:TestMethodNotAllowedNoRoute"},   // "not" after noun
		// File-level entry (non-Go): no function name → skipped.
		{ID: "spec/thing.spec.ts"},
	}

	got := map[string]invariantCandidate{}
	for _, inv := range extractInvariantCandidates(tests) {
		got[inv.ID] = inv
	}

	wantIn := []string{
		"invariant.candidate.context_copy_should_not_cancel",
		"invariant.candidate.concurrent_handle_context",
		"invariant.candidate.render_h_t_m_l_debug_panics",
		"invariant.candidate.reload_is_idempotent",
	}
	for _, id := range wantIn {
		if _, ok := got[id]; !ok {
			t.Errorf("expected rule-signaling test to yield %s", id)
		}
	}
	wantOut := []string{
		"invariant.candidate.context_file_not_found",
		"invariant.candidate.only_files_f_s_open",
		"invariant.candidate.add_returns_sum",
		"invariant.candidate.method_not_allowed_no_route",
	}
	for _, id := range wantOut {
		if _, ok := got[id]; ok {
			t.Errorf("example-based test wrongly lifted to invariant: %s", id)
		}
	}

	// The proving test is carried as the required_test, and protects.files maps
	// the test file to its source sibling.
	if inv := got["invariant.candidate.context_copy_should_not_cancel"]; inv.Status != "candidate" ||
		len(inv.RequiredTests) != 1 || inv.RequiredTests[0] != "context_test.go:TestContextCopyShouldNotCancel" {
		t.Errorf("required_test not carried as proof: %+v", inv)
	}
	if inv := got["invariant.candidate.reload_is_idempotent"]; len(inv.Protects.Files) != 1 || inv.Protects.Files[0] != "store.go" {
		t.Errorf("protects.files should map store_test.go -> store.go: %+v", inv.Protects)
	}
}

func TestAnyRuleToken_NegatedModal(t *testing.T) {
	rule := [][]string{
		{"context", "copy", "should", "not", "cancel"}, // should not
		{"reload", "does", "not", "panic"},             // does not
		{"config", "cannot", "be", "empty"},            // single token
		{"reload", "is", "idempotent"},                 // property token
	}
	for _, w := range rule {
		if !anyRuleToken(w) {
			t.Errorf("expected rule for %v", w)
		}
	}
	notRule := [][]string{
		{"context", "file", "not", "found"},   // not after noun
		{"only", "files", "f", "s", "open"},   // "only" no longer a token
		{"add", "returns", "sum"},             // plain example
		{"method", "not", "allowed", "route"}, // not after noun
	}
	for _, w := range notRule {
		if anyRuleToken(w) {
			t.Errorf("did not expect rule for %v", w)
		}
	}
}
