// SPDX-License-Identifier: AGPL-3.0-only

package resultpipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// Machine-stable validation error codes. Callers and tests distinguish outcomes
// by Code, never by prose.
const (
	CodeBuildResultShapeInvalid      = "resultpipeline.build_result_shape_invalid"
	CodeResultBindingMismatch        = "resultpipeline.result_binding_mismatch"
	CodeStageContractInvalid         = "resultpipeline.stage_contract_invalid"
	CodeStageBytesInvalid            = "resultpipeline.stage_bytes_invalid"
	CodeStageSemanticDigestMismatch  = "resultpipeline.stage_semantic_digest_mismatch"
	CodeStageReceiptMismatch         = "resultpipeline.stage_receipt_mismatch"
	CodeStageDerivationMismatch      = "resultpipeline.stage_derivation_mismatch"
	CodeStageBindingMismatch         = "resultpipeline.stage_binding_mismatch"
	CodeStageContentMismatch         = "resultpipeline.stage_content_mismatch"
	CodeArtifactManifestMismatch     = "resultpipeline.artifact_manifest_mismatch"
	CodeProofExtractionIncomplete    = "resultpipeline.proof_extraction_incomplete"
	CodeProofExtractionUncertifiable = "resultpipeline.proof_extraction_uncertifiable"
	CodeTransitionContractInvalid    = "resultpipeline.transition_contract_invalid"
)

// ValidationError is a typed, code-stable validation failure.
type ValidationError struct {
	Code   string
	Stage  closureprotocol.ResultPipelineStage
	Detail string
}

func (e *ValidationError) Error() string {
	if e.Stage != "" {
		return fmt.Sprintf("%s [%s]: %s", e.Code, e.Stage, e.Detail)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Detail)
}

func verr(code string, stage closureprotocol.ResultPipelineStage, format string, args ...any) *ValidationError {
	return &ValidationError{Code: code, Stage: stage, Detail: fmt.Sprintf(format, args...)}
}

// stageContract is the closed contract for one canonical stage.
type stageContract struct {
	Stage      closureprotocol.ResultPipelineStage
	Kind       string
	Path       string
	MediaType  string
	ProducerID string
	Inputs     []closureprotocol.ResultPipelineStage
}

// stageContracts is the one authoritative contract table, keyed by stage. The
// canonical order is closureprotocol.ResultPipelineStages; this table never
// defines a competing order.
var stageContracts = map[closureprotocol.ResultPipelineStage]stageContract{
	closureprotocol.StageGovernedSourceManifest: {
		Stage: closureprotocol.StageGovernedSourceManifest, Kind: "governed_source_manifest",
		Path: "result-pipeline/governed-source-manifest.json", MediaType: jsonMediaType, ProducerID: ProducerGraphbuild,
		Inputs: nil,
	},
	closureprotocol.StageGeneratedRepositoryArtifacts: {
		Stage: closureprotocol.StageGeneratedRepositoryArtifacts, Kind: "generated_repository_artifacts",
		Path: "result-pipeline/generated-repository-artifacts.json", MediaType: jsonMediaType, ProducerID: ProducerGeneratedArtifact,
		Inputs: []closureprotocol.ResultPipelineStage{closureprotocol.StageGovernedSourceManifest, closureprotocol.StageArchitectureGraph},
	},
	closureprotocol.StageArchitectureGraph: {
		Stage: closureprotocol.StageArchitectureGraph, Kind: "architecture_graph",
		Path: "result-pipeline/architecture.nt", MediaType: ntriplesMediaType, ProducerID: ProducerGraphbuild,
		Inputs: []closureprotocol.ResultPipelineStage{closureprotocol.StageGovernedSourceManifest},
	},
	closureprotocol.StageInferredClaims: {
		Stage: closureprotocol.StageInferredClaims, Kind: "inferred_claims",
		Path: "result-pipeline/inferred-claims.json", MediaType: jsonMediaType, ProducerID: ProducerClaimbuild,
		Inputs: []closureprotocol.ResultPipelineStage{closureprotocol.StageArchitectureGraph},
	},
	closureprotocol.StageMaintainedClaims: {
		Stage: closureprotocol.StageMaintainedClaims, Kind: "maintained_claims",
		Path: "result-pipeline/maintained-claims.json", MediaType: jsonMediaType, ProducerID: ProducerMaintenance,
		Inputs: []closureprotocol.ResultPipelineStage{closureprotocol.StageInferredClaims},
	},
	closureprotocol.StagePlaneAssessment: {
		Stage: closureprotocol.StagePlaneAssessment, Kind: "plane_assessment",
		Path: "result-pipeline/plane-assessment.json", MediaType: jsonMediaType, ProducerID: ProducerPlane,
		Inputs: []closureprotocol.ResultPipelineStage{closureprotocol.StageMaintainedClaims, closureprotocol.StageArchitectureGraph},
	},
	closureprotocol.StageClosureAssessment: {
		Stage: closureprotocol.StageClosureAssessment, Kind: "closure_assessment",
		Path: "result-pipeline/closure-assessment.json", MediaType: jsonMediaType, ProducerID: ProducerClosure,
		Inputs: []closureprotocol.ResultPipelineStage{closureprotocol.StagePlaneAssessment, closureprotocol.StageMaintainedClaims, closureprotocol.StageArchitectureGraph},
	},
	closureprotocol.StageArchitectQuestions: {
		Stage: closureprotocol.StageArchitectQuestions, Kind: "architect_questions",
		Path: "result-pipeline/architect-questions.json", MediaType: jsonMediaType, ProducerID: ProducerQuestiongen,
		Inputs: []closureprotocol.ResultPipelineStage{closureprotocol.StageClosureAssessment, closureprotocol.StageMaintainedClaims, closureprotocol.StageArchitectureGraph},
	},
	closureprotocol.StageProofRequirements: {
		Stage: closureprotocol.StageProofRequirements, Kind: "proof_requirements",
		Path: "result-pipeline/proof-requirements.json", MediaType: jsonMediaType, ProducerID: ProducerProofRequirements,
		Inputs: []closureprotocol.ResultPipelineStage{closureprotocol.StageGeneratedRepositoryArtifacts, closureprotocol.StageArchitectureGraph, closureprotocol.StageClosureAssessment, closureprotocol.StageArchitectQuestions},
	},
	closureprotocol.StageArtifactManifest: {
		Stage: closureprotocol.StageArtifactManifest, Kind: "artifact_manifest",
		Path: "result-pipeline/artifact-manifest.json", MediaType: jsonMediaType, ProducerID: ProducerResultPipeline,
		Inputs: firstNineStages(),
	},
}

