// SPDX-License-Identifier: Apache-2.0

package repoeval

import (
	"strings"
	"testing"
)

func TestDeriveConfidence_HonestBasis(t *testing.T) {
	// Well-governed, non-empty surfaces, clean integrity → high, but the
	// freshness caveat is ALWAYS disclosed.
	conf, caveats := deriveConfidence(Inputs{
		CriticalSurfaceTotal: 5, AuthoritySurfaceTotal: 3, HighRiskSurfaceTotal: 8,
	})
	if conf != "high" {
		t.Errorf("clean+non-empty confidence = %q, want high", conf)
	}
	if !containsSubstr(caveats, "freshness vs current source is NOT verified") {
		t.Errorf("freshness caveat must always be present; got %v", caveats)
	}

	// A perfect score over empty surfaces must NOT be high confidence.
	conf, caveats = deriveConfidence(Inputs{CriticalSurfaceTotal: 0, AuthoritySurfaceTotal: 0, HighRiskSurfaceTotal: 8})
	if conf != "medium" {
		t.Errorf("empty-surface confidence = %q, want medium", conf)
	}
	if !containsSubstr(caveats, "EMPTY measured surface") {
		t.Errorf("empty-surface caveat missing; got %v", caveats)
	}

	// Failing integrity makes the whole score untrustworthy → low.
	conf, _ = deriveConfidence(Inputs{IntegrityFails: 2, CriticalSurfaceTotal: 5, AuthoritySurfaceTotal: 5, HighRiskSurfaceTotal: 5})
	if conf != "low" {
		t.Errorf("integrity-failing confidence = %q, want low", conf)
	}
}

func TestEvaluate_ConfidenceIsNotHardcoded(t *testing.T) {
	// The 100/100 sealed-corpus case that previously reported "high": empty
	// surfaces must pull confidence down to medium and disclose why.
	rep := Evaluate(Inputs{WeightedCoveragePercent: 100})
	if rep.Confidence == "high" {
		t.Error("empty-surface repo should not report high confidence")
	}
	if rep.AgentReadiness.Confidence != rep.Confidence {
		t.Errorf("readiness confidence %q should match report confidence %q", rep.AgentReadiness.Confidence, rep.Confidence)
	}
	if len(rep.Caveats) == 0 {
		t.Error("report must disclose caveats")
	}
}

// The readiness block must NOT vouch for itself with a fixed "high" — it takes
// its confidence straight from deriveConfidence, so an integrity-failing or
// empty-surface repo can never report a confidence it did not earn. This guards
// against the hardcoded default returning to evaluateAgentReadiness.
func TestEvaluateAgentReadiness_ConfidenceTracksDerived(t *testing.T) {
	cases := []struct {
		name string
		in   Inputs
		want string
	}{
		{"integrity fails => low", Inputs{IntegrityFails: 1, CriticalSurfaceTotal: 5, AuthoritySurfaceTotal: 5, HighRiskSurfaceTotal: 5}, "low"},
		{"empty surface => medium", Inputs{WeightedCoveragePercent: 100}, "medium"},
		{"full clean surface => high", Inputs{CriticalSurfaceTotal: 5, AuthoritySurfaceTotal: 5, HighRiskSurfaceTotal: 5}, "high"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want, _ := deriveConfidence(tc.in)
			if want != tc.want {
				t.Fatalf("test premise wrong: deriveConfidence=%q, want %q", want, tc.want)
			}
			got := evaluateAgentReadiness(tc.in, 50, want)
			if got.Confidence != tc.want {
				t.Errorf("readiness confidence = %q, want %q (must track deriveConfidence, not a fixed 'high')", got.Confidence, tc.want)
			}
		})
	}
}

