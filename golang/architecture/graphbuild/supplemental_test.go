// SPDX-License-Identifier: Apache-2.0

package graphbuild

import (
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/seedmeta"
)

func markedGraph(t *testing.T, body string) ([]byte, string) {
	t.Helper()
	nt, marker := seedmeta.AppendMarker([]byte(body))
	return nt, marker.Digest
}

func TestSupplementalGraphArtifactKey(t *testing.T) {
	ok := map[string]string{
		"pack.a":                  "supplemental_graph.pack.a",
		"sensei.governance.pack1": "supplemental_graph.sensei.governance.pack1",
	}
	for id, want := range ok {
		got, err := SupplementalGraphArtifactKey(id)
		if err != nil || got != want {
			t.Fatalf("key(%q)=%q,%v want %q", id, got, err, want)
		}
	}
	for _, bad := range []string{"", "Pack", "a/b", "a b", "..", "a..b", ".pack", "pack/../x"} {
		if _, err := SupplementalGraphArtifactKey(bad); err == nil {
			t.Fatalf("id %q should be rejected", bad)
		}
	}
}

func TestVerifySupplementalGraph(t *testing.T) {
	nt, digest := markedGraph(t, "<https://x/s> <https://x/p> <https://x/o> .\n")
	binding := SupplementalGraphBinding{ID: "pack.a", Version: "v1", SemanticDigestSHA256: digest, ArtifactKey: "supplemental_graph.pack.a"}

	got, err := VerifySupplementalGraph(binding, nt)
	if err != nil {
		t.Fatalf("valid supplemental must verify: %v", err)
	}
	if got.ExpectedSemanticDigestSHA256 != digest || string(got.NTriples) != string(nt) {
		t.Fatalf("verified supplemental not carried through: %+v", got)
	}

	if _, err := VerifySupplementalGraph(binding, nil); err == nil {
		t.Fatal("empty bytes must be rejected")
	}
	wrong := binding
	wrong.SemanticDigestSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := VerifySupplementalGraph(wrong, nt); err == nil {
		t.Fatal("semantic digest mismatch must be rejected")
	}
	if _, err := VerifySupplementalGraph(binding, []byte("<https://x/s> <https://x/p> <https://x/o> .\n")); err == nil {
		t.Fatal("bytes with no seed marker must be rejected")
	}
}

func TestSnapshotFromBuildInputs(t *testing.T) {
	root := filepath.FromSlash("/repo")
	nt, _ := markedGraph(t, "<https://x/s> <https://x/p> <https://x/o> .\n")
	snap, supBytes, err := SnapshotFromBuildInputs(
		"sensei.resultpipeline.graph-inputs/v1", root, "github.com/globulario/sensei",
		[]SourceRoot{{FilesystemPath: filepath.Join(root, "docs", "awareness"), SkipNestedGenerated: true}},
		[]SupplementalGraph{{ID: "pack.a", Version: "v1", NTriples: nt}},
	)
	if err != nil {
		t.Fatalf("SnapshotFromBuildInputs: %v", err)
	}
	if err := ValidateBoundGraphInputSnapshot(snap); err != nil {
		t.Fatalf("produced snapshot must be a valid bound snapshot: %v", err)
	}
	if len(snap.SourceRoots) != 1 || snap.SourceRoots[0].LogicalPath != "docs/awareness" {
		t.Fatalf("logical source root wrong: %+v", snap.SourceRoots)
	}
	if len(snap.SupplementalGraphs) != 1 || snap.SupplementalGraphs[0].ArtifactKey != "supplemental_graph.pack.a" {
		t.Fatalf("supplemental binding wrong: %+v", snap.SupplementalGraphs)
	}
	if _, ok := supBytes["supplemental_graph.pack.a"]; !ok {
		t.Fatalf("supplemental bytes not keyed by artifact key: %v", supBytes)
	}

	// A source root outside the repository root is refused.
	if _, _, err := SnapshotFromBuildInputs("p", root, "d",
		[]SourceRoot{{FilesystemPath: filepath.FromSlash("/elsewhere/docs")}}, nil); err == nil {
		t.Fatal("external source root must be rejected")
	}
}

func TestValidateBoundGraphInputSnapshotRequiresDigest(t *testing.T) {
	s := validSnapshot() // unstamped
	if err := ValidateBoundGraphInputSnapshot(s); err == nil {
		t.Fatal("an unstamped snapshot is not a valid bound snapshot")
	}
}
