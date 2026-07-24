// SPDX-License-Identifier: AGPL-3.0-only

package investigationsurface

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
	"github.com/globulario/sensei/golang/architecture/investigator"
)

func TestEvidenceSnapshotIsDeterministicAndTamperEvident(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "runtime.txt"), []byte("observation.fact_1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := CaptureEvidence(root, "2026-07-22T18:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	second, err := CaptureEvidence(root, "2026-07-22T18:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if first.SnapshotDigestSHA256 != second.SnapshotDigestSHA256 {
		t.Fatal("same bounded evidence produced different snapshot digests")
	}
	first.Entries[0].Content = []byte("tampered")
	if ValidateEvidenceSnapshot(first) == nil {
		t.Fatal("tampered evidence content was accepted")
	}
}

func TestImportEvidenceKeepsManifestOutsideEvidenceScan(t *testing.T) {
	source := t.TempDir()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "runtime.txt"), []byte("fact_1"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureEvidence(source, "2026-07-22T18:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := ImportEvidence(snapshot, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.ImportedPaths) != 1 {
		t.Fatalf("expected one imported file, got %+v", receipt.ImportedPaths)
	}
	if filepath.Dir(filepath.FromSlash(receipt.ManifestPath)) != filepath.Join(".sensei", "evidence-manifests") {
		t.Fatalf("manifest entered evidence scan path: %s", receipt.ManifestPath)
	}
}

func TestCoveragePreservesUnavailableAndNoResult(t *testing.T) {
	doc := validSurfaceDocument()
	doc.Coverage = append(doc.Coverage, investigation.CoverageEntry{ProviderID: "unavailable", ProviderVersion: "1", Category: investigation.EvidenceRuntime, TargetDigestSHA256: surfaceSHA, Status: investigation.CoverageUnavailable, Reason: "not captured"})
	digest, _ := investigation.CalculateDocumentDigest(doc)
	doc.Receipt.OutputDocumentDigestSHA256 = digest
	report, err := Coverage(doc)
	if err != nil {
		t.Fatal(err)
	}
	if report.ByStatus[string(investigation.CoverageSupporting)] != 1 || report.ByStatus[string(investigation.CoverageUnavailable)] != 1 {
		t.Fatalf("coverage statuses were collapsed: %+v", report.ByStatus)
	}
}

func TestReadArtifactFailureDoesNotMutateDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(path, []byte(`{"files":["changed.go"],"symbols":[}`), 0o644); err != nil {
		t.Fatal(err)
	}
	destination := investigator.GroundingSnapshot{Files: []string{"sentinel.go"}}
	if err := ReadArtifact(path, &destination); err == nil {
		t.Fatal("malformed artifact was accepted")
	}
	if len(destination.Files) != 1 || destination.Files[0] != "sentinel.go" {
		t.Fatalf("failed decode mutated destination: %+v", destination)
	}
}

func TestValidateArtifactRecognizesGroundingSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grounding.json")
	grounding := investigator.GroundingSnapshot{Files: []string{"a.go"}, ObservationIDs: []string{"fact_1"}}
	if err := WriteArtifact(path, "json", grounding); err != nil {
		t.Fatal(err)
	}
	report := ValidateArtifact(path)
	if !report.Valid || report.ArtifactKind != ArtifactGrounding || report.DigestSHA256 == "" {
		t.Fatalf("grounding snapshot was not recognized: %+v", report)
	}
}

func TestRunArchitectureRefusesNonCanonicalGrounding(t *testing.T) {
	how := validSurfaceDocument()
	why := how
	why.Mode = investigation.ModeWhy
	grounding := GroundingFromDocuments(how, why)
	grounding.Files = append(grounding.Files, "unbound.go")
	_, err := RunArchitecture(ArchitectureRequest{How: how, Why: why, Grounding: grounding})
	if err == nil || !strings.Contains(err.Error(), "exactly match the canonical HOW and WHY grounding") {
		t.Fatalf("non-canonical grounding was not refused: %v", err)
	}
}

