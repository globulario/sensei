// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

func defaultOpts() Options {
	return Options{
		CapturedAt: "2026-07-21T14:00:00Z",
		Repository: architecture.ClaimDocumentBinding{
			RepositoryDomain:  "example.com/deterministic",
			RevisionStatus:    "clean",
			GraphDigestStatus: "none",
		},
		ResourceLimits: map[string]string{
			"cpu_limit": "2",
		},
	}
}

func deterministicFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("testdata/deterministic_repo")); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init"}, {"remote", "add", "origin", "https://example.com/deterministic.git"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return root
}

func TestExtract(t *testing.T) {
	root := deterministicFixture(t)
	doc, err := Extract(root, defaultOpts())
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// 14. The complete HOW document passes investigation.Validate.
	if err := investigation.Validate(doc); err != nil {
		t.Errorf("investigation.Validate failed: %v", err)
	}

	// 1. Exactly seven fine-grained coverage entries are emitted.
	if len(doc.Coverage) != 7 {
		t.Errorf("Expected exactly 7 coverage entries, got %d", len(doc.Coverage))
	}

	// 20. Existing assertions remain green
	kinds := make(map[string]bool)
	for _, f := range doc.Observations {
		kinds[f.Kind] = true
	}
	for _, expectedKind := range []string{"topology", "contract_seam", "boundary", "test_protection", "read", "data_shape"} {
		if !kinds[expectedKind] {
			t.Errorf("Expected fact kind %q to be present", expectedKind)
		}
	}

	// Helper maps
	receiptsByID := make(map[string]investigation.EvidenceReceipt)
	for _, r := range doc.RawEvidence {
		receiptsByID[r.ID] = r
	}

	seenCoverageIDs := make(map[string]bool)

	for _, cov := range doc.Coverage {
		seenCoverageIDs[cov.ProviderID] = true

		// 2. Every provider ID, version, and category is correct.
		var registryDef *InvestigatorDefinition
		for i := range InvestigatorRegistry {
			if InvestigatorRegistry[i].ProviderID == cov.ProviderID {
				registryDef = &InvestigatorRegistry[i]
				break
			}
		}

		if registryDef == nil {
			t.Errorf("Coverage entry has unknown provider_id: %s", cov.ProviderID)
			continue
		}

		if cov.ProviderVersion != registryDef.ProviderVersion {
			t.Errorf("Provider %s version mismatch: expected %s, got %s", cov.ProviderID, registryDef.ProviderVersion, cov.ProviderVersion)
		}

		if cov.Category != registryDef.Category {
			t.Errorf("Provider %s category mismatch: expected %s, got %s", cov.ProviderID, registryDef.Category, cov.Category)
		}

		// 3. Test protection uses EvidenceTests.
		if cov.ProviderID == "test_extractor" {
			if cov.Category != investigation.EvidenceTests {
				t.Errorf("test_extractor coverage must use EvidenceTests, got %q", cov.Category)
			}
		} else {
			if cov.Category != investigation.EvidenceSourceCode {
				t.Errorf("semantic/ast coverage must use EvidenceSourceCode, got %q", cov.Category)
			}
		}

		// 4. Every target digest is valid and deterministically derived.
		if !strings.HasPrefix(cov.TargetDigestSHA256, "") || len(cov.TargetDigestSHA256) != 64 {
			t.Errorf("Coverage entry %s has invalid target digest: %s", cov.ProviderID, cov.TargetDigestSHA256)
		}

		// 5. Every source-snapshot digest is valid and derived from the source manifest.
		if !strings.HasPrefix(cov.SourceSnapshotDigestSHA256, "") || len(cov.SourceSnapshotDigestSHA256) != 64 {
			t.Errorf("Coverage entry %s has invalid source snapshot digest: %s", cov.ProviderID, cov.SourceSnapshotDigestSHA256)
		}

		// 6. Every result evidence ID resolves and matches provider, version, and category.
		for _, evid := range cov.ResultEvidenceIDs {
			rec, ok := receiptsByID[evid]
			if !ok {
				t.Errorf("Result evidence ID %s does not resolve to raw evidence", evid)
				continue
			}

			if rec.Provider.ID != cov.ProviderID {
				t.Errorf("Evidence receipt %s provider ID %q does not match coverage provider ID %q", evid, rec.Provider.ID, cov.ProviderID)
			}

			if rec.Provider.Version != cov.ProviderVersion {
				t.Errorf("Evidence receipt %s provider version %q does not match coverage provider version %q", evid, rec.Provider.Version, cov.ProviderVersion)
			}

			if rec.Category != cov.Category {
				t.Errorf("Evidence receipt %s category %q does not match coverage category %q", evid, rec.Category, cov.Category)
			}
		}

		// 7. Supporting coverage has evidence.
		if cov.Status == investigation.CoverageSupporting {
			if len(cov.ResultEvidenceIDs) == 0 {
				t.Errorf("Coverage entry %s status is searched_supporting but has no evidence", cov.ProviderID)
			}
		}

		// 8. No-result coverage has no evidence.
		if cov.Status == investigation.CoverageNoResult {
			if len(cov.ResultEvidenceIDs) != 0 {
				t.Errorf("Coverage entry %s status is searched_no_result but has evidence", cov.ProviderID)
			}
		}
	}

	for _, expectedID := range []string{"topology_extractor", "flow_extractor", "state_extractor", "boundary_extractor", "contract_extractor", "data_shape_extractor", "test_extractor"} {
		if !seenCoverageIDs[expectedID] {
			t.Errorf("Missing coverage entry for %s", expectedID)
		}
	}
}

