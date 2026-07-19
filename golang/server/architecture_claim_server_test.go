// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store"
)

const (
	testClaimID      = "claim.config.writer"
	testDependencyID = "claim.config.guard"
	testEvidenceID   = "evidence.config.writer"
	testClaimFile    = "golang/server/config.go"
)

func currentAuthorityStore(st fakeStore) fakeStore {
	st.graphFreshness = func(context.Context) seedmeta.Verification {
		return seedmeta.Verification{
			State: seedmeta.FreshnessCurrent,
			Expected: seedmeta.Marker{
				Digest:      "abc123",
				IRI:         "https://globular.io/awareness#seedBuild/sha256-abc123",
				TripleCount: 7,
			},
			Live: seedmeta.Marker{
				Digest:      "abc123",
				IRI:         "https://globular.io/awareness#seedBuild/sha256-abc123",
				TripleCount: 7,
			},
			Detail: "current",
		}
	}
	return st
}

func architectureClaimIRI(id string) string {
	return mintTestIRI(rdf.ClassArchitectureClaim, id)
}

func evidenceIRI(id string) string {
	return mintTestIRI(rdf.ClassEvidence, id)
}

func claimTriples(id string) []store.Triple {
	return []store.Triple{
		{Predicate: rdf.PropType, Object: rdf.ClassArchitectureClaim, ObjectIsIRI: true},
		{Predicate: rdf.PropLabel, Object: "Config writer claim"},
		{Predicate: rdf.PropClaimSubject, Object: "component.config"},
		{Predicate: rdf.PropClaimPredicate, Object: "controls_lifecycle"},
		{Predicate: rdf.PropClaimObject, Object: "config_state"},
		{Predicate: rdf.PropArchitecturalPlane, Object: "observed"},
		{Predicate: rdf.PropAssertionOrigin, Object: "derived"},
		{Predicate: rdf.PropEpistemicStatus, Object: "supported"},
		{Predicate: rdf.PropDerivedFromFact, Object: "fact:mutates_state"},
		{Predicate: rdf.PropSupportedByEvidence, Object: evidenceIRI(testEvidenceID), ObjectIsIRI: true},
		{Predicate: rdf.PropDependsOnClaim, Object: architectureClaimIRI(testDependencyID), ObjectIsIRI: true},
		{Predicate: rdf.PropHasInvalidationCondition, Object: "writer stops mutating config"},
		{Predicate: rdf.PropConfidenceScore, Object: "0.82"},
		{Predicate: rdf.PropPromotionStatus, Object: "candidate"},
		{Predicate: rdf.PropHumanReviewRequired, Object: "true"},
		{Predicate: rdf.PropAnchoredIn, Object: mintTestIRI(rdf.ClassSourceFile, testClaimFile), ObjectIsIRI: true},
	}
}

func evidenceTriples() []store.Triple {
	return []store.Triple{
		{Predicate: rdf.PropType, Object: rdf.ClassEvidence, ObjectIsIRI: true},
		{Predicate: rdf.PropLabel, Object: "Config writer evidence"},
		{Predicate: rdf.PropAuthoredIn, Object: "docs/awareness/generated/config.yaml"},
	}
}

func claimClassFacts() []store.ImpactFact {
	facts := make([]store.ImpactFact, 0, len(claimTriples(testClaimID)))
	iri := architectureClaimIRI(testClaimID)
	for _, tr := range claimTriples(testClaimID) {
		facts = append(facts, store.ImpactFact{
			NodeIRI:     iri,
			TypeIRI:     rdf.ClassArchitectureClaim,
			Predicate:   tr.Predicate,
			Object:      tr.Object,
			ObjectIsIRI: tr.ObjectIsIRI,
		})
	}
	return facts
}

func claimImpactFacts() []store.ImpactFact {
	facts := claimClassFacts()
	for i := range facts {
		facts[i].TypeIRI = rdf.ClassArchitectureClaim
	}
	return facts
}

func newArchitectureClaimTestServer() *server {
	return newServer(currentAuthorityStore(fakeStore{
		describe: func(_ context.Context, iri string) ([]store.Triple, error) {
			switch iri {
			case architectureClaimIRI(testClaimID):
				return claimTriples(testClaimID), nil
			case architectureClaimIRI(testDependencyID):
				return []store.Triple{
					{Predicate: rdf.PropType, Object: rdf.ClassArchitectureClaim, ObjectIsIRI: true},
					{Predicate: rdf.PropLabel, Object: "Config guard claim"},
					{Predicate: rdf.PropEpistemicStatus, Object: "unknown"},
				}, nil
			case evidenceIRI(testEvidenceID):
				return evidenceTriples(), nil
			default:
				return nil, nil
			}
		},
		describeInbound: func(_ context.Context, iri string) ([]store.InboundTriple, error) {
			if iri == architectureClaimIRI(testClaimID) {
				return []store.InboundTriple{
					{Subject: architectureClaimIRI(testDependencyID), Predicate: rdf.PropDependsOnClaim},
				}, nil
			}
			return nil, nil
		},
		classFacts: func(_ context.Context, classIRI string, _ int) ([]store.ImpactFact, error) {
			if classIRI == rdf.ClassArchitectureClaim {
				return claimClassFacts(), nil
			}
			return nil, nil
		},
		impactForFile: func(_ context.Context, iri string) ([]store.ImpactFact, error) {
			if iri == fileIRI(testClaimFile) {
				return claimImpactFacts(), nil
			}
			return nil, nil
		},
		countTriples: func(context.Context) (int64, error) {
			return 7, nil
		},
		countByClass: func(_ context.Context, classIRI string) (int64, error) {
			if classIRI == rdf.ClassArchitectureClaim {
				return 2, nil
			}
			return 0, nil
		},
	}))
}

