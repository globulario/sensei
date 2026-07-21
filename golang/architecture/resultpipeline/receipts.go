// SPDX-License-Identifier: AGPL-3.0-only

package resultpipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

// Producer identity is the algorithm identity of a stage, deliberately stable and
// independent of the Sensei release number so a receipt records what produced the
// artifact, not which build shipped it.
const (
	ProducerGraphbuild        = "sensei.graphbuild"
	ProducerGeneratedArtifact = "sensei.generated-artifacts"
	ProducerClaimbuild        = "sensei.claimbuild"
	ProducerMaintenance       = "sensei.maintenance"
	ProducerPlane             = "sensei.plane"
	ProducerClosure           = "sensei.closure"
	ProducerQuestiongen       = "sensei.questiongen"
	ProducerProofRequirements = "sensei.proofrequirements"
	ProducerResultPipeline    = "sensei.resultpipeline"

	ProducerVersion = "v1"

	// NTriplesMediaType and jsonMediaType are the serialized media types of stage
	// artifacts.
	ntriplesMediaType = "application/n-triples"
	jsonMediaType     = "application/json"

	// DefaultPipelinePolicyID names the fixed source-root selection and validation
	// policy for a Phase 7 result pipeline.
	DefaultPipelinePolicyID = "sensei.resultpipeline.closure-strict/v1"
)

// BuildRequest asks for the complete result architecture of one admitted,
// scope-verified task.
type BuildRequest struct {
	RepositoryRoot string
	TaskDirectory  string

	ResultMode     resulttransition.ResultMode
	ResultRevision string

	// RepositoryDomain tags governed structural nodes and the result binding to
	// this repository. When empty it is resolved from the admitted base binding.
	RepositoryDomain string

	// PipelinePolicyID overrides DefaultPipelinePolicyID when set.
	PipelinePolicyID string
}

// PipelineArtifact is one mandatory stage's canonical output: its serialized
// bytes, its first-class artifact receipt, and its derivation edge.
type PipelineArtifact struct {
	Stage       closureprotocol.ResultPipelineStage
	LogicalPath string
	MediaType   string
	Bytes       []byte

	Receipt    closureprotocol.ArtifactReceipt
	Derivation closureprotocol.ArtifactDerivation
}

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// stageJSONBytes renders a stage bundle to stable, human-inspectable JSON. It is
// deterministic (encoding/json sorts map keys and preserves struct field order),
// and distinct from the canonical semantic form, so an artifact's byte digest and
// semantic digest are separate identities.
func stageJSONBytes(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// producer returns a stable ArtifactProducer.
func producer(id string) closureprotocol.ArtifactProducer {
	return closureprotocol.ArtifactProducer{ID: id, Version: ProducerVersion}
}

// newReceipt builds and self-digests an operational artifact receipt bound to the
// exact current result. semanticSource is the value whose canonical semantic
// digest is the artifact's content identity; artifactBytes is its serialized
// form. For an artifact with pre-serialized bytes (e.g. N-Triples) pass the same
// bytes as both the byte form and, via semanticBytes, the semantic source.
func newReceipt(
	stage closureprotocol.ResultPipelineStage,
	kind, logicalPath, mediaType string,
	semanticDigest, byteDigest, resultBindingDigest string,
	prod closureprotocol.ArtifactProducer,
) (closureprotocol.ArtifactReceipt, error) {
	r := closureprotocol.ArtifactReceipt{
		ID:                        "artifact." + string(stage),
		Kind:                      kind,
		Path:                      logicalPath,
		MediaType:                 mediaType,
		SemanticDigestSHA256:      semanticDigest,
		ByteDigestSHA256:          byteDigest,
		Producer:                  prod,
		ResultBindingDigestSHA256: resultBindingDigest,
	}
	d, err := closureprotocol.ArtifactReceiptDigest(r)
	if err != nil {
		return closureprotocol.ArtifactReceipt{}, err
	}
	r.ReceiptDigestSHA256 = d
	return r, nil
}

// jsonArtifact assembles a JSON-serialized stage artifact: it renders the bundle,
// computes the semantic digest over the bundle and the byte digest over the
// serialized bytes, and builds the self-digested receipt and the derivation edge.
func jsonArtifact(
	stage closureprotocol.ResultPipelineStage,
	kind, logicalPath string,
	bundle any,
	resultBindingDigest string,
	prod closureprotocol.ArtifactProducer,
	inputReceiptDigests []string,
) (PipelineArtifact, error) {
	bytes, err := stageJSONBytes(bundle)
	if err != nil {
		return PipelineArtifact{}, err
	}
	sem, err := closureprotocol.SemanticDigest(bundle)
	if err != nil {
		return PipelineArtifact{}, err
	}
	receipt, err := newReceipt(stage, kind, logicalPath, jsonMediaType, sem, sha256hex(bytes), resultBindingDigest, prod)
	if err != nil {
		return PipelineArtifact{}, err
	}
	return PipelineArtifact{
		Stage:       stage,
		LogicalPath: logicalPath,
		MediaType:   jsonMediaType,
		Bytes:       bytes,
		Receipt:     receipt,
		Derivation:  newDerivation(stage, receipt.ReceiptDigestSHA256, inputReceiptDigests, resultBindingDigest),
	}, nil
}

// newDerivation builds a derivation edge whose only input binding is the current
// result binding (the frozen validator requires every input binding to equal the
// current result binding digest).
func newDerivation(stage closureprotocol.ResultPipelineStage, output string, inputReceiptDigests []string, resultBindingDigest string) closureprotocol.ArtifactDerivation {
	inputs := make([]string, 0, len(inputReceiptDigests))
	for _, d := range inputReceiptDigests {
		if d != "" {
			inputs = append(inputs, d)
		}
	}
	return closureprotocol.ArtifactDerivation{
		Stage:                             stage,
		OutputArtifactReceiptDigestSHA256: output,
		InputArtifactReceiptDigestsSHA256: inputs,
		InputBindingDigestsSHA256:         []string{resultBindingDigest},
	}
}
