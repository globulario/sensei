// SPDX-License-Identifier: AGPL-3.0-only

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
	testQuestionID   = "question.config_writer"
	testAnswerID     = "answer.config_writer"
	testDialogueFile = "golang/server/config_dialogue.go"
)

func openQuestionIRI(id string) string {
	return mintTestIRI(rdf.ClassOpenQuestion, id)
}

func architectAnswerIRI(id string) string {
	return mintTestIRI(rdf.ClassArchitectAnswer, id)
}

func openQuestionTriples() []store.Triple {
	return []store.Triple{
		{Predicate: rdf.PropType, Object: rdf.ClassOpenQuestion, ObjectIsIRI: true},
		{Predicate: rdf.PropLabel, Object: "Config writer question"},
		{Predicate: rdf.PropQuestionText, Object: "Who writes config?"},
		{Predicate: rdf.PropBlocksClosureDimension, Object: "authority"},
		{Predicate: rdf.PropBlocksClaim, Object: architectureClaimIRI(testClaimID), ObjectIsIRI: true},
		{Predicate: rdf.PropAcceptedAnswerType, Object: "intent_statement"},
		{Predicate: rdf.PropReasonOpen, Object: "Two writers are observed."},
		{Predicate: rdf.PropKnownFact, Object: "fact.config.writer"},
		{Predicate: rdf.PropGroundedByEvidence, Object: evidenceIRI(testEvidenceID), ObjectIsIRI: true},
		{Predicate: rdf.PropCompetingHypothesis, Object: "hypothesis.owner_a: Component A owns it."},
		{Predicate: rdf.PropMissingEvidence, Object: "A governed decision."},
		{Predicate: rdf.PropQuestionPriority, Object: "high"},
		{Predicate: rdf.PropRiskIfUnresolved, Object: "Authority split persists."},
		{Predicate: rdf.PropArchitectRequired, Object: "true"},
		{Predicate: rdf.PropQuestionStatus, Object: "resolved"},
		{Predicate: rdf.PropResolvedByAnswer, Object: architectAnswerIRI(testAnswerID), ObjectIsIRI: true},
		{Predicate: rdf.PropCreatedAt, Object: "2026-07-13T12:00:00Z"},
		{Predicate: rdf.PropValidForCommit, Object: "0123456789abcdef"},
		{Predicate: rdf.PropValidForGraphDigest, Object: "abcdef0123456789"},
		{Predicate: rdf.PropAnchoredIn, Object: fileIRI(testDialogueFile), ObjectIsIRI: true},
	}
}

func architectAnswerTriples() []store.Triple {
	return []store.Triple{
		{Predicate: rdf.PropType, Object: rdf.ClassArchitectAnswer, ObjectIsIRI: true},
		{Predicate: rdf.PropLabel, Object: "Config writer answer"},
		{Predicate: rdf.PropAnswersQuestion, Object: openQuestionIRI(testQuestionID), ObjectIsIRI: true},
		{Predicate: rdf.PropAuthorRole, Object: "project_architect"},
		{Predicate: rdf.PropAuthorID, Object: "architect.local"},
		{Predicate: rdf.PropAnswerStatement, Object: "Component A is the intended writer."},
		{Predicate: rdf.PropAnswerClassification, Object: "intent_statement"},
		{Predicate: rdf.PropAnswerCondition, Object: "Component B remains temporary."},
		{Predicate: rdf.PropCitesEvidence, Object: evidenceIRI(testEvidenceID), ObjectIsIRI: true},
		{Predicate: rdf.PropEvidencePointer, Object: "docs/decisions/config_writer.md"},
		{Predicate: rdf.PropSelectedHypothesis, Object: "question.config_writer:hypothesis.owner_a"},
		{Predicate: rdf.PropRecordedAt, Object: "2026-07-13T12:15:00Z"},
		{Predicate: rdf.PropAnswerGovernanceStatus, Object: "accepted_for_question"},
		{Predicate: rdf.PropValidForCommit, Object: "0123456789abcdef"},
		{Predicate: rdf.PropValidForGraphDigest, Object: "abcdef0123456789"},
		{Predicate: rdf.PropAnchoredIn, Object: fileIRI(testDialogueFile), ObjectIsIRI: true},
	}
}

func dialogueClassFacts(classIRI, nodeIRI string, triples []store.Triple) []store.ImpactFact {
	facts := make([]store.ImpactFact, 0, len(triples))
	for _, tr := range triples {
		facts = append(facts, store.ImpactFact{
			NodeIRI:     nodeIRI,
			TypeIRI:     classIRI,
			Predicate:   tr.Predicate,
			Object:      tr.Object,
			ObjectIsIRI: tr.ObjectIsIRI,
		})
	}
	return facts
}

