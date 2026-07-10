// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/store"
)

// ─── helpers ─────────────────────────────────────────────────────────────

// mintTestIRI reuses the production minter and strips the angle brackets
// — the in-memory fact rows hold raw IRIs, not their N-Triples form.
func mintTestIRI(classIRI, id string) string {
	s := rdf.MintIRI(classIRI, id)
	return strings.TrimSuffix(strings.TrimPrefix(s, "<"), ">")
}

// invariantFact builds a one-row ImpactFact for an invariant with a given
// id + label + severity. Two rows are emitted (one per property) to mirror
// production behaviour where applyNodeFact runs over multiple facts to
// fill out a single KnowledgeNode.
func anchorFacts(class, id, label, severity string) []store.ImpactFact {
	iri := mintTestIRI(class, id)
	mk := func(p, o string) store.ImpactFact {
		return store.ImpactFact{NodeIRI: iri, TypeIRI: class, Predicate: p, Object: o}
	}
	return []store.ImpactFact{
		mk(rdf.PropLabel, label),
		mk(rdf.PropSeverity, severity),
	}
}

func invariantFacts(id, label, severity string) []store.ImpactFact {
	return anchorFacts(rdf.ClassInvariant, id, label, severity)
}

// newPreflightTestServer builds a server whose ImpactForFile returns the
// facts associated with the requested file IRI, and whose ClassFacts
// returns the canonical grpc_client_standard pattern. Tests pass nil for
// either map to opt out.
func newPreflightTestServer(
	t *testing.T,
	perFileFacts map[string][]store.ImpactFact,
	includePattern bool,
) *server {
	t.Helper()
	invalidateImplementationPatternCacheForTest()
	return newServer(fakeStore{
		impactForFile: func(_ context.Context, iri string) ([]store.ImpactFact, error) {
			return perFileFacts[iri], nil
		},
		classFacts: func(_ context.Context, classIRI string, _ int) ([]store.ImpactFact, error) {
			if includePattern && classIRI == rdf.ClassImplementationPattern {
				return grpcClientStandardFacts(), nil
			}
			return nil, nil
		},
	})
}

// fileIRI mirrors mintedIRI(ClassSourceFile, path) — used as the map key
// for perFileFacts.
func fileIRI(path string) string {
	return mintTestIRI(rdf.ClassSourceFile, path)
}

// ────────────────────────────────────────────────────────────────────────
// 1. Store unavailable → DEGRADED with UNKNOWN_IMPACT (adjustment #1)
// ────────────────────────────────────────────────────────────────────────

func TestPreflight_StoreUnavailableReturnsDegradedUnknownImpact(t *testing.T) {
	s := newServer(nil)
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "do a thing",
		Files: []string{"golang/node_agent/foo.go"},
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if resp.GetStatus() != awarenesspb.PreflightStatus_PREFLIGHT_STATUS_DEGRADED {
		t.Errorf("status: want DEGRADED, got %v", resp.GetStatus())
	}
	if resp.GetRiskClass() != awarenesspb.RiskClass_UNKNOWN_IMPACT {
		t.Errorf("risk: want UNKNOWN_IMPACT, got %v", resp.GetRiskClass())
	}
	if resp.GetConfidence() != awarenesspb.Confidence_CONFIDENCE_LOW {
		t.Errorf("confidence: want LOW, got %v", resp.GetConfidence())
	}
	if len(resp.GetBlindSpots()) == 0 {
		t.Errorf("blind_spots: want non-empty, got 0")
	}
	if resp.GetCoverage().GetSufficient() {
		t.Errorf("coverage.sufficient: want false in degraded response")
	}
	if len(resp.GetRequiredActions()) == 0 {
		t.Errorf("required_actions: want at least one retry hint, got 0")
	}
	if resp.GetAuthority() == nil {
		t.Fatal("authority should be populated on degraded response")
	}
	if resp.GetAuthority().GetAuthoritative() {
		t.Fatal("degraded preflight must not report authoritative=true")
	}
	if resp.GetAuthority().GetGraphFreshnessState() != awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CHECK_ERROR {
		t.Fatalf("authority freshness=%s, want CHECK_ERROR", resp.GetAuthority().GetGraphFreshnessState())
	}
}

// ────────────────────────────────────────────────────────────────────────
// 2. Task-only client task fires required_actions from pattern
// ────────────────────────────────────────────────────────────────────────

