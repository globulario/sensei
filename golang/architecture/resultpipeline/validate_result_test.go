// SPDX-License-Identifier: AGPL-3.0-only

package resultpipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/proofrequirements"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

// validBuilt returns a fresh, gate-valid BuildResult from the clean fixture. Each
// call rebuilds, so a test may mutate its result in isolation.
func validBuilt(t *testing.T) BuildResult {
	t.Helper()
	repo, taskDir, resultRev := e2eSeedClean(t)
	res, err := Build(context.Background(), BuildRequest{
		RepositoryRoot: repo, TaskDirectory: taskDir,
		ResultMode: resulttransition.ResultModeRevision, ResultRevision: resultRev, RepositoryDomain: e2eDomain,
	})
	if err != nil {
		t.Fatalf("build clean: %v", err)
	}
	return res
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %s, got nil", code)
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError %s, got %T: %v", code, err, err)
	}
	if ve.Code != code {
		t.Fatalf("expected code %s, got %s (%s)", code, ve.Code, ve.Detail)
	}
}

func stageIndex(res BuildResult, stage closureprotocol.ResultPipelineStage) int {
	for i, a := range res.StageArtifacts {
		if a.Stage == stage {
			return i
		}
	}
	return -1
}

// --- validity boundary ---

func TestValidateAcceptsCleanResult(t *testing.T) {
	if err := ValidateBuildResult(validBuilt(t)); err != nil {
		t.Fatalf("clean result rejected: %v", err)
	}
}

func TestValidateAcceptsCompleteButBlocked(t *testing.T) {
	res := validBuilt(t)
	// Force a complete-but-blocked document with a represented reason, and rebind
	// its stage artifact so the whole chain stays consistent.
	res.ProofRequirements.ProvingDisposition = proofrequirements.ProvingBlocked
	res.ProofRequirements.ArchitectQuestions = []proofrequirements.Requirement{
		{Class: "ArchitectQuestion", ID: "q.1", Origins: []string{proofrequirements.OriginArchitectQuestions}, Status: "unresolved"},
	}
	rebindStage9(t, &res)
	if err := ValidateBuildResult(res); err != nil {
		t.Fatalf("complete+blocked rejected: %v", err)
	}
}

func TestValidateRejectsIncomplete(t *testing.T) {
	res := validBuilt(t)
	res.ProofRequirements.ExtractionCompleteness = proofrequirements.ExtractionIncomplete
	res.ProofRequirements.SourceCoverage[0].Status = proofrequirements.CoverageInvalid
	rebindStage9(t, &res)
	wantCode(t, ValidateBuildResult(res), CodeProofExtractionIncomplete)
}

func TestValidateRejectsUncertifiableExtraction(t *testing.T) {
	res := validBuilt(t)
	res.ProofRequirements.ExtractionCompleteness = proofrequirements.ExtractionUncertifiable
	rebindStage9(t, &res)
	wantCode(t, ValidateBuildResult(res), CodeProofExtractionUncertifiable)
}

// --- top-level binding ---

func TestValidateRejectsForgedBindingDigest(t *testing.T) {
	res := validBuilt(t)
	res.ResultBindingDigestSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	wantCode(t, ValidateBuildResult(res), CodeResultBindingMismatch)
}

func TestValidateRejectsBaseRevisionMismatch(t *testing.T) {
	res := validBuilt(t)
	res.BoundRepositoryResult.RepositoryResult.BaseRevision = "deadbeef"
	wantCode(t, ValidateBuildResult(res), CodeResultBindingMismatch)
}

func TestValidateRejectsForgedUpstreamDigest(t *testing.T) {
	res := validBuilt(t)
	res.BoundRepositoryResult.AdmissionDecisionDigestSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	wantCode(t, ValidateBuildResult(res), CodeResultBindingMismatch)
}

// --- stage set ---

func TestValidateRejectsMissingStage(t *testing.T) {
	res := validBuilt(t)
	res.StageArtifacts = res.StageArtifacts[:len(res.StageArtifacts)-1]
	wantCode(t, ValidateBuildResult(res), CodeBuildResultShapeInvalid)
}

func TestValidateRejectsReorderedStage(t *testing.T) {
	res := validBuilt(t)
	res.StageArtifacts[0], res.StageArtifacts[1] = res.StageArtifacts[1], res.StageArtifacts[0]
	wantCode(t, ValidateBuildResult(res), CodeStageContractInvalid)
}

// --- envelope integrity ---

func TestValidateRejectsTamperedBytes(t *testing.T) {
	res := validBuilt(t)
	i := stageIndex(res, closureprotocol.StageClosureAssessment)
	res.StageArtifacts[i].Bytes = append([]byte{}, append(res.StageArtifacts[i].Bytes, ' ')...)
	wantCode(t, ValidateBuildResult(res), CodeStageBytesInvalid)
}

func TestValidateRejectsWrongReceiptSelfDigest(t *testing.T) {
	res := validBuilt(t)
	i := stageIndex(res, closureprotocol.StageInferredClaims)
	res.StageArtifacts[i].Receipt.ReceiptDigestSHA256 = "1111111111111111111111111111111111111111111111111111111111111111"
	wantCode(t, ValidateBuildResult(res), CodeStageReceiptMismatch)
}