func newArchitectureDialogueTestServer() *server {
	return newServer(currentAuthorityStore(fakeStore{
		describe: func(_ context.Context, iri string) ([]store.Triple, error) {
			switch iri {
			case openQuestionIRI(testQuestionID):
				return openQuestionTriples(), nil
			case architectAnswerIRI(testAnswerID):
				return architectAnswerTriples(), nil
			case architectureClaimIRI(testClaimID):
				return claimTriples(testClaimID), nil
			case evidenceIRI(testEvidenceID):
				return evidenceTriples(), nil
			default:
				return nil, nil
			}
		},
		classFacts: func(_ context.Context, classIRI string, _ int) ([]store.ImpactFact, error) {
			switch classIRI {
			case rdf.ClassOpenQuestion:
				return dialogueClassFacts(rdf.ClassOpenQuestion, openQuestionIRI(testQuestionID), openQuestionTriples()), nil
			case rdf.ClassArchitectAnswer:
				return dialogueClassFacts(rdf.ClassArchitectAnswer, architectAnswerIRI(testAnswerID), architectAnswerTriples()), nil
			}
			return nil, nil
		},
		impactForFile: func(_ context.Context, iri string) ([]store.ImpactFact, error) {
			if iri == fileIRI(testDialogueFile) {
				facts := dialogueClassFacts(rdf.ClassOpenQuestion, openQuestionIRI(testQuestionID), openQuestionTriples())
				facts = append(facts, dialogueClassFacts(rdf.ClassArchitectAnswer, architectAnswerIRI(testAnswerID), architectAnswerTriples())...)
				return facts, nil
			}
			return nil, nil
		},
		countTriples: func(context.Context) (int64, error) { return 12, nil },
		countByClass: func(_ context.Context, classIRI string) (int64, error) {
			switch classIRI {
			case rdf.ClassOpenQuestion:
				return 1, nil
			case rdf.ClassArchitectAnswer:
				return 1, nil
			}
			return 0, nil
		},
	}))
}

func TestQueryByClassOpenQuestion(t *testing.T) {
	s := newArchitectureDialogueTestServer()
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{Mode: awarenesspb.QueryMode_QUERY_MODE_BY_CLASS, Class: awarenesspb.QueryClass_QUERY_CLASS_OPEN_QUESTION})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(resp.GetRows()) != 1 || resp.GetRows()[0].GetStatus() != "resolved" {
		t.Fatalf("rows=%+v", resp.GetRows())
	}
}

func TestQueryByClassArchitectAnswer(t *testing.T) {
	s := newArchitectureDialogueTestServer()
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{Mode: awarenesspb.QueryMode_QUERY_MODE_BY_CLASS, Class: awarenesspb.QueryClass_QUERY_CLASS_ARCHITECT_ANSWER})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(resp.GetRows()) != 1 || resp.GetRows()[0].GetStatus() != "accepted_for_question" {
		t.Fatalf("rows=%+v", resp.GetRows())
	}
}

func TestQueryByIDOpenQuestion(t *testing.T) {
	s := newArchitectureDialogueTestServer()
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{Mode: awarenesspb.QueryMode_QUERY_MODE_BY_ID, Id: "open_question:" + testQuestionID})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(resp.GetRows()) != 1 || resp.GetRows()[0].GetClass() != "open_question" {
		t.Fatalf("rows=%+v", resp.GetRows())
	}
}

func TestQueryByIDArchitectAnswer(t *testing.T) {
	s := newArchitectureDialogueTestServer()
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{Mode: awarenesspb.QueryMode_QUERY_MODE_BY_ID, Id: "architect_answer:" + testAnswerID})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(resp.GetRows()) != 1 || resp.GetRows()[0].GetClass() != "architect_answer" {
		t.Fatalf("rows=%+v", resp.GetRows())
	}
}

func TestResolveOpenQuestionShowsDialogueFacts(t *testing.T) {
	s := newArchitectureDialogueTestServer()
	resp, err := s.Resolve(context.Background(), &awarenesspb.ResolveRequest{Class: "open_question", Id: testQuestionID})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !hasNodeFact(resp.GetNode(), "question text", "Who writes config?") || !hasNodeFact(resp.GetNode(), "question status", "resolved") {
		t.Fatalf("facts=%+v", resp.GetNode().GetFacts())
	}
}

func TestResolveArchitectAnswerShowsExactStatementAndClassification(t *testing.T) {
	s := newArchitectureDialogueTestServer()
	resp, err := s.Resolve(context.Background(), &awarenesspb.ResolveRequest{Class: "architect_answer", Id: testAnswerID})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !hasNodeFact(resp.GetNode(), "answer statement", "Component A is the intended writer.") || !hasNodeFact(resp.GetNode(), "classification", "intent_statement") {
		t.Fatalf("facts=%+v", resp.GetNode().GetFacts())
	}
}

