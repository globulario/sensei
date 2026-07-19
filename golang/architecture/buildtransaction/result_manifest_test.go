// SPDX-License-Identifier: AGPL-3.0-only

package buildtransaction

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/graphbuild"
)

func sampleInput() ResultManifestInput {
	return ResultManifestInput{
		GraphInputPolicyID:             "sensei.resultpipeline.graph-inputs/v1",
		GraphInputSnapshotDigestSHA256: "aa",
		SourceManifestDigestSHA256:     "bb",
		GraphSemanticDigestSHA256:      "cc",
		GraphByteDigestSHA256:          "dd",
		GraphTripleCount:               10,
		MarkerTripleCount:              6,
		ArtifactTripleCount:            16,
		SupplementalGraphs: []graphbuild.SupplementalGraphBinding{
			{ID: "pack.b", Version: "v1", SemanticDigestSHA256: "b2"},
			{ID: "pack.a", Version: "v1", SemanticDigestSHA256: "a1"},
		},
		GeneratedOutputs: []GeneratedOutputIdentity{
			{Path: "golang/server/embeddata/awareness.nt", SemanticDigestSHA256: "g", ByteDigestSHA256: "gb"},
			{Path: "docs/awareness/generated/proof_obligations.yaml", SemanticDigestSHA256: "p", ByteDigestSHA256: "pb"},
		},
		ProducerVersions: []ProducerIdentity{
			{ID: "sensei.graphbuild.embedded-artifact", Version: "v1"},
			{ID: "sensei.proofrequirements.repository-artifact", Version: "v1"},
		},
	}
}

func TestResultManifestDeterministicAndOrderIndependent(t *testing.T) {
	a := sampleInput()
	b := sampleInput()
	// reverse the collections in b
	b.SupplementalGraphs[0], b.SupplementalGraphs[1] = b.SupplementalGraphs[1], b.SupplementalGraphs[0]
	b.GeneratedOutputs[0], b.GeneratedOutputs[1] = b.GeneratedOutputs[1], b.GeneratedOutputs[0]
	b.ProducerVersions[0], b.ProducerVersions[1] = b.ProducerVersions[1], b.ProducerVersions[0]
	ba, err := RenderResultManifestV1(a)
	if err != nil {
		t.Fatal(err)
	}
	bb, err := RenderResultManifestV1(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(ba) != string(bb) {
		t.Fatalf("manifest is order-dependent:\n%s\n---\n%s", ba, bb)
	}
}

func TestResultManifestContainsRequiredIdentities(t *testing.T) {
	out, err := RenderResultManifestV1(sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"format\t" + ResultManifestFormat,
		"graph_input_policy_id\t",
		"graph_input_snapshot_digest_sha256\t",
		"source_manifest_digest_sha256\t",
		"graph_semantic_digest_sha256\t",
		"graph_byte_digest_sha256\t",
		"supplemental\tpack.a\t",
		"output\tdocs/awareness/generated/proof_obligations.yaml\t",
		"producer\tsensei.graphbuild.embedded-artifact\t",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("manifest missing %q:\n%s", want, s)
		}
	}
}

func TestResultManifestForbidsUnsafeAndCyclicInputs(t *testing.T) {
	// No graph digest.
	if _, err := RenderResultManifestV1(ResultManifestInput{}); err == nil {
		t.Fatal("manifest without graph digests must fail")
	}
	// Absolute / escaping output path.
	in := sampleInput()
	in.GeneratedOutputs = append(in.GeneratedOutputs, GeneratedOutputIdentity{Path: "/etc/passwd"})
	if _, err := RenderResultManifestV1(in); err == nil {
		t.Fatal("absolute output path must fail")
	}
	in = sampleInput()
	in.GeneratedOutputs = append(in.GeneratedOutputs, GeneratedOutputIdentity{Path: "../escape"})
	if _, err := RenderResultManifestV1(in); err == nil {
		t.Fatal("escaping output path must fail")
	}
}

func TestResultManifestExcludesForbiddenIdentities(t *testing.T) {
	out, err := RenderResultManifestV1(sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// The manifest must never carry a result tree digest, a self output, a commit,
	// or a self digest. (The sample never sets them; assert the row keys are absent.)
	for _, forbidden := range []string{"result_tree", "result_manifest", "git", "commit", "recorded_at", "self_digest"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("manifest must not contain %q:\n%s", forbidden, s)
		}
	}
}