func firstNineStages() []closureprotocol.ResultPipelineStage {
	return append([]closureprotocol.ResultPipelineStage{}, closureprotocol.ResultPipelineStages[:9]...)
}

// ValidateBuildResult is the final pure, offline semantic gate. It reads nothing
// and reconstructs nothing: given a completed in-memory BuildResult it establishes
// that the exact upstream truth, the frozen result binding, all ten canonical
// stage artifacts with their receipts and derivation graph, the Stage 10 manifest,
// the duplicated top-level result views, and complete proof-requirement extraction
// describe one internally consistent result architecture. It never mutates result.
func ValidateBuildResult(result BuildResult) error {
	// §2: validity is derived, never a stored flag.
	if strings.TrimSpace(result.ResultBindingDigestSHA256) == "" {
		return verr(CodeBuildResultShapeInvalid, "", "missing result binding digest")
	}
	if len(result.StageArtifacts) != len(closureprotocol.ResultPipelineStages) {
		return verr(CodeBuildResultShapeInvalid, "", "expected %d stage artifacts, got %d", len(closureprotocol.ResultPipelineStages), len(result.StageArtifacts))
	}

	// §8: top-level result binding.
	if err := validateTopLevelBinding(result); err != nil {
		return err
	}
	// §9: carried upstream truth.
	if err := validateUpstreamTruth(result); err != nil {
		return err
	}

	// Stage set: exactly the canonical stages, in canonical order, each once.
	byStage := map[closureprotocol.ResultPipelineStage]PipelineArtifact{}
	for i, art := range result.StageArtifacts {
		want := closureprotocol.ResultPipelineStages[i]
		if art.Stage != want {
			return verr(CodeStageContractInvalid, art.Stage, "stage %d is %q, want canonical %q", i, art.Stage, want)
		}
		if _, dup := byStage[art.Stage]; dup {
			return verr(CodeStageContractInvalid, art.Stage, "duplicate stage")
		}
		byStage[art.Stage] = art
	}

	// Pass 1: envelope (stage identity, receipt identity, self-digest, byte digest).
	receiptDigest := map[closureprotocol.ResultPipelineStage]string{}
	for _, stage := range closureprotocol.ResultPipelineStages {
		art := byStage[stage]
		if err := validateEnvelope(result, art); err != nil {
			return err
		}
		receiptDigest[stage] = art.Receipt.ReceiptDigestSHA256
	}

	// Pass 2: derivation identity + strict typed decode + semantic identity +
	// cross-stage consistency. Cross-stage digests are threaded through `dc`.
	dc := &decodeContext{result: result}
	for _, stage := range closureprotocol.ResultPipelineStages[:9] {
		art := byStage[stage]
		if err := validateDerivation(art, receiptDigest); err != nil {
			return err
		}
		if err := decodeAndCheckStage(dc, art); err != nil {
			return err
		}
	}

	// Stage 10 manifest, exact.
	if err := validateManifest(result, byStage, receiptDigest); err != nil {
		return err
	}

	// §18: the shared generic topology contract, last.
	receipts := make([]closureprotocol.ArtifactReceipt, 0, len(result.StageArtifacts))
	derivations := make([]closureprotocol.ArtifactDerivation, 0, len(result.StageArtifacts))
	for _, art := range result.StageArtifacts {
		receipts = append(receipts, art.Receipt)
		derivations = append(derivations, art.Derivation)
	}
	if err := closureprotocol.ValidateResultPipelineContract(closureprotocol.ResultPipelineContract{
		BaseBindingDigestSHA256:       result.BoundRepositoryResult.BaseBindingDigestSHA256,
		ObservedChangeSetDigestSHA256: result.BoundRepositoryResult.ObservedChangeSetDigestSHA256,
		ResultBinding:                 result.ResultBinding,
		ResultBindingDigestSHA256:     result.ResultBindingDigestSHA256,
		OperationalArtifactReceipts:   receipts,
		Derivations:                   derivations,
	}); err != nil {
		return verr(CodeTransitionContractInvalid, "", "%v", err)
	}
	return nil
}

