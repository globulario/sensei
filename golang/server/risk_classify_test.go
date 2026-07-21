// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// ─── helpers ─────────────────────────────────────────────────────────────

func mkNode(id, label, severity string) *awarenesspb.KnowledgeNode {
	return &awarenesspb.KnowledgeNode{
		Id:       id,
		Label:    label,
		Severity: severity,
		Class:    "Invariant",
	}
}

func sufficientCoverage() *awarenesspb.CoverageSummary {
	return &awarenesspb.CoverageSummary{Sufficient: true}
}

func insufficientCoverage() *awarenesspb.CoverageSummary {
	return &awarenesspb.CoverageSummary{Sufficient: false}
}

func mkPattern(strength string) *awarenesspb.MatchedImplementationPattern {
	return &awarenesspb.MatchedImplementationPattern{
		Id:            "implementation_pattern:test.pattern",
		MatchStrength: strength,
	}
}

// ─── Tests ───────────────────────────────────────────────────────────────

// 1. Priority table — each higher-priority category beats the lower ones
// when ALL keywords are present.
func TestClassifyRisk_TablePriority(t *testing.T) {
	// Anchor with all four keyword groups embedded → DATA_LOSS_RISK wins
	// (highest priority).
	all := mkNode("etcd.wipe_and_security.rbac_and_convergence.reconcile_loop",
		"data_loss + security + convergence in one entity", "warning")
	risk, reasons := classifyRisk(ClassifyInputs{
		Direct:   []*awarenesspb.KnowledgeNode{all},
		Coverage: sufficientCoverage(),
	})
	if risk != awarenesspb.RiskClass_DATA_LOSS_RISK {
		t.Errorf("priority: want DATA_LOSS_RISK, got %v (reasons=%v)", risk, reasons)
	}
}

// 2. Each keyword family fires its own category — separate fixtures.
func TestClassifyRisk_KeywordCoverage(t *testing.T) {
	cases := []struct {
		name string
		node *awarenesspb.KnowledgeNode
		want awarenesspb.RiskClass
	}{
		{"data_loss explicit", mkNode("minio.format_json_rewrite", "blob_missing after reformat", "high"),
			awarenesspb.RiskClass_DATA_LOSS_RISK},
		{"security namespace", mkNode("security.deny_overrides_allow", "RBAC deny order", "warning"),
			awarenesspb.RiskClass_SECURITY_RISK},
		{"convergence namespace", mkNode("convergence.installed_state_drift", "drift detection", "high"),
			awarenesspb.RiskClass_CONVERGENCE_RISK},
		{"runtime identity", mkNode("runtime.identity_attestation", "PID proof", "high"),
			awarenesspb.RiskClass_CONVERGENCE_RISK},
		{"build_id family", mkNode("desired.build_id_immutable", "build_id is identity", "high"),
			awarenesspb.RiskClass_CONVERGENCE_RISK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			risk, reasons := classifyRisk(ClassifyInputs{
				Direct:   []*awarenesspb.KnowledgeNode{tc.node},
				Coverage: sufficientCoverage(),
			})
			if risk != tc.want {
				t.Errorf("want %v, got %v (reasons=%v)", tc.want, risk, reasons)
			}
		})
	}
}

// 3. High-risk directory escalates to ARCHITECTURE_SENSITIVE minimum
// when no higher-priority keyword fires.
func TestClassifyRisk_HighRiskDirectoryEscalates(t *testing.T) {
	risk, _ := classifyRisk(ClassifyInputs{
		Direct:   []*awarenesspb.KnowledgeNode{mkNode("benign.invariant", "nothing exciting", "warning")},
		Coverage: sufficientCoverage(),
		Files:    []string{"golang/node_agent/internal/foo.go"},
	})
	if risk != awarenesspb.RiskClass_ARCHITECTURE_SENSITIVE {
		t.Errorf("want ARCHITECTURE_SENSITIVE, got %v", risk)
	}
}

// 4. Critical severity escalates to ARCHITECTURE_SENSITIVE even outside
// high-risk dirs and without keyword hits.
func TestClassifyRisk_CriticalSeverityEscalates(t *testing.T) {
	risk, _ := classifyRisk(ClassifyInputs{
		Direct:   []*awarenesspb.KnowledgeNode{mkNode("some.invariant", "any title", "critical")},
		Coverage: sufficientCoverage(),
	})
	if risk != awarenesspb.RiskClass_ARCHITECTURE_SENSITIVE {
		t.Errorf("want ARCHITECTURE_SENSITIVE, got %v", risk)
	}
}