func TestRunArchitectureRefusesPreCompositionCandidates(t *testing.T) {
	how := validSurfaceDocument()
	why := how
	how.CandidateClaims = []architecture.Claim{{ID: "claim.preexisting"}}
	grounding := GroundingFromDocuments(how, why)
	_, err := RunArchitecture(ArchitectureRequest{How: how, Why: why, Grounding: grounding})
	if err == nil || !strings.Contains(err.Error(), "must not contain pre-composition candidates or questions") {
		t.Fatalf("pre-composition candidate was not refused: %v", err)
	}
}

func TestGroundingFromResultExcludesGeneratedCandidateClaims(t *testing.T) {
	result := investigator.Result{Document: investigation.Document{
		Observations:    []architecture.Fact{{ID: "fact_1", Scope: architecture.Scope{Files: []string{"observed.go"}}}},
		CandidateClaims: []architecture.Claim{{ID: "claim.generated", Scope: architecture.ClaimScope{Files: []string{"generated.go"}}}},
	}}
	grounding := GroundingFromResult(result)
	if len(grounding.ClaimIDs) != 0 {
		t.Fatalf("generated claims entered pre-candidate grounding: %+v", grounding.ClaimIDs)
	}
	if len(grounding.Files) != 1 || grounding.Files[0] != "observed.go" {
		t.Fatalf("generated claim scope entered grounding: %+v", grounding.Files)
	}
}

const (
	surfaceSHA = "4a8e63db7cc5173b82bd3ba6019d30ce9e22db84d852bd3ba6019d30ce922db8"
	contentSHA = "ed7002b439e9ac845f22357d822bac1444730fbdb6016d3ec9432297b9ec9f73"
)

func validSurfaceDocument() investigation.Document {
	doc := investigation.Document{SchemaVersion: "1", GeneratedBy: "test", Mode: investigation.ModeHow, Binding: investigation.Binding{Repository: architecture.ClaimDocumentBinding{RepositoryDomain: "example/repo", Revision: "abc123", RevisionStatus: "resolved", GraphDigestSHA256: surfaceSHA, GraphDigestStatus: "resolved"}, EvidenceSnapshotDigestSHA256: surfaceSHA, InvestigationPlanDigestSHA256: surfaceSHA, ExtractorProfileDigestSHA256: surfaceSHA, Model: investigation.ModelBinding{Status: investigation.ModelStatusDisabled}}, Plan: investigation.Plan{ID: "plan"}, Coverage: []investigation.CoverageEntry{{ProviderID: "provider", ProviderVersion: "1", Category: investigation.EvidenceSourceCode, TargetDigestSHA256: surfaceSHA, SourceSnapshotDigestSHA256: surfaceSHA, Status: investigation.CoverageSupporting, ResultEvidenceIDs: []string{"evidence_1"}}}, RawEvidence: []investigation.EvidenceReceipt{{ID: "evidence_1", Category: investigation.EvidenceSourceCode, Provider: investigation.ProviderBinding{ID: "provider", Version: "1"}, ProofStrength: investigation.ProofStaticSource, SourceIdentity: "source", SourceDigestSHA256: surfaceSHA, ContentDigestSHA256: contentSHA, CapturedContent: "content", Scope: architecture.ClaimScope{Repository: "example/repo", Files: []string{"a.go"}}, CapturedAt: "2026-07-22T18:00:00Z"}}, Receipt: investigation.RunReceipt{SchemaVersion: "1", GeneratedBy: "test", Repository: architecture.ClaimDocumentBinding{RepositoryDomain: "example/repo", Revision: "abc123", RevisionStatus: "resolved", GraphDigestSHA256: surfaceSHA, GraphDigestStatus: "resolved"}, GraphDigestSHA256: surfaceSHA, PlanDigestSHA256: surfaceSHA, ExtractorProfileDigestSHA256: surfaceSHA, EvidenceSnapshotDigestSHA256: surfaceSHA, Model: investigation.ModelBinding{Status: investigation.ModelStatusDisabled}, PostProcessingVersion: "1", TimestampSource: "2026-07-22T18:00:00Z", ResourceLimits: map[string]string{"cpu": "1"}, NondeterminismDeclaration: "none"}}
	digest, _ := investigation.CalculateDocumentDigest(doc)
	doc.Receipt.OutputDocumentDigestSHA256 = digest
	return doc
}
