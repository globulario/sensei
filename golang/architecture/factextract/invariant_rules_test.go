// SPDX-License-Identifier: AGPL-3.0-only

package factextract

import "testing"

func TestSplitCamelAndHumanize(t *testing.T) {
	got := splitCamel("ContextCopyShouldNotCancel")
	want := []string{"context", "copy", "should", "not", "cancel"}
	if len(got) != len(want) {
		t.Fatalf("splitCamel = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("splitCamel[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Subtest suffix is dropped.
	if g := splitCamel("RaceContextCopy/sub"); g[len(g)-1] != "copy" {
		t.Errorf("subtest suffix not dropped: %v", g)
	}
	if h := humanizeWords(want); h != "Context copy should not cancel" {
		t.Errorf("humanizeWords = %q", h)
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
