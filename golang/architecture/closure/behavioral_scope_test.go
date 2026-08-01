// SPDX-License-Identifier: AGPL-3.0-only

package closure

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/plane"
)

func fileScopedBehavioralClaim(id, file string) architecture.Claim {
	return architecture.Claim{
		ID:                 id,
		Statement:          architecture.ClaimStatement{Predicate: "asserts_rule"},
		Scope:              architecture.ClaimScope{Files: []string{file}},
		ArchitecturalPlane: architecture.PlaneObserved,
	}
}

func TestProjectApplicableBehavioral_NarrowExplainableBindingAndDeterministic(t *testing.T) {
	req := Scope{Files: []string{"a.go"}}

	applicable := fileScopedBehavioralClaim("claim.applicable", "a.go")
	unanchored := architecture.Claim{
		ID:                 "claim.unanchored",
		Statement:          architecture.ClaimStatement{Predicate: "asserts_rule"},
		ArchitecturalPlane: architecture.PlaneObserved,
	}
	nonCurrent := architecture.Claim{
		ID:                 "claim.historical",
		Statement:          architecture.ClaimStatement{Predicate: "asserts_rule"},
		Scope:              architecture.ClaimScope{Files: []string{"a.go"}},
		ArchitecturalPlane: architecture.PlaneHistorical,
	}

	scope := resolvedScope{Claims: []architecture.Claim{applicable, unanchored, nonCurrent}}
	facts := map[string]architecture.ClaimFactReceipt{}
	planes := map[string]plane.ClaimAssessment{}

	first := projectApplicableBehavioral(req, scope, facts, planes)
	second := projectApplicableBehavioral(req, scope, facts, planes)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected identical selection for identical (scope, facts, planes) inputs:\n%+v\nvs\n%+v", first, second)
	}

	if len(first.Applicable) != 1 || first.Applicable[0].ClaimID != applicable.ID {
		t.Fatalf("expected exactly the file-scoped claim to bind narrowly as applicable, got %+v", first.Applicable)
	}
	if len(first.Applicable[0].RelationPath) == 0 {
		t.Fatal("expected the applicable binding to carry a non-empty, explainable relation path")
	}

	if len(first.Background) != 2 {
		t.Fatalf("expected the unanchored and non-current claims to remain visible as background debt, not dropped or blocking, got %+v", first.Background)
	}
	byID := map[string]behavioralBinding{}
	for _, b := range first.Background {
		byID[b.ClaimID] = b
	}
	if got := byID[unanchored.ID].Explanation; got != "claim lacks an explainable task-to-behavior anchor" {
		t.Fatalf("expected unanchored claim explanation, got %q", got)
	}
	if got := byID[nonCurrent.ID].Explanation; got != "directional or historical claim does not satisfy current behavioral closure" {
		t.Fatalf("expected non-current-plane claim explanation, got %q", got)
	}
}

func TestProjectApplicableBehavioral_NoArbitraryCountLimit(t *testing.T) {
	req := Scope{Files: []string{"a.go"}}

	const claimCount = 250
	claims := make([]architecture.Claim, 0, claimCount)
	for i := 0; i < claimCount; i++ {
		claims = append(claims, fileScopedBehavioralClaim(fmt.Sprintf("claim.bulk.%03d", i), "a.go"))
	}
	scope := resolvedScope{Claims: claims}

	out := projectApplicableBehavioral(req, scope, map[string]architecture.ClaimFactReceipt{}, map[string]plane.ClaimAssessment{})
	if len(out.Applicable) != claimCount {
		t.Fatalf("expected every task-local claim to bind (no arbitrary count limit): got %d of %d", len(out.Applicable), claimCount)
	}
}
