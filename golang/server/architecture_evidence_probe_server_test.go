// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

const (
	testProbeID   = "probe.config.writer"
	testProbeFile = "docs/awareness/generated/probes.yaml"
)

func evidenceProbeIRI(id string) string {
	return mintTestIRI(rdf.ClassEvidenceProbe, id)
}

func evidenceProbeTriples() []store.Triple {
	return []store.Triple{
		{Predicate: rdf.PropType, Object: rdf.ClassEvidenceProbe, ObjectIsIRI: true},
		{Predicate: rdf.PropLabel, Object: "Config writer evidence probe"},
		{Predicate: rdf.PropStatus, Object: "proposed"},
		{Predicate: rdf.PropProbeForQuestion, Object: openQuestionIRI(testQuestionID), ObjectIsIRI: true},
		{Predicate: rdf.PropAddressesClosureBlocker, Object: "blocker.authority.config_writer"},
		{Predicate: rdf.PropTargetsClaim, Object: architectureClaimIRI(testClaimID), ObjectIsIRI: true},
		{Predicate: rdf.PropProducesEvidence, Object: evidenceIRI(testEvidenceID), ObjectIsIRI: true},
		{Predicate: rdf.PropProbeTemplateID, Object: "probe.existing_test_execution.v1"},
		{Predicate: rdf.PropProbeTemplateVersion, Object: "1"},
		{Predicate: rdf.PropProbeKind, Object: "test_execution"},
		{Predicate: rdf.PropHasEvidenceLane, Object: "test"},
		{Predicate: rdf.PropEvidenceRole, Object: "supporting"},
		{Predicate: rdf.PropProbeForTest, Object: mintTestIRI(rdf.ClassTest, "golang/server/config_test.go:TestConfigWriter"), ObjectIsIRI: true},
		{Predicate: rdf.PropSafetyClass, Object: "local_test"},
		{Predicate: rdf.PropRequiresApprovalGate, Object: "none"},
		{Predicate: rdf.PropAutomaticExecutionAllowed, Object: "false"},
		{Predicate: rdf.PropHasProbeStep, Object: "1 run_existing_test golang/server/config_test.go:TestConfigWriter"},
		{Predicate: rdf.PropExpectedArtifactKind, Object: "test_output"},
		{Predicate: rdf.PropSourceDialogueDigest, Object: strings.Repeat("a", 64)},
		{Predicate: rdf.PropSourceClaimDocumentDigest, Object: strings.Repeat("b", 64)},
		{Predicate: rdf.PropSourceClosureAssessmentDigest, Object: strings.Repeat("c", 64)},
		{Predicate: rdf.PropValidForCommit, Object: "0123456789abcdef"},
		{Predicate: rdf.PropValidForGraphDigest, Object: "abcdef0123456789"},
		{Predicate: rdf.PropAnchoredIn, Object: fileIRI(testProbeFile), ObjectIsIRI: true},
	}
}

func evidenceProbeClassFacts() []store.ImpactFact {
	iri := evidenceProbeIRI(testProbeID)
	facts := make([]store.ImpactFact, 0, len(evidenceProbeTriples()))
	for _, tr := range evidenceProbeTriples() {
		facts = append(facts, store.ImpactFact{
			NodeIRI:     iri,
			TypeIRI:     rdf.ClassEvidenceProbe,
			Predicate:   tr.Predicate,
			Object:      tr.Object,
			ObjectIsIRI: tr.ObjectIsIRI,
		})
	}
	return facts
}

func newEvidenceProbeTestServer() *server {
	return newServer(currentAuthorityStore(fakeStore{
		describe: func(_ context.Context, iri string) ([]store.Triple, error) {
			switch iri {
			case evidenceProbeIRI(testProbeID):
				return evidenceProbeTriples(), nil
			case architectureClaimIRI(testClaimID):
				return claimTriples(testClaimID), nil
			case openQuestionIRI(testQuestionID):
				return openQuestionTriples(), nil
			case evidenceIRI(testEvidenceID):
				return evidenceTriples(), nil
			default:
				return nil, nil
			}
		},
		classFacts: func(_ context.Context, classIRI string, _ int) ([]store.ImpactFact, error) {
			if classIRI == rdf.ClassEvidenceProbe {
				return evidenceProbeClassFacts(), nil
			}
			return nil, nil
		},
		impactForFile: func(_ context.Context, iri string) ([]store.ImpactFact, error) {
			if iri == fileIRI(testProbeFile) {
				return evidenceProbeClassFacts(), nil
			}
			return nil, nil
		},
		countTriples: func(context.Context) (int64, error) { return 22, nil },
		countByClass: func(_ context.Context, classIRI string) (int64, error) {
			if classIRI == rdf.ClassEvidenceProbe {
				return 1, nil
			}
			return 0, nil
		},
	}))
}