func TestPreflight_TaskOnlyClientTask_FiresPatternRequiredActions(t *testing.T) {
	s := newPreflightTestServer(t, nil, true)
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task: "create a new Go client for a Globular gRPC service",
		Mode: awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if len(resp.GetImplementationPatterns()) == 0 {
		t.Fatalf("expected ≥1 implementation pattern match, got 0")
	}
	assertCurrentAuthority(t, resp.GetAuthority())
	if resp.GetImplementationPatterns()[0].GetMatchStrength() != "strong" {
		t.Errorf("match strength: want strong, got %q", resp.GetImplementationPatterns()[0].GetMatchStrength())
	}
	// required_actions must include at least one "Call globular.X" line.
	foundCall := false
	for _, a := range resp.GetRequiredActions() {
		if strings.Contains(a, "globular.InitClient") {
			foundCall = true
			break
		}
	}
	if !foundCall {
		t.Errorf("required_actions did not surface globular.InitClient from pattern; got %v", resp.GetRequiredActions())
	}
	// files_to_read must include the canonical reference file.
	foundRef := false
	for _, f := range resp.GetFilesToRead() {
		if strings.Contains(f, "echo_client.go") {
			foundRef = true
			break
		}
	}
	if !foundRef {
		t.Errorf("files_to_read missing canonical reference; got %v", resp.GetFilesToRead())
	}
}

// ────────────────────────────────────────────────────────────────────────
// 3. File with security invariant → SECURITY_RISK
// ────────────────────────────────────────────────────────────────────────

func TestPreflight_FileWithSecurityInvariants_RiskClassSecurity(t *testing.T) {
	file := "golang/rbac/rbac_server/permissions.go"
	facts := invariantFacts("security.rbac.deny_overrides_allow", "RBAC deny overrides allow", "high")
	s := newPreflightTestServer(t, map[string][]store.ImpactFact{
		fileIRI(file): facts,
	}, false)
	resp, _ := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "tweak the rbac matcher",
		Files: []string{file},
	})
	if resp.GetRiskClass() != awarenesspb.RiskClass_SECURITY_RISK {
		t.Errorf("risk: want SECURITY_RISK, got %v", resp.GetRiskClass())
	}
	if len(resp.GetDirectInvariants()) == 0 {
		t.Errorf("expected direct_invariants populated")
	}
}

// TestPreflight_FileAnchoringArchitecture_SurfacesDirectArchitecture verifies
// the spine/pattern nodes governing a touched file flow through Preflight's
// direct_architecture (reusing collectImpact's bucket), and that they are
// deduped across repeated anchors.
func TestPreflight_FileAnchoringArchitecture_SurfacesDirectArchitecture(t *testing.T) {
	file := "golang/server/preflight.go"
	facts := append(
		anchorFacts(rdf.ClassComponent, "server.preflight", "Preflight handler", "info"),
		anchorFacts(rdf.ClassBoundary, "domain_scope", "Domain scope boundary", "info")...,
	)
	// Duplicate the component anchor to exercise dedupNodesByID.
	facts = append(facts, anchorFacts(rdf.ClassComponent, "server.preflight", "Preflight handler", "info")...)
	s := newPreflightTestServer(t, map[string][]store.ImpactFact{
		fileIRI(file): facts,
	}, false)
	resp, _ := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "edit the preflight handler",
		Files: []string{file},
	})
	arch := resp.GetDirectArchitecture()
	if len(arch) != 2 {
		t.Fatalf("expected 2 deduped architecture nodes (component+boundary), got %d", len(arch))
	}
	classes := map[string]bool{}
	for _, n := range arch {
		classes[n.GetClass()] = true
	}
	if !classes["component"] || !classes["boundary"] {
		t.Errorf("expected component + boundary in direct_architecture, got %v", classes)
	}
}

// ────────────────────────────────────────────────────────────────────────
// 4. File with convergence invariant → CONVERGENCE_RISK
// ────────────────────────────────────────────────────────────────────────

func TestPreflight_FileWithConvergenceInvariants_RiskClassConvergence(t *testing.T) {
	file := "golang/cluster_controller/reconciler.go"
	facts := invariantFacts("convergence.installed_state_drift", "installed vs desired drift", "high")
	s := newPreflightTestServer(t, map[string][]store.ImpactFact{
		fileIRI(file): facts,
	}, false)
	resp, _ := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "reconciler tweak",
		Files: []string{file},
	})
	if resp.GetRiskClass() != awarenesspb.RiskClass_CONVERGENCE_RISK {
		t.Errorf("risk: want CONVERGENCE_RISK, got %v (reasons=%v)", resp.GetRiskClass(), resp.GetBlindSpots())
	}
}

