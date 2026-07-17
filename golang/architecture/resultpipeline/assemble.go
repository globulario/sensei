// SPDX-License-Identifier: Apache-2.0

package resultpipeline

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/graphbuild"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/plane"
)

// GovernedSourceManifestBundle is the stage-1 canonical output. Its semantic
// identity includes the graph-input policy, the immutable snapshot digest, the
// logical source roots, and the supplemental graph identities — so graph-input
// parity is inspectable from the stage artifact — plus the graphbuild source
// manifest. It never contains an absolute path, supplemental bytes, an
// active-pointer location, or a temporary path.
type GovernedSourceManifestBundle struct {
	SchemaVersion                  string                                `json:"schema_version" yaml:"schema_version"`
	GraphInputPolicyID             string                                `json:"graph_input_policy_id" yaml:"graph_input_policy_id"`
	GraphInputSnapshotDigestSHA256 string                                `json:"graph_input_snapshot_digest_sha256" yaml:"graph_input_snapshot_digest_sha256"`
	RepositoryDomain               string                                `json:"repository_domain" yaml:"repository_domain"`
	LogicalSourceRoots             []graphbuild.LogicalSourceRoot        `json:"logical_source_roots" yaml:"logical_source_roots"`
	SupplementalGraphs             []graphbuild.SupplementalGraphBinding `json:"supplemental_graphs" yaml:"supplemental_graphs"`
	SourceManifest                 graphbuild.SourceManifest             `json:"source_manifest" yaml:"source_manifest"`
}

func governedSourceManifestBundle(cg compiledGraph) GovernedSourceManifestBundle {
	return GovernedSourceManifestBundle{
		SchemaVersion:                  "resultpipeline.governed-source-manifest/v1",
		GraphInputPolicyID:             cg.snapshot.PolicyID,
		GraphInputSnapshotDigestSHA256: cg.snapshot.SnapshotDigestSHA256,
		RepositoryDomain:               cg.snapshot.RepositoryDomain,
		LogicalSourceRoots:             cg.snapshot.SourceRoots,
		SupplementalGraphs:             cg.snapshot.SupplementalGraphs,
		SourceManifest:                 cg.compilation.SourceManifest,
	}
}

// governedGeneratedPaths is the closed, versioned set of repository-generated
// artifacts a Phase 7 result may carry. Stage 2 verifies them by presence and
// exact byte digest against the materialized result tree; it never writes them.
var governedGeneratedPaths = []string{
	"docs/awareness/generated/proof_obligations.yaml",
	"golang/server/embeddata/awareness.nt",
	"golang/server/embeddata/awareness.transaction.tsv",
}

// GeneratedArtifactEntry records one governed generated artifact observed in the
// result tree.
type GeneratedArtifactEntry struct {
	Path             string `json:"path" yaml:"path"`
	ByteDigestSHA256 string `json:"byte_digest_sha256" yaml:"byte_digest_sha256"`
	Status           string `json:"status" yaml:"status"`
}