func TestQueryByClassEvidenceProbe(t *testing.T) {
	s := newEvidenceProbeTestServer()
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{Mode: awarenesspb.QueryMode_QUERY_MODE_BY_CLASS, Class: awarenesspb.QueryClass_QUERY_CLASS_EVIDENCE_PROBE})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(resp.GetRows()) != 1 || resp.GetRows()[0].GetId() != "evidence_probe:"+testProbeID || resp.GetRows()[0].GetStatus() != "proposed" {
		t.Fatalf("rows=%+v", resp.GetRows())
	}
}

func TestQueryByIDEvidenceProbe(t *testing.T) {
	s := newEvidenceProbeTestServer()
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{Mode: awarenesspb.QueryMode_QUERY_MODE_BY_ID, Id: "evidence_probe:" + testProbeID})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(resp.GetRows()) != 1 || resp.GetRows()[0].GetClass() != "evidence_probe" {
		t.Fatalf("rows=%+v", resp.GetRows())
	}
}

func TestResolveEvidenceProbeShowsPlanFacts(t *testing.T) {
	s := newEvidenceProbeTestServer()
	resp, err := s.Resolve(context.Background(), &awarenesspb.ResolveRequest{Class: "evidence_probe", Id: testProbeID})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !resp.GetFound() {
		t.Fatal("probe not found")
	}
	node := resp.GetNode()
	if node.GetStatus() != "proposed" {
		t.Fatalf("status=%q, want proposed", node.GetStatus())
	}
	if !hasNodeFact(node, "probe kind", "test_execution") || !hasNodeFact(node, "approval gate", "none") {
		t.Fatalf("facts=%+v", node.GetFacts())
	}
}

func TestRelatedEvidenceProbeShowsQuestionClaimAndEvidence(t *testing.T) {
	s := newEvidenceProbeTestServer()
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{Mode: awarenesspb.QueryMode_QUERY_MODE_RELATED, Id: "evidence_probe:" + testProbeID, Limit: 10})
	if err != nil {
		t.Fatalf("Query related: %v", err)
	}
	if !hasQueryRow(resp.GetRows(), "open_question:"+testQuestionID, "probeForQuestion") ||
		!hasQueryRow(resp.GetRows(), "architecture_claim:"+testClaimID, "targetsClaim") ||
		!hasQueryRow(resp.GetRows(), "evidence:"+testEvidenceID, "producesEvidence") {
		t.Fatalf("rows=%+v", resp.GetRows())
	}
}

func TestMetadataCountsEvidenceProbesSeparately(t *testing.T) {
	s := newEvidenceProbeTestServer()
	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if resp.GetEvidenceProbeCount() != 1 {
		t.Fatalf("evidence_probe_count=%d", resp.GetEvidenceProbeCount())
	}
}

func TestEvidenceProbeDoesNotAppearInImpactDirectArchitecture(t *testing.T) {
	s := newEvidenceProbeTestServer()
	resp, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{File: testProbeFile})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	for _, n := range resp.GetDirectArchitecture() {
		if n.GetClass() == "evidence_probe" {
			t.Fatalf("direct_architecture surfaced probe: %+v", n)
		}
	}
}

func TestEvidenceProbeDoesNotAppearInBriefing(t *testing.T) {
	s := newEvidenceProbeTestServer()
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: testProbeFile})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if strings.Contains(resp.GetProse(), testProbeID) || containsString(resp.GetReferencedIds(), "evidence_probe:"+testProbeID) {
		t.Fatalf("briefing surfaced probe: prose=%q ids=%v", resp.GetProse(), resp.GetReferencedIds())
	}
}

func TestEvidenceProbeDoesNotAppearInPreflight(t *testing.T) {
	s := newEvidenceProbeTestServer()
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{Task: "edit config", Files: []string{testProbeFile}})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	for _, n := range resp.GetDirectArchitecture() {
		if n.GetClass() == "evidence_probe" {
			t.Fatalf("preflight surfaced probe: %+v", n)
		}
	}
}