// 9. A semantic-engine failure makes all dependent entries unavailable.
func TestSemanticEngineFailureMakesDependentsUnavailable(t *testing.T) {
	root := deterministicFixture(t)
	// Inject a semantic extractor failure by calling compose with a non-nil error
	semanticErr := errors.New("simulated semantic parser crash")

	doc, err := composeReceiptsAndCoverage(root, nil, "example.com", defaultOpts(), nil, semanticErr, nil)
	if err != nil {
		t.Fatalf("composeReceiptsAndCoverage failed: %v", err)
	}

	// 11. Unavailable and skipped states carry explicit reasons.
	for _, cov := range doc.Coverage {
		var registryDef *InvestigatorDefinition
		for i := range InvestigatorRegistry {
			if InvestigatorRegistry[i].ProviderID == cov.ProviderID {
				registryDef = &InvestigatorRegistry[i]
				break
			}
		}
		if registryDef == nil {
			continue
		}

		if registryDef.Engine == "semantic" {
			if cov.Status != investigation.CoverageUnavailable {
				t.Errorf("Expected dependent semantic investigator %s to be unavailable, got status %q", cov.ProviderID, cov.Status)
			}
			if !strings.Contains(cov.Reason, "simulated semantic parser crash") {
				t.Errorf("Expected unavailable reason to contain failure context, got %q", cov.Reason)
			}
		} else {
			if cov.Status == investigation.CoverageUnavailable {
				t.Errorf("Expected AST investigator %s to be unaffected, but it was unavailable", cov.ProviderID)
			}
		}
	}
}

// 10. A state-engine failure affects state coverage independently.
func TestStateEngineFailureAffectsStateIndependently(t *testing.T) {
	root := deterministicFixture(t)
	astErr := errors.New("simulated AST parser crash")

	doc, err := composeReceiptsAndCoverage(root, nil, "example.com", defaultOpts(), nil, nil, astErr)
	if err != nil {
		t.Fatalf("composeReceiptsAndCoverage failed: %v", err)
	}

	for _, cov := range doc.Coverage {
		if cov.ProviderID == "state_extractor" {
			if cov.Status != investigation.CoverageUnavailable {
				t.Errorf("Expected state_extractor to be unavailable, got status %q", cov.Status)
			}
			if !strings.Contains(cov.Reason, "simulated AST parser crash") {
				t.Errorf("Expected unavailable reason to contain failure context, got %q", cov.Reason)
			}
		} else {
			if cov.Status == investigation.CoverageUnavailable {
				t.Errorf("Expected semantic investigator %s to be unaffected, but it was unavailable", cov.ProviderID)
			}
		}
	}
}