// 5. Anchors + clean path + no keyword + no critical → LOW_RISK.
func TestClassifyRisk_AnchorsCleanLowRisk(t *testing.T) {
	risk, _ := classifyRisk(ClassifyInputs{
		Direct:   []*awarenesspb.KnowledgeNode{mkNode("benign.documented", "documented behavior", "info")},
		Coverage: sufficientCoverage(),
		Files:    []string{"golang/echo/echo_client/echo_client.go"},
	})
	if risk != awarenesspb.RiskClass_LOW_RISK {
		t.Errorf("want LOW_RISK, got %v", risk)
	}
}

// 6. Insufficient coverage trumps everything else (rule 1).
func TestClassifyRisk_InsufficientCoverageAlwaysUnknown(t *testing.T) {
	risk, _ := classifyRisk(ClassifyInputs{
		Direct: []*awarenesspb.KnowledgeNode{
			mkNode("security.foo", "x", "critical"),
		},
		Coverage: insufficientCoverage(),
		Files:    []string{"golang/security/foo.go"},
	})
	if risk != awarenesspb.RiskClass_UNKNOWN_IMPACT {
		t.Errorf("insufficient coverage must yield UNKNOWN_IMPACT regardless of anchors; got %v", risk)
	}
}

// 7. Pattern-only + high-risk path → ARCHITECTURE_SENSITIVE (NEW per adj. 2).
// This is the load-bearing safety rule: a recipe match alone does not
// certify the change is safe.
func TestClassifyRisk_PatternOnlyDoesNotOverrideHighRiskPath(t *testing.T) {
	risk, reasons := classifyRisk(ClassifyInputs{
		Patterns: []*awarenesspb.MatchedImplementationPattern{mkPattern("strong")},
		Coverage: sufficientCoverage(),
		Files:    []string{"golang/node_agent/node_agent_server/server.go"},
	})
	if risk != awarenesspb.RiskClass_ARCHITECTURE_SENSITIVE {
		t.Errorf("pattern + high-risk path: want ARCHITECTURE_SENSITIVE, got %v (reasons=%v)", risk, reasons)
	}
}

// 8. Pattern-only + clean path → LOW_RISK.
func TestClassifyRisk_PatternOnlyCleanPathLowRisk(t *testing.T) {
	risk, _ := classifyRisk(ClassifyInputs{
		Patterns: []*awarenesspb.MatchedImplementationPattern{mkPattern("strong")},
		Coverage: sufficientCoverage(),
		Files:    nil, // task-only
	})
	if risk != awarenesspb.RiskClass_LOW_RISK {
		t.Errorf("pattern + clean path: want LOW_RISK, got %v", risk)
	}
}

// 9. Narrow-tier pattern doesn't count — only strong/medium.
func TestClassifyRisk_NarrowPatternAloneDoesNotCount(t *testing.T) {
	// Narrow pattern, no anchors, no high-risk path, BUT coverage marked
	// sufficient by some other signal (we test the classifier in isolation
	// from coverage computation here).
	risk, _ := classifyRisk(ClassifyInputs{
		Patterns: []*awarenesspb.MatchedImplementationPattern{mkPattern("narrow")},
		Coverage: sufficientCoverage(),
	})
	// Falls through to rule 9 — no anchors, no strong/medium pattern, but
	// coverage said sufficient → LOW_RISK ("graph knows, says nothing").
	if risk != awarenesspb.RiskClass_LOW_RISK {
		t.Errorf("narrow-only with sufficient coverage: want LOW_RISK, got %v", risk)
	}
}

// 9b. NEW (Phase 5): high-risk path + zero anchors + no strong pattern
// + sufficient coverage → UNKNOWN_IMPACT (NOT LOW_RISK). Without this
// rule the classifier would have returned LOW_RISK as a false-OK on an
// unannotated high-risk file (e.g. rules/heal_policy.go before its
// Phase 2 @awareness header was added).
func TestClassifyRisk_HighRiskPathNoAnchorsNoPatternUnknown(t *testing.T) {
	risk, reasons := classifyRisk(ClassifyInputs{
		Coverage: sufficientCoverage(),
		Files:    []string{"golang/cluster_doctor/cluster_doctor_server/rules/heal_policy.go"},
	})
	if risk != awarenesspb.RiskClass_UNKNOWN_IMPACT {
		t.Fatalf("high-risk path with zero anchors must NOT be LOW_RISK; got %v", risk)
	}
	if len(reasons) == 0 || !strings.HasPrefix(reasons[0], HighRiskNoAnchorReasonPrefix) {
		t.Fatalf("reason must start with %q; got reasons=%v", HighRiskNoAnchorReasonPrefix, reasons)
	}
}