// GeneratedManifest is the stage-2 canonical output.
type GeneratedManifest struct {
	SchemaVersion string                   `json:"schema_version" yaml:"schema_version"`
	GeneratedBy   string                   `json:"generated_by" yaml:"generated_by"`
	Artifacts     []GeneratedArtifactEntry `json:"artifacts" yaml:"artifacts"`
	Limitations   []string                 `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

// verifyGeneratedArtifacts records the governed generated artifacts present in
// the materialized result tree by exact byte digest and populates the result
// binding's GeneratedArtifacts. It never writes a repository file. Producer
// regeneration (regenerate-and-compare) is recorded as a limitation rather than
// silently claimed.
func verifyGeneratedArtifacts(resultRoot string) ([]closureprotocol.ResultArtifact, GeneratedManifest, []string) {
	manifest := GeneratedManifest{SchemaVersion: "1", GeneratedBy: "sensei.generated-artifacts"}
	var generated []closureprotocol.ResultArtifact
	for _, rel := range governedGeneratedPaths {
		abs := filepath.Join(resultRoot, filepath.FromSlash(rel))
		data, err := os.ReadFile(abs)
		if err != nil {
			manifest.Artifacts = append(manifest.Artifacts, GeneratedArtifactEntry{Path: rel, Status: "not_present"})
			continue
		}
		digest := sha256hex(data)
		manifest.Artifacts = append(manifest.Artifacts, GeneratedArtifactEntry{Path: rel, ByteDigestSHA256: digest, Status: "present"})
		generated = append(generated, closureprotocol.ResultArtifact{Path: rel, DigestSHA256: digest})
	}
	sort.Slice(manifest.Artifacts, func(i, j int) bool { return manifest.Artifacts[i].Path < manifest.Artifacts[j].Path })
	sort.Slice(generated, func(i, j int) bool { return generated[i].Path < generated[j].Path })
	limitations := []string{"generated repository artifacts recorded by presence and exact byte digest; producer regeneration-and-compare verification is not yet performed"}
	manifest.Limitations = limitations
	return generated, manifest, limitations
}

// graphArtifact builds the stage-3 architecture-graph artifact from the stamped
// N-Triples: its semantic digest is the graph marker digest and its byte digest
// is over the serialized N-Triples.
func graphArtifact(rbDigest, graphSemanticDigest string, ntriples []byte, inputReceiptDigests []string) (PipelineArtifact, error) {
	receipt, err := newReceipt(
		closureprotocol.StageArchitectureGraph,
		"architecture_graph",
		"result-pipeline/architecture.nt",
		ntriplesMediaType,
		graphSemanticDigest,
		sha256hex(ntriples),
		rbDigest,
		producer(ProducerGraphbuild),
	)
	if err != nil {
		return PipelineArtifact{}, err
	}
	return PipelineArtifact{
		Stage:       closureprotocol.StageArchitectureGraph,
		LogicalPath: "result-pipeline/architecture.nt",
		MediaType:   ntriplesMediaType,
		Bytes:       ntriples,
		Receipt:     receipt,
		Derivation:  newDerivation(closureprotocol.StageArchitectureGraph, receipt.ReceiptDigestSHA256, inputReceiptDigests, rbDigest),
	}, nil
}

// assembleStages builds the first nine stage artifacts in order, threading each
// prior stage's receipt digest into the next stage's derivation inputs. Input
// bindings are always the current result binding; the base-claim reference is an
// internal maintenance input, not a frozen derivation edge.
func assembleStages(
	rbDigest string,
	cg compiledGraph,
	inferred InferredClaimsBundle,
	maint maintenance.Result,
	planeRep plane.Report,
	closureRep closure.Report,
	questions ArchitectQuestionsBundle,
	proofDoc ProofRequirementDocument,
	gen GeneratedManifest,
) ([]PipelineArtifact, error) {
	var arts []PipelineArtifact

	a1, err := jsonArtifact(closureprotocol.StageGovernedSourceManifest, "governed_source_manifest",
		"result-pipeline/governed-source-manifest.json", governedSourceManifestBundle(cg), rbDigest, producer(ProducerGraphbuild), nil)
	if err != nil {
		return nil, err
	}
	d1 := a1.Receipt.ReceiptDigestSHA256

	// The architecture graph derives from the governed source manifest alone. The
	// generated-repository-artifact stage derives from the governed source
	// manifest AND the architecture graph, because it verifies the graph's output
	// mirrors — so it is built after the graph but presented before it in the
	// canonical stage order.
	a3, err := graphArtifact(rbDigest, cg.artifact.GraphSemanticDigestSHA256, cg.artifact.NTriples, []string{d1})
	if err != nil {
		return nil, err
	}
	d3 := a3.Receipt.ReceiptDigestSHA256

	a2, err := jsonArtifact(closureprotocol.StageGeneratedRepositoryArtifacts, "generated_repository_artifacts",
		"result-pipeline/generated-repository-artifacts.json", gen, rbDigest, producer(ProducerGeneratedArtifact), []string{d1, d3})
	if err != nil {
		return nil, err
	}

	arts = append(arts, a1, a2, a3)

	a4, err := jsonArtifact(closureprotocol.StageInferredClaims, "inferred_claims",
		"result-pipeline/inferred-claims.json", inferred, rbDigest, producer(ProducerClaimbuild), []string{d3})
	if err != nil {
		return nil, err
	}
	arts = append(arts, a4)
	d4 := a4.Receipt.ReceiptDigestSHA256

	maintBundle := MaintainedClaimsBundle{Document: maint.Document, Report: maint.Report}
	a5, err := jsonArtifact(closureprotocol.StageMaintainedClaims, "maintained_claims",
		"result-pipeline/maintained-claims.json", maintBundle, rbDigest, producer(ProducerMaintenance), []string{d4})
	if err != nil {
		return nil, err
	}
	arts = append(arts, a5)
	d5 := a5.Receipt.ReceiptDigestSHA256

	a6, err := jsonArtifact(closureprotocol.StagePlaneAssessment, "plane_assessment",
		"result-pipeline/plane-assessment.json", planeRep, rbDigest, producer(ProducerPlane), []string{d5, d3})
	if err != nil {
		return nil, err
	}
	arts = append(arts, a6)
	d6 := a6.Receipt.ReceiptDigestSHA256

	a7, err := jsonArtifact(closureprotocol.StageClosureAssessment, "closure_assessment",
		"result-pipeline/closure-assessment.json", closureRep, rbDigest, producer(ProducerClosure), []string{d6, d5, d3})
	if err != nil {
		return nil, err
	}
	arts = append(arts, a7)
	d7 := a7.Receipt.ReceiptDigestSHA256

	a8, err := jsonArtifact(closureprotocol.StageArchitectQuestions, "architect_questions",
		"result-pipeline/architect-questions.json", questions, rbDigest, producer(ProducerQuestiongen), []string{d7, d5, d3})
	if err != nil {
		return nil, err
	}
	arts = append(arts, a8)
	d8 := a8.Receipt.ReceiptDigestSHA256

	a9, err := jsonArtifact(closureprotocol.StageProofRequirements, "proof_requirements",
		"result-pipeline/proof-requirements.json", proofDoc, rbDigest, producer(ProducerProofRequirements), []string{d7, d8, d3})
	if err != nil {
		return nil, err
	}
	arts = append(arts, a9)

	return arts, nil
}

// ArtifactManifestEntry lists one stage artifact inside the stage-10 manifest.
type ArtifactManifestEntry struct {
	Stage                string   `json:"stage" yaml:"stage"`
	LogicalPath          string   `json:"logical_path" yaml:"logical_path"`
	MediaType            string   `json:"media_type" yaml:"media_type"`
	SemanticDigestSHA256 string   `json:"semantic_digest_sha256" yaml:"semantic_digest_sha256"`
	ByteDigestSHA256     string   `json:"byte_digest_sha256" yaml:"byte_digest_sha256"`
	ProducerID           string   `json:"producer_id" yaml:"producer_id"`
	ProducerVersion      string   `json:"producer_version" yaml:"producer_version"`
	ReceiptDigestSHA256  string   `json:"receipt_digest_sha256" yaml:"receipt_digest_sha256"`
	DerivationInputs     []string `json:"derivation_inputs,omitempty" yaml:"derivation_inputs,omitempty"`
}

// ArtifactManifestBundle is the stage-10 content: it lists and binds the first
// nine stage artifacts and their derivations, and never contains its own
// receipt, receipt digest, or derivation output (no self-reference).
type ArtifactManifestBundle struct {
	SchemaVersion             string                  `json:"schema_version" yaml:"schema_version"`
	GeneratedBy               string                  `json:"generated_by" yaml:"generated_by"`
	ResultBindingDigestSHA256 string                  `json:"result_binding_digest_sha256" yaml:"result_binding_digest_sha256"`
	Stages                    []ArtifactManifestEntry `json:"stages" yaml:"stages"`
}

// artifactManifest builds the stage-10 artifact from the first nine stages, then
// creates its own receipt and derivation over the manifest bytes.
func artifactManifest(rbDigest string, priorStages []PipelineArtifact) (PipelineArtifact, error) {
	bundle := ArtifactManifestBundle{
		SchemaVersion:             "1",
		GeneratedBy:               ProducerResultPipeline,
		ResultBindingDigestSHA256: rbDigest,
	}
	var inputs []string
	for _, a := range priorStages {
		bundle.Stages = append(bundle.Stages, ArtifactManifestEntry{
			Stage:                string(a.Stage),
			LogicalPath:          a.LogicalPath,
			MediaType:            a.MediaType,
			SemanticDigestSHA256: a.Receipt.SemanticDigestSHA256,
			ByteDigestSHA256:     a.Receipt.ByteDigestSHA256,
			ProducerID:           a.Receipt.Producer.ID,
			ProducerVersion:      a.Receipt.Producer.Version,
			ReceiptDigestSHA256:  a.Receipt.ReceiptDigestSHA256,
			DerivationInputs:     a.Derivation.InputArtifactReceiptDigestsSHA256,
		})
		inputs = append(inputs, a.Receipt.ReceiptDigestSHA256)
	}
	return jsonArtifact(closureprotocol.StageArtifactManifest, "artifact_manifest",
		"result-pipeline/artifact-manifest.json", bundle, rbDigest, producer(ProducerResultPipeline), inputs)
}
