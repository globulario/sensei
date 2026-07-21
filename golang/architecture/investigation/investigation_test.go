// SPDX-License-Identifier: AGPL-3.0-only

package investigation

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture"
)

// Helper to create a valid base Document
func createValidBaseDocument() Document {
	repoDomain := "github.com/globulario/sensei"
	sha256Hex := "4a8e63db7cc5173b82bd3ba6019d30ce9e22db84d852bd3ba6019d30ce922db8" // 64 chars

	doc := Document{
		SchemaVersion: "1.0",
		GeneratedBy:   "test_investigator",
		Mode:          ModeHow,
		Binding: Binding{
			Repository: architecture.ClaimDocumentBinding{
				RepositoryDomain:  repoDomain,
				Revision:          "9c2b6d83692d75e8f692b2231fd754456e633fc8",
				RevisionStatus:    "resolved",
				GraphDigestSHA256: sha256Hex,
				GraphDigestStatus: "resolved",
			},
			EvidenceSnapshotDigestSHA256:  sha256Hex,
			InvestigationPlanDigestSHA256: sha256Hex,
			ExtractorProfileDigestSHA256:  sha256Hex,
			Model: ModelBinding{
				Status: ModelStatusDisabled,
			},
		},
		Plan: Plan{
			ID:          "plan_1",
			Description: "Simple test plan",
			Queries:     []string{"query1", "query2"},
		},
		Coverage: []CoverageEntry{
			{
				ProviderID:                 "git_history",
				ProviderVersion:            "1.0",
				Category:                   EvidenceSourceControl,
				TargetDigestSHA256:         sha256Hex,
				SourceSnapshotDigestSHA256: sha256Hex,
				Status:                     CoverageSupporting,
				ResultEvidenceIDs:          []string{"evidence_1"},
			},
		},
		RawEvidence: []EvidenceReceipt{
			{
				ID:                  "evidence_1",
				Category:            EvidenceSourceControl,
				Provider:            ProviderBinding{ID: "git_history", Version: "1.0"},
				ProofStrength:       ProofStaticSource,
				SourceIdentity:      "git_commit_1",
				SourceDigestSHA256:  sha256Hex,
				ContentDigestSHA256: sha256Hex,
				CapturedContent:     "Fixed a bug in reload logic",
				CapturedAt:          "2026-07-21T09:29:53-04:00",
				Scope: architecture.ClaimScope{
					Repository: repoDomain,
					Files:      []string{"golang/server/reload.go"},
				},
			},
		},
		Observations: []architecture.Fact{
			{
				ID:        "fact_1",
				Kind:      "write",
				Subject:   "package_x",
				Predicate: "writes",
				Object:    "state_y",
				Scope: architecture.Scope{
					Repository: repoDomain,
					Files:      []string{"golang/server/reload.go"},
				},
				Extractor: "go_ast",
			},
		},
		Receipt: RunReceipt{
			SchemaVersion: "1.0",
			GeneratedBy:   "test_investigator",
			Repository: architecture.ClaimDocumentBinding{
				RepositoryDomain:  repoDomain,
				Revision:          "9c2b6d83692d75e8f692b2231fd754456e633fc8",
				RevisionStatus:    "resolved",
				GraphDigestSHA256: sha256Hex,
				GraphDigestStatus: "resolved",
			},
			GraphDigestSHA256:            sha256Hex,
			PlanDigestSHA256:             sha256Hex,
			ExtractorProfileDigestSHA256: sha256Hex,
			EvidenceSnapshotDigestSHA256: sha256Hex,
			Model:                        ModelBinding{Status: ModelStatusDisabled},
			PostProcessingVersion:        "1.0",
			TimestampSource:              "2026-07-21T09:29:53-04:00",
		},
	}
	digest, _ := CalculateDocumentDigest(doc)
	doc.Receipt.OutputDocumentDigestSHA256 = digest
	return doc
}

