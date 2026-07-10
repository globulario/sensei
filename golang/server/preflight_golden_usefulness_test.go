// SPDX-License-Identifier: Apache-2.0

package main

// preflight_golden_usefulness_test.go — Phase 5.
//
// The graph must be tested for USEFULNESS, not just syntactic validity. This
// table-driven test runs the full Preflight pipeline against the real compiled
// graph (the embedded seed, via seedStore) for representative edit tasks and
// asserts the concrete signals an agent needs surface: the right implementation
// pattern, the right authority guidance (owner / forbidden bypass / evidence
// freshness), and the real file-anchored invariants/intents.
//
// Assertions are on node and pattern IDs (and authored authority strings), not
// fragile prose — so a regression in awareness retrieval (a deleted pattern, a
// broken matcher, a dropped anchor edge, a mis-wired Preflight assembler) fails
// CI before agents silently lose guidance.

import (
	"context"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"github.com/globulario/awareness-graph/golang/rdf"
)

func TestPreflightGoldenUsefulness(t *testing.T) {
	requireCombinedSeed(t)
	// Sanity: the embedded seed must actually carry the new surfaces, else the
	// whole test is vacuous. Fail loudly rather than silently pass.
	g := loadSeedGraph()
	if len(g.byClass[rdf.ClassImplementationPattern]) == 0 {
		t.Fatal("seed has no ImplementationPattern nodes — rebuild the seed before running golden tests")
	}
	if len(g.byClass[rdf.ClassAuthorityDomain]) == 0 {
		t.Fatal("seed has no AuthorityDomain nodes — rebuild the seed before running golden tests")
	}

	cases := []struct {
		name string
		task string
		// files that would be touched by the task.
		files []string
		// wantPattern is the implementation pattern id that must surface.
		wantPattern string
		// wantForbidden substrings must each appear in forbidden_fixes.
		wantForbidden []string
		// wantAction substrings must each appear in required_actions.
		wantAction []string
		// wantAnchorIDs are graph-anchored invariant/intent/failure_mode ids
		// that must surface as direct anchors (verified via awareness.impact).
		wantAnchorIDs []string
	}{
		{
			name:        "add cluster-doctor rule",
			task:        "add a new cluster-doctor rule that detects stale repository findings",
			files:       []string{"golang/cluster_doctor/cluster_doctor_server/rules/repository_findings.go"},
			wantPattern: "implementation_pattern:globular.pattern.doctor_rule_diagnostic_only",
			// Diagnostic-only: runtime observation, not mutation; evidence freshness.
			wantForbidden: []string{"doctor rule executing mutation inside Evaluate"},
			wantAction:    []string{"evidence freshness"},
		},
		{
			name:          "change repository publish installability",
			task:          "change repository publish workflow installability behavior",
			files:         []string{"golang/repository/repository_server/publish_workflow.go"},
			wantPattern:   "implementation_pattern:globular.pattern.repository_metadata_authority",
			wantForbidden: []string{"object presence in MinIO or CAS as installability authority"},
			// The artifact installability invariant is anchored to this file.
			wantAnchorIDs: []string{"repository.artifact.installable_compound_predicate"},
		},
		{
			name:        "modify workflow resume after failed step",
			task:        "modify workflow resume after a failed step",
			files:       []string{"golang/workflow/workflow_server/executor_resume.go", "golang/workflow/workflow_server/step_receipts.go"},
			wantPattern: "implementation_pattern:globular.pattern.workflow_durable_step_receipt",
			// The verify-before-reexecute (idempotent, bounded) intent is anchored here.
			wantAnchorIDs: []string{"workflow.resume_policy_verifies_before_reexecute"},
		},
		{
			name:          "change RBAC access validation",
			task:          "change RBAC access validation",
			files:         []string{"golang/rbac/rbac_server/rbac_access.go"},
			wantPattern:   "implementation_pattern:globular.pattern.rbac_explicit_deny_precedence",
			wantForbidden: []string{"explicit-deny check"},
			// The path-hierarchy resolution intent (deny checked first, flat) is anchored here.
			wantAnchorIDs: []string{"rbac.permission_resolution_walks_path_hierarchy"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			invalidateImplementationPatternCacheForTest()
			invalidateAuthorityDomainCacheForTest()
			s := newServer(newEmbeddedSeedStore())

			resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
				Task:  tc.task,
				Files: tc.files,
				Mode:  awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
			})
			if err != nil {
				t.Fatalf("Preflight: %v", err)
			}

			// Implementation pattern.
			if tc.wantPattern != "" && !hasPatternID(resp, tc.wantPattern) {
				t.Errorf("missing implementation pattern %q\n  got patterns: %v",
					tc.wantPattern, patternIDs(resp))
			}
			// Authority forbidden bypasses.
			for _, want := range tc.wantForbidden {
				if !anyContains(resp.GetForbiddenFixes(), want) {
					t.Errorf("forbidden_fixes missing %q\n  got: %v", want, resp.GetForbiddenFixes())
				}
			}
			// Authority required actions (owner / freshness).
			for _, want := range tc.wantAction {
				if !anyContains(resp.GetRequiredActions(), want) {
					t.Errorf("required_actions missing %q\n  got: %v", want, resp.GetRequiredActions())
				}
			}
			// Graph-anchored invariants/intents/failure modes.
			anchors := anchorIDs(resp)
			for _, want := range tc.wantAnchorIDs {
				if !anchors[want] {
					t.Errorf("missing graph-anchored node %q\n  got anchors: %v", want, keysOf(anchors))
				}
			}
		})
	}
}

func hasPatternID(resp *awarenesspb.PreflightResponse, id string) bool {
	for _, p := range resp.GetImplementationPatterns() {
		if p.GetId() == id {
			return true
		}
	}
	return false
}

func patternIDs(resp *awarenesspb.PreflightResponse) []string {
	var out []string
	for _, p := range resp.GetImplementationPatterns() {
		out = append(out, p.GetId())
	}
	return out
}

// anchorIDs collects the bare ids of every direct anchor (invariant, intent,
// failure mode) the Preflight response surfaced.
func anchorIDs(resp *awarenesspb.PreflightResponse) map[string]bool {
	out := map[string]bool{}
	for _, lst := range [][]*awarenesspb.KnowledgeNode{
		resp.GetDirectInvariants(),
		resp.GetDirectIntents(),
		resp.GetDirectFailureModes(),
	} {
		for _, n := range lst {
			if n.GetId() != "" {
				out[n.GetId()] = true
			}
		}
	}
	return out
}

func keysOf(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
