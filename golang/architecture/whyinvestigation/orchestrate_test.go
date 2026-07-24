// SPDX-License-Identifier: AGPL-3.0-only

package whyinvestigation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/investigation"
)

func TestMultiProviderOrchestration(t *testing.T) {
	root, req, first, second := gitFixture(t)
	req.Range = GitRange{Start: first + "~1", End: second}

	// 1. Write mock local evidence files containing target ID "obs.cli"
	// (obs.cli is how.Observations[0].ID since gitFixture returns how.Observations[0].ID)
	targetID := req.Query.TargetObservationIDs[0]

	// Documentation
	docDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docDir, 0755); err != nil {
		t.Fatal(err)
	}
	docFile := filepath.Join(docDir, "design.md")
	docContent := "Design document referring to " + targetID + "\n"
	if err := os.WriteFile(docFile, []byte(docContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Tests
	testFile := filepath.Join(root, "api", "api_extended_test.go")
	testContent := "package api\nimport \"testing\"\nfunc TestSomething(t *testing.T) { // target: " + targetID + "\n}\n"
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Awareness records
	awarenessDir := filepath.Join(root, "docs", "awareness")
	if err := os.MkdirAll(awarenessDir, 0755); err != nil {
		t.Fatal(err)
	}
	awarenessFile := filepath.Join(awarenessDir, "invariants.yaml")
	awarenessContent := "invariants:\n  - target: " + targetID + "\n"
	if err := os.WriteFile(awarenessFile, []byte(awarenessContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Incident scars
	scarsDir := filepath.Join(root, "docs", "scars")
	if err := os.MkdirAll(scarsDir, 0755); err != nil {
		t.Fatal(err)
	}
	scarsFile := filepath.Join(scarsDir, "scar_01.yaml")
	scarsContent := "scar:\n  id: scar.1\n  details: Incident regarding " + targetID + "\n"
	if err := os.WriteFile(scarsFile, []byte(scarsContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Architect Answers
	dialogueDir := filepath.Join(root, "docs", "dialogue")
	if err := os.MkdirAll(dialogueDir, 0755); err != nil {
		t.Fatal(err)
	}
	dialogueFile := filepath.Join(dialogueDir, "qna.md")
	dialogueContent := "Architect dialogue answer referencing " + targetID + "\n"
	if err := os.WriteFile(dialogueFile, []byte(dialogueContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Imported Evidence
	importedDir := filepath.Join(root, ".sensei", "evidence")
	if err := os.MkdirAll(importedDir, 0755); err != nil {
		t.Fatal(err)
	}
	importedFile := filepath.Join(importedDir, "receipt_123.json")
	importedContent := "{\"id\": \"imported.1\", \"content\": \"" + targetID + "\"}\n"
	if err := os.WriteFile(importedFile, []byte(importedContent), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Define the explicit plans (one with sorted order, one with scrambled order)
	planA := Plan{
		ID:          "plan.why.multi.v1",
		Description: "Multi-provider WHY orchestration plan",
		RequestedProviderIDs: []string{
			"documentation_provider",
			"git_history_provider",
			"tests_provider",
			"awareness_provider",
			"scars_provider",
			"architect_answers_provider",
			"imported_evidence_provider",
		},
	}

	planB := Plan{
		ID:          "plan.why.multi.v1",
		Description: "Multi-provider WHY orchestration plan",
		RequestedProviderIDs: []string{
			"imported_evidence_provider",
			"git_history_provider",
			"architect_answers_provider",
			"scars_provider",
			"tests_provider",
			"awareness_provider",
			"documentation_provider",
		},
	}

	// 3. Verify: provider ordering does not change the output digest
	ctx := context.Background()
	docA, err := Orchestrate(ctx, root, req, planA)
	if err != nil {
		t.Fatalf("Orchestrate planA failed: %v", err)
	}

	docB, err := Orchestrate(ctx, root, req, planB)
	if err != nil {
		t.Fatalf("Orchestrate planB failed: %v", err)
	}

	if docA.Receipt.OutputDocumentDigestSHA256 != docB.Receipt.OutputDocumentDigestSHA256 {
		t.Fatalf("provider execution order changed output document digest:\nA=%s\nB=%s",
			docA.Receipt.OutputDocumentDigestSHA256, docB.Receipt.OutputDocumentDigestSHA256)
	}

	// Verify deep equality of A and B
	if !reflect.DeepEqual(docA, docB) {
		t.Fatalf("Orchestrate output docs differed by execution order ordering")
	}

	// 4. Verify: raw evidence survives normalization unchanged
	for _, receipt := range docA.RawEvidence {
		computedDigest := investigation.SHA256Bytes([]byte(receipt.CapturedContent))
		if computedDigest != receipt.ContentDigestSHA256 {
			t.Fatalf("raw evidence content digest changed: computed=%s, receipt=%s", computedDigest, receipt.ContentDigestSHA256)
		}
	}

	// 5. Verify: missing providers produce honest coverage states (no fabricated metadata)
	planWithMissing := Plan{
		ID:          "plan.why.missing.v1",
		Description: "Plan containing an unregistered provider",
		RequestedProviderIDs: []string{
			"unregistered_dummy_provider",
		},
	}
	docWithMissing, err := Orchestrate(ctx, root, req, planWithMissing)
	if err != nil {
		t.Fatalf("Orchestrate missing plan failed: %v", err)
	}
	if len(docWithMissing.Coverage) != 1 {
		t.Fatalf("expected 1 coverage entry, got %d", len(docWithMissing.Coverage))
	}
	coverage := docWithMissing.Coverage[0]
	if coverage.Status != investigation.CoverageNotConfigured {
		t.Fatalf("expected CoverageNotConfigured status for unregistered provider, got %q", coverage.Status)
	}
	if coverage.ProviderVersion != "" || coverage.Category != "" {
		t.Fatalf("unregistered provider had fabricated version %q or category %q", coverage.ProviderVersion, coverage.Category)
	}

	// 6. Verify: an unsearched source is never represented as "searched and absent"
	planPartial := Plan{
		ID:          "plan.why.partial.v1",
		Description: "Plan requesting only documentation",
		RequestedProviderIDs: []string{
			"documentation_provider",
		},
	}
	docPartial, err := Orchestrate(ctx, root, req, planPartial)
	if err != nil {
		t.Fatalf("Orchestrate partial plan failed: %v", err)
	}
	// Assert only documentation_provider is in coverage
	for _, entry := range docPartial.Coverage {
		if entry.ProviderID != "documentation_provider" {
			t.Fatalf("unrequested provider %q was represented in coverage list", entry.ProviderID)
		}
	}

	// 7. Verify: every observation stays within the bound HOW targets
	for _, receipt := range docA.RawEvidence {
		for _, sym := range receipt.Scope.Symbols {
			if sym != targetID {
				t.Fatalf("observation %q falls outside the bound HOW targets %q", sym, targetID)
			}
		}
	}

	// 8. Verify: repeated runs over identical snapshots produce byte-identical output
	docA2, err := Orchestrate(ctx, root, req, planA)
	if err != nil {
		t.Fatalf("Orchestrate planA second run failed: %v", err)
	}
	if !reflect.DeepEqual(docA, docA2) {
		t.Fatal("repeated runs over identical snapshots were not deterministic/byte-identical")
	}

	// 9. Verify: model-disabled execution produces same canonical truth (which we enforce by plan setup)
	if docA.Binding.Model.Status != investigation.ModelStatusDisabled {
		t.Fatalf("model execution was not disabled: %q", docA.Binding.Model.Status)
	}
}

func TestImmutableEvidenceSnapshotFreeze(t *testing.T) {
	root, req, first, second := gitFixture(t)
	req.Range = GitRange{Start: first + "~1", End: second}
	targetID := req.Query.TargetObservationIDs[0]

	// Setup doc file
	docDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docDir, 0755); err != nil {
		t.Fatal(err)
	}
	docFile := filepath.Join(docDir, "freeze-test.md")
	originalContent := "Original target doc containing " + targetID + "\n"
	if err := os.WriteFile(docFile, []byte(originalContent), 0644); err != nil {
		t.Fatal(err)
	}

	p := DocumentationProvider{Root: root}
	ctx := context.Background()

	// Capture snapshot
	snap, err := p.Capture(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Invariant: File modified after capture
	modifiedContent := "Modified target doc containing different data " + targetID + "\n"
	if err := os.WriteFile(docFile, []byte(modifiedContent), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := p.Investigate(ctx, snap, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.RawEvidence) != 1 || res.RawEvidence[0].CapturedContent != originalContent {
		t.Fatal("file modification after capture leaked into evidence investigation")
	}

	// Reset to original to proceed
	if err := os.WriteFile(docFile, []byte(originalContent), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Invariant: File deleted after capture
	if err := os.Remove(docFile); err != nil {
		t.Fatal(err)
	}

	res2, err := p.Investigate(ctx, snap, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.RawEvidence) != 1 || res2.RawEvidence[0].CapturedContent != originalContent {
		t.Fatal("file deletion after capture resulted in evidence loss during investigation")
	}

	// 3. Invariant: File added after capture
	addedFile := filepath.Join(docDir, "added-later.md")
	addedContent := "Added target doc containing " + targetID + "\n"
	if err := os.WriteFile(addedFile, []byte(addedContent), 0644); err != nil {
		t.Fatal(err)
	}

	res3, err := p.Investigate(ctx, snap, req)
	if err != nil {
		t.Fatal(err)
	}
	// The added file must not appear in investigation because it wasn't in the snapshot!
	for _, receipt := range res3.RawEvidence {
		if strings.Contains(receipt.SourceIdentity, "added-later.md") {
			t.Fatal("untracked file added after capture was returned in evidence investigation")
		}
	}
}

func TestProviderAndPlanDigestBindings(t *testing.T) {
	root, req, first, second := gitFixture(t)
	req.Range = GitRange{Start: first + "~1", End: second}

	planX := Plan{
		ID:                   "plan.v1",
		RequestedProviderIDs: []string{"git_history_provider", "documentation_provider"},
	}

	planY := Plan{
		ID:                   "plan.v1",
		RequestedProviderIDs: []string{"git_history_provider", "documentation_provider", "tests_provider"},
	}

	ctx := context.Background()

	// 4. Invariant: provider-set change must change plan digest
	docX, err := Orchestrate(ctx, root, req, planX)
	if err != nil {
		t.Fatal(err)
	}
	docY, err := Orchestrate(ctx, root, req, planY)
	if err != nil {
		t.Fatal(err)
	}

	if docX.Binding.InvestigationPlanDigestSHA256 == docY.Binding.InvestigationPlanDigestSHA256 {
		t.Fatal("changing the requested provider set did not change the investigation plan digest")
	}

	// 5. Invariant: provider version change must change profile digest
	// Let's temporarily register a mock provider with a different version
	registryMu.RLock()
	originalFactory := registry["git_history_provider"]
	registryMu.RUnlock()
	defer func() {
		Register("git_history_provider", originalFactory)
	}()

	Register("git_history_provider", func(r string) Provider {
		return MockGitProvider{GitProvider{Root: r}}
	})

	docMock, err := Orchestrate(ctx, root, req, planX)
	if err != nil {
		t.Fatal(err)
	}

	if docX.Binding.ExtractorProfileDigestSHA256 == docMock.Binding.ExtractorProfileDigestSHA256 {
		t.Fatal("changing the provider version/implementation did not change the extractor profile digest")
	}
}

type MockGitProvider struct {
	GitProvider
}

func (MockGitProvider) Identity() investigation.ProviderBinding {
	return investigation.ProviderBinding{ID: GitProviderID, Version: "2.0"} // changed version!
}

func TestPartialHistoryAndContradictions(t *testing.T) {
	root, req, first, second := gitFixture(t)
	req.Range = GitRange{Start: first, End: second}

	// Make Git repository shallow
	if err := os.WriteFile(root+"/.git/shallow", []byte(first+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	plan := Plan{
		ID:                   "plan.why.git.only.v1",
		Description:          "Git only plan",
		RequestedProviderIDs: []string{"git_history_provider"},
	}

	doc, err := Orchestrate(context.Background(), root, req, plan)
	if err != nil {
		t.Fatal(err)
	}

	// 10. Verify: partial Git history remains visibly partial
	gitCoverageFound := false
	for _, entry := range doc.Coverage {
		if entry.ProviderID == "git_history_provider" {
			gitCoverageFound = true
			if entry.Status != investigation.CoverageSupporting || len(entry.Limitations) == 0 {
				t.Fatalf("shallow git history was not represented as partial/limited: %+v", entry)
			}
		}
	}
	if !gitCoverageFound {
		t.Fatal("expected git_history_provider coverage entry")
	}

	// 11. Verify: contradictory evidence is retained
	// Re-run gitFixture without shallow but over contradictory range
	rootNormal, reqNormal, firstNormal, secondNormal := gitFixture(t)
	reqNormal.Range = GitRange{Start: firstNormal + "~1", End: secondNormal}
	docContradictory, err := Orchestrate(context.Background(), rootNormal, reqNormal, plan)
	if err != nil {
		t.Fatal(err)
	}

	foundFirst, foundSecond := false, false
	for _, receipt := range docContradictory.RawEvidence {
		if strings.Contains(receipt.CapturedContent, "Move configuration ownership to controller") {
			foundFirst = true
		}
		if strings.Contains(receipt.CapturedContent, "Configuration ownership remains with node agent") {
			foundSecond = true
		}
	}

	if !foundFirst || !foundSecond {
		t.Fatal("contradictory commits were not both retained as evidence")
	}
}

func TestProvenanceAndCaptureFailurePreservation(t *testing.T) {
	root, req, first, second := gitFixture(t)
	req.Range = GitRange{Start: first + "~1", End: second}

	// Register a mock provider that intentionally fails during Capture
	registryMu.RLock()
	originalFactory := registry["documentation_provider"]
	registryMu.RUnlock()
	defer func() {
		Register("documentation_provider", originalFactory)
	}()

	expectedErr := "permission denied or missing directory"
	Register("documentation_provider", func(r string) Provider {
		return FailingCaptureProvider{
			DocumentationProvider: DocumentationProvider{Root: r},
			ErrText:               expectedErr,
		}
	})

	plan := Plan{
		ID:                   "plan.why.fail.v1",
		RequestedProviderIDs: []string{"documentation_provider", "unconfigured_dummy_provider"},
	}

	doc, err := Orchestrate(context.Background(), root, req, plan)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Verify that registered but failing provider retains its version and category
	var docCovFound, unconfiguredFound bool
	for _, entry := range doc.Coverage {
		if entry.ProviderID == "documentation_provider" {
			docCovFound = true
			if entry.Status != investigation.CoverageUnavailable {
				t.Fatalf("expected status CoverageUnavailable, got %q", entry.Status)
			}
			if entry.ProviderVersion != "1.0" {
				t.Fatalf("expected version 1.0, got %q", entry.ProviderVersion)
			}
			if entry.Category != investigation.EvidenceDocumentation {
				t.Fatalf("expected category %q, got %q", investigation.EvidenceDocumentation, entry.Category)
			}
			if !strings.Contains(entry.Reason, expectedErr) {
				t.Fatalf("expected error text %q in reason, got %q", expectedErr, entry.Reason)
			}
		}
		if entry.ProviderID == "unconfigured_dummy_provider" {
			unconfiguredFound = true
			if entry.Status != investigation.CoverageNotConfigured {
				t.Fatalf("expected status CoverageNotConfigured, got %q", entry.Status)
			}
			if entry.ProviderVersion != "" || entry.Category != "" {
				t.Fatalf("unconfigured provider had non-empty metadata: version=%q category=%q", entry.ProviderVersion, entry.Category)
			}
		}
	}

	if !docCovFound {
		t.Fatal("expected coverage entry for documentation_provider")
	}
	if !unconfiguredFound {
		t.Fatal("expected coverage entry for unconfigured_dummy_provider")
	}

	// 2. Verify the limitation records the real error reason
	var limitationFound bool
	for _, lim := range doc.Limitations {
		if lim.Source == "documentation_provider" {
			limitationFound = true
			if !strings.Contains(lim.Reason, expectedErr) {
				t.Fatalf("expected limitation reason containing %q, got %q", expectedErr, lim.Reason)
			}
		}
	}
	if !limitationFound {
		t.Fatal("expected limitation entry for documentation_provider capture failure")
	}
}

type FailingCaptureProvider struct {
	DocumentationProvider
	ErrText string
}

func (FailingCaptureProvider) Identity() investigation.ProviderBinding {
	return investigation.ProviderBinding{ID: "documentation_provider", Version: "1.0"}
}

func (f FailingCaptureProvider) Capture(ctx context.Context, req CaptureRequest) (Snapshot, error) {
	return Snapshot{}, fmt.Errorf("%s", f.ErrText)
}