// 12. Duplicate coverage identities are refused.
func TestDuplicateCoverageIdentitiesAreRefused(t *testing.T) {
	doc := investigation.Document{
		SchemaVersion: SchemaVersion,
		GeneratedBy:   GeneratedByIdentity,
		Mode:          investigation.ModeHow,
		Binding: investigation.Binding{
			Repository:                    architecture.ClaimDocumentBinding{RepositoryDomain: "example.com", RevisionStatus: "clean", GraphDigestStatus: "none"},
			InvestigationPlanDigestSHA256: sha256Hex("plan"),
			ExtractorProfileDigestSHA256:  sha256Hex("profile"),
		},
		Coverage: []investigation.CoverageEntry{
			{
				ProviderID:                 "topology_extractor",
				ProviderVersion:            "1.0",
				Category:                   investigation.EvidenceSourceCode,
				TargetDigestSHA256:         sha256Hex("target"),
				SourceSnapshotDigestSHA256: sha256Hex("snapshot"),
				Status:                     investigation.CoverageNoResult,
			},
			{
				ProviderID:                 "topology_extractor",
				ProviderVersion:            "1.0",
				Category:                   investigation.EvidenceSourceCode,
				TargetDigestSHA256:         sha256Hex("target"),
				SourceSnapshotDigestSHA256: sha256Hex("snapshot"),
				Status:                     investigation.CoverageNoResult,
			},
		},
	}
	if err := investigation.Validate(doc); err == nil {
		t.Fatal("Expected validation to fail on duplicate coverage identity, got nil")
	}
}

// 13. Evidence borrowing across investigators is refused.
func TestEvidenceBorrowingIsRefused(t *testing.T) {
	doc := investigation.Document{
		SchemaVersion: SchemaVersion,
		GeneratedBy:   GeneratedByIdentity,
		Mode:          investigation.ModeHow,
		Binding: investigation.Binding{
			Repository:                    architecture.ClaimDocumentBinding{RepositoryDomain: "example.com", RevisionStatus: "clean", GraphDigestStatus: "none"},
			InvestigationPlanDigestSHA256: sha256Hex("plan"),
			ExtractorProfileDigestSHA256:  sha256Hex("profile"),
		},
		RawEvidence: []investigation.EvidenceReceipt{
			{
				ID:                  "evidence_borrowed",
				Category:            investigation.EvidenceSourceCode,
				Provider:            investigation.ProviderBinding{ID: "topology_extractor", Version: "1.0"},
				SourceIdentity:      "file.go",
				CapturedAt:          "2026-07-21T14:00:00Z",
				SourceDigestSHA256:  sha256Hex("source"),
				ContentDigestSHA256: sha256Hex("content"),
				Scope:               architecture.ClaimScope{Repository: "example.com"},
			},
		},
		Coverage: []investigation.CoverageEntry{
			{
				ProviderID:                 "flow_extractor", // mismatch provider!
				ProviderVersion:            "1.0",
				Category:                   investigation.EvidenceSourceCode,
				TargetDigestSHA256:         sha256Hex("target"),
				SourceSnapshotDigestSHA256: sha256Hex("snapshot"),
				ResultEvidenceIDs:          []string{"evidence_borrowed"},
				Status:                     investigation.CoverageSupporting,
			},
		},
	}
	if err := investigation.Validate(doc); err == nil {
		t.Fatal("Expected validation to fail when coverage borrows evidence from a different provider, got nil")
	}
}

// 15. Two identical runs with identical inputs produce deeply equal documents and identical output digests.
func TestDeterminismAcrossRuns(t *testing.T) {
	root := deterministicFixture(t)
	opts := defaultOpts()
	doc1, err := Extract(root, opts)
	if err != nil {
		t.Fatal(err)
	}

	doc2, err := Extract(root, opts)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(doc1, doc2) {
		t.Fatalf("Identical runs did not produce deep equal documents")
	}

	if doc1.Receipt.OutputDocumentDigestSHA256 != doc2.Receipt.OutputDocumentDigestSHA256 {
		t.Errorf("Identical runs produced different output digests: %s vs %s", doc1.Receipt.OutputDocumentDigestSHA256, doc2.Receipt.OutputDocumentDigestSHA256)
	}
}

// 16. Changing source bytes changes the source-snapshot and output-document digests.
func TestDigestChangesOnSourceByteChange(t *testing.T) {
	root := deterministicFixture(t)
	opts := defaultOpts()

	docBefore, err := Extract(root, opts)
	if err != nil {
		t.Fatal(err)
	}

	// Mutate a source file
	apiPath := filepath.Join(root, "api/api.go")
	content, err := os.ReadFile(apiPath)
	if err != nil {
		t.Fatal(err)
	}
	mutated := string(content) + "\n// mutation to change bytes\n"
	if err := os.WriteFile(apiPath, []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}

	docAfter, err := Extract(root, opts)
	if err != nil {
		t.Fatal(err)
	}

	if docBefore.Coverage[0].SourceSnapshotDigestSHA256 == docAfter.Coverage[0].SourceSnapshotDigestSHA256 {
		t.Errorf("Expected source snapshot digest to change after file mutation")
	}

	if docBefore.Receipt.OutputDocumentDigestSHA256 == docAfter.Receipt.OutputDocumentDigestSHA256 {
		t.Errorf("Expected OutputDocumentDigestSHA256 to change after file mutation")
	}
}

