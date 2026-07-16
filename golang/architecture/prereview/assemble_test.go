// SPDX-License-Identifier: Apache-2.0

package prereview

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeDiffSource struct {
	diff BoundDiff
	err  error
}

func (f fakeDiffSource) ResolveReviewDiff(context.Context, DiffRequest) (BoundDiff, error) {
	return f.diff, f.err
}

type fakeGraphSource struct {
	graph GraphContext
	err   error
}

func (f fakeGraphSource) CollectArchitecturalContext(context.Context, GraphRequest) (GraphContext, error) {
	return f.graph, f.err
}

func sampleDiff() BoundDiff {
	return BoundDiff{
		RepositoryDomain:     "github.com/example/project",
		BaseRevision:         "aaaa",
		BaseTreeDigestSHA256: "basetree00000000000000000000000000000000000000000000000000001111",
		HeadRevision:         "bbbb",
		HeadTreeDigestSHA256: "headtree00000000000000000000000000000000000000000000000000002222",
		DiffDigestSHA256:     "diff000000000000000000000000000000000000000000000000000000003333",
		FilesModified:        []string{"gin.go"},
		FilesCreated:         []string{"route.go"},
	}
}

func sampleGraph() GraphContext {
	return GraphContext{
		RiskClass:          "architecture_sensitive",
		AffectedComponents: []ImpactItem{{ID: "component.gateway", Epistemic: EpistemicGoverned, EvidenceRefs: []string{"graph:component.gateway"}}},
		Invariants:         []ProtectionItem{{ID: "inv.owner", Title: "Single owner", Severity: SeverityHigh, Status: "applicable", Epistemic: EpistemicGoverned, EvidenceRefs: []string{"graph:inv.owner"}}},
		ForbiddenFixes:     []ProtectionItem{{ID: "ff.cache", Title: "Do not cache the reload path", Severity: SeverityHigh, Status: "applicable", Epistemic: EpistemicGoverned, EvidenceRefs: []string{"graph:ff.cache"}}},
		ReviewerConcerns: []ReviewerAttentionItem{{
			ID: "concern.ff.cache", Category: AttentionArchitectQuestion,
			Question: "Does this change reintroduce the forbidden cached reload path?",
			Blocking: true, Severity: SeverityHigh, Epistemic: EpistemicGoverned,
			RelatedFiles: []string{"gin.go"}, ArchitecturalReach: 3,
		}},
		Available: []string{"graph_briefing", "edit_check"},
	}
}

func TestGenerateAdvisoryHappyPath(t *testing.T) {
	r, err := GenerateAdvisory(context.Background(), fakeDiffSource{diff: sampleDiff()}, fakeGraphSource{graph: sampleGraph()}, GenerateRequest{Purpose: "Add a route."})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if r.Coverage.Level != CoverageAdvisory {
		t.Fatalf("coverage = %q, want advisory", r.Coverage.Level)
	}
	if r.Binding.DiffDigestSHA256 != sampleDiff().DiffDigestSHA256 {
		t.Fatal("diff digest not bound")
	}
	// Advisory coverage never populates governance, proof, or result.
	if governanceIsPopulated(r.Governance) {
		t.Fatal("advisory report populated governance")
	}
	if r.Result.Available {
		t.Fatal("advisory report claimed a result architecture")
	}
	if r.Disposition != DispositionArchitectDecisionRequired {
		t.Fatalf("disposition = %q, want architect_decision_required (blocking concern)", r.Disposition)
	}
	if len(r.Protection.ForbiddenFixes) != 1 || len(r.Protection.Invariants) != 1 {
		t.Fatalf("protection not carried: %+v", r.Protection)
	}
	if r.ReviewerAttention[0].ID != "concern.ff.cache" {
		t.Fatalf("blocking concern not ranked first: %+v", r.ReviewerAttention)
	}
	if !containsSubstring(r.Coverage.Unavailable, "certification") {
		t.Fatalf("advisory coverage did not name certification unavailable: %v", r.Coverage.Unavailable)
	}
}

func TestGenerateAdvisoryDegradesWhenGraphFails(t *testing.T) {
	r, err := GenerateAdvisory(context.Background(), fakeDiffSource{diff: sampleDiff()}, fakeGraphSource{err: errors.New("graph offline")}, GenerateRequest{})
	if err != nil {
		t.Fatalf("non-strict generate should degrade, not fail: %v", err)
	}
	if !anyContains(r.Limitations, "graph context could not be collected") {
		t.Fatalf("degraded report did not name the gap: %v", r.Limitations)
	}
	if r.Disposition != DispositionReadyForHumanReview {
		t.Fatalf("disposition = %q, want ready_for_human_review", r.Disposition)
	}
}

func TestGenerateAdvisoryStrictFailsWhenGraphFails(t *testing.T) {
	_, err := GenerateAdvisory(context.Background(), fakeDiffSource{diff: sampleDiff()}, fakeGraphSource{err: errors.New("graph offline")}, GenerateRequest{Strict: true})
	if err == nil {
		t.Fatal("strict generate accepted an uncollectable graph")
	}
}

func TestGenerateAdvisoryIsDeterministic(t *testing.T) {
	a, err := GenerateAdvisory(context.Background(), fakeDiffSource{diff: sampleDiff()}, fakeGraphSource{graph: sampleGraph()}, GenerateRequest{Purpose: "Add a route."})
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateAdvisory(context.Background(), fakeDiffSource{diff: sampleDiff()}, fakeGraphSource{graph: sampleGraph()}, GenerateRequest{Purpose: "Add a route."})
	if err != nil {
		t.Fatal(err)
	}
	if a.ReportDigestSHA256 != b.ReportDigestSHA256 {
		t.Fatalf("advisory generation not deterministic: %q != %q", a.ReportDigestSHA256, b.ReportDigestSHA256)
	}
}

func TestGenerateAdvisoryPropagatesDiffError(t *testing.T) {
	_, err := GenerateAdvisory(context.Background(), fakeDiffSource{err: errors.New("not a git repo")}, fakeGraphSource{}, GenerateRequest{})
	if err == nil {
		t.Fatal("diff resolution error was swallowed")
	}
}

func containsSubstring(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func anyContains(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