// validateTopLevelBinding implements §8.
func validateTopLevelBinding(result BuildResult) error {
	if err := closureprotocol.ValidateResultBinding(result.ResultBinding); err != nil {
		return verr(CodeResultBindingMismatch, "", "invalid result binding: %v", err)
	}
	d, err := closureprotocol.ResultBindingDigest(result.ResultBinding)
	if err != nil {
		return verr(CodeResultBindingMismatch, "", "result binding digest: %v", err)
	}
	if d != result.ResultBindingDigestSHA256 {
		return verr(CodeResultBindingMismatch, "", "recomputed result binding digest does not match")
	}
	rr := result.BoundRepositoryResult.RepositoryResult
	if result.ResultBinding.BaseRevision != rr.BaseRevision {
		return verr(CodeResultBindingMismatch, "", "base revision differs from bound repository result")
	}
	if result.ResultBinding.PatchDigestSHA256 != rr.PatchDigestSHA256 {
		return verr(CodeResultBindingMismatch, "", "patch digest differs from bound repository result")
	}
	if result.ResultBinding.ResultTreeDigestSHA256 != rr.ResultTreeDigestSHA256 {
		return verr(CodeResultBindingMismatch, "", "result tree digest differs from bound repository result")
	}
	if result.ResultBinding.ResultRevision != rr.ResultRevision {
		return verr(CodeResultBindingMismatch, "", "result revision differs from bound repository result")
	}
	return nil
}

// validateUpstreamTruth implements §9: recompute the carried records and require
// their digests to equal the binding's fields. No ledger read.
func validateUpstreamTruth(result BuildResult) error {
	b := result.BoundRepositoryResult
	authD, err := closureprotocol.AuthorityResolutionDigest(b.AuthorityResolution)
	if err != nil {
		return verr(CodeBuildResultShapeInvalid, "", "authority resolution digest: %v", err)
	}
	if authD != b.AuthorityResolutionDigestSHA256 {
		return verr(CodeResultBindingMismatch, "", "carried authority resolution digest does not recompute")
	}
	if sd := strings.TrimSpace(b.AuthorityResolution.AuthorityResolutionDigestSHA256); sd != "" && sd != authD {
		return verr(CodeResultBindingMismatch, "", "authority resolution embedded self-digest is inconsistent")
	}
	if closureprotocol.MustSemanticDigest(b.AdmissionDecision) != b.AdmissionDecisionDigestSHA256 {
		return verr(CodeResultBindingMismatch, "", "carried admission decision digest does not recompute")
	}
	if closureprotocol.MustSemanticDigest(b.ObservedChange) != b.ObservedChangeSetDigestSHA256 {
		return verr(CodeResultBindingMismatch, "", "carried observed change digest does not recompute")
	}
	for name, v := range map[string]string{
		"base_binding_digest_sha256":         b.BaseBindingDigestSHA256,
		"authority_resolution_digest_sha256": b.AuthorityResolutionDigestSHA256,
		"admission_decision_digest_sha256":   b.AdmissionDecisionDigestSHA256,
		"observed_change_set_digest_sha256":  b.ObservedChangeSetDigestSHA256,
	} {
		if !isHex64(v) {
			return verr(CodeResultBindingMismatch, "", "%s is not a 64-hex sha256", name)
		}
	}
	return nil
}

