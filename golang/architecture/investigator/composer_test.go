// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

const composerFixtureTime = "2026-07-22T12:00:00Z"

func TestComposeProducesDeterministicBoundaryCandidate(t *testing.T) {
	input := composerFixture(t, false)
	options := ComposeOptions{
		TimestampSource: composerFixtureTime,
		ResourceLimits:  map[string]string{"mode": "fixture"},
	}

	first, err := Compose(input, options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Compose(input, options)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatal("identical frozen inputs produced different results")
	}
	if len(first.Candidates) != 1 || first.Candidates[0].OutputKind != KindBoundary {
		t.Fatalf("expected one boundary candidate, got %+v", first.Candidates)
	}
	if len(first.Document.CandidateQuestions) != 0 {
		t.Fatal("Phase 10.4 composer must not generate canonical questions directly")
	}
	claim := first.Document.CandidateClaims[0]
	if claim.Confidence != 0 || claim.PromotionStatus != architecture.PromotionCandidate || !claim.HumanReviewRequired {
		t.Fatalf("candidate authority boundary changed: %+v", claim)
	}
	if len(first.EvidenceRequests) != 0 {
		t.Fatalf("searched-no-result design coverage should not create a missing-evidence request: %+v", first.EvidenceRequests)
	}
	if len(first.Challenges) != 1 || first.Challenges[0].Status != ChallengeSurvived {
		t.Fatalf("expected one survived challenge, got %+v", first.Challenges)
	}
	if len(first.Rankings) != 1 || first.Rankings[0].Rank != 1 {
		t.Fatalf("expected one deterministic ranking, got %+v", first.Rankings)
	}
}