// 9c. NEW (Phase 5): high-risk path + zero anchors + STRONG pattern.
// Rule 7 (pattern-only + high-risk path → ARCHITECTURE_SENSITIVE) still
// wins — pattern signal does NOT downgrade us to LOW_RISK in this case,
// but it DOES upgrade us out of the high-risk-no-anchor branch because
// a strong-pattern match is a real graph signal (not the "graph has no
// facts" case).
func TestClassifyRisk_HighRiskPathNoAnchorsWithStrongPatternIsSensitive(t *testing.T) {
	risk, _ := classifyRisk(ClassifyInputs{
		Patterns: []*awarenesspb.MatchedImplementationPattern{mkPattern("strong")},
		Coverage: sufficientCoverage(),
		Files:    []string{"golang/cluster_doctor/cluster_doctor_server/rules/heal_policy.go"},
	})
	if risk != awarenesspb.RiskClass_ARCHITECTURE_SENSITIVE {
		t.Fatalf("high-risk path + strong pattern: rule 7 must win → ARCHITECTURE_SENSITIVE; got %v", risk)
	}
}

// 9d. NEW (Phase 5): non-high-risk path + zero anchors + sufficient
// coverage → still LOW_RISK via rule 10. The new rule 9 must NOT fire
// for files outside the high-risk directory list.
func TestClassifyRisk_CleanPathNoAnchorsStillLowRisk(t *testing.T) {
	risk, _ := classifyRisk(ClassifyInputs{
		Coverage: sufficientCoverage(),
		Files:    []string{"golang/echo/echo_server/server.go"},
	})
	if risk != awarenesspb.RiskClass_LOW_RISK {
		t.Fatalf("clean-path no-anchor case must stay LOW_RISK; got %v", risk)
	}
}

// 10. Empty inputs with sufficient coverage → LOW_RISK (graph knows).
func TestClassifyRisk_NoAnchorsNoPatternsCoverageSufficientLowRisk(t *testing.T) {
	risk, _ := classifyRisk(ClassifyInputs{
		Coverage: sufficientCoverage(),
	})
	if risk != awarenesspb.RiskClass_LOW_RISK {
		t.Errorf("graph-indexed-no-rules-apply: want LOW_RISK, got %v", risk)
	}
}

// 11. Anchors dominate pattern signal — pattern in high-risk path is
// drowned out by a security-keyword anchor (higher priority).
func TestClassifyRisk_AnchorsDominatePatterns(t *testing.T) {
	risk, _ := classifyRisk(ClassifyInputs{
		Direct: []*awarenesspb.KnowledgeNode{
			mkNode("security.rbac.permission_resolution", "permission walks tree", "high"),
		},
		Patterns: []*awarenesspb.MatchedImplementationPattern{mkPattern("strong")},
		Coverage: sufficientCoverage(),
		Files:    []string{"golang/node_agent/server.go"}, // high-risk path
	})
	if risk != awarenesspb.RiskClass_SECURITY_RISK {
		t.Errorf("anchor's security keyword must dominate pattern+high-risk path; want SECURITY_RISK, got %v", risk)
	}
}

// 12. Confidence tiering — 0/1-2/≥3 anchor bands.
func TestComputeConfidence_TieredByAnchorCount(t *testing.T) {
	cases := []struct {
		name     string
		direct   []*awarenesspb.KnowledgeNode
		pats     []*awarenesspb.MatchedImplementationPattern
		coverage *awarenesspb.CoverageSummary
		want     awarenesspb.Confidence
	}{
		{"3 anchors + sufficient → HIGH",
			[]*awarenesspb.KnowledgeNode{mkNode("a", "", ""), mkNode("b", "", ""), mkNode("c", "", "")},
			nil, sufficientCoverage(),
			awarenesspb.Confidence_CONFIDENCE_HIGH},
		{"1 anchor → MEDIUM",
			[]*awarenesspb.KnowledgeNode{mkNode("a", "", "")},
			nil, sufficientCoverage(),
			awarenesspb.Confidence_CONFIDENCE_MEDIUM},
		{"0 anchors + strong pattern → MEDIUM",
			nil,
			[]*awarenesspb.MatchedImplementationPattern{mkPattern("strong")},
			sufficientCoverage(),
			awarenesspb.Confidence_CONFIDENCE_MEDIUM},
		{"empty → LOW",
			nil, nil, sufficientCoverage(),
			awarenesspb.Confidence_CONFIDENCE_LOW},
		{"3 anchors but insufficient coverage → MEDIUM (not HIGH)",
			[]*awarenesspb.KnowledgeNode{mkNode("a", "", ""), mkNode("b", "", ""), mkNode("c", "", "")},
			nil, insufficientCoverage(),
			awarenesspb.Confidence_CONFIDENCE_MEDIUM},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeConfidence(tc.direct, tc.pats, tc.coverage)
			if got != tc.want {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}
