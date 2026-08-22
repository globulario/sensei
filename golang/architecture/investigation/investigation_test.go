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
			Model:                         DisabledModelBinding(),
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
				ContentDigestSHA256: "18a82ad0428f38eb219034afff34d4995f7492ee667476da6717f0ba472c175f",
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
			Model:                        DisabledModelBinding(),
			PostProcessingVersion:        "1.0",
			TimestampSource:              "2026-07-21T09:29:53-04:00",
			ResourceLimits:               map[string]string{"cpu_seconds": "10"},
			NondeterminismDeclaration:    "pure_deterministic",
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
	doc.Binding.Model = DisabledModelBinding()
	doc.Binding.Model.ModelName = "gemini-pro"
	doc.Receipt.Model = doc.Binding.Model
	digest, _ := CalculateDocumentDigest(doc)
	doc.Receipt.OutputDocumentDigestSHA256 = digest
	if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "model_name must be empty when model status is") {
		t.Errorf("Expected error for disabled model status with model name, got: %v", err)
	}

	// 3. A resolved binding that carries every execution identity validates.
	//    A model name and digest alone no longer do: naming a model is not
	//    evidence that one ran (#256).
	doc = createValidBaseDocument()
	doc.Binding.Model = resolvedModelFixture(sha256Hex)
	doc.Receipt.Model = doc.Binding.Model
	doc.Receipt.ModelArtifactDigestSHA256 = doc.Binding.Model.ArtifactDigestSHA256
	digest, _ = CalculateDocumentDigest(doc)
	doc.Receipt.OutputDocumentDigestSHA256 = digest
	if err := Validate(doc); err != nil {
		t.Errorf("Expected valid document with resolved model, got error: %v", err)
	}
}

// resolvedModelFixture is a binding that records a genuinely observed
// execution: who ran, what ran, the exact request, the accepted artifact, and
// what may differ on replay.
func resolvedModelFixture(sha string) ModelBinding {
	return ModelBinding{
		Status:                    ModelStatusResolved,
		Provider:                  ProviderBinding{ID: "fake", Version: "v1"},
		ModelName:                 "gemini-pro",
		ModelDigestSHA256:         sha,
		RequestDigestSHA256:       sha,
		ArtifactDigestSHA256:      sha,
		NondeterminismDeclaration: "model_response_not_replayable",
	}
}

// TestResolvedModelStatusCannotBeConfigured is the #256 proof that resolved is
// earned rather than set. Each case is a caller declaring success while missing
// one identity that only an actual execution could supply.
func TestResolvedModelStatusCannotBeConfigured(t *testing.T) {
	sha := "4a8e63db7cc5173b82bd3ba6019d30ce9e22db84d852bd3ba6019d30ce922db8"
	for _, tc := range []struct {
		name    string
		mutate  func(*ModelBinding)
		wantErr string
	}{
		{"no provider", func(m *ModelBinding) { m.Provider.ID = "" }, "requires a provider id"},
		{"no provider version", func(m *ModelBinding) { m.Provider.Version = "" }, "requires a provider version"},
		{"no request digest", func(m *ModelBinding) { m.RequestDigestSHA256 = "" }, "requires the exact request digest"},
		{"no artifact digest", func(m *ModelBinding) { m.ArtifactDigestSHA256 = "" }, "requires the accepted artifact digest"},
		{"no nondeterminism declaration", func(m *ModelBinding) { m.NondeterminismDeclaration = "" }, "requires an explicit nondeterminism declaration"},
		{"no model identity at all", func(m *ModelBinding) { m.ModelDigestSHA256 = "" }, "requires either a model digest or the typed absence"},
		{"digest and typed absence both claimed", func(m *ModelBinding) { m.ModelDigestAbsence = ModelDigestAbsent }, "cannot both be present"},
		{"success carrying a failure reason", func(m *ModelBinding) { m.Reason = ModelReasonProviderRefused }, "must not carry a failure reason"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := resolvedModelFixture(sha)
			tc.mutate(&m)
			errs := ValidateModelBinding(m)
			if len(errs) == 0 {
				t.Fatalf("a caller manufactured %q with %s", ModelStatusResolved, tc.name)
			}
			if !strings.Contains(strings.Join(errs, "; "), tc.wantErr) {
				t.Errorf("errors %v do not report %q", errs, tc.wantErr)
			}
		})
	}
}

