// SPDX-License-Identifier: AGPL-3.0-only

package questiongen

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
	"github.com/globulario/sensei/golang/architecture/investigator"
)

const investigationQuestionFixtureTime = "2026-07-22T12:00:00Z"

func TestGenerateFromInvestigationRoutesEvidenceRequestIntoDialogue(t *testing.T) {
	result := investigationQuestionFixture(t, false, false)
	generated, err := GenerateFromInvestigation(InvestigationQuestionContext{
		Investigation: result,
		CreatedAt:     investigationQuestionFixtureTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	question := questionBySourceKind(t, generated.Dialogue, architecture.SourceEvidenceGap)
	if question.QuestionTemplateID != TemplateEvidenceRequest || question.Status != architecture.QuestionStatusAwaitingArchitect {
		t.Fatalf("unexpected evidence question: %+v", question)
	}
	if question.SourceArtifactDigestSHA256 != result.Receipt.ExactResultDigestSHA256 {
		t.Fatal("question lost exact investigation receipt binding")
	}
	if len(question.BlocksClaims) != 1 || len(question.SourceReferenceIDs) < 2 {
		t.Fatalf("question lost candidate grounding: %+v", question)
	}
	if len(generated.Report.Generated) == 0 {
		t.Fatal("generation report did not account for the question")
	}
}

func TestGenerateFromInvestigationRoutesContestedChallengeThroughExistingAnswerPath(t *testing.T) {
	result := investigationQuestionFixture(t, true, false)
	generated, err := GenerateFromInvestigation(InvestigationQuestionContext{
		Investigation: result,
		CreatedAt:     investigationQuestionFixtureTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	question := questionBySourceKind(t, generated.Dialogue, architecture.SourceCounterexample)
	if question.Status != architecture.QuestionStatusAwaitingArchitect || !question.ArchitectRequired {
		t.Fatalf("contested challenge must await governed architect review: %+v", question)
	}
	if len(question.CompetingHypotheses) != 2 {
		t.Fatalf("contested challenge must preserve alternatives: %+v", question.CompetingHypotheses)
	}

	doc, recording, err := RecordAnswer(generated.Dialogue, RecordAnswerOptions{
		QuestionID:       question.ID,
		Statement:        "The boundary is intentional but limited to the current repository scope.",
		Classifications:  []string{architecture.AnswerTypeGovernedDecisionCandidate},
		AuthorRole:       "architect",
		AuthorID:         "fixture",
		RecordedAt:       investigationQuestionFixtureTime,
		GovernanceStatus: architecture.AnswerGovernanceRecorded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc.OpenQuestions[questionIndex(doc.OpenQuestions, question.ID)].Status != architecture.QuestionStatusAnswered {
		t.Fatal("existing answer path did not record the investigator-backed answer")
	}
	doc, adjudication, err := AdjudicateAnswer(doc, AdjudicateAnswerOptions{
		AnswerID:      recording.AnswerID,
		Status:        architecture.AnswerGovernanceAcceptedForQuestion,
		AdjudicatedAt: investigationQuestionFixtureTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved := doc.OpenQuestions[questionIndex(doc.OpenQuestions, question.ID)]
	if resolved.Status != architecture.QuestionStatusResolved || adjudication.GovernanceStatus != architecture.AnswerGovernanceAcceptedForQuestion {
		t.Fatalf("governed answer path did not resolve the question: %+v / %+v", resolved, adjudication)
	}
	if len(result.Document.CandidateClaims) != 1 || result.Document.CandidateClaims[0].PromotionStatus != architecture.PromotionCandidate {
		t.Fatal("question adjudication must not promote the investigator candidate")
	}
}

func TestGenerateFromInvestigationUsesStableCandidateTemplateWhenCoverageWasSearched(t *testing.T) {
	result := investigationQuestionFixture(t, false, true)
	generated, err := GenerateFromInvestigation(InvestigationQuestionContext{
		Investigation: result,
		CreatedAt:     investigationQuestionFixtureTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	question := questionBySourceKind(t, generated.Dialogue, architecture.SourceInvestigationCandidate)
	if question.QuestionTemplateID != TemplateStructuralWhy {
		t.Fatalf("boundary candidate did not use the stable structural-WHY template: %+v", question)
	}
	if len(question.FalsificationConditions) == 0 || question.SuggestedAnswerOwner == "" {
		t.Fatalf("candidate question omitted falsification or answer ownership: %+v", question)
	}
}

func TestGenerateFromInvestigationIsDeterministicAndDeduplicates(t *testing.T) {
	result := investigationQuestionFixture(t, false, false)
	first, err := GenerateFromInvestigation(InvestigationQuestionContext{
		Investigation: result,
		CreatedAt:     investigationQuestionFixtureTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateFromInvestigation(InvestigationQuestionContext{
		Investigation: result,
		Existing:      &first.Dialogue,
		CreatedAt:     investigationQuestionFixtureTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Dialogue.OpenQuestions) != len(first.Dialogue.OpenQuestions) {
		t.Fatalf("repeated generation duplicated questions: %d != %d", len(second.Dialogue.OpenQuestions), len(first.Dialogue.OpenQuestions))
	}
	if len(second.Report.ExistingCoverage) == 0 {
		t.Fatal("repeated generation was not accounted as existing coverage")
	}
}

func TestGenerateFromInvestigationRefusesStaleReceipt(t *testing.T) {
	result := investigationQuestionFixture(t, false, false)
	result.Document.CandidateClaims[0].Statement.Object = "changed.after.receipt"
	_, err := GenerateFromInvestigation(InvestigationQuestionContext{
		Investigation: result,
		CreatedAt:     investigationQuestionFixtureTime,
	})
	if err == nil || !strings.Contains(err.Error(), "receipt") {
		t.Fatalf("stale investigation receipt must be refused, got %v", err)
	}
}

func questionBySourceKind(t *testing.T, doc architecture.DialogueDocument, kind architecture.QuestionSourceKind) architecture.OpenQuestion {
	t.Helper()
	for _, question := range doc.OpenQuestions {
		if question.QuestionSourceKind == kind {
			return question
		}
	}
	t.Fatalf("question source kind %s not found in %+v", kind, doc.OpenQuestions)
	return architecture.OpenQuestion{}
}

func investigationQuestionFixture(t *testing.T, contested, searchedNoResult bool) investigator.Result {
	t.Helper()
	repository := architecture.ClaimDocumentBinding{
		RepositoryDomain:  "example/repo",
		Revision:          "abc123",
		RevisionStatus:    architecture.RevisionResolved,
		GraphDigestSHA256: investigation.SHA256String("graph"),
		GraphDigestStatus: architecture.GraphDigestResolved,
	}
	fact := architecture.Fact{
		ID:         "fact.boundary.one",
		Kind:       "boundary",
		Subject:    "api.Call",
		Predicate:  "crosses_component_boundary_to",
		Object:     "store.Load",
		Confidence: 0.9,
		Extractor:  "boundary_extractor",
		Scope: architecture.Scope{
			Repository: repository.RepositoryDomain,
			Files:      []string{"api.go", "store.go"},
			Symbols:    []string{"api.Call", "store.Load"},
		},
		Evidence: architecture.Evidence{SourceFile: "api.go", LineStart: 10, LineEnd: 10},
	}
	howEvidence := investigationQuestionEvidence(
		"evidence_boundary",
		investigation.EvidenceSourceCode,
		"boundary_extractor",
		"store.Load()",
		"api.go",
		[]string{"api.Call", "store.Load"},
		[]string{"component.api"},
	)
	how := investigation.Document{
		SchemaVersion: "investigation.schema.v1",
		GeneratedBy:   "sensei.how.fixture",
		Mode:          investigation.ModeHow,
		Binding: investigation.Binding{
			Repository:                    repository,
			InvestigationPlanDigestSHA256: investigation.SHA256String("how plan"),
			ExtractorProfileDigestSHA256:  investigation.SHA256String("how profile"),
			Model:                         investigation.ModelBinding{Status: investigation.ModelStatusDisabled},
		},
		Plan: investigation.Plan{ID: "plan.how.fixture", Description: "fixture HOW plan", Queries: []string{"boundary"}},
		Coverage: []investigation.CoverageEntry{{
			ProviderID:                 howEvidence.Provider.ID,
			ProviderVersion:            howEvidence.Provider.Version,
			Category:                   howEvidence.Category,
			TargetDigestSHA256:         investigation.SHA256String("how target"),
			SourceSnapshotDigestSHA256: investigation.SHA256String("how snapshot"),
			ResultEvidenceIDs:          []string{howEvidence.ID},
			Status:                     investigation.CoverageSupporting,
		}},
		RawEvidence:  []investigation.EvidenceReceipt{howEvidence},
		Observations: []architecture.Fact{fact},
		Receipt: investigation.RunReceipt{
			SchemaVersion:                "investigation.schema.v1",
			GeneratedBy:                  "sensei.how.fixture",
			Repository:                   repository,
			GraphDigestSHA256:            repository.GraphDigestSHA256,
			PlanDigestSHA256:             investigation.SHA256String("how plan"),
			ExtractorProfileDigestSHA256: investigation.SHA256String("how profile"),
			Model:                        investigation.ModelBinding{Status: investigation.ModelStatusDisabled},
			PostProcessingVersion:        "fixture.v1",
			OutputCandidateIDsAndDigests: map[string]string{},
			TimestampSource:              investigationQuestionFixtureTime,
			ResourceLimits:               map[string]string{"fixture": "bounded"},
			NondeterminismDeclaration:    "deterministic_only",
		},
	}
	finalizeInvestigationQuestionDocument(t, &how)

	why := investigation.Document{
		SchemaVersion: "investigation.schema.v1",
		GeneratedBy:   "sensei.why.fixture",
		Mode:          investigation.ModeWhy,
		Binding: investigation.Binding{
			Repository:                    repository,
			EvidenceSnapshotDigestSHA256:  investigation.SHA256String("why snapshot"),
			InvestigationPlanDigestSHA256: investigation.SHA256String("why plan"),
			ExtractorProfileDigestSHA256:  investigation.SHA256String("why profile"),
			Model:                         investigation.ModelBinding{Status: investigation.ModelStatusDisabled},
			Why: investigation.WhyBinding{
				HowDocumentDigestSHA256:   how.Receipt.OutputDocumentDigestSHA256,
				QueryDigestSHA256:         investigation.SHA256String("why query"),
				TargetObservationIDs:      []string{fact.ID},
				HistoryRangeStart:         "abc000",
				HistoryRangeEnd:           "abc123",
				ResolvedHistoryRangeStart: "abc000",
				ResolvedHistoryRangeEnd:   "abc123",
			},
		},
		Plan: investigation.Plan{ID: "plan.why.fixture", Description: "fixture WHY plan", Queries: []string{"why query"}},
		Receipt: investigation.RunReceipt{
			SchemaVersion:                "investigation.schema.v1",
			GeneratedBy:                  "sensei.why.fixture",
			Repository:                   repository,
			GraphDigestSHA256:            repository.GraphDigestSHA256,
			PlanDigestSHA256:             investigation.SHA256String("why plan"),
			ExtractorProfileDigestSHA256: investigation.SHA256String("why profile"),
			EvidenceSnapshotDigestSHA256: investigation.SHA256String("why snapshot"),
			Model:                        investigation.ModelBinding{Status: investigation.ModelStatusDisabled},
			PostProcessingVersion:        "fixture.v1",
			OutputCandidateIDsAndDigests: map[string]string{},
			TimestampSource:              investigationQuestionFixtureTime,
			ResourceLimits:               map[string]string{"fixture": "bounded"},
			NondeterminismDeclaration:    "deterministic_only",
		},
	}
	if searchedNoResult {
		why.Coverage = []investigation.CoverageEntry{{
			ProviderID:                 "design_documents_provider",
			ProviderVersion:            "1.0",
			Category:                   investigation.EvidenceDesignDocuments,
			TargetDigestSHA256:         investigation.SHA256String("why target"),
			SourceSnapshotDigestSHA256: investigation.SHA256String("design snapshot"),
			Status:                     investigation.CoverageNoResult,
		}}
	}
	if contested {
		scar := investigationQuestionEvidence(
			"evidence_scar",
			investigation.EvidenceErrorTracking,
			"scars_provider",
			"recorded incident contests the boundary",
			"scars/incident.yaml",
			[]string{fact.ID},
			nil,
		)
		why.RawEvidence = []investigation.EvidenceReceipt{scar}
		why.Coverage = []investigation.CoverageEntry{{
			ProviderID:                 scar.Provider.ID,
			ProviderVersion:            scar.Provider.Version,
			Category:                   scar.Category,
			TargetDigestSHA256:         investigation.SHA256String("scar target"),
			SourceSnapshotDigestSHA256: investigation.SHA256String("scar snapshot"),
			ResultEvidenceIDs:          []string{scar.ID},
			Status:                     investigation.CoverageRefuting,
		}}
	}
	finalizeInvestigationQuestionDocument(t, &why)

	grounding := investigator.GroundingSnapshot{
		Files:              []string{"api.go", "store.go", "scars/incident.yaml"},
		Symbols:            []string{"api.Call", "store.Load"},
		GraphNodeIDs:       []string{"component.api"},
		ObservationIDs:     []string{fact.ID},
		EvidenceReceiptIDs: []string{howEvidence.ID, "evidence_scar"},
	}
	result, err := investigator.Compose(investigator.ComposeInput{
		How:       how,
		Why:       why,
		Grounding: grounding,
		Digests: investigator.InputDigests{
			GraphDigestSHA256:             repository.GraphDigestSHA256,
			CurrentClaimsDigestSHA256:     investigation.SHA256String("claims"),
			ClosureStateDigestSHA256:      investigation.SHA256String("closure"),
			ExistingQuestionsDigestSHA256: investigation.SHA256String("questions"),
			ReviewHistoryDigestSHA256:     investigation.SHA256String("reviews"),
		},
	}, investigator.ComposeOptions{
		TimestampSource: investigationQuestionFixtureTime,
		ResourceLimits:  map[string]string{"fixture": "bounded"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func investigationQuestionEvidence(id string, category investigation.EvidenceCategory, providerID, content, file string, symbols, components []string) investigation.EvidenceReceipt {
	return investigation.EvidenceReceipt{
		ID:                  id,
		Category:            category,
		Provider:            investigation.ProviderBinding{ID: providerID, Version: "1.0"},
		SourceIdentity:      file,
		SourceDigestSHA256:  investigation.SHA256String("source:" + file),
		ContentDigestSHA256: investigation.SHA256String(content),
		CapturedContent:     content,
		Scope: architecture.ClaimScope{
			Repository: "example/repo",
			Files:      []string{file},
			Symbols:    symbols,
			Components: components,
		},
		CapturedAt:    investigationQuestionFixtureTime,
		ProofStrength: investigation.ProofStaticSource,
	}
}

func finalizeInvestigationQuestionDocument(t *testing.T, document *investigation.Document) {
	t.Helper()
	normalized, err := investigation.Normalize(*document)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := investigation.CalculateDocumentDigest(normalized)
	if err != nil {
		t.Fatal(err)
	}
	normalized.Receipt.OutputDocumentDigestSHA256 = digest
	if err := investigation.Validate(normalized); err != nil {
		t.Fatal(err)
	}
	*document = normalized
}