func TestRelatedQuestionShowsBlockedClaimAnswerAndEvidence(t *testing.T) {
	s := newArchitectureDialogueTestServer()
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{Mode: awarenesspb.QueryMode_QUERY_MODE_RELATED, Id: "open_question:" + testQuestionID, Limit: 10})
	if err != nil {
		t.Fatalf("Query related: %v", err)
	}
	if !hasQueryRow(resp.GetRows(), "architecture_claim:"+testClaimID, "blocksClaim") ||
		!hasQueryRow(resp.GetRows(), "architect_answer:"+testAnswerID, "resolvedByAnswer") ||
		!hasQueryRow(resp.GetRows(), "evidence:"+testEvidenceID, "groundedByEvidence") {
		t.Fatalf("rows=%+v", resp.GetRows())
	}
}

func TestRelatedAnswerShowsQuestionAndEvidence(t *testing.T) {
	s := newArchitectureDialogueTestServer()
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{Mode: awarenesspb.QueryMode_QUERY_MODE_RELATED, Id: "architect_answer:" + testAnswerID, Limit: 10})
	if err != nil {
		t.Fatalf("Query related: %v", err)
	}
	if !hasQueryRow(resp.GetRows(), "open_question:"+testQuestionID, "answersQuestion") ||
		!hasQueryRow(resp.GetRows(), "evidence:"+testEvidenceID, "citesEvidence") {
		t.Fatalf("rows=%+v", resp.GetRows())
	}
}

func TestMetadataCountsOpenQuestionsSeparately(t *testing.T) {
	s := newArchitectureDialogueTestServer()
	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if resp.GetOpenQuestionCount() != 1 {
		t.Fatalf("open_question_count=%d", resp.GetOpenQuestionCount())
	}
}

func TestMetadataCountsArchitectAnswersSeparately(t *testing.T) {
	s := newArchitectureDialogueTestServer()
	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if resp.GetArchitectAnswerCount() != 1 {
		t.Fatalf("architect_answer_count=%d", resp.GetArchitectAnswerCount())
	}
}

func TestDialogueCountsDoNotAffectCoverage(t *testing.T) {
	s := newArchitectureDialogueTestServer()
	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if resp.GetCoverageState() != awarenesspb.CoverageState_COVERAGE_STATE_THIN {
		t.Fatalf("coverage_state=%s, want THIN", resp.GetCoverageState())
	}
}

func TestOpenQuestionDoesNotAppearInImpactDirectArchitecture(t *testing.T) {
	s := newArchitectureDialogueTestServer()
	resp, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{File: testDialogueFile})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	for _, n := range resp.GetDirectArchitecture() {
		if n.GetClass() == "open_question" {
			t.Fatalf("direct_architecture surfaced question: %+v", n)
		}
	}
}

func TestArchitectAnswerDoesNotAppearInImpactDirectArchitecture(t *testing.T) {
	s := newArchitectureDialogueTestServer()
	resp, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{File: testDialogueFile})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	for _, n := range resp.GetDirectArchitecture() {
		if n.GetClass() == "architect_answer" {
			t.Fatalf("direct_architecture surfaced answer: %+v", n)
		}
	}
}

func TestOpenQuestionDoesNotAppearInBriefing(t *testing.T) {
	s := newArchitectureDialogueTestServer()
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: testDialogueFile})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if strings.Contains(resp.GetProse(), testQuestionID) || containsString(resp.GetReferencedIds(), "open_question:"+testQuestionID) {
		t.Fatalf("briefing surfaced question: prose=%q ids=%v", resp.GetProse(), resp.GetReferencedIds())
	}
}

func TestArchitectAnswerDoesNotAppearInBriefing(t *testing.T) {
	s := newArchitectureDialogueTestServer()
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: testDialogueFile})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if strings.Contains(resp.GetProse(), testAnswerID) || containsString(resp.GetReferencedIds(), "architect_answer:"+testAnswerID) {
		t.Fatalf("briefing surfaced answer: prose=%q ids=%v", resp.GetProse(), resp.GetReferencedIds())
	}
}

func TestOpenQuestionDoesNotAppearInPreflight(t *testing.T) {
	s := newArchitectureDialogueTestServer()
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{Task: "edit config", Files: []string{testDialogueFile}})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	for _, n := range resp.GetDirectArchitecture() {
		if n.GetClass() == "open_question" {
			t.Fatalf("preflight surfaced question: %+v", n)
		}
	}
}

func TestArchitectAnswerDoesNotAppearInPreflight(t *testing.T) {
	s := newArchitectureDialogueTestServer()
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{Task: "edit config", Files: []string{testDialogueFile}})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	for _, n := range resp.GetDirectArchitecture() {
		if n.GetClass() == "architect_answer" {
			t.Fatalf("preflight surfaced answer: %+v", n)
		}
	}
}

func TestDialogueDoesNotAffectRiskClassification(t *testing.T) {
	s := newArchitectureDialogueTestServer()
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{Task: "edit config", Files: []string{testDialogueFile}})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if resp.GetRiskClass() != awarenesspb.RiskClass_UNKNOWN_IMPACT {
		t.Fatalf("risk_class=%s, want UNKNOWN_IMPACT", resp.GetRiskClass())
	}
}