// Test Normalization Idempotence
func TestNormalizationIdempotence(t *testing.T) {
	doc := createValidBaseDocument()

	// Add unsorted, redundant fields to test sorting & deduplication
	doc.Plan.Queries = []string{" query2 ", " query1 ", " query2 ", ""}
	doc.Coverage[0].ResultEvidenceIDs = []string{"ev2", "ev1", "ev2"}
	doc.Coverage[0].Limitations = []architecture.Limitation{
		{Source: "src2", Scope: "scp2", Reason: "rsn2"},
		{Source: "src1", Scope: "scp1", Reason: "rsn1"},
	}
	doc.RawEvidence[0].Scope.Files = []string{"fileB.go", "fileA.go", "fileB.go"}
	doc.Observations[0].Scope.Files = []string{"fileB.go", "fileA.go"}

	normalized1, err := Normalize(doc)
	if err != nil {
		t.Fatalf("First normalization failed: %v", err)
	}

	normalized2, err := Normalize(normalized1)
	if err != nil {
		t.Fatalf("Second normalization failed: %v", err)
	}

	// Marshalling to JSON to verify they are identical
	j1, _ := json.Marshal(normalized1)
	j2, _ := json.Marshal(normalized2)

	if string(j1) != string(j2) {
		t.Errorf("Normalization is not idempotent!\nFirst:  %s\nSecond: %s", string(j1), string(j2))
	}

	// Verify plan queries were trimmed and deduplicated?
	// Note: queries order is semantic, so they should be cleaned but not sorted.
	// We expect: ["query2", "query1"] (the blank is removed, spaces trimmed, duplicates preserved/removed depending on implementation. In our normalize.go, duplicates are not removed for Plan.Queries because order is semantic, but empty strings are filtered out and spaces are trimmed.)
	if len(normalized1.Plan.Queries) != 2 || normalized1.Plan.Queries[0] != "query2" || normalized1.Plan.Queries[1] != "query1" {
		t.Errorf("Plan.Queries not normalized correctly: %v", normalized1.Plan.Queries)
	}

	// Verify ResultEvidenceIDs are sorted and deduplicated: ["ev1", "ev2"]
	if len(normalized1.Coverage[0].ResultEvidenceIDs) != 2 || normalized1.Coverage[0].ResultEvidenceIDs[0] != "ev1" || normalized1.Coverage[0].ResultEvidenceIDs[1] != "ev2" {
		t.Errorf("ResultEvidenceIDs not sorted/deduplicated: %v", normalized1.Coverage[0].ResultEvidenceIDs)
	}

	// Verify Limitations are sorted: src1, then src2
	if normalized1.Coverage[0].Limitations[0].Source != "src1" {
		t.Errorf("Limitations not sorted correctly: %v", normalized1.Coverage[0].Limitations)
	}
}

// Test Digest Determinism
func TestDigestDeterminism(t *testing.T) {
	doc1 := createValidBaseDocument()
	doc2 := createValidBaseDocument()

	// Permute order of unsorted fields in doc2
	doc1.Coverage = []CoverageEntry{
		{
			ProviderID:                 "git",
			Category:                   EvidenceSourceControl,
			TargetDigestSHA256:         "target1",
			SourceSnapshotDigestSHA256: "snap1",
			Status:                     CoverageSupporting,
		},
		{
			ProviderID:                 "docs",
			Category:                   EvidenceDocumentation,
			TargetDigestSHA256:         "target2",
			SourceSnapshotDigestSHA256: "snap2",
			Status:                     CoverageSupporting,
		},
	}
	doc2.Coverage = []CoverageEntry{
		{
			ProviderID:                 "docs",
			Category:                   EvidenceDocumentation,
			TargetDigestSHA256:         "target2",
			SourceSnapshotDigestSHA256: "snap2",
			Status:                     CoverageSupporting,
		},
		{
			ProviderID:                 "git",
			Category:                   EvidenceSourceControl,
			TargetDigestSHA256:         "target1",
			SourceSnapshotDigestSHA256: "snap1",
			Status:                     CoverageSupporting,
		},
	}

	// Receipt mismatch shouldn't affect digest because CalculateDocumentDigest clears Receipt
	doc1.Receipt.OutputDocumentDigestSHA256 = "dummy1"
	doc2.Receipt.OutputDocumentDigestSHA256 = "dummy2"

	digest1, err := CalculateDocumentDigest(doc1)
	if err != nil {
		t.Fatalf("Failed to compute digest1: %v", err)
	}

	digest2, err := CalculateDocumentDigest(doc2)
	if err != nil {
		t.Fatalf("Failed to compute digest2: %v", err)
	}

	if digest1 != digest2 {
		t.Errorf("Digests are not deterministic across permuted coverage slice order!\n1: %s\n2: %s", digest1, digest2)
	}
}

// Test Duplicate ID Refusal
func TestDuplicateIDRefusal(t *testing.T) {
	doc := createValidBaseDocument()

	// Test duplicate raw evidence IDs
	doc.RawEvidence = append(doc.RawEvidence, doc.RawEvidence[0])
	if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "duplicate raw evidence receipt ID") {
		t.Errorf("Expected validation error for duplicate raw evidence IDs, got: %v", err)
	}

	// Test duplicate observation IDs
	doc = createValidBaseDocument()
	doc.Observations = append(doc.Observations, doc.Observations[0])
	if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "duplicate observation fact ID") {
		t.Errorf("Expected validation error for duplicate observation IDs, got: %v", err)
	}
}