func TestQueryByClassArchitectureClaim(t *testing.T) {
	s := newArchitectureClaimTestServer()
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{
		Mode:  awarenesspb.QueryMode_QUERY_MODE_BY_CLASS,
		Class: awarenesspb.QueryClass_QUERY_CLASS_ARCHITECTURE_CLAIM,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(resp.GetRows()) != 1 {
		t.Fatalf("rows=%d, want 1", len(resp.GetRows()))
	}
	row := resp.GetRows()[0]
	if row.GetId() != "architecture_claim:"+testClaimID || row.GetStatus() != "supported" {
		t.Fatalf("row=%+v", row)
	}
}

func TestQueryByIDArchitectureClaim(t *testing.T) {
	s := newArchitectureClaimTestServer()
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{
		Mode: awarenesspb.QueryMode_QUERY_MODE_BY_ID,
		Id:   "architecture_claim:" + testClaimID,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(resp.GetRows()) != 1 || resp.GetRows()[0].GetClass() != "architecture_claim" {
		t.Fatalf("rows=%+v", resp.GetRows())
	}
}

func TestResolveArchitectureClaimShowsProofFacts(t *testing.T) {
	s := newArchitectureClaimTestServer()
	resp, err := s.Resolve(context.Background(), &awarenesspb.ResolveRequest{
		Class: "architecture_claim",
		Id:    testClaimID,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !resp.GetFound() {
		t.Fatal("claim not found")
	}
	node := resp.GetNode()
	if node.GetStatus() != "supported" {
		t.Fatalf("status=%q, want supported", node.GetStatus())
	}
	if !hasNodeFact(node, "premise fact", "fact:mutates_state") {
		t.Fatalf("facts=%+v, want premise fact", node.GetFacts())
	}
	if !hasNodeFact(node, "human review required", "true") {
		t.Fatalf("facts=%+v, want human review required", node.GetFacts())
	}
}

func TestRelatedArchitectureClaimShowsEvidenceAndDependencies(t *testing.T) {
	s := newArchitectureClaimTestServer()
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{
		Mode:  awarenesspb.QueryMode_QUERY_MODE_RELATED,
		Id:    "architecture_claim:" + testClaimID,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Query related: %v", err)
	}
	if !hasQueryRow(resp.GetRows(), "evidence:"+testEvidenceID, "supportedByEvidence") {
		t.Fatalf("rows=%+v, want supporting evidence", resp.GetRows())
	}
	if !hasQueryRow(resp.GetRows(), "architecture_claim:"+testDependencyID, "dependsOnClaim") {
		t.Fatalf("rows=%+v, want dependent claim", resp.GetRows())
	}
}

func TestMetadataCountsArchitectureClaimsSeparately(t *testing.T) {
	s := newArchitectureClaimTestServer()
	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if resp.GetArchitectureClaimCount() != 2 {
		t.Fatalf("architecture_claim_count=%d, want 2", resp.GetArchitectureClaimCount())
	}
}

func TestArchitectureClaimDoesNotAffectCoverageState(t *testing.T) {
	s := newArchitectureClaimTestServer()
	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if resp.GetCoverageState() != awarenesspb.CoverageState_COVERAGE_STATE_THIN {
		t.Fatalf("coverage_state=%s, want THIN", resp.GetCoverageState())
	}
}

func TestArchitectureClaimDoesNotAppearInImpactDirectArchitecture(t *testing.T) {
	s := newArchitectureClaimTestServer()
	resp, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{File: testClaimFile})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if len(resp.GetDirectArchitecture()) != 0 {
		t.Fatalf("direct_architecture=%+v, want empty", resp.GetDirectArchitecture())
	}
}

func TestArchitectureClaimDoesNotAppearInBriefing(t *testing.T) {
	s := newArchitectureClaimTestServer()
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: testClaimFile})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if strings.Contains(resp.GetProse(), testClaimID) || containsString(resp.GetReferencedIds(), "architecture_claim:"+testClaimID) {
		t.Fatalf("briefing surfaced claim: prose=%q ids=%v", resp.GetProse(), resp.GetReferencedIds())
	}
}

func TestArchitectureClaimDoesNotAppearInPreflight(t *testing.T) {
	s := newArchitectureClaimTestServer()
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "edit config writer",
		Files: []string{testClaimFile},
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	for _, n := range resp.GetDirectArchitecture() {
		if n.GetClass() == "architecture_claim" {
			t.Fatalf("preflight surfaced claim in direct architecture: %+v", n)
		}
	}
}

func TestArchitectureClaimDoesNotAffectRiskClassification(t *testing.T) {
	s := newArchitectureClaimTestServer()
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "edit config writer",
		Files: []string{testClaimFile},
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if resp.GetRiskClass() != awarenesspb.RiskClass_UNKNOWN_IMPACT {
		t.Fatalf("risk_class=%s, want UNKNOWN_IMPACT", resp.GetRiskClass())
	}
}

func hasNodeFact(node *awarenesspb.KnowledgeNode, name, value string) bool {
	for _, f := range node.GetFacts() {
		if f.GetPredicate() == name && f.GetValue() == value {
			return true
		}
	}
	return false
}

func hasQueryRow(rows []*awarenesspb.QueryRow, id, relation string) bool {
	for _, row := range rows {
		if row.GetId() == id && row.GetRelation() == relation {
			return true
		}
	}
	return false
}