// validateEnvelope implements the §7 stage/receipt identity, receipt self-digest,
// and byte-digest laws for one artifact.
func validateEnvelope(result BuildResult, art PipelineArtifact) error {
	c, ok := stageContracts[art.Stage]
	if !ok {
		return verr(CodeStageContractInvalid, art.Stage, "unknown stage")
	}
	if art.LogicalPath != c.Path {
		return verr(CodeStageContractInvalid, art.Stage, "logical path %q, want %q", art.LogicalPath, c.Path)
	}
	if art.MediaType != c.MediaType {
		return verr(CodeStageContractInvalid, art.Stage, "media type %q, want %q", art.MediaType, c.MediaType)
	}
	r := art.Receipt
	if r.ID != "artifact."+string(art.Stage) {
		return verr(CodeStageReceiptMismatch, art.Stage, "receipt id %q", r.ID)
	}
	if r.Kind != c.Kind {
		return verr(CodeStageReceiptMismatch, art.Stage, "receipt kind %q, want %q", r.Kind, c.Kind)
	}
	if r.Path != art.LogicalPath {
		return verr(CodeStageReceiptMismatch, art.Stage, "receipt path %q, want %q", r.Path, art.LogicalPath)
	}
	if r.MediaType != art.MediaType {
		return verr(CodeStageReceiptMismatch, art.Stage, "receipt media type %q", r.MediaType)
	}
	if r.Producer.ID != c.ProducerID {
		return verr(CodeStageReceiptMismatch, art.Stage, "producer %q, want %q", r.Producer.ID, c.ProducerID)
	}
	if r.Producer.Version != ProducerVersion {
		return verr(CodeStageReceiptMismatch, art.Stage, "producer version %q, want %q", r.Producer.Version, ProducerVersion)
	}
	if r.ResultBindingDigestSHA256 != result.ResultBindingDigestSHA256 {
		return verr(CodeStageBindingMismatch, art.Stage, "receipt bound to a different result")
	}
	if err := closureprotocol.ValidateArtifactReceipt(r); err != nil {
		return verr(CodeStageReceiptMismatch, art.Stage, "invalid artifact receipt: %v", err)
	}
	self, err := closureprotocol.ArtifactReceiptDigest(r)
	if err != nil {
		return verr(CodeStageReceiptMismatch, art.Stage, "receipt digest: %v", err)
	}
	if self != r.ReceiptDigestSHA256 {
		return verr(CodeStageReceiptMismatch, art.Stage, "receipt self-digest does not recompute")
	}
	if sha256hex(art.Bytes) != r.ByteDigestSHA256 {
		return verr(CodeStageBytesInvalid, art.Stage, "byte digest does not match artifact bytes")
	}
	return nil
}

// validateDerivation implements the §7 derivation-identity law for a first-nine
// stage: the edge names this stage, outputs this receipt, has exactly one input
// binding (the current result), and its input receipt digests exactly match the
// contract's input stages in order.
func validateDerivation(art PipelineArtifact, receiptDigest map[closureprotocol.ResultPipelineStage]string) error {
	c := stageContracts[art.Stage]
	d := art.Derivation
	if d.Stage != art.Stage {
		return verr(CodeStageDerivationMismatch, art.Stage, "derivation stage %q", d.Stage)
	}
	if d.OutputArtifactReceiptDigestSHA256 != art.Receipt.ReceiptDigestSHA256 {
		return verr(CodeStageDerivationMismatch, art.Stage, "derivation output is not this receipt")
	}
	if len(d.InputBindingDigestsSHA256) != 1 || d.InputBindingDigestsSHA256[0] != art.Receipt.ResultBindingDigestSHA256 {
		return verr(CodeStageDerivationMismatch, art.Stage, "derivation input binding is not exactly the current result")
	}
	want := make([]string, 0, len(c.Inputs))
	for _, in := range c.Inputs {
		want = append(want, receiptDigest[in])
	}
	if len(d.InputArtifactReceiptDigestsSHA256) != len(want) {
		return verr(CodeStageDerivationMismatch, art.Stage, "derivation has %d inputs, want %d", len(d.InputArtifactReceiptDigestsSHA256), len(want))
	}
	for i := range want {
		if d.InputArtifactReceiptDigestsSHA256[i] != want[i] {
			return verr(CodeStageDerivationMismatch, art.Stage, "derivation input %d does not match contract stage %q", i, c.Inputs[i])
		}
	}
	return nil
}

// decodeContext threads cross-stage values discovered while decoding.
type decodeContext struct {
	result BuildResult

	repoDomain            string
	graphSemantic         string
	stage2Semantic        string
	proofObligationSem    string
	closureSemantic       string
	questionsSemantic     string
	generatedArtifactsSem string
	closureBlockers       map[string]bool
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// strictDecode decodes exactly one JSON document into v, rejecting unknown fields
// and any trailing content.
func strictDecode(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("trailing content after JSON document")
	}
	return nil
}