// TestNonResolvedStatusCannotCarryExecutionEvidence is the other half: a status
// must not describe a run it cannot have had. Without this, "unavailable" could
// carry an artifact and "disabled" could name a provider.
func TestNonResolvedStatusCannotCarryExecutionEvidence(t *testing.T) {
	sha := "4a8e63db7cc5173b82bd3ba6019d30ce9e22db84d852bd3ba6019d30ce922db8"
	for _, tc := range []struct {
		name    string
		binding ModelBinding
		wantErr string
	}{
		{"unavailable with an accepted artifact",
			ModelBinding{Status: ModelStatusUnavailable, Reason: ModelReasonProviderUnknown, ArtifactDigestSHA256: sha},
			"artifact_digest_sha256 must be empty"},
		{"unavailable claiming a request was sent",
			ModelBinding{Status: ModelStatusUnavailable, Reason: ModelReasonProviderUnknown, RequestDigestSHA256: sha},
			"no request was sent"},
		{"disabled naming a provider",
			ModelBinding{Status: ModelStatusDisabled, Reason: ModelReasonCapabilityDisabled, Provider: ProviderBinding{ID: "fake", Version: "v1"}},
			"provider identity must be empty"},
		{"absence with no typed reason",
			ModelBinding{Status: ModelStatusErrored},
			"requires a typed reason"},
		{"invoked without the request that was sent",
			ModelBinding{Status: ModelStatusRefused, Reason: ModelReasonProviderRefused, Provider: ProviderBinding{ID: "fake", Version: "v1"}},
			"the exact request digest is required"},
		{"refused claiming a nondeterminism declaration",
			ModelBinding{Status: ModelStatusRefused, Reason: ModelReasonProviderRefused, Provider: ProviderBinding{ID: "fake", Version: "v1"}, RequestDigestSHA256: sha, NondeterminismDeclaration: "x"},
			"nondeterminism_declaration must be empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := ValidateModelBinding(tc.binding)
			if len(errs) == 0 {
				t.Fatalf("%s was accepted", tc.name)
			}
			if !strings.Contains(strings.Join(errs, "; "), tc.wantErr) {
				t.Errorf("errors %v do not report %q", errs, tc.wantErr)
			}
		})
	}
}

