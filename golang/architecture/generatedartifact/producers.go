// SPDX-License-Identifier: Apache-2.0

package generatedartifact

import (
	"context"
	"fmt"

	"github.com/globulario/sensei/golang/architecture/buildtransaction"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/factextract"
	"github.com/globulario/sensei/golang/architecture/proofrequirements"
)

const (
	proofObligationsProducerID = "sensei.proofrequirements.repository-artifact"
	embeddedGraphProducerID    = "sensei.graphbuild.embedded-artifact"
	resultManifestProducerID   = "sensei.buildtransaction"

	proofObligationsPath = "docs/awareness/generated/proof_obligations.yaml"
	embeddedGraphPath    = "golang/server/embeddata/awareness.nt"
	resultManifestPath   = "golang/server/embeddata/awareness.result-manifest.tsv"
)

// ProofObligationsPath is the repository path of the generated proof-obligations
// artifact, exported so a downstream stage can reuse its expected output.
const ProofObligationsPath = proofObligationsPath

// Producer A: proof obligations, derived directly from the exact result root's
// authority surfaces — never from the (possibly stale) candidate YAML.
type proofObligationsProducer struct{}

func (proofObligationsProducer) ID() string             { return proofObligationsProducerID }
func (proofObligationsProducer) Version() string        { return "v1" }
func (proofObligationsProducer) OutputPath() string     { return proofObligationsPath }
func (proofObligationsProducer) Dependencies() []string { return nil }

func (proofObligationsProducer) Generate(_ context.Context, in Context, _ map[string]Output) (Output, error) {
	surfaces, err := factextract.ExtractAuthorityCandidates(in.RepositoryRoot)
	if err != nil {
		return Output{}, fmt.Errorf("extract authority surfaces: %w", err)
	}
	doc := proofrequirements.BuildObligations(surfaces)
	body, err := proofrequirements.RenderObligations(doc)
	if err != nil {
		return Output{}, fmt.Errorf("render proof obligations: %w", err)
	}
	sem, err := closureprotocol.SemanticDigest(doc)
	if err != nil {
		return Output{}, err
	}
	return Output{
		ProducerID: proofObligationsProducerID, ProducerVersion: "v1",
		Path: proofObligationsPath, MediaType: yamlMediaType, Bytes: body,
		SemanticDigestSHA256: sem, ByteDigestSHA256: sha256hex(body),
	}, nil
}

// Producer B: the embedded architecture graph. Its expected bytes are exactly the
// canonical graph already produced in Stage 3 — never a second build.
type embeddedGraphProducer struct{}

func (embeddedGraphProducer) ID() string             { return embeddedGraphProducerID }
func (embeddedGraphProducer) Version() string        { return "v1" }
func (embeddedGraphProducer) OutputPath() string     { return embeddedGraphPath }
func (embeddedGraphProducer) Dependencies() []string { return nil }

func (embeddedGraphProducer) Generate(_ context.Context, in Context, _ map[string]Output) (Output, error) {
	if len(in.GraphArtifact.NTriples) == 0 {
		return Output{}, fmt.Errorf("no canonical graph available")
	}
	return Output{
		ProducerID: embeddedGraphProducerID, ProducerVersion: "v1",
		Path: embeddedGraphPath, MediaType: ntriplesMediaType, Bytes: in.GraphArtifact.NTriples,
		SemanticDigestSHA256: in.GraphArtifact.GraphSemanticDigestSHA256,
		ByteDigestSHA256:     in.GraphArtifact.ArtifactByteDigestSHA256,
	}, nil
}

// Producer C: the deterministic result manifest. It depends on A and B and
// receives their expected identities through the prior-output map — it never
// reads their committed files, so it represents the expected derivation.
type resultManifestProducer struct{}

func (resultManifestProducer) ID() string         { return resultManifestProducerID }
func (resultManifestProducer) Version() string    { return "v1" }
func (resultManifestProducer) OutputPath() string { return resultManifestPath }
func (resultManifestProducer) Dependencies() []string {
	return []string{proofObligationsProducerID, embeddedGraphProducerID}
}

func (resultManifestProducer) Generate(_ context.Context, in Context, prior map[string]Output) (Output, error) {
	proof, ok := prior[proofObligationsProducerID]
	if !ok {
		return Output{}, fmt.Errorf("missing proof-obligation output")
	}
	graph, ok := prior[embeddedGraphProducerID]
	if !ok {
		return Output{}, fmt.Errorf("missing embedded-graph output")
	}
	manifestInput := buildtransaction.ResultManifestInput{
		GraphInputPolicyID:             in.GraphInputPolicyID,
		GraphInputSnapshotDigestSHA256: in.GraphInputSnapshotDigestSHA256,
		SourceManifestDigestSHA256:     in.SourceManifestDigestSHA256,
		GraphSemanticDigestSHA256:      in.GraphArtifact.GraphSemanticDigestSHA256,
		GraphByteDigestSHA256:          in.GraphArtifact.ArtifactByteDigestSHA256,
		GraphTripleCount:               in.GraphArtifact.GraphTripleCount,
		MarkerTripleCount:              in.GraphArtifact.MarkerTripleCount,
		ArtifactTripleCount:            in.GraphArtifact.ArtifactTripleCount,
		SupplementalGraphs:             in.SupplementalGraphs,
		GeneratedOutputs: []buildtransaction.GeneratedOutputIdentity{
			{Path: proof.Path, SemanticDigestSHA256: proof.SemanticDigestSHA256, ByteDigestSHA256: proof.ByteDigestSHA256},
			{Path: graph.Path, SemanticDigestSHA256: graph.SemanticDigestSHA256, ByteDigestSHA256: graph.ByteDigestSHA256},
		},
		ProducerVersions: []buildtransaction.ProducerIdentity{
			{ID: proofObligationsProducerID, Version: "v1"},
			{ID: embeddedGraphProducerID, Version: "v1"},
			{ID: resultManifestProducerID, Version: "v1"},
		},
	}
	body, err := buildtransaction.RenderResultManifestV1(manifestInput)
	if err != nil {
		return Output{}, fmt.Errorf("render result manifest: %w", err)
	}
	return Output{
		ProducerID: resultManifestProducerID, ProducerVersion: "v1",
		Path: resultManifestPath, MediaType: tsvMediaType, Bytes: body,
		SemanticDigestSHA256: sha256hex(body), ByteDigestSHA256: sha256hex(body),
	}, nil
}
