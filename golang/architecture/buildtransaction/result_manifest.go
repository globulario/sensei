// SPDX-License-Identifier: AGPL-3.0-only

// Package buildtransaction renders the deterministic, acyclic result-artifact
// manifest for a Phase 7 result architecture. It is NOT the legacy
// awareness.transaction.tsv (a runtime / cross-repository provenance stamp that
// reads Git commits and sibling repository state, and is not a pure function of
// the bound result architecture). The result manifest is a pure derivation of the
// exact graph inputs, the canonical graph identity, and the other generated
// repository artifacts' identities — with no Git revision, no wall-clock time, no
// result tree digest, and no self reference, so it can never form a cycle with
// the result tree it lives in.
package buildtransaction

import (
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/graphbuild"
)

// ResultManifestFormat identifies the acyclic result-manifest v1 shape.
const ResultManifestFormat = "buildtransaction.result-manifest/v1"

// GeneratedOutputIdentity is the immutable identity of one generated repository
// artifact referenced by the manifest. The manifest never references itself.
type GeneratedOutputIdentity struct {
	Path                 string
	SemanticDigestSHA256 string
	ByteDigestSHA256     string
}

// ProducerIdentity pins a generated-artifact producer.
type ProducerIdentity struct {
	ID      string
	Version string
}

// ResultManifestInput is the pure input to the result manifest. It carries only
// deterministic, result-local identities.
type ResultManifestInput struct {
	GraphInputPolicyID             string
	GraphInputSnapshotDigestSHA256 string
	SourceManifestDigestSHA256     string

	GraphSemanticDigestSHA256 string
	GraphByteDigestSHA256     string
	GraphTripleCount          int
	MarkerTripleCount         int
	ArtifactTripleCount       int

	SupplementalGraphs []graphbuild.SupplementalGraphBinding
	GeneratedOutputs   []GeneratedOutputIdentity
	ProducerVersions   []ProducerIdentity
}

func tsvRow(fields ...string) string { return strings.Join(fields, "\t") }

// RenderResultManifestV1 renders the manifest to deterministic TSV bytes. Rows
// are in a fixed order; the supplemental, output, and producer rows are sorted by
// their stable key. It refuses forbidden inputs (absolute paths, a "." graph
// digest, a self-referential output) and any non-repository-relative path.
func RenderResultManifestV1(in ResultManifestInput) ([]byte, error) {
	if strings.TrimSpace(in.GraphSemanticDigestSHA256) == "" || strings.TrimSpace(in.GraphByteDigestSHA256) == "" {
		return nil, fmt.Errorf("buildtransaction: result manifest requires the graph semantic and byte digests")
	}
	var b strings.Builder
	write := func(row string) { b.WriteString(row); b.WriteByte('\n') }

	write(tsvRow("format", ResultManifestFormat))
	write(tsvRow("graph_input_policy_id", strings.TrimSpace(in.GraphInputPolicyID)))
	write(tsvRow("graph_input_snapshot_digest_sha256", strings.TrimSpace(in.GraphInputSnapshotDigestSHA256)))
	write(tsvRow("source_manifest_digest_sha256", strings.TrimSpace(in.SourceManifestDigestSHA256)))
	write(tsvRow("graph_semantic_digest_sha256", strings.TrimSpace(in.GraphSemanticDigestSHA256)))
	write(tsvRow("graph_byte_digest_sha256", strings.TrimSpace(in.GraphByteDigestSHA256)))
	write(tsvRow("graph_triple_count", fmt.Sprintf("%d", in.GraphTripleCount)))
	write(tsvRow("marker_triple_count", fmt.Sprintf("%d", in.MarkerTripleCount)))
	write(tsvRow("artifact_triple_count", fmt.Sprintf("%d", in.ArtifactTripleCount)))

	sups := append([]graphbuild.SupplementalGraphBinding(nil), in.SupplementalGraphs...)
	sort.Slice(sups, func(i, j int) bool { return sups[i].ID < sups[j].ID })
	for _, s := range sups {
		write(tsvRow("supplemental", s.ID, s.Version, s.SemanticDigestSHA256))
	}

	outs := append([]GeneratedOutputIdentity(nil), in.GeneratedOutputs...)
	sort.Slice(outs, func(i, j int) bool { return outs[i].Path < outs[j].Path })
	for _, o := range outs {
		p := strings.TrimSpace(o.Path)
		if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "..") {
			return nil, fmt.Errorf("buildtransaction: generated output path %q is not repository-relative", o.Path)
		}
		write(tsvRow("output", p, o.SemanticDigestSHA256, o.ByteDigestSHA256))
	}

	prods := append([]ProducerIdentity(nil), in.ProducerVersions...)
	sort.Slice(prods, func(i, j int) bool {
		if prods[i].ID != prods[j].ID {
			return prods[i].ID < prods[j].ID
		}
		return prods[i].Version < prods[j].Version
	})
	for _, p := range prods {
		write(tsvRow("producer", p.ID, p.Version))
	}

	return []byte(b.String()), nil
}