// TestBindingAndReceiptCannotTellTwoStories: RunReceipt carries its own Model
// plus a separate ModelArtifactDigestSHA256. Two fields describing one artifact
// is exactly the shape that drifts, so disagreement must fail closed.
func TestBindingAndReceiptCannotTellTwoStories(t *testing.T) {
	sha := "4a8e63db7cc5173b82bd3ba6019d30ce9e22db84d852bd3ba6019d30ce922db8"
	other := "1111111111111111111111111111111111111111111111111111111111111111"
	binding := resolvedModelFixture(sha)

	if errs := ValidateModelExecutionAgreement(binding, RunReceipt{Model: binding, ModelArtifactDigestSHA256: sha}); len(errs) != 0 {
		t.Fatalf("agreeing records rejected: %v", errs)
	}
	receipt := RunReceipt{Model: binding, ModelArtifactDigestSHA256: other}
	if errs := ValidateModelExecutionAgreement(binding, receipt); len(errs) == 0 {
		t.Error("a receipt artifact digest disagreeing with the binding was accepted")
	}
	drifted := binding
	drifted.Status = ModelStatusErrored
	if errs := ValidateModelExecutionAgreement(binding, RunReceipt{Model: drifted, ModelArtifactDigestSHA256: sha}); len(errs) == 0 {
		t.Error("a receipt reporting a different model status was accepted")
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
	doc.Receipt.OutputCandidateIDsAndDigests = map[string]string{
		"claim_1": sha256Hex,
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
	doc.Binding.Model = DisabledModelBinding()
	doc.Receipt.Model = doc.Binding.Model
	digest, _ := CalculateDocumentDigest(doc)
	doc.Receipt.OutputDocumentDigestSHA256 = digest

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

func TestContradictionPreservation(t *testing.T) {
	// Mixed evidence supporting and refuting the same claim must coexist and normalize/validate successfully
	doc := createValidBaseDocument()

	// Add an evidence receipt for refuting evidence
	sha256Hex := "4a8e63db7cc5173b82bd3ba6019d30ce9e22db84d852bd3ba6019d30ce922db8"
	refutingEvidence := EvidenceReceipt{
		ID:                  "evidence_refute",
		Category:            EvidenceSourceControl,
		Provider:            ProviderBinding{ID: "git_history", Version: "1.0"},
		ProofStrength:       ProofStaticSource,
		SourceIdentity:      "git_commit_refute",
		SourceDigestSHA256:  sha256Hex,
		ContentDigestSHA256: "18a82ad0428f38eb219034afff34d4995f7492ee667476da6717f0ba472c175f",
		CapturedContent:     "Fixed a bug in reload logic",
		CapturedAt:          "2026-07-21T09:29:53-04:00",
		Scope: architecture.ClaimScope{
			Repository: "github.com/globulario/sensei",
			Files:      []string{"golang/server/reload.go"},
		},
	}
	doc.RawEvidence = append(doc.RawEvidence, refutingEvidence)

	doc.CandidateClaims = []architecture.Claim{
		{
			ID:                  "claim_1",
			Label:               "test_claim",
			EpistemicStatus:     "contested", // status indicates mixed/contradictory evidence
			PromotionStatus:     "candidate",
			HumanReviewRequired: true,
			ArchitecturalPlane:  "intended",
			AssertionOrigin:     "observed",
			Scope: architecture.ClaimScope{
				Repository: "github.com/globulario/sensei",
				Files:      []string{"golang/server/reload.go"},
			},
			Statement: architecture.ClaimStatement{
				Subject:   "a",
				Predicate: "b",
				Object:    "c",
			},
			SupportingEvidence: []string{"evidence:evidence_1"},
			RefutingEvidence:   []string{"evidence:evidence_refute"},
		},
	}

	doc.Receipt.OutputCandidateIDsAndDigests = map[string]string{
		"claim_1": sha256Hex,
	}

	normalized, err := Normalize(doc)
	if err != nil {
		t.Fatalf("Expected normalization of contested claim to pass: %v", err)
	}

	digest, _ := CalculateDocumentDigest(normalized)
	normalized.Receipt.OutputDocumentDigestSHA256 = digest

	if err := Validate(normalized); err != nil {
		t.Errorf("Expected validation to pass, showing supporting and refuting evidence coexist: %v", err)
	}
}

func TestConfidenceIndependentCandidateIdentity(t *testing.T) {
	// Stable ID / candidate identity must NOT depend on confidence
	claim1 := architecture.Claim{
		ID:                  "claim_1",
		Label:               "test_claim",
		EpistemicStatus:     "supported",
		PromotionStatus:     "candidate",
		HumanReviewRequired: true,
		Confidence:          0.2, // Low confidence
		ArchitecturalPlane:  "intended",
		AssertionOrigin:     "observed",
		Scope: architecture.ClaimScope{
			Repository: "github.com/globulario/sensei",
			Files:      []string{"golang/server/reload.go"},
		},
		Statement: architecture.ClaimStatement{
			Subject:   "a",
			Predicate: "b",
			Object:    "c",
		},
	}

	claim2 := claim1
	claim2.Confidence = 0.95 // High confidence

	id1 := architecture.StableClaimID(claim1)
	id2 := architecture.StableClaimID(claim2)

	if id1 != id2 {
		t.Errorf("StableClaimID must be independent of Confidence! got %s vs %s", id1, id2)
	}
}

func TestProofStrengthIndependenceFromConfidence(t *testing.T) {
	// ProofStrength is independent of Confidence and both can coexist in any combination
	doc := createValidBaseDocument()

	// High confidence (0.99) but low proof strength (P0 - assertion only)
	doc.RawEvidence[0].ProofStrength = ProofAssertionOnly
	doc.CandidateClaims = []architecture.Claim{
		{
			ID:                  "claim_1",
			Label:               "test_claim",
			EpistemicStatus:     "supported",
			PromotionStatus:     "candidate",
			HumanReviewRequired: true,
			Confidence:          0.99,
			ArchitecturalPlane:  "intended",
			AssertionOrigin:     "observed",
			Scope: architecture.ClaimScope{
				Repository: "github.com/globulario/sensei",
				Files:      []string{"golang/server/reload.go"},
			},
			Statement: architecture.ClaimStatement{
				Subject:   "a",
				Predicate: "b",
				Object:    "c",
			},
			SupportingEvidence: []string{"evidence:evidence_1"},
		},
	}

	sha256Hex := "4a8e63db7cc5173b82bd3ba6019d30ce9e22db84d852bd3ba6019d30ce922db8"
	doc.Receipt.OutputCandidateIDsAndDigests = map[string]string{
		"claim_1": sha256Hex,
	}

	digest, _ := CalculateDocumentDigest(doc)
	doc.Receipt.OutputDocumentDigestSHA256 = digest

	if err := Validate(doc); err != nil {
		t.Errorf("Expected high confidence + none proof strength to be valid: %v", err)
	}

	// Low confidence (0.01) but high proof strength (P5 - static source proof)
	doc.RawEvidence[0].ProofStrength = ProofStaticSource
	doc.CandidateClaims[0].Confidence = 0.01

	digest, _ = CalculateDocumentDigest(doc)
	doc.Receipt.OutputDocumentDigestSHA256 = digest

	if err := Validate(doc); err != nil {
		t.Errorf("Expected low confidence + static source proof strength to be valid: %v", err)
	}
}

func TestClaimScopeComponentsAndSourceSetGrounding(t *testing.T) {
	doc := createValidBaseDocument()
	doc.RawEvidence[0].Scope.Components = []string{"component_a"}
	doc.RawEvidence[0].Scope.SourceSet = "sourceset_x"

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
				Files:      []string{"golang/server/reload.go"},
				Components: []string{"component_b"}, // Not grounded!
				SourceSet:  "sourceset_x",
			},
			Statement: architecture.ClaimStatement{
				Subject:   "a",
				Predicate: "b",
				Object:    "c",
			},
			SupportingEvidence: []string{"evidence:evidence_1"},
		},
	}

	sha256Hex := "4a8e63db7cc5173b82bd3ba6019d30ce9e22db84d852bd3ba6019d30ce922db8"
	doc.Receipt.OutputCandidateIDsAndDigests = map[string]string{
		"claim_1": sha256Hex,
	}

	digest, _ := CalculateDocumentDigest(doc)
	doc.Receipt.OutputDocumentDigestSHA256 = digest

	// Expect grounding failure on component_b
	if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "scope component \"component_b\" is not grounded") {
		t.Errorf("Expected grounding error on component_b, got: %v", err)
	}

	// Correct component scope
	doc.CandidateClaims[0].Scope.Components = []string{"component_a"}
	// Ground sourceset to incorrect value
	doc.CandidateClaims[0].Scope.SourceSet = "sourceset_y" // Not grounded!

	digest, _ = CalculateDocumentDigest(doc)
	doc.Receipt.OutputDocumentDigestSHA256 = digest

	// Expect grounding failure on sourceset_y
	if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "scope source set \"sourceset_y\" is not grounded") {
		t.Errorf("Expected grounding error on sourceset_y, got: %v", err)
	}

	// Correct sourceset scope
	doc.CandidateClaims[0].Scope.SourceSet = "sourceset_x"
	digest, _ = CalculateDocumentDigest(doc)
	doc.Receipt.OutputDocumentDigestSHA256 = digest

	// Expect valid grounding to succeed
	if err := Validate(doc); err != nil {
		t.Errorf("Expected validation to pass with grounded components and source set, got: %v", err)
	}
}