// ────────────────────────────────────────────────────────────────────────
// 5. File with data-loss keyword → DATA_LOSS_RISK
// ────────────────────────────────────────────────────────────────────────

func TestPreflight_FileWithDataLossKeyword_RiskClassDataLoss(t *testing.T) {
	file := "golang/node_agent/node_agent_server/scylla_install.go"
	facts := invariantFacts("scylla.format_json_rewrite", "blob missing after reformat", "high")
	s := newPreflightTestServer(t, map[string][]store.ImpactFact{
		fileIRI(file): facts,
	}, false)
	resp, _ := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "scylla install tweak",
		Files: []string{file},
	})
	if resp.GetRiskClass() != awarenesspb.RiskClass_DATA_LOSS_RISK {
		t.Errorf("risk: want DATA_LOSS_RISK, got %v (reasons=%v)", resp.GetRiskClass(), resp.GetBlindSpots())
	}
}

// ────────────────────────────────────────────────────────────────────────
// 6. Unrelated task, no coverage → UNKNOWN_IMPACT
// ────────────────────────────────────────────────────────────────────────

func TestPreflight_UnrelatedTaskNoCoverage_RiskClassUnknownImpact(t *testing.T) {
	s := newPreflightTestServer(t, nil, true) // pattern store loaded, but task won't match
	resp, _ := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task: "rename the lobster",
	})
	if resp.GetRiskClass() != awarenesspb.RiskClass_UNKNOWN_IMPACT {
		t.Errorf("risk: want UNKNOWN_IMPACT, got %v", resp.GetRiskClass())
	}
	if resp.GetCoverage().GetSufficient() {
		t.Errorf("coverage.sufficient should be false for task with no anchors and no pattern")
	}
}

// ────────────────────────────────────────────────────────────────────────
// 7. Strong pattern match on clean path → LOW_RISK
// ────────────────────────────────────────────────────────────────────────

func TestPreflight_UnrelatedTaskGoodCoverage_LowRisk(t *testing.T) {
	s := newPreflightTestServer(t, nil, true)
	resp, _ := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		// matches "wrapping a protobuf client with Globular helpers"
		Task: "wrapping a protobuf client with Globular helpers",
	})
	if resp.GetRiskClass() != awarenesspb.RiskClass_LOW_RISK {
		t.Errorf("risk: want LOW_RISK (strong pattern + clean path), got %v (patterns=%d strength=%q)",
			resp.GetRiskClass(),
			len(resp.GetImplementationPatterns()),
			func() string {
				if len(resp.GetImplementationPatterns()) > 0 {
					return resp.GetImplementationPatterns()[0].GetMatchStrength()
				}
				return ""
			}())
	}
}

// ────────────────────────────────────────────────────────────────────────
// 8. Confidence tier scales with anchor count
// ────────────────────────────────────────────────────────────────────────

func TestPreflight_ConfidenceTieredByAnchorCount(t *testing.T) {
	file := "golang/echo/echo_server/server.go"
	// Three distinct invariants on the same file → ≥3 anchors → HIGH.
	threeFacts := append(append(
		invariantFacts("benign.one", "one", "info"),
		invariantFacts("benign.two", "two", "info")...),
		invariantFacts("benign.three", "three", "info")...)
	s := newPreflightTestServer(t, map[string][]store.ImpactFact{
		fileIRI(file): threeFacts,
	}, false)
	resp, _ := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "tweak echo server",
		Files: []string{file},
	})
	if got := resp.GetConfidence(); got != awarenesspb.Confidence_CONFIDENCE_HIGH {
		t.Errorf("3-anchor file: want HIGH, got %v (anchors=%d)", got, len(resp.GetDirectInvariants()))
	}

	// Single-anchor file → MEDIUM.
	oneFile := "golang/echo/echo_server/single.go"
	s = newPreflightTestServer(t, map[string][]store.ImpactFact{
		fileIRI(oneFile): invariantFacts("benign.solo", "solo", "info"),
	}, false)
	resp, _ = s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "tweak echo server",
		Files: []string{oneFile},
	})
	if got := resp.GetConfidence(); got != awarenesspb.Confidence_CONFIDENCE_MEDIUM {
		t.Errorf("1-anchor file: want MEDIUM, got %v", got)
	}

	// No anchors, no pattern → LOW.
	s = newPreflightTestServer(t, nil, false)
	resp, _ = s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task: "say hello",
	})
	if got := resp.GetConfidence(); got != awarenesspb.Confidence_CONFIDENCE_LOW {
		t.Errorf("empty inputs: want LOW, got %v", got)
	}
}

