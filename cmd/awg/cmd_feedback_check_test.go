// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func hasSignalContaining(signals []string, substr string) bool {
	for _, s := range signals {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

func TestEvaluateFeedbackGap_WarnsOnRiskWithoutFeedback(t *testing.T) {
	changed := []string{
		"golang/server/auth.go",
		"golang/server/auth_incident_test.go",
	}
	res := evaluateFeedbackGap(changed, nil)
	if !res.Warn {
		t.Fatalf("expected warn=true for risky change without feedback; signals=%v", res.RiskSignals)
	}
	if res.FeedbackWritten {
		t.Fatal("feedbackWritten should be false")
	}
	if res.Reminder != feedbackReminderText {
		t.Fatalf("reminder = %q, want the canonical reminder text", res.Reminder)
	}
	if len(res.Suggestions) == 0 {
		t.Fatal("expected awg propose suggestions")
	}
	if !hasSignalContaining(res.RiskSignals, "incident/regression test added") {
		t.Errorf("expected incident-test signal, got %v", res.RiskSignals)
	}
	if !hasSignalContaining(res.RiskSignals, "auth/storage/cluster") {
		t.Errorf("expected sensitive-subsystem signal, got %v", res.RiskSignals)
	}
	if !hasSignalContaining(res.RiskSignals, "error class fixed with a test") {
		t.Errorf("expected code+test scar signal, got %v", res.RiskSignals)
	}
}

func TestEvaluateFeedbackGap_QuietWhenFeedbackAdded(t *testing.T) {
	changed := []string{
		"golang/server/auth.go",
		"golang/server/auth_incident_test.go",
		"docs/awareness/failure_modes.yaml", // the scar was recorded
	}
	res := evaluateFeedbackGap(changed, nil)
	if res.Warn {
		t.Fatalf("expected warn=false when feedback file changed; signals=%v", res.RiskSignals)
	}
	if !res.FeedbackWritten {
		t.Fatal("feedbackWritten should be true")
	}
	if res.Reminder != "" {
		t.Errorf("no reminder expected, got %q", res.Reminder)
	}
}

func TestEvaluateFeedbackGap_QuietOnBenignChanges(t *testing.T) {
	changed := []string{
		"README.md",
		"docs/notes.txt",
	}
	res := evaluateFeedbackGap(changed, nil)
	if res.Warn {
		t.Fatalf("benign changes should not warn; signals=%v", res.RiskSignals)
	}
}

func TestEvaluateFeedbackGap_HighRiskPrefixMatch(t *testing.T) {
	changed := []string{"golang/store/oxigraph/oxigraph.go"}
	res := evaluateFeedbackGap(changed, []string{"golang/store/"})
	if !res.Warn {
		t.Fatalf("edit under a high-risk prefix should warn; signals=%v", res.RiskSignals)
	}
	if !hasSignalContaining(res.RiskSignals, "high-risk path edited") {
		t.Errorf("expected high-risk-path signal, got %v", res.RiskSignals)
	}
}

func TestEvaluateFeedbackGap_ContractUnknownCountsAsFeedback(t *testing.T) {
	changed := []string{
		"golang/server/auth.go",
		"golang/server/auth_incident_test.go",
		"docs/awareness/candidates/contract_unknown_foo.yaml",
	}
	res := evaluateFeedbackGap(changed, nil)
	if res.Warn {
		t.Fatalf("a queued contract_unknown counts as feedback; signals=%v", res.RiskSignals)
	}
	if !res.FeedbackWritten {
		t.Fatal("feedbackWritten should be true for a contract_unknown candidate")
	}
}