func TestRawEvidencePreservesExactCapturedContentAndDigestValidation(t *testing.T) {
	doc := createValidBaseDocument()

	// 1. Set CapturedContent containing leading spaces, trailing spaces, and a newline
	rawContent := "  exact statement\n"
	doc.RawEvidence[0].CapturedContent = rawContent
	contentHash := SHA256String(rawContent)
	doc.RawEvidence[0].ContentDigestSHA256 = contentHash

	// Normalize
	normalized, err := Normalize(doc)
	if err != nil {
		t.Fatalf("Expected normalization to pass: %v", err)
	}

	// Prove normalization preserves the exact string (without trimming)
	if normalized.RawEvidence[0].CapturedContent != rawContent {
		t.Errorf("Normalization trimmed CapturedContent! Expected %q, got %q", rawContent, normalized.RawEvidence[0].CapturedContent)
	}

	// Recalculate document digest and assert validation passes
	digest, _ := CalculateDocumentDigest(normalized)
	normalized.Receipt.OutputDocumentDigestSHA256 = digest
	if err := Validate(normalized); err != nil {
		t.Errorf("Expected validation to pass with exact untrimmed CapturedContent, got error: %v", err)
	}

	// 2. A one-byte mutation of CapturedContent invalidates the content binding
	normalized.RawEvidence[0].CapturedContent = " exact statement\n" // removed one space

	// Recalculate digest and assert validation fails
	digest, _ = CalculateDocumentDigest(normalized)
	normalized.Receipt.OutputDocumentDigestSHA256 = digest
	if err := Validate(normalized); err == nil || !strings.Contains(err.Error(), "content digest mismatch") {
		t.Errorf("Expected validation to fail with content digest mismatch after one-byte mutation, got: %v", err)
	}
}

