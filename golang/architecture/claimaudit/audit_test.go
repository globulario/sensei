// SPDX-License-Identifier: AGPL-3.0-only

package claimaudit_test

import (
	"reflect"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/claimaudit"
	"gopkg.in/yaml.v3"
)

func auditClaim(id, rule, predicate, object, file string) architecture.Claim {
	return architecture.Claim{
		ID: id, Statement: architecture.ClaimStatement{Subject: "gin.Engine", Predicate: predicate, Object: object},
		Scope:              architecture.ClaimScope{Repository: "github.com/gin-gonic/gin", Files: []string{file}, Components: []string{"component.gin"}},
		ArchitecturalPlane: architecture.PlaneObserved, AssertionOrigin: architecture.OriginDerived,
		EpistemicStatus: architecture.StatusSupported, InferenceRule: rule, PremiseFacts: []string{"fact." + id},
		Unknowns: []string{"whether public compatibility is intended"}, AlternativeExplanations: []string{"local behavior only"},
		Confidence: .8, HumanReviewRequired: true, PromotionStatus: architecture.PromotionCandidate,
	}
}

func auditDoc(claims ...architecture.Claim) architecture.ClaimDocument {
	doc := architecture.ClaimDocument{Claims: claims}
	for _, claim := range claims {
		source := ""
		if len(claim.Scope.Files) > 0 {
			source = claim.Scope.Files[0]
		}
		doc.FactReceipts = append(doc.FactReceipts, architecture.ClaimFactReceipt{Fact: architecture.Fact{
			ID: claim.PremiseFacts[0], Evidence: architecture.Evidence{SourceFile: source},
		}})
	}
	return doc
}

func TestClaimAuditCountsByRuleAndPredicate(t *testing.T) {
	report := claimaudit.Build(auditDoc(
		auditClaim("a", "rule.a", "calls", "one", "gin.go"), auditClaim("b", "rule.a", "writes", "two", "tree.go"),
	), claimaudit.Options{RootComponentID: "component.gin", CoreFiles: []string{"gin.go", "tree.go"}})
	if report.TotalClaims != 2 || len(report.ClaimsByInferenceRule) != 1 || report.ClaimsByInferenceRule[0].Count != 2 || len(report.ClaimsByPredicate) != 2 {
		t.Fatalf("report=%+v", report)
	}
}

func TestClaimAuditCountsDistinctPropositions(t *testing.T) {
	report := claimaudit.Build(auditDoc(
		auditClaim("a", "rule.a", "calls", "one", "gin.go"), auditClaim("b", "rule.a", "calls", "two", "gin.go"),
	), claimaudit.Options{})
	if report.DistinctPropositionKeys != 2 {
		t.Fatalf("distinct=%d", report.DistinctPropositionKeys)
	}
}

func TestClaimAuditDetectsDuplicatePropositionInflation(t *testing.T) {
	a := auditClaim("a", "rule.a", "calls", "one", "gin.go")
	b := auditClaim("b", "rule.b", "calls", "one", "tree.go")
	report := claimaudit.Build(auditDoc(a, b), claimaudit.Options{})
	if len(report.DuplicatePropositionGroups) != 1 || report.LargestClaimGroup != 2 {
		t.Fatalf("report=%+v", report)
	}
}

func TestClaimAuditReportsRootCoreCoverage(t *testing.T) {
	report := claimaudit.Build(auditDoc(auditClaim("a", "rule.a", "calls", "one", "gin.go")), claimaudit.Options{
		RootComponentID: "component.gin", CoreFiles: []string{"gin.go", "tree.go"},
	})
	if report.ClaimsAnchoredToRootComponent != 1 || report.ClaimsAnchoredToCoreFiles[0].Count != 1 || report.ClaimsAnchoredToCoreFiles[1].Count != 0 {
		t.Fatalf("report=%+v", report)
	}
}

func TestClaimAuditReportsUnanchoredClaims(t *testing.T) {
	claim := auditClaim("a", "rule.a", "calls", "one", "gin.go")
	claim.Scope.Files, claim.Scope.Components = nil, nil
	report := claimaudit.Build(auditDoc(claim), claimaudit.Options{})
	if !reflect.DeepEqual(report.UnanchoredClaims, []string{"a"}) {
		t.Fatalf("unanchored=%v", report.UnanchoredClaims)
	}
}

func TestClaimAuditHasNoCompositeScore(t *testing.T) {
	raw, err := yaml.Marshal(claimaudit.Build(auditDoc(), claimaudit.Options{}))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, exists := doc["score"]; exists {
		t.Fatalf("claim audit must not emit a composite score:\n%s", raw)
	}
}
