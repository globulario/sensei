// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

func TestComposerCompositionAndVerification(t *testing.T) {
	root := t.TempDir()

	// Write mock source files to verify post-processor grounding
	fileRelPath := "golang/server/namespace.go"
	fullPath := filepath.Join(root, fileRelPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte("package server\n"), 0644); err != nil {
		t.Fatal(err)
	}

	capturedAt := time.Now().Format(time.RFC3339)
	opts := Options{
		Root:       root,
		CapturedAt: capturedAt,
		ResourceLimits: map[string]string{
			"synthesizer": "local",
		},
	}

	content := "namespace_registry definition and tests"
	digest := investigation.SHA256String(content)

	// 1. Construct base investigation document as input
	baseDoc := investigation.Document{
		SchemaVersion: "investigation.schema.v1",
		GeneratedBy:   "sensei.whyinvestigation.orchestrator",
		Mode:          investigation.ModeWhy,
		Binding: investigation.Binding{
			Repository: architecture.ClaimDocumentBinding{
				RepositoryDomain:  "github.com/globulario/sensei",
				Revision:          "0000000000000000000000000000000000000000",
				RevisionStatus:    "resolved",
				GraphDigestStatus: "resolved",
				GraphDigestSHA256: investigation.SHA256String("mock_graph_digest"),
			},
			EvidenceSnapshotDigestSHA256:  investigation.SHA256String("mock_evidence_snapshot_digest"),
			InvestigationPlanDigestSHA256: investigation.SHA256String("mock_plan_digest"),
			ExtractorProfileDigestSHA256:  investigation.SHA256String("mock_profile_digest"),
			Model: investigation.ModelBinding{
				Status: investigation.ModelStatusDisabled,
			},
		},
		Coverage: []investigation.CoverageEntry{
			{
				ProviderID:                 "documentation_provider",
				ProviderVersion:            "1.0",
				Category:                   investigation.EvidenceDocumentation,
				Status:                     investigation.CoverageSupporting,
				TargetDigestSHA256:         investigation.SHA256String("target_1"),
				SourceSnapshotDigestSHA256: investigation.SHA256String("source_1"),
				ResultEvidenceIDs:          []string{"evidence_rec_1"},
			},
			{
				ProviderID:         "scars_provider",
				ProviderVersion:    "1.0",
				Category:           investigation.EvidenceErrorTracking,
				Status:             investigation.CoverageUnavailable,
				TargetDigestSHA256: investigation.SHA256String("target_2"),
				Reason:             "scars directory does not exist",
			},
		},
		RawEvidence: []investigation.EvidenceReceipt{
			{
				ID:                  "evidence_rec_1",
				Category:            investigation.EvidenceDocumentation,
				Provider:            investigation.ProviderBinding{ID: "documentation_provider", Version: "1.0"},
				ProofStrength:       investigation.ProofStaticSource,
				SourceIdentity:      "file:" + fileRelPath,
				SourceDigestSHA256:  digest,
				ContentDigestSHA256: digest,
				CapturedContent:     content,
				CapturedAt:          capturedAt,
				Scope: architecture.ClaimScope{
					Repository: "github.com/globulario/sensei",
					Files:      []string{fileRelPath},
					Symbols:    []string{"namespace_registry"},
				},
			},
		},
		Observations: []architecture.Fact{
			{
				ID:        "fact.1",
				Subject:   "namespace_registry",
				Predicate: "defined_in",
				Object:    fileRelPath,
			},
		},
	}

	input := Input{
		Document: baseDoc,
	}

	engine := NewEngine(
		NewSynthesizer(),
		NewChallenger(),
		NewRanker(),
		NewPostProcessor(),
	)

	// 2. Perform composition
	doc, err := engine.Compose(context.Background(), input, opts)
	if err != nil {
		t.Fatalf("Engine Compose failed: %v", err)
	}

	// 3. Assert candidate outputs are correctly formed and populated
	if len(doc.CandidateClaims) == 0 {
		t.Fatal("expected candidate claims to be synthesized")
	}
	if len(doc.CandidateQuestions) == 0 {
		t.Fatal("expected candidate questions to be synthesized")
	}

	// 4. Assert RunReceipt matching candidate IDs & digests
	for _, claim := range doc.CandidateClaims {
		digestVal, ok := doc.Receipt.OutputCandidateIDsAndDigests[claim.ID]
		if !ok || digestVal == "" {
			t.Fatalf("receipt missing output digest for claim %s", claim.ID)
		}
	}

	// 5. Assert default template rules like evidence request
	evidenceRequestFound := false
	for _, q := range doc.CandidateQuestions {
		if q.QuestionTemplateID == "question.evidence_request.v1" {
			evidenceRequestFound = true
			if !strings.Contains(q.QuestionText, "scars_provider") {
				t.Fatalf("expected evidence request to refer to scars_provider, got %q", q.QuestionText)
			}
		}
	}
	if !evidenceRequestFound {
		t.Fatal("expected evidence request question template to be generated")
	}
}