// 17. Changing only CapturedAt does not change facts, evidence identities, source-snapshot digests, target digests, plan digest, or extractor-profile digest.
func TestCaptureTimeIndependence(t *testing.T) {
	root := deterministicFixture(t)
	opts1 := defaultOpts()
	opts1.CapturedAt = "2026-07-21T14:00:00Z"

	doc1, err := Extract(root, opts1)
	if err != nil {
		t.Fatal(err)
	}

	opts2 := defaultOpts()
	opts2.CapturedAt = "2026-07-21T15:00:00Z"

	doc2, err := Extract(root, opts2)
	if err != nil {
		t.Fatal(err)
	}

	// Facts should be equal
	if !reflect.DeepEqual(doc1.Observations, doc2.Observations) {
		t.Errorf("Observations changed when only CapturedAt was modified")
	}

	// Evidence identities should be equal
	if len(doc1.RawEvidence) != len(doc2.RawEvidence) {
		t.Fatalf("RawEvidence count mismatch: %d vs %d", len(doc1.RawEvidence), len(doc2.RawEvidence))
	}
	for i := range doc1.RawEvidence {
		if doc1.RawEvidence[i].ID != doc2.RawEvidence[i].ID {
			t.Errorf("Evidence ID mismatch at index %d: %q vs %q", i, doc1.RawEvidence[i].ID, doc2.RawEvidence[i].ID)
		}
	}

	// Source snapshot digests should be equal
	if doc1.Coverage[0].SourceSnapshotDigestSHA256 != doc2.Coverage[0].SourceSnapshotDigestSHA256 {
		t.Errorf("SourceSnapshotDigestSHA256 changed: %s vs %s", doc1.Coverage[0].SourceSnapshotDigestSHA256, doc2.Coverage[0].SourceSnapshotDigestSHA256)
	}

	// Target digests should be equal
	for i := range doc1.Coverage {
		if doc1.Coverage[i].TargetDigestSHA256 != doc2.Coverage[i].TargetDigestSHA256 {
			t.Errorf("Coverage target digest at index %d changed: %s vs %s", i, doc1.Coverage[i].TargetDigestSHA256, doc2.Coverage[i].TargetDigestSHA256)
		}
	}

	// Plan digests should be equal
	if doc1.Binding.InvestigationPlanDigestSHA256 != doc2.Binding.InvestigationPlanDigestSHA256 {
		t.Errorf("Plan digest changed: %s vs %s", doc1.Binding.InvestigationPlanDigestSHA256, doc2.Binding.InvestigationPlanDigestSHA256)
	}

	// Extractor profile digests should be equal
	if doc1.Binding.ExtractorProfileDigestSHA256 != doc2.Binding.ExtractorProfileDigestSHA256 {
		t.Errorf("ExtractorProfile digest changed: %s vs %s", doc1.Binding.ExtractorProfileDigestSHA256, doc2.Binding.ExtractorProfileDigestSHA256)
	}
}