// ────────────────────────────────────────────────────────────────────────
// 9. Standard mode returns more entries than compact
// ────────────────────────────────────────────────────────────────────────

func TestPreflight_StandardModeHasMoreEntriesThanCompact(t *testing.T) {
	file := "golang/echo/echo_server/server.go"
	var many []store.ImpactFact
	for i := 0; i < 8; i++ {
		many = append(many, invariantFacts(
			"benign.bulk."+string(rune('a'+i)),
			"bulk "+string(rune('a'+i)),
			"info",
		)...)
	}
	build := func(mode awarenesspb.PreflightMode) *awarenesspb.PreflightResponse {
		s := newPreflightTestServer(t, map[string][]store.ImpactFact{
			fileIRI(file): many,
		}, false)
		resp, _ := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
			Files: []string{file},
			Mode:  mode,
		})
		return resp
	}
	compact := build(awarenesspb.PreflightMode_PREFLIGHT_COMPACT)
	standard := build(awarenesspb.PreflightMode_PREFLIGHT_STANDARD)
	if len(standard.GetDirectInvariants()) <= len(compact.GetDirectInvariants()) {
		t.Errorf("standard should carry more invariants than compact; standard=%d compact=%d",
			len(standard.GetDirectInvariants()), len(compact.GetDirectInvariants()))
	}
}

// ────────────────────────────────────────────────────────────────────────
// 10. blind_spots explicitly listed when risk is UNKNOWN_IMPACT
// ────────────────────────────────────────────────────────────────────────

func TestPreflight_BlindSpotsListedExplicitly(t *testing.T) {
	s := newPreflightTestServer(t, nil, false)
	resp, _ := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task: "do something obscure with rare words",
	})
	if resp.GetRiskClass() != awarenesspb.RiskClass_UNKNOWN_IMPACT {
		t.Fatalf("precondition: want UNKNOWN_IMPACT, got %v", resp.GetRiskClass())
	}
	if len(resp.GetBlindSpots()) == 0 {
		t.Errorf("blind_spots must be non-empty when risk is UNKNOWN_IMPACT")
	}
}

// ────────────────────────────────────────────────────────────────────────
// 11. forbidden_fixes surfaced from pattern's forbidden_calls
// ────────────────────────────────────────────────────────────────────────

func TestPreflight_ForbiddenFixesSurfaceFromPattern(t *testing.T) {
	s := newPreflightTestServer(t, nil, true)
	resp, _ := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task: "create a new Go client for a Globular gRPC service",
		Mode: awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
	})
	found := false
	for _, f := range resp.GetForbiddenFixes() {
		if strings.Contains(f, "grpc.Dial") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("forbidden_fixes did not surface pattern's forbidden_calls; got %v", resp.GetForbiddenFixes())
	}
}

// ────────────────────────────────────────────────────────────────────────
// 12. Task-only pattern that doesn't score strong → UNKNOWN_IMPACT
//     (adjustment #2: pattern alone must not certify low-risk)
// ────────────────────────────────────────────────────────────────────────

func TestPreflight_TaskOnlyPatternWeakCoverageUnknownImpact(t *testing.T) {
	s := newPreflightTestServer(t, nil, true)
	// "grpc client service" → ≤3 keyword overlap → medium tier (or narrow,
	// but narrow requires a file). Medium tier alone for task-only requests
	// must NOT flip coverage.sufficient to true — adjustment #2.
	resp, _ := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task: "grpc client service",
	})
	// We expect either no strong-tier match (so risk goes UNKNOWN_IMPACT),
	// or the matcher returns medium and our coverage gate still rejects it.
	if resp.GetCoverage().GetSufficient() {
		t.Errorf("task-only medium/narrow pattern must not make coverage sufficient; matches=%d",
			len(resp.GetImplementationPatterns()))
	}
	if resp.GetRiskClass() != awarenesspb.RiskClass_UNKNOWN_IMPACT {
		t.Errorf("risk: want UNKNOWN_IMPACT, got %v (patterns=%d)",
			resp.GetRiskClass(), len(resp.GetImplementationPatterns()))
	}
}