// Coverage entries resolve their evidence IDs through an index now, rather than
// by scanning every receipt. Profiled over a whole-repository self-extraction
// (233,479 receipts) that scan was 52% of the entire run's CPU — quadratic in
// the size of the document being validated. These pin the behaviour the index
// has to preserve, since a lookup that resolves the wrong receipt, or resolves
// one that should not have been found, would be a silent validation hole rather
// than a visible failure.
func TestCoverageEvidenceResolutionSurvivesTheIndex(t *testing.T) {
	t.Run("an unresolvable evidence ID is refused", func(t *testing.T) {
		doc := createValidBaseDocument()
		doc.Coverage[0].ResultEvidenceIDs = append(doc.Coverage[0].ResultEvidenceIDs, "evidence_nonexistent")
		if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "does not resolve to raw evidence") {
			t.Fatalf("want unresolved-evidence refusal, got %v", err)
		}
	})

	t.Run("a resolved receipt is checked against its coverage entry", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			mutate func(*Document)
			want   string
		}{
			{"provider id", func(d *Document) { d.RawEvidence[0].Provider.ID = "other_provider" }, "provider ID"},
			{"provider version", func(d *Document) { d.RawEvidence[0].Provider.Version = "v9.9.9" }, "provider version"},
			{"category", func(d *Document) { d.RawEvidence[0].Category = EvidenceTests }, "category"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				doc := createValidBaseDocument()
				if len(doc.Coverage) == 0 || len(doc.Coverage[0].ResultEvidenceIDs) == 0 {
					t.Skip("fixture carries no resolved coverage evidence")
				}
				tc.mutate(&doc)
				err := Validate(doc)
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("want a %s mismatch refusal, got %v", tc.want, err)
				}
			})
		}
	})

	t.Run("a duplicate evidence ID in one entry is refused", func(t *testing.T) {
		doc := createValidBaseDocument()
		if len(doc.Coverage) == 0 || len(doc.Coverage[0].ResultEvidenceIDs) == 0 {
			t.Skip("fixture carries no resolved coverage evidence")
		}
		id := doc.Coverage[0].ResultEvidenceIDs[0]
		doc.Coverage[0].ResultEvidenceIDs = append(doc.Coverage[0].ResultEvidenceIDs, id)
		if err := Validate(doc); err == nil || !strings.Contains(err.Error(), "duplicate result evidence ID") {
			t.Fatalf("want duplicate-ID refusal, got %v", err)
		}
	})

	t.Run("a document whose coverage resolves cleanly still validates", func(t *testing.T) {
		if err := Validate(createValidBaseDocument()); err != nil {
			t.Fatalf("the base fixture stopped validating: %v", err)
		}
	})
}