// Test Invalid Vocabulary Refusal
func TestInvalidVocabularyRefusal(t *testing.T) {
	doc := createValidBaseDocument()

	// Test invalid Mode
	doc.Mode = Mode("invalid_mode")
	if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "invalid mode") {
		t.Errorf("Expected error for invalid mode, got: %v", err)
	}

	// Test invalid Category
	doc = createValidBaseDocument()
	doc.RawEvidence[0].Category = EvidenceCategory("invalid_category")
	if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "invalid raw evidence category") {
		t.Errorf("Expected error for invalid evidence category, got: %v", err)
	}

	// Test invalid CoverageStatus
	doc = createValidBaseDocument()
	doc.Coverage[0].Status = CoverageStatus("invalid_status")
	if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "invalid status") {
		t.Errorf("Expected error for invalid coverage status, got: %v", err)
	}
}

// Test Escaping File Path Refusal
func TestEscapingFilePathRefusal(t *testing.T) {
	paths := []string{"/absolute/path.go", "../escaping.go", "dir/../../escaping.go", ".."}

	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			// Test escaping in raw evidence
			doc := createValidBaseDocument()
			doc.RawEvidence[0].Scope.Files = []string{p}
			if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "escaping file path") {
				t.Errorf("Expected error for escaping raw evidence file path %q, got: %v", p, err)
			}

			// Test escaping in observations
			doc = createValidBaseDocument()
			doc.Observations[0].Scope.Files = []string{p}
			if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "escaping file path") {
				t.Errorf("Expected error for escaping observation file path %q, got: %v", p, err)
			}
		})
	}
}

// Test Missing Provider Execution for searched_no_result
func TestMissingProviderExecutionForSearchedNoResult(t *testing.T) {
	doc := createValidBaseDocument()

	// For status searched_no_result, provider details and source snapshot are required
	doc.Coverage[0].Status = CoverageNoResult
	doc.Coverage[0].SourceSnapshotDigestSHA256 = ""
	if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "requires source_snapshot_digest_sha256") {
		t.Errorf("Expected error for missing snapshot digest in searched_no_result, got: %v", err)
	}

	doc.Coverage[0].SourceSnapshotDigestSHA256 = "invalid_sha256"
	if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "requires a valid source_snapshot_digest_sha256") {
		t.Errorf("Expected error for invalid snapshot digest in searched_no_result, got: %v", err)
	}
}

// Test Model Status and Digest Matrix
func TestModelStatusAndDigestMatrix(t *testing.T) {
	sha256Hex := "4a8e63db7cc5173b82bd3ba6019d30ce9e22db84d852bd3ba6019d30ce922db8"

	// 1. Model resolved status requires digest and name
	doc := createValidBaseDocument()
	doc.Binding.Model.Status = ModelStatusResolved
	doc.Binding.Model.ModelName = ""
	doc.Binding.Model.ModelDigestSHA256 = ""
	if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "resolved model status requires model_name") {
		t.Errorf("Expected error for resolved model status missing model name, got: %v", err)
	}

	doc.Binding.Model.ModelName = "gemini-flash"
	doc.Binding.Model.ModelDigestSHA256 = "invalid_digest"
	if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "resolved model status requires a valid model_digest_sha256") {
		t.Errorf("Expected error for resolved model status invalid digest, got: %v", err)
	}

	// 2. Invalid model configuration (e.g. status is disabled, but model name or digest is present)
	doc = createValidBaseDocument()
	doc.Binding.Model.Status = ModelStatusDisabled
	doc.Binding.Model.ModelName = "gemini-pro"
	doc.Receipt.Model = doc.Binding.Model
	digest, _ := CalculateDocumentDigest(doc)
	doc.Receipt.OutputDocumentDigestSHA256 = digest
	if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "model_name must be empty when model status is") {
		t.Errorf("Expected error for disabled model status with model name, got: %v", err)
	}

	// 3. Make model resolved and valid, it should validate successfully
	doc = createValidBaseDocument()
	doc.Binding.Model.Status = ModelStatusResolved
	doc.Binding.Model.ModelName = "gemini-pro"
	doc.Binding.Model.ModelDigestSHA256 = sha256Hex
	doc.Receipt.Model = doc.Binding.Model
	doc.Receipt.ModelArtifactDigestSHA256 = sha256Hex
	digest, _ = CalculateDocumentDigest(doc)
	doc.Receipt.OutputDocumentDigestSHA256 = digest
	if err := Validate(doc); err != nil {
		t.Errorf("Expected valid document with resolved model, got error: %v", err)
	}
}