// ────────────────────────────────────────────────────────────────────────
// Phase 5 — honest-DEGRADED for high-risk path + zero anchors
// ────────────────────────────────────────────────────────────────────────

// TestPreflight_HighRiskNoAnchorsReturnsDegraded covers the Phase 5 ask:
// a file under a high-risk directory with zero anchors must return
// PREFLIGHT_STATUS_DEGRADED (not OK with LOW_RISK), Confidence=LOW, and
// required_actions naming the "read source directly + add candidate
// annotations" guidance.
//
// Mirrors the real-world case where rules/heal_policy.go was patched 3×
// in Patch C while returning EMPTY/UNKNOWN_IMPACT under v0.0.10.
func TestPreflight_HighRiskNoAnchorsReturnsDegraded(t *testing.T) {
	s := newPreflightTestServer(t, nil, false)
	resp, _ := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "tweak the auto-heal policy table",
		Files: []string{"golang/cluster_doctor/cluster_doctor_server/rules/heal_policy.go"},
	})
	if resp.GetStatus() != awarenesspb.PreflightStatus_PREFLIGHT_STATUS_DEGRADED {
		t.Fatalf("status: want DEGRADED, got %v (risk=%v)", resp.GetStatus(), resp.GetRiskClass())
	}
	if resp.GetRiskClass() != awarenesspb.RiskClass_UNKNOWN_IMPACT {
		t.Fatalf("risk_class: want UNKNOWN_IMPACT, got %v", resp.GetRiskClass())
	}
	if resp.GetConfidence() != awarenesspb.Confidence_CONFIDENCE_LOW {
		t.Fatalf("confidence: want LOW (clamped), got %v", resp.GetConfidence())
	}
	// Required actions must surface the honest-DEGRADED guidance first.
	if len(resp.GetRequiredActions()) == 0 {
		t.Fatalf("required_actions must not be empty")
	}
	if !strings.Contains(resp.GetRequiredActions()[0], "Read the source file directly") {
		t.Fatalf("first required_action must name read-source guidance; got %q",
			resp.GetRequiredActions()[0])
	}
	foundCandidatesHint := false
	for _, a := range resp.GetRequiredActions() {
		if strings.Contains(a, "docs/awareness/candidates/") {
			foundCandidatesHint = true
			break
		}
	}
	if !foundCandidatesHint {
		t.Fatalf("required_actions must point at docs/awareness/candidates/; got %v",
			resp.GetRequiredActions())
	}
	// blind_spots must include the explicit "NOT proof of safety" line.
	foundNotProof := false
	for _, b := range resp.GetBlindSpots() {
		if strings.Contains(b, "NOT proof of safety") {
			foundNotProof = true
			break
		}
	}
	if !foundNotProof {
		t.Fatalf("blind_spots must include \"NOT proof of safety\"; got %v", resp.GetBlindSpots())
	}
}

// TestPreflight_HighRiskWithAnchorsDoesNotDegrade verifies the gate is
// path-conditional, not blanket. A high-risk file WITH any direct anchor
// returns normal OK status — the honest-DEGRADED branch only fires when
// the graph has literally nothing to say.
func TestPreflight_HighRiskWithAnchorsDoesNotDegrade(t *testing.T) {
	file := "golang/cluster_doctor/cluster_doctor_server/rules/heal_policy.go"
	facts := invariantFacts("benign.documented", "documented invariant", "info")
	s := newPreflightTestServer(t, map[string][]store.ImpactFact{
		fileIRI(file): facts,
	}, false)
	resp, _ := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Files: []string{file},
	})
	if resp.GetStatus() == awarenesspb.PreflightStatus_PREFLIGHT_STATUS_DEGRADED {
		t.Fatalf("DEGRADED gate fired despite anchors present; status=%v risk=%v",
			resp.GetStatus(), resp.GetRiskClass())
	}
}

// TestPreflight_CleanPathNoAnchorsRemainsOk verifies non-high-risk
// files with no anchors do NOT trigger DEGRADED — the rule is strictly
// scoped to the CLAUDE.md R2 directory list.
func TestPreflight_CleanPathNoAnchorsRemainsOk(t *testing.T) {
	s := newPreflightTestServer(t, nil, false)
	resp, _ := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Files: []string{"golang/echo/echo_server/server.go"},
	})
	if resp.GetStatus() == awarenesspb.PreflightStatus_PREFLIGHT_STATUS_DEGRADED {
		t.Fatalf("clean-path no-anchor file must NOT degrade; got DEGRADED")
	}
}