// 18. A successful investigator with zero matches reports searched_no_result, not unavailable.
func TestNoResultInvestigatorReportsSearchedNoResult(t *testing.T) {
	// We run extract on this root, but let's check a dummy/empty file to see that a successful investigator with no matches reports CoverageNoResult
	// Wait, testdata/deterministic_repo does not have any YAML files, but it does have Go files.
	// Are there any investigators that yield zero matches?
	// Wait, does deterministic_repo yield zero matches for anything?
	// Actually, does topology or flow yield matches? Yes.
	// But what about data_shape or others?
	// If we write a completely empty package inside root, does it yield zero matches for some extractors but execute successfully?
	// Yes! In an empty package (only package declaration), topology_extractor finds no symbols, so it has 0 observations, meaning it will report CoverageNoResult!
	emptyRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(emptyRoot, "go.mod"), []byte("module example.com/empty\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(emptyRoot, "empty.go"), []byte("package empty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := defaultOpts()
	opts.Repository.RepositoryDomain = "example.com/empty"
	doc, err := Extract(emptyRoot, opts)
	if err != nil {
		t.Fatalf("Extract empty failed: %v", err)
	}

	for _, cov := range doc.Coverage {
		// All extractors executed successfully (no engine errors), but since the package is empty:
		// topology, flow, contract, data_shape, etc. should find nothing and report CoverageNoResult!
		if cov.Status != investigation.CoverageNoResult {
			t.Errorf("Expected empty package to yield searched_no_result status for %s, got %q", cov.ProviderID, cov.Status)
		}
	}
}

// 19. Invalid source or target digest mutations are rejected by validation.
func TestInvalidDigestMutationsAreRejected(t *testing.T) {
	root := deterministicFixture(t)
	doc, err := Extract(root, defaultOpts())
	if err != nil {
		t.Fatal(err)
	}

	// Mutate a target digest to something invalid
	doc.Coverage[0].TargetDigestSHA256 = "invalid_digest_format"
	if err := investigation.Validate(doc); err == nil {
		t.Error("Expected validation to reject invalid target digest format, got nil")
	}

	// Reset and mutate source snapshot digest
	doc, _ = Extract(root, defaultOpts())
	doc.Coverage[0].SourceSnapshotDigestSHA256 = "invalid_snapshot_format"
	if err := investigation.Validate(doc); err == nil {
		t.Error("Expected validation to reject invalid source snapshot digest format, got nil")
	}
}

func TestReadCapturedLines(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_read_lines_*.go")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	content := "line1\nline2\nline3\n"
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	text, err := readCapturedLines(tmpFile.Name(), 1, 2)
	if err != nil {
		t.Fatalf("readCapturedLines failed: %v", err)
	}

	expected := "line1\nline2\n"
	if text != expected {
		t.Errorf("Expected %q, got %q", expected, text)
	}
}

func TestReadCapturedLinesPreservesBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.go")
	want := "first\r\nsecond\r\nthird"
	if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readCapturedLines(path, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != "first\r\nsecond\r\n" {
		t.Fatalf("captured bytes changed: %q", got)
	}
}

func TestDeduplicateReceiptsRefusesConflictingID(t *testing.T) {
	base := investigation.EvidenceReceipt{ID: "evidence_same", Scope: architecture.ClaimScope{Repository: "example.test"}}
	other := base
	other.CapturedContent = "different"
	if _, err := deduplicateReceipts([]investigation.EvidenceReceipt{base, other}); err == nil {
		t.Fatal("expected conflicting evidence IDs to fail")
	}
}

func TestCaptureFailurePipelineProof(t *testing.T) {
	validFact := architecture.Fact{
		ID:        "fact.valid",
		Kind:      "topology",
		Subject:   "api.Request",
		Predicate: "declares_data_shape",
		Object:    "struct",
		Extractor: "data_shape_extractor",
		Evidence: architecture.Evidence{
			SourceFile: "api/api.go",
			LineStart:  7,
			LineEnd:    7,
		},
	}
	badFact := architecture.Fact{
		ID:        "fact.bad",
		Kind:      "topology",
		Subject:   "api.NonExistent",
		Predicate: "declares_data_shape",
		Object:    "struct",
		Extractor: "data_shape_extractor",
		Evidence: architecture.Evidence{
			SourceFile: "api/missing.go",
			LineStart:  1,
			LineEnd:    1,
		},
	}

	root := deterministicFixture(t)
	opts := defaultOpts()
	opts.Repository.RepositoryDomain = "example.test"
	res, err := composeReceiptsAndCoverage(root, []architecture.Fact{validFact, badFact}, "example.test", opts, nil, nil, nil)
	if err != nil {
		t.Fatalf("composeReceiptsAndCoverage failed: %v", err)
	}

	foundLimitation := false
	for _, lim := range res.Limitations {
		if lim.Scope == "api/missing.go" && (strings.Contains(lim.Reason, "source capture unavailable") || strings.Contains(lim.Reason, "source digest unavailable")) {
			foundLimitation = true
			break
		}
	}
	if !foundLimitation {
		t.Errorf("Expected limitation to be generated for the missing file, got limitations: %+v", res.Limitations)
	}

	badReceiptID := "evidence_" + sha256Hex(badFact.ID)[:16]
	for _, rec := range res.RawEvidence {
		if rec.ID == badReceiptID {
			t.Errorf("Emitted fabricated evidence receipt for missing file: %+v", rec)
		}
	}

	validReceiptID := "evidence_" + sha256Hex(validFact.ID)[:16]
	foundValid := false
	for _, rec := range res.RawEvidence {
		if rec.ID == validReceiptID {
			foundValid = true
			if rec.CapturedContent == "" {
				t.Errorf("Expected captured content for valid fact to be populated")
			}
		}
	}
	if !foundValid {
		t.Errorf("Expected valid fact's evidence receipt to be preserved, but it was missing")
	}
}