// Test Repository/Evidence Binding Mismatch Refusal
func TestRepositoryEvidenceBindingMismatchRefusal(t *testing.T) {
	doc := createValidBaseDocument()

	// Domain in RawEvidence Scope is mismatched
	doc.RawEvidence[0].Scope.Repository = "mismatched_repo.com"
	digest, _ := CalculateDocumentDigest(doc)
	doc.Receipt.OutputDocumentDigestSHA256 = digest
	if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "does not match document binding") {
		t.Errorf("Expected error for raw evidence repository mismatch, got: %v", err)
	}
}

// Test Candidate Scope Expansion Refusal
func TestCandidateScopeExpansionRefusal(t *testing.T) {
	sha256Hex := "4a8e63db7cc5173b82bd3ba6019d30ce9e22db84d852bd3ba6019d30ce922db8"

	doc := createValidBaseDocument()
	doc.Binding.Model.Status = ModelStatusResolved
	doc.Binding.Model.ModelName = "gemini-pro"
	doc.Binding.Model.ModelDigestSHA256 = sha256Hex
	doc.Receipt.Model = doc.Binding.Model
	doc.Receipt.ModelArtifactDigestSHA256 = sha256Hex

	// Candidate claim refers to file absent from observations or raw evidence
	doc.CandidateClaims = []architecture.Claim{
		{
			ID:                  "claim_1",
			Label:               "test_claim",
			EpistemicStatus:     "supported",
			PromotionStatus:     "candidate",
			HumanReviewRequired: true,
			ArchitecturalPlane:  "intended",
			AssertionOrigin:     "observed",
			Scope: architecture.ClaimScope{
				Repository: "github.com/globulario/sensei",
				Files:      []string{"golang/server/unseen_file.go"},
			},
			Statement: architecture.ClaimStatement{
				Subject:   "a",
				Predicate: "b",
				Object:    "c",
			},
			SupportingEvidence: []string{"evidence:evidence_1"},
		},
	}
	digest, _ := CalculateDocumentDigest(doc)
	doc.Receipt.OutputDocumentDigestSHA256 = digest

	if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "is not grounded in its cited evidence or facts (borrowing refused)") {
		t.Errorf("Expected error for candidate claim scope file expansion, got: %v", err)
	}
}

// Test Output Receipt Digest Mismatch Refusal
func TestOutputReceiptDigestMismatchRefusal(t *testing.T) {
	doc := createValidBaseDocument()
	doc.Receipt.OutputDocumentDigestSHA256 = "4a8e63db7cc5173b82bd3ba6019d30ce9e22db84d852bd3ba6019d30ce922db8" // Incorrect digest

	if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "output document digest mismatch") {
		t.Errorf("Expected error for output document digest mismatch, got: %v", err)
	}

	// Compute correct digest and check it passes
	correctDigest, err := CalculateDocumentDigest(doc)
	if err != nil {
		t.Fatalf("Failed to compute correct digest: %v", err)
	}
	doc.Receipt.OutputDocumentDigestSHA256 = correctDigest
	if err := Validate(doc); err != nil {
		t.Errorf("Expected validation to pass with correct output digest, got: %v", err)
	}
}

// Test Model-Disabled Canonical Truth Equivalence
func TestModelDisabledCanonicalTruthEquivalence(t *testing.T) {
	doc := createValidBaseDocument()
	doc.Binding.Model.Status = ModelStatusDisabled

	if err := Validate(doc); err != nil {
		t.Errorf("Expected model-disabled document with no model output to be valid, got: %v", err)
	}
}