func TestComposePreservesBoundRefutingEvidenceAsCounterexample(t *testing.T) {
	input := composerFixture(t, true)
	result, err := Compose(input, ComposeOptions{
		TimestampSource: composerFixtureTime,
		ResourceLimits:  map[string]string{"mode": "fixture"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Counterexamples) != 1 {
		t.Fatalf("expected one counterexample, got %+v", result.Counterexamples)
	}
	if len(result.Challenges) != 1 || result.Challenges[0].Status != ChallengeContested {
		t.Fatalf("refuting evidence must contest the candidate: %+v", result.Challenges)
	}
	if got := result.Counterexamples[0].Counterexample.EvidenceRefIDs; len(got) != 1 || got[0] != "evidence_scar" {
		t.Fatalf("counterexample lost exact evidence binding: %v", got)
	}
	if result.Document.CandidateClaims[0].EpistemicStatus != architecture.StatusUnknown {
		t.Fatal("challenge result must not promote or refute the canonical candidate claim")
	}
}

func TestComposeRefusesMismatchedHOWBinding(t *testing.T) {
	input := composerFixture(t, false)
	input.Why.Binding.Why.HowDocumentDigestSHA256 = SHA256String("different HOW")
	if _, err := Compose(input, ComposeOptions{
		TimestampSource: composerFixtureTime,
		ResourceLimits:  map[string]string{"mode": "fixture"},
	}); err == nil {
		t.Fatal("mismatched WHY-to-HOW binding must be refused")
	}
}

func TestComposeIgnoresUnregisteredObservationPredicates(t *testing.T) {
	input := composerFixture(t, false)
	input.How.Observations[0].Kind = "invented"
	input.How.Observations[0].Predicate = "looks_important"
	finalizeInvestigationDocument(t, &input.How)
	input.Why.Binding.Why.HowDocumentDigestSHA256 = input.How.Receipt.OutputDocumentDigestSHA256
	finalizeInvestigationDocument(t, &input.Why)

	result, err := Compose(input, ComposeOptions{
		TimestampSource: composerFixtureTime,
		ResourceLimits:  map[string]string{"mode": "fixture"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 0 || len(result.Challenges) != 0 || len(result.Rankings) != 0 {
		t.Fatalf("unregistered predicates must not synthesize candidates: %+v", result)
	}
}

func TestComposeCreatesOwnerCandidateOnlyForSingleWriter(t *testing.T) {
	input := composerFixture(t, false)
	input.How.Observations = []architecture.Fact{writerFact("fact.writer.one", "store.Write", "state.Generation", "store.go")}
	writerEvidence := sourceEvidence(
		"evidence_writer_one",
		"state.Generation = next",
		"store.go",
		[]string{"store.Write"},
		[]string{"component.store"},
	)
	writerEvidence.Provider = investigation.ProviderBinding{ID: "state_extractor", Version: "1.0"}
	input.How.RawEvidence = []investigation.EvidenceReceipt{writerEvidence}
	input.How.Coverage[0].ProviderID = "state_extractor"
	input.How.Coverage[0].ResultEvidenceIDs = []string{"evidence_writer_one"}
	finalizeInvestigationDocument(t, &input.How)
	input.Why.Binding.Why.HowDocumentDigestSHA256 = input.How.Receipt.OutputDocumentDigestSHA256
	input.Why.Binding.Why.TargetObservationIDs = []string{"fact.writer.one"}
	finalizeInvestigationDocument(t, &input.Why)
	input.Grounding = groundingFor(input.How, input.Why)

	result, err := Compose(input, ComposeOptions{
		TimestampSource: composerFixtureTime,
		ResourceLimits:  map[string]string{"mode": "fixture"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].OutputKind != KindOwner {
		t.Fatalf("expected one owner candidate, got %+v", result.Candidates)
	}

	input.How.Observations = append(input.How.Observations, writerFact("fact.writer.two", "repair.Write", "state.Generation", "repair.go"))
	secondWriterEvidence := sourceEvidence(
		"evidence_writer_two",
		"state.Generation = repaired",
		"repair.go",
		[]string{"repair.Write"},
		[]string{"component.repair"},
	)
	secondWriterEvidence.Provider = investigation.ProviderBinding{ID: "state_extractor", Version: "1.0"}
	input.How.RawEvidence = append(input.How.RawEvidence, secondWriterEvidence)
	input.How.Coverage[0].ResultEvidenceIDs = append(input.How.Coverage[0].ResultEvidenceIDs, "evidence_writer_two")
	finalizeInvestigationDocument(t, &input.How)
	input.Why.Binding.Why.HowDocumentDigestSHA256 = input.How.Receipt.OutputDocumentDigestSHA256
	input.Why.Binding.Why.TargetObservationIDs = []string{"fact.writer.one", "fact.writer.two"}
	finalizeInvestigationDocument(t, &input.Why)
	input.Grounding = groundingFor(input.How, input.Why)

	result, err = Compose(input, ComposeOptions{
		TimestampSource: composerFixtureTime,
		ResourceLimits:  map[string]string{"mode": "fixture"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 0 {
		t.Fatalf("multiple writers must not produce an owner candidate: %+v", result.Candidates)
	}
}

func composerFixture(t *testing.T, withRefutingEvidence bool) ComposeInput {
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
	howEvidence := sourceEvidence(
		"evidence_boundary",
		"store.Load()",
		"api.go",
		[]string{"api.Call", "store.Load"},
		[]string{"component.api"},
	)
	howEvidence.Provider = investigation.ProviderBinding{ID: "boundary_extractor", Version: "1.0"}
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
			ProviderID:                 "boundary_extractor",
			ProviderVersion:            "1.0",
			Category:                   investigation.EvidenceSourceCode,
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
			TimestampSource:              composerFixtureTime,
			ResourceLimits:               map[string]string{"fixture": "bounded"},
			NondeterminismDeclaration:    "deterministic_only",
		},
	}
	finalizeInvestigationDocument(t, &how)

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
		Coverage: []investigation.CoverageEntry{{
			ProviderID:                 "design_documents_provider",
			ProviderVersion:            "1.0",
			Category:                   investigation.EvidenceDesignDocuments,
			TargetDigestSHA256:         investigation.SHA256String("why target"),
			SourceSnapshotDigestSHA256: investigation.SHA256String("design snapshot"),
			Status:                     investigation.CoverageNoResult,
		}},
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
			TimestampSource:              composerFixtureTime,
			ResourceLimits:               map[string]string{"fixture": "bounded"},
			NondeterminismDeclaration:    "deterministic_only",
		},
	}
	if withRefutingEvidence {
		scar := sourceEvidence("evidence_scar", "recorded incident", "scars/incident.yaml", []string{fact.ID}, nil)
		scar.Category = investigation.EvidenceErrorTracking
		scar.Provider = investigation.ProviderBinding{ID: "scars_provider", Version: "1.0"}
		why.RawEvidence = append(why.RawEvidence, scar)
		why.Coverage = append(why.Coverage, investigation.CoverageEntry{
			ProviderID:                 scar.Provider.ID,
			ProviderVersion:            scar.Provider.Version,
			Category:                   scar.Category,
			TargetDigestSHA256:         investigation.SHA256String("scar target"),
			SourceSnapshotDigestSHA256: investigation.SHA256String("scar snapshot"),
			ResultEvidenceIDs:          []string{scar.ID},
			Status:                     investigation.CoverageSupporting,
		})
	}
	finalizeInvestigationDocument(t, &why)

	return ComposeInput{
		How:       how,
		Why:       why,
		Grounding: groundingFor(how, why),
		Digests: InputDigests{
			GraphDigestSHA256:             repository.GraphDigestSHA256,
			CurrentClaimsDigestSHA256:     investigation.SHA256String("claims"),
			ClosureStateDigestSHA256:      investigation.SHA256String("closure"),
			ExistingQuestionsDigestSHA256: investigation.SHA256String("questions"),
			ReviewHistoryDigestSHA256:     investigation.SHA256String("reviews"),
		},
	}
}

func writerFact(id, subject, object, file string) architecture.Fact {
	return architecture.Fact{
		ID:         id,
		Kind:       "write",
		Subject:    subject,
		Predicate:  "writes",
		Object:     object,
		Confidence: 0.55,
		Extractor:  "state_extractor",
		Scope: architecture.Scope{
			Repository: "example/repo",
			Files:      []string{file},
			Symbols:    []string{subject},
		},
		Evidence: architecture.Evidence{SourceFile: file, LineStart: 1, LineEnd: 1},
	}
}

func sourceEvidence(id, content, file string, symbols, components []string) investigation.EvidenceReceipt {
	return investigation.EvidenceReceipt{
		ID:                  id,
		Category:            investigation.EvidenceSourceCode,
		Provider:            investigation.ProviderBinding{ID: "fixture_provider", Version: "1.0"},
		ProofStrength:       investigation.ProofStaticSource,
		SourceIdentity:      file,
		SourceDigestSHA256:  investigation.SHA256String("source|" + file),
		ContentDigestSHA256: investigation.SHA256String(content),
		CapturedContent:     content,
		Scope: architecture.ClaimScope{
			Repository: "example/repo",
			Files:      []string{file},
			Symbols:    symbols,
			Components: components,
		},
		CapturedAt: composerFixtureTime,
	}
}

func finalizeInvestigationDocument(t *testing.T, document *investigation.Document) {
	t.Helper()
	document.Receipt.OutputDocumentDigestSHA256 = ""
	normalized, err := investigation.Normalize(*document)
	if err != nil {
		t.Fatal(err)
	}
	*document = normalized
	digest, err := investigation.CalculateDocumentDigest(*document)
	if err != nil {
		t.Fatal(err)
	}
	document.Receipt.OutputDocumentDigestSHA256 = digest
	if err := investigation.Validate(*document); err != nil {
		t.Fatalf("fixture investigation document is invalid: %v", err)
	}
}

func groundingFor(documents ...investigation.Document) GroundingSnapshot {
	var snapshot GroundingSnapshot
	for _, document := range documents {
		for _, fact := range document.Observations {
			snapshot.ObservationIDs = append(snapshot.ObservationIDs, fact.ID)
			snapshot.Files = append(snapshot.Files, fact.Scope.Files...)
			snapshot.Symbols = append(snapshot.Symbols, fact.Scope.Symbols...)
		}
		for _, evidence := range document.RawEvidence {
			snapshot.EvidenceReceiptIDs = append(snapshot.EvidenceReceiptIDs, evidence.ID)
			snapshot.Files = append(snapshot.Files, evidence.Scope.Files...)
			snapshot.Symbols = append(snapshot.Symbols, evidence.Scope.Symbols...)
			snapshot.GraphNodeIDs = append(snapshot.GraphNodeIDs, evidence.Scope.Components...)
		}
	}
	snapshot.Files = sortedUnique(snapshot.Files)
	snapshot.Symbols = sortedUnique(snapshot.Symbols)
	snapshot.GraphNodeIDs = sortedUnique(snapshot.GraphNodeIDs)
	snapshot.ObservationIDs = sortedUnique(snapshot.ObservationIDs)
	snapshot.EvidenceReceiptIDs = sortedUnique(snapshot.EvidenceReceiptIDs)
	return snapshot
}

func assertContains(t *testing.T, value, fragment string) {
	t.Helper()
	if !strings.Contains(value, fragment) {
		t.Fatalf("%q does not contain %q", value, fragment)
	}
}