func TestComposerAdversarialChallengeAndCounterexample(t *testing.T) {
	root := t.TempDir()

	fileRelPath := "namespace.go"
	if err := os.WriteFile(filepath.Join(root, fileRelPath), []byte("package server\n"), 0644); err != nil {
		t.Fatal(err)
	}

	capturedAt := time.Now().Format(time.RFC3339)
	opts := Options{Root: root, CapturedAt: capturedAt}

	content1 := "namespace_registry definition and tests"
	digest1 := investigation.SHA256String(content1)

	content2 := "incident scar violation: namespace_registry failed invariant checks"
	digest2 := investigation.SHA256String(content2)

	// Setup base doc with contradictory scar incident evidence receipt
	baseDoc := investigation.Document{
		SchemaVersion: "investigation.schema.v1",
		GeneratedBy:   "sensei.whyinvestigation.orchestrator",
		Mode:          investigation.ModeWhy,
		Binding: investigation.Binding{
			Repository: architecture.ClaimDocumentBinding{
				RepositoryDomain:  "github.com/globulario/sensei",
				Revision:          "0000000000000000000000000000000000000000",
				RevisionStatus:    "resolved",
				GraphDigestStatus: "resolved",
				GraphDigestSHA256: investigation.SHA256String("mock_graph_digest"),
			},
			EvidenceSnapshotDigestSHA256:  investigation.SHA256String("mock_evidence_snapshot_digest"),
			InvestigationPlanDigestSHA256: investigation.SHA256String("mock_plan_digest"),
			ExtractorProfileDigestSHA256:  investigation.SHA256String("mock_profile_digest"),
			Model: investigation.ModelBinding{
				Status: investigation.ModelStatusDisabled,
			},
		},
		Coverage: []investigation.CoverageEntry{
			{
				ProviderID:                 "documentation_provider",
				ProviderVersion:            "1.0",
				Category:                   investigation.EvidenceDocumentation,
				Status:                     investigation.CoverageSupporting,
				TargetDigestSHA256:         investigation.SHA256String("target_1"),
				SourceSnapshotDigestSHA256: investigation.SHA256String("source_1"),
				ResultEvidenceIDs:          []string{"evidence_rec_1"},
			},
			{
				ProviderID:                 "scars_provider",
				ProviderVersion:            "1.0",
				Category:                   investigation.EvidenceErrorTracking,
				Status:                     investigation.CoverageSupporting,
				TargetDigestSHA256:         investigation.SHA256String("target_2"),
				SourceSnapshotDigestSHA256: investigation.SHA256String("source_2"),
				ResultEvidenceIDs:          []string{"evidence_rec_contradiction"},
			},
		},
		RawEvidence: []investigation.EvidenceReceipt{
			{
				ID:                  "evidence_rec_1",
				Category:            investigation.EvidenceDocumentation,
				Provider:            investigation.ProviderBinding{ID: "documentation_provider", Version: "1.0"},
				ProofStrength:       investigation.ProofStaticSource,
				SourceIdentity:      "file:" + fileRelPath,
				SourceDigestSHA256:  digest1,
				ContentDigestSHA256: digest1,
				CapturedContent:     content1,
				CapturedAt:          capturedAt,
				Scope: architecture.ClaimScope{
					Repository: "github.com/globulario/sensei",
					Files:      []string{fileRelPath},
					Symbols:    []string{"namespace_registry"},
				},
			},
			// Contradictory error scar receipt
			{
				ID:                  "evidence_rec_contradiction",
				Category:            investigation.EvidenceErrorTracking,
				Provider:            investigation.ProviderBinding{ID: "scars_provider", Version: "1.0"},
				ProofStrength:       investigation.ProofStaticSource,
				SourceIdentity:      "scar:scars/scar_01.yaml",
				SourceDigestSHA256:  digest2,
				ContentDigestSHA256: digest2,
				CapturedContent:     content2,
				CapturedAt:          capturedAt,
				Scope: architecture.ClaimScope{
					Repository: "github.com/globulario/sensei",
					Files:      []string{fileRelPath},
					Symbols:    []string{"namespace_registry"},
				},
			},
		},
		Observations: []architecture.Fact{
			{
				ID:        "fact.1",
				Subject:   "namespace_registry",
				Predicate: "defined_in",
				Object:    fileRelPath,
			},
		},
	}

	input := Input{Document: baseDoc}

	engine := NewEngine(
		NewSynthesizer(),
		NewChallenger(),
		NewRanker(),
		NewPostProcessor(),
	)

	doc, err := engine.Compose(context.Background(), input, opts)
	if err != nil {
		t.Fatalf("Engine Compose failed: %v", err)
	}

	// 1. Assert adversarial challenger skeptic role: Counterexample and Dialogue Question are generated
	if len(doc.Counterexamples) == 0 {
		t.Fatal("expected counterexamples to be generated due to contradictory evidence")
	}

	ce := doc.Counterexamples[0]
	if ce.ClaimID == "" {
		t.Fatal("counterexample must bind to refuted claim ID")
	}
	if ce.EvidenceRefIDs[0] != "evidence_rec_contradiction" {
		t.Fatalf("expected counterexample to cite refuting evidence ID, got %q", ce.EvidenceRefIDs)
	}

	// 2. Verification that template question is structural counterexample challenge
	adversarialQFound := false
	for _, q := range doc.CandidateQuestions {
		if q.QuestionTemplateID == "question.counterexample_validation.v1" {
			adversarialQFound = true
			if len(q.KnownEvidence) == 0 || q.KnownEvidence[0] != "evidence:evidence_rec_contradiction" {
				t.Fatalf("adversarial question missing refuting evidence link: %+v", q)
			}
		}
	}
	if !adversarialQFound {
		t.Fatal("expected adversarial challenge question template to be generated")
	}
}