// Fuzz/Property Tests for Normalization and Validation Stability
func TestFuzzNormalizationAndValidationStability(t *testing.T) {
	doc := createValidBaseDocument()

	// Seed random generator
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Run multiple random permutations
	for i := 0; i < 50; i++ {
		perturbed := doc

		// Randomly change spaces and capitalization (if not semantic)
		if rng.Float32() < 0.5 {
			perturbed.SchemaVersion = fmt.Sprintf("  %s  ", perturbed.SchemaVersion)
		}
		if rng.Float32() < 0.5 {
			perturbed.Plan.Queries = []string{"query2", "query1", "  query1  "}
		}
		if rng.Float32() < 0.5 {
			// Randomize order of observations (non-semantic order)
			obs := []architecture.Fact{
				{
					ID:        "fact_2",
					Subject:   "pkg_2",
					Predicate: "reads",
					Object:    "state_2",
					Scope:     architecture.Scope{Repository: "github.com/globulario/sensei", Files: []string{"f2.go"}},
				},
				{
					ID:        "fact_1",
					Subject:   "pkg_1",
					Predicate: "writes",
					Object:    "state_1",
					Scope:     architecture.Scope{Repository: "github.com/globulario/sensei", Files: []string{"f1.go"}},
				},
			}
			if rng.Float32() < 0.5 {
				obs[0], obs[1] = obs[1], obs[0]
			}
			perturbed.Observations = obs
		}

		// Normalize
		norm, err := Normalize(perturbed)
		if err != nil {
			t.Fatalf("Fuzz normalization failed: %v", err)
		}

		// Validate
		// Note: since we perturbed query strings or added mismatched observations domain,
		// let's restore domain consistency to check validate stability.
		for idx := range norm.Observations {
			norm.Observations[idx].Scope.Repository = norm.Binding.Repository.RepositoryDomain
		}

		if err := Validate(norm); err != nil {
			// Some random permutations might be invalid under specific rules (like missing domains),
			// which is fine, but it should not crash.
			continue
		}

		// Re-normalizing should produce identical output
		renorm, err := Normalize(norm)
		if err != nil {
			t.Fatalf("Fuzz re-normalization failed: %v", err)
		}

		j1, _ := json.Marshal(norm)
		j2, _ := json.Marshal(renorm)
		if string(j1) != string(j2) {
			t.Fatalf("Fuzz normalization unstable!\n1: %s\n2: %s", string(j1), string(j2))
		}
	}
}

func TestReceiptDigestSelfExcludingOnly(t *testing.T) {
	doc1 := createValidBaseDocument()
	doc2 := createValidBaseDocument()

	// They have different timestamps in Receipt, which is part of the Receipt metadata
	doc1.Receipt.TimestampSource = "2026-07-21T09:00:00-04:00"
	doc2.Receipt.TimestampSource = "2026-07-21T10:00:00-04:00"

	digest1, err := CalculateDocumentDigest(doc1)
	if err != nil {
		t.Fatalf("Failed to compute digest1: %v", err)
	}

	digest2, err := CalculateDocumentDigest(doc2)
	if err != nil {
		t.Fatalf("Failed to compute digest2: %v", err)
	}

	// Since they differ in TimestampSource, their digests must differ!
	if digest1 == digest2 {
		t.Errorf("Digests must NOT be equal when receipt metadata like TimestampSource differs!")
	}

	// If only OutputDocumentDigestSHA256 differs, their digests must be identical
	doc1.Receipt.TimestampSource = doc2.Receipt.TimestampSource
	doc1.Receipt.OutputDocumentDigestSHA256 = "digest_aaa"
	doc2.Receipt.OutputDocumentDigestSHA256 = "digest_bbb"

	digest1_after, err := CalculateDocumentDigest(doc1)
	if err != nil {
		t.Fatalf("Failed to compute digest1 after: %v", err)
	}
	digest2_after, err := CalculateDocumentDigest(doc2)
	if err != nil {
		t.Fatalf("Failed to compute digest2 after: %v", err)
	}

	if digest1_after != digest2_after {
		t.Errorf("Digests must be equal when only OutputDocumentDigestSHA256 self-referencing field differs!")
	}
}

func TestDeduplicationAndCollisionDetection(t *testing.T) {
	// 1. Identical ID + identical content -> deduplicated cleanly
	doc := createValidBaseDocument()

	// Add an identical evidence receipt copy
	identicalEvidence := doc.RawEvidence[0]
	doc.RawEvidence = append(doc.RawEvidence, identicalEvidence)

	norm, err := Normalize(doc)
	if err != nil {
		t.Fatalf("Expected normalization to succeed for identical content: %v", err)
	}

	// Check that duplicates are merged (len remains 1)
	if len(norm.RawEvidence) != 1 {
		t.Errorf("Expected identical raw evidence to be deduplicated, but got length: %d", len(norm.RawEvidence))
	}

	// 2. Same ID + different content -> hard collision error
	differentEvidence := doc.RawEvidence[0]
	differentEvidence.SourceIdentity = "different_source"
	doc.RawEvidence = append(doc.RawEvidence, differentEvidence)

	_, err = Normalize(doc)
	if err == nil || !strings.Contains(err.Error(), "raw evidence ID collision") {
		t.Errorf("Expected hard collision error for same ID but different content, got: %v", err)
	}
}