func containsSubstr(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func TestEvaluate_EmitsVisibleSubscoresAndFindings(t *testing.T) {
	rep := Evaluate(Inputs{
		IntegrityFails: 1,
		IntegrityIssues: []IntegrityIssue{
			{
				Check:    "yaml-validity",
				Severity: "fail",
				Summary:  "12 invalid awareness files",
				Evidence: []string{"docs/awareness/generated/components.yaml"},
			},
		},
		WeightedCoveragePercent:           62,
		CriticalCoveragePercent:           40,
		CriticalSurfaceTotal:              5,
		HighRiskCoveragePercent:           55,
		HighRiskSurfaceTotal:              8,
		AuthorityCoveragePercent:          35,
		AuthoritySurfaceTotal:             3,
		UnknownHighRiskCount:              3,
		UnknownHighRiskFiles:              []string{"golang/repository/x.go"},
		CriticalHighInvariantCount:        10,
		MissingCriticalHighInvariantTests: 2,
		ContractFoundCount:                4,
		ContractSynthesisSafeCount:        2,
		ContractProposalOnlyCount:         1,
		ContractUnknownCount:              1,
		StaleFileRefCount:                 1,
		StaleFileRefs:                     []string{"golang/old/missing.go"},
		PatternMisuseCount:                2,
		PatternMisuseIDs:                  []string{"pm.cross_domain_leak"},
	})
	if rep.OverallScore <= 0 {
		t.Fatalf("overall score not computed: %+v", rep)
	}
	if len(rep.Dimensions) != 6 {
		t.Fatalf("dimensions=%d, want 6", len(rep.Dimensions))
	}
	if len(rep.Findings) == 0 || len(rep.Recommendations) == 0 {
		t.Fatalf("missing findings/recommendations: %+v", rep)
	}
	if rep.AgentReadiness.Verdict != "not_ready_for_confident_agents" {
		t.Fatalf("agent readiness=%q, want not_ready_for_confident_agents", rep.AgentReadiness.Verdict)
	}
	if len(rep.AgentReadiness.Blockers) == 0 {
		t.Fatalf("expected readiness blockers: %+v", rep.AgentReadiness)
	}
	if !strings.Contains(rep.AgentReadiness.Blockers[0], "yaml-validity") {
		t.Fatalf("expected named integrity blocker, got %+v", rep.AgentReadiness.Blockers)
	}
	if len(rep.IntegrityFindings) != 1 || rep.IntegrityFindings[0].Check != "yaml-validity" {
		t.Fatalf("integrity findings missing or wrong: %+v", rep.IntegrityFindings)
	}
	if !strings.Contains(rep.Dimensions[0].Summary, "yaml-validity") {
		t.Fatalf("integrity summary should name issue types: %q", rep.Dimensions[0].Summary)
	}
}

func TestEvaluate_TreatsMissingSurfaceClassesAsNotApplicable(t *testing.T) {
	rep := Evaluate(Inputs{
		WeightedCoveragePercent:           48,
		CriticalCoveragePercent:           0,
		CriticalSurfaceTotal:              0,
		HighRiskCoveragePercent:           0,
		HighRiskSurfaceTotal:              0,
		AuthorityCoveragePercent:          0,
		AuthoritySurfaceTotal:             0,
		CriticalHighInvariantCount:        1,
		MissingCriticalHighInvariantTests: 0,
		ContractFoundCount:                1,
	})

	if rep.Dimensions[4].Score <= 0 {
		t.Fatalf("architecture drift should not collapse to zero for non-applicable surface classes: %+v", rep.Dimensions[4])
	}
	if !strings.Contains(rep.Dimensions[1].Summary, "critical n/a") {
		t.Fatalf("coverage summary should mark critical as n/a: %q", rep.Dimensions[1].Summary)
	}
	if !strings.Contains(rep.Dimensions[1].Summary, "authority n/a") {
		t.Fatalf("coverage summary should mark authority as n/a: %q", rep.Dimensions[1].Summary)
	}
}

func TestEvaluate_CanMarkRepoReadyForControlledAgents(t *testing.T) {
	rep := Evaluate(Inputs{
		WeightedCoveragePercent:           84,
		CriticalCoveragePercent:           90,
		CriticalSurfaceTotal:              5,
		HighRiskCoveragePercent:           85,
		HighRiskSurfaceTotal:              8,
		AuthorityCoveragePercent:          88,
		AuthoritySurfaceTotal:             4,
		CriticalHighInvariantCount:        9,
		MissingCriticalHighInvariantTests: 0,
		ContractFoundCount:                6,
		ContractSynthesisSafeCount:        3,
		ContractProposalOnlyCount:         0,
		ContractUnknownCount:              0,
	})

	if rep.AgentReadiness.Verdict != "ready_for_controlled_agents" {
		t.Fatalf("agent readiness=%q, want ready_for_controlled_agents", rep.AgentReadiness.Verdict)
	}
	if rep.AgentReadiness.Score < 80 {
		t.Fatalf("agent readiness score=%d, want >= 80", rep.AgentReadiness.Score)
	}
	if len(rep.AgentReadiness.Blockers) != 0 {
		t.Fatalf("blockers=%v, want none", rep.AgentReadiness.Blockers)
	}
}

func TestEvaluate_CleanBootstrapCorpusOnlyGetsGuardedRepairMode(t *testing.T) {
	rep := Evaluate(Inputs{
		UpgradePath: UpgradePath{
			Invariants: []UpgradeCandidate{{
				ID:            "invariant.component.cmd.caddy",
				Kind:          "invariant",
				Title:         "Protect component.cmd.caddy",
				Rationale:     "entrypoint",
				SuggestedFile: "docs/awareness/invariants.yaml",
				Paths:         []string{"cmd/caddy/main.go"},
			}},
			Contracts: []UpgradeCandidate{{
				ID:            "component.cmd.caddy",
				Kind:          "contract",
				Title:         "Contract for component.cmd.caddy",
				Rationale:     "entrypoint contract",
				SuggestedFile: "docs/intent/component.cmd.caddy.yaml",
				Paths:         []string{"cmd/caddy/main.go"},
			}},
		},
		WeightedCoveragePercent:           81,
		CriticalCoveragePercent:           0,
		CriticalSurfaceTotal:              0,
		HighRiskCoveragePercent:           80,
		HighRiskSurfaceTotal:              8,
		AuthorityCoveragePercent:          0,
		AuthoritySurfaceTotal:             0,
		CriticalHighInvariantCount:        0,
		MissingCriticalHighInvariantTests: 0,
		ContractFoundCount:                0,
		ContractSynthesisSafeCount:        0,
		ContractProposalOnlyCount:         0,
		ContractUnknownCount:              0,
	})

	if rep.AgentReadiness.Verdict != "guarded_repair_only" {
		t.Fatalf("agent readiness=%q, want guarded_repair_only", rep.AgentReadiness.Verdict)
	}
	if !strings.Contains(rep.AgentReadiness.Requirements[len(rep.AgentReadiness.Requirements)-1], "critical/high invariants") {
		t.Fatalf("expected baseline requirement in guarded mode: %+v", rep.AgentReadiness.Requirements)
	}
	if !strings.Contains(strings.Join(rep.AgentReadiness.OperatorAdvice, " "), "clean bootstrap corpus") {
		t.Fatalf("expected bootstrap warning in operator advice: %+v", rep.AgentReadiness.OperatorAdvice)
	}
	if len(rep.UpgradePath.Invariants) != 1 || len(rep.UpgradePath.Contracts) != 1 {
		t.Fatalf("upgrade path missing: %+v", rep.UpgradePath)
	}
	if rep.UpgradePath.Summary == "" {
		t.Fatalf("upgrade path summary missing: %+v", rep.UpgradePath)
	}
}

func TestEvaluate_IntegrityFindingFallsBackWithoutStructuredIssues(t *testing.T) {
	rep := Evaluate(Inputs{IntegrityFails: 1})
	if len(rep.IntegrityFindings) != 0 {
		t.Fatalf("integrity findings=%+v, want none", rep.IntegrityFindings)
	}
	if len(rep.Findings) == 0 || !strings.Contains(rep.Findings[0].Summary, "hard graph or source integrity failures") {
		t.Fatalf("fallback integrity finding missing: %+v", rep.Findings)
	}
}
