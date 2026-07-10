// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"github.com/globulario/awareness-graph/golang/rdf"
)

// Command-path contract: rebuilding from inside awareness-graph must preserve
// the paired services corpus in the embedded offline seed, or Preflight loses
// strong repair guidance for service-owned files.
func TestEmbeddedSeedRetainsCrossRepoRepositoryRepairAuthority(t *testing.T) {
	requireCombinedSeed(t)
	g := loadSeedGraph()

	for _, want := range []struct {
		classIRI string
		id       string
	}{
		{rdf.ClassAuthorityDomain, "authority.repository_artifact_metadata"},
		{rdf.ClassRepairPlan, "globular.repair.repository_artifact_lifecycle_stuck"},
		{rdf.ClassImplementationPattern, "globular.pattern.repository_metadata_authority"},
	} {
		iri := strings.Trim(rdf.MintIRI(want.classIRI, want.id), "<>")
		if _, ok := g.bySubject[iri]; !ok {
			t.Fatalf("embedded seed missing %s %q — seed likely collapsed to AWG-local-only subset", want.classIRI, want.id)
		}
	}

	invalidateImplementationPatternCacheForTest()
	invalidateAuthorityDomainCacheForTest()
	invalidateRepairPlanCacheForTest()
	invalidateRuntimeEvidenceCacheForTest()

	s := newServer(newEmbeddedSeedStore())
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "extend the awareness-graph preflight handler with an offline CLI mode",
		Files: []string{"golang/repository/repository_server/publish_workflow.go"},
		Mode:  awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if !anyContains(resp.GetRequiredActions(), "globular.repair.repository_artifact_lifecycle_stuck") {
		t.Fatalf("required_actions missing repository repair guidance; got %v", resp.GetRequiredActions())
	}
}