func TestComposerRankingAndConfidence(t *testing.T) {
	root := t.TempDir()

	fileRelPath := "namespace.go"
	if err := os.WriteFile(filepath.Join(root, fileRelPath), []byte("package server\n"), 0644); err != nil {
		t.Fatal(err)
	}

	capturedAt := time.Now().Format(time.RFC3339)
	opts := Options{Root: root, CapturedAt: capturedAt}

	content := "namespace_registry definition and tests"
	digest := investigation.SHA256String(content)

	baseDoc := investigation.Document{
		SchemaVersion: "investigation.schema.v1",
		GeneratedBy:   "sensei.whyinvestigation.orchestrator",
		Mode:          investigation.ModeWhy,
		Binding: investigation.Binding{
			Repository: architecture.ClaimDocumentBinding{
				RepositoryDomain:  "github.com/globulario/sensei",
				Revision:          "0000000000000000000000000000000000000000",
				RevisionStatus:    "resolved",
				GraphDigestStatus: "resolved",
				GraphDigestSHA256: investigation.SHA256String("mock_graph_digest"),
			},
			EvidenceSnapshotDigestSHA256:  investigation.SHA256String("mock_evidence_snapshot_digest"),
			InvestigationPlanDigestSHA256: investigation.SHA256String("mock_plan_digest"),
			ExtractorProfileDigestSHA256:  investigation.SHA256String("mock_profile_digest"),
			Model: investigation.ModelBinding{
				Status: investigation.ModelStatusDisabled,
			},
		},
		Coverage: []investigation.CoverageEntry{
			{
				ProviderID:                 "documentation_provider",
				ProviderVersion:            "1.0",
				Category:                   investigation.EvidenceDocumentation,
				Status:                     investigation.CoverageSupporting,
				TargetDigestSHA256:         investigation.SHA256String("target_1"),
				SourceSnapshotDigestSHA256: investigation.SHA256String("source_1"),
				ResultEvidenceIDs:          []string{"evidence_rec_1"},
			},
		},
		RawEvidence: []investigation.EvidenceReceipt{
			{
				ID:                  "evidence_rec_1",
				Category:            investigation.EvidenceDocumentation,
				Provider:            investigation.ProviderBinding{ID: "documentation_provider", Version: "1.0"},
				ProofStrength:       investigation.ProofStaticSource,
				SourceIdentity:      "file:" + fileRelPath,
				SourceDigestSHA256:  digest,
				ContentDigestSHA256: digest,
				CapturedContent:     content,
				CapturedAt:          capturedAt,
				Scope: architecture.ClaimScope{
					Repository: "github.com/globulario/sensei",
					Files:      []string{fileRelPath},
					Symbols:    []string{"namespace_registry"},
				},
			},
		},
		Observations: []architecture.Fact{
			{
				ID:        "fact.1",
				Subject:   "namespace_registry",
				Predicate: "defined_in",
				Object:    fileRelPath,
			},
		},
	}

	input := Input{Document: baseDoc}

	engine := NewEngine(
		NewSynthesizer(),
		NewChallenger(),
		NewRanker(),
		NewPostProcessor(),
	)

	doc, err := engine.Compose(context.Background(), input, opts)
	if err != nil {
		t.Fatalf("Engine Compose failed: %v", err)
	}

	// 1. Assert confidence remains ranking metadata only (epistemic status is still "unknown")
	for _, claim := range doc.CandidateClaims {
		if claim.EpistemicStatus != architecture.StatusUnknown {
			t.Fatalf("expected epistemic status %q, got %q", architecture.StatusUnknown, claim.EpistemicStatus)
		}
		if claim.PromotionStatus != architecture.PromotionCandidate {
			t.Fatalf("expected promotion status candidate, got %q", claim.PromotionStatus)
		}
		if claim.Confidence <= 0.0 || claim.Confidence > 1.0 {
			t.Fatalf("expected confidence between 0 and 1, got %f", claim.Confidence)
		}
	}
}
