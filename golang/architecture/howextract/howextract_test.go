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
			Revision:          "0123456789abcdef0123456789abcdef01234567",
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
	planDigest, err := CalculatePlanDigest(doc.Plan)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Binding.InvestigationPlanDigestSHA256 != planDigest {
		t.Fatalf("plan digest mismatch: got %s want %s", doc.Binding.InvestigationPlanDigestSHA256, planDigest)
	}
	profile := ExtractorProfileV1{
		SchemaVersion:        "profile.schema.v1",
		ProfileName:          ExtractorProfileName,
		EnabledInvestigators: doc.Plan.Queries,
		SourceSnapshotAlgo:   "semantic-input-manifest.v1",
	}
	profileDigest, err := CalculateProfileDigest(profile)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Binding.ExtractorProfileDigestSHA256 != profileDigest {
		t.Fatalf("extractor profile digest mismatch: got %s want %s", doc.Binding.ExtractorProfileDigestSHA256, profileDigest)
	}
	snapshotDigest, err := BuildSourceSnapshotManifest(root, defaultOpts().Repository.RepositoryDomain)
	if err != nil {
		t.Fatal(err)
	}

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

		// 4. Every target digest equals the canonical target descriptor digest.
		wantTarget, err := CalculateTargetDigest(CoverageTargetV1{
			SchemaVersion:          "target.schema.v1",
			Mode:                   investigation.ModeHow,
			ProviderID:             cov.ProviderID,
			ProviderVersion:        cov.ProviderVersion,
			Category:               cov.Category,
			RepositoryDomain:       defaultOpts().Repository.RepositoryDomain,
			Scope:                  "repository",
			PlanDigestSHA256:       planDigest,
			ExtractorProfileDigest: profileDigest,
		})
		if err != nil {
			t.Fatal(err)
		}
		if cov.TargetDigestSHA256 != wantTarget {
			t.Errorf("Coverage entry %s target digest mismatch: got %s want %s", cov.ProviderID, cov.TargetDigestSHA256, wantTarget)
		}

		// 5. Every coverage entry binds to the exact canonical source manifest.
		if cov.SourceSnapshotDigestSHA256 != snapshotDigest {
			t.Errorf("Coverage entry %s source snapshot mismatch: got %s want %s", cov.ProviderID, cov.SourceSnapshotDigestSHA256, snapshotDigest)
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

func TestExtractRequiresExactRepositoryBinding(t *testing.T) {
	opts := defaultOpts()
	opts.Repository.Revision = ""
	opts.Repository.TreeDigestSHA256 = ""
	if _, err := Extract(deterministicFixture(t), opts); err == nil {
		t.Fatal("missing revision and tree digest produced a HOW document")
	}
}

func TestExtractPreservesBoundRepositoryDigests(t *testing.T) {
	opts := defaultOpts()
	opts.Repository.TreeDigestSHA256 = sha256Hex("canonical tree")
	opts.Repository.GraphDigestSHA256 = sha256Hex("canonical graph")
	opts.Repository.GraphDigestStatus = architecture.GraphDigestResolved
	doc, err := Extract(deterministicFixture(t), opts)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Receipt.GraphDigestSHA256 != opts.Repository.GraphDigestSHA256 {
		t.Fatalf("run receipt graph digest mismatch: got %s want %s", doc.Receipt.GraphDigestSHA256, opts.Repository.GraphDigestSHA256)
	}
	if doc.Binding.Repository.Revision != opts.Repository.Revision || doc.Binding.Repository.TreeDigestSHA256 != opts.Repository.TreeDigestSHA256 || doc.Binding.Repository.GraphDigestSHA256 != opts.Repository.GraphDigestSHA256 {
		t.Fatalf("repository binding was not preserved: %+v", doc.Binding.Repository)
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

func TestPartialSemanticExecutionCannotClaimNoResult(t *testing.T) {
	root := deterministicFixture(t)
	limitations := []architecture.Limitation{{
		Source: "go_semantic_extractor",
		Scope:  "example.com/deterministic/api",
		Reason: "type check failed",
	}}
	doc, err := composeReceiptsAndCoverage(root, nil, "example.com/deterministic", defaultOpts(), limitations, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range doc.Coverage {
		if entry.ProviderID == "state_extractor" {
			continue
		}
		if entry.Status != investigation.CoverageUnavailable {
			t.Errorf("partial semantic execution reported %s as %q, want unavailable", entry.ProviderID, entry.Status)
		}
		if len(entry.Limitations) != 1 || entry.Limitations[0].Source != "go_semantic_extractor" {
			t.Errorf("semantic limitation not attached to %s: %+v", entry.ProviderID, entry.Limitations)
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

func TestSourceSnapshotUsesCanonicalSearchedFiles(t *testing.T) {
	root := deterministicFixture(t)
	before, err := BuildSourceSnapshotManifest(root, "example.com/deterministic")
	if err != nil {
		t.Fatal(err)
	}

	excludedDir := filepath.Join(root, "testdata")
	if err := os.MkdirAll(excludedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	excluded := filepath.Join(excludedDir, "ignored.go")
	if err := os.WriteFile(excluded, []byte("package testdata\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	afterIgnored, err := BuildSourceSnapshotManifest(root, "example.com/deterministic")
	if err != nil {
		t.Fatal(err)
	}
	if before != afterIgnored {
		t.Fatal("excluded source changed the semantic input snapshot")
	}
	if err := os.WriteFile(excluded, []byte("package testdata\n// changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if afterMutation, err := BuildSourceSnapshotManifest(root, "example.com/deterministic"); err != nil || afterMutation != before {
		t.Fatalf("excluded mutation changed the semantic input snapshot: digest=%q err=%v", afterMutation, err)
	}

	searched := filepath.Join(root, "api", "api.go")
	content, err := os.ReadFile(searched)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(searched, append(content, []byte("\n// searched mutation\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	afterSearched, err := BuildSourceSnapshotManifest(root, "example.com/deterministic")
	if err != nil {
		t.Fatal(err)
	}
	if before == afterSearched {
		t.Fatal("searched source mutation did not change the snapshot")
	}

	if again, err := BuildSourceSnapshotManifest(root, "example.com/deterministic"); err != nil || again != afterSearched {
		t.Fatalf("searched manifest is not deterministic: digest=%q err=%v", again, err)
	}
}

func TestGeneratedCompilerInputChangesSnapshotAndSemantics(t *testing.T) {
	root := deterministicFixture(t)
	apiPath := filepath.Join(root, "api", "api.go")
	apiSource, err := os.ReadFile(apiPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(apiPath, append(apiSource, []byte("\ntype GeneratedCapability interface { Generated() }\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	before, err := Extract(root, defaultOpts())
	if err != nil {
		t.Fatal(err)
	}
	if hasGeneratedCapabilityImplementation(before) {
		t.Fatal("generated capability existed before the generated compiler input was added")
	}

	generated := filepath.Join(root, "impl", "service_generated.go")
	if err := os.WriteFile(generated, []byte("// Code generated by test. DO NOT EDIT.\npackage impl\n\nfunc (ServiceImpl) Generated() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := Extract(root, defaultOpts())
	if err != nil {
		t.Fatal(err)
	}
	if !hasGeneratedCapabilityImplementation(after) {
		t.Fatal("generated method did not change semantic interface implementation output")
	}
	if before.Coverage[0].SourceSnapshotDigestSHA256 == after.Coverage[0].SourceSnapshotDigestSHA256 {
		t.Fatal("generated compiler input changed semantic output without changing the bound snapshot")
	}
}

func hasGeneratedCapabilityImplementation(doc investigation.Document) bool {
	for _, fact := range doc.Observations {
		if fact.Extractor == "contract_extractor" && fact.Predicate == "implements_interface" && fact.Subject == "impl.ServiceImpl" && fact.Object == "api.GeneratedCapability" {
			return true
		}
	}
	return false
}

func TestSourceSnapshotRefusesExternalSymlink(t *testing.T) {
	root := deterministicFixture(t)
	external := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(external, []byte("package outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "external.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildSourceSnapshotManifest(root, "example.com/deterministic"); err == nil {
		t.Fatal("external symlink was accepted into the searched source snapshot")
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

func TestUnknownExtractorIsRefused(t *testing.T) {
	root := deterministicFixture(t)
	fact := architecture.Fact{
		ID:        "fact.unknown-extractor",
		Kind:      "topology",
		Subject:   "api.Service",
		Predicate: "defines",
		Object:    "Service",
		Extractor: "unknown_extractor",
		Evidence: architecture.Evidence{
			SourceFile: "api/api.go",
			LineStart:  1,
			LineEnd:    1,
		},
	}
	if _, err := composeReceiptsAndCoverage(root, []architecture.Fact{fact}, "example.com/deterministic", defaultOpts(), nil, nil, nil); err == nil {
		t.Fatal("unknown extractor emitted evidence instead of failing closed")
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
	for _, coverage := range res.Coverage {
		if coverage.ProviderID != "data_shape_extractor" {
			continue
		}
		if coverage.Status != investigation.CoverageSupporting || len(coverage.ResultEvidenceIDs) != 1 || len(coverage.Limitations) == 0 {
			t.Errorf("partial capture did not remain supporting with limitations: %+v", coverage)
		}
		return
	}
	t.Error("data-shape coverage entry not found")
}

func TestAllCaptureFailuresAreUnavailable(t *testing.T) {
	root := deterministicFixture(t)
	fact := architecture.Fact{
		ID:        "fact.missing-source",
		Kind:      "data_shape",
		Subject:   "api.Missing",
		Predicate: "declares_data_shape",
		Object:    "struct",
		Extractor: "data_shape_extractor",
		Evidence:  architecture.Evidence{SourceFile: "api/missing.go", LineStart: 1, LineEnd: 1},
	}
	doc, err := composeReceiptsAndCoverage(root, []architecture.Fact{fact}, "example.com/deterministic", defaultOpts(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, coverage := range doc.Coverage {
		if coverage.ProviderID != "data_shape_extractor" {
			continue
		}
		if coverage.Status != investigation.CoverageUnavailable {
			t.Fatalf("all capture failures reported %q, want unavailable", coverage.Status)
		}
		if coverage.Reason != "all discovered evidence failed capture" {
			t.Fatalf("unexpected unavailable reason %q", coverage.Reason)
		}
		if len(coverage.ResultEvidenceIDs) != 0 || len(coverage.Limitations) == 0 {
			t.Fatalf("capture failure coverage concealed evidence loss: %+v", coverage)
		}
		return
	}
	t.Fatal("data-shape coverage entry not found")
}