func TestValidateRejectsWrongProducer(t *testing.T) {
	res := validBuilt(t)
	i := stageIndex(res, closureprotocol.StagePlaneAssessment)
	res.StageArtifacts[i].Receipt.Producer.ID = "sensei.impostor"
	wantCode(t, ValidateBuildResult(res), CodeStageReceiptMismatch)
}

func TestValidateRejectsWrongLogicalPath(t *testing.T) {
	res := validBuilt(t)
	i := stageIndex(res, closureprotocol.StageInferredClaims)
	res.StageArtifacts[i].LogicalPath = "result-pipeline/somewhere-else.json"
	wantCode(t, ValidateBuildResult(res), CodeStageContractInvalid)
}

func TestValidateRejectsCrossResultReceipt(t *testing.T) {
	res := validBuilt(t)
	i := stageIndex(res, closureprotocol.StageMaintainedClaims)
	res.StageArtifacts[i].Receipt.ResultBindingDigestSHA256 = "2222222222222222222222222222222222222222222222222222222222222222"
	wantCode(t, ValidateBuildResult(res), CodeStageBindingMismatch)
}

// --- derivation ---

func TestValidateRejectsTamperedDerivationInputs(t *testing.T) {
	res := validBuilt(t)
	i := stageIndex(res, closureprotocol.StagePlaneAssessment)
	res.StageArtifacts[i].Derivation.InputArtifactReceiptDigestsSHA256 = []string{"3333333333333333333333333333333333333333333333333333333333333333"}
	wantCode(t, ValidateBuildResult(res), CodeStageDerivationMismatch)
}

// --- manifest ---

func TestValidateRejectsManifestStaleEntry(t *testing.T) {
	res := validBuilt(t)
	i := stageIndex(res, closureprotocol.StageArtifactManifest)
	var m ArtifactManifestBundle
	if err := strictDecode(res.StageArtifacts[i].Bytes, &m); err != nil {
		t.Fatal(err)
	}
	m.Stages[0].SemanticDigestSHA256 = "4444444444444444444444444444444444444444444444444444444444444444"
	// Re-render and rebind the manifest so it is internally canonical but stale
	// relative to its source artifact.
	rebindManifest(t, &res, m)
	wantCode(t, ValidateBuildResult(res), CodeArtifactManifestMismatch)
}

// --- strict decode unit ---

func TestStrictDecodeRejectsUnknownField(t *testing.T) {
	var m ArtifactManifestBundle
	if err := strictDecode([]byte(`{"schema_version":"1","nope":true}`), &m); err == nil {
		t.Fatal("expected unknown-field rejection")
	}
}

func TestStrictDecodeRejectsTrailing(t *testing.T) {
	var m ArtifactManifestBundle
	if err := strictDecode([]byte(`{"schema_version":"1"}{"x":1}`), &m); err == nil {
		t.Fatal("expected trailing-content rejection")
	}
}

// rebindStage9 re-renders the mutated top-level proof document into its Stage 9
// artifact and rebinds the receipt + derivation + manifest so only the intended
// semantic law is under test.
func rebindStage9(t *testing.T, res *BuildResult) {
	t.Helper()
	i := stageIndex(*res, closureprotocol.StageProofRequirements)
	art, err := jsonArtifact(closureprotocol.StageProofRequirements, "proof_requirements",
		"result-pipeline/proof-requirements.json", res.ProofRequirements, res.ResultBindingDigestSHA256,
		producer(ProducerProofRequirements), res.StageArtifacts[i].Derivation.InputArtifactReceiptDigestsSHA256)
	if err != nil {
		t.Fatal(err)
	}
	res.StageArtifacts[i] = art
	rebuildManifestFrom(t, res)
}

// rebindManifest replaces the Stage 10 artifact with a canonical rendering of m.
func rebindManifest(t *testing.T, res *BuildResult, m ArtifactManifestBundle) {
	t.Helper()
	i := stageIndex(*res, closureprotocol.StageArtifactManifest)
	inputs := res.StageArtifacts[i].Derivation.InputArtifactReceiptDigestsSHA256
	art, err := jsonArtifact(closureprotocol.StageArtifactManifest, "artifact_manifest",
		"result-pipeline/artifact-manifest.json", m, res.ResultBindingDigestSHA256, producer(ProducerResultPipeline), inputs)
	if err != nil {
		t.Fatal(err)
	}
	res.StageArtifacts[i] = art
}

// rebuildManifestFrom rebuilds the Stage 10 manifest from the current first-nine
// artifacts so the manifest stays consistent after a stage was rebound.
func rebuildManifestFrom(t *testing.T, res *BuildResult) {
	t.Helper()
	first9 := make([]PipelineArtifact, 0, 9)
	for _, stage := range closureprotocol.ResultPipelineStages[:9] {
		first9 = append(first9, res.StageArtifacts[stageIndex(*res, stage)])
	}
	manifest, err := artifactManifest(res.ResultBindingDigestSHA256, first9)
	if err != nil {
		t.Fatal(err)
	}
	res.StageArtifacts[stageIndex(*res, closureprotocol.StageArtifactManifest)] = manifest
}
