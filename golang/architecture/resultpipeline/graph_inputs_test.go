// SPDX-License-Identifier: AGPL-3.0-only

package resultpipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/graphbuild"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
	"github.com/globulario/sensei/golang/seedmeta"
)

func snapshotForRoot(t *testing.T, root string, sup []graphbuild.SupplementalGraph) (graphbuild.GraphInputSnapshot, map[string][]byte) {
	t.Helper()
	snap, bytes, err := graphbuild.SnapshotFromBuildInputs(
		GraphInputPolicyV1, root, e2eDomain,
		[]graphbuild.SourceRoot{{FilesystemPath: filepath.Join(root, "docs", "awareness"), SkipNestedGenerated: true}},
		sup,
	)
	if err != nil {
		t.Fatal(err)
	}
	return snap, bytes
}

// §15: an unknown graph-input policy never falls back to a default.
func TestGraphInputPolicyUnknown(t *testing.T) {
	dir := t.TempDir()
	snap, byteMap := snapshotForRoot(t, dir, nil)
	snap.PolicyID = "sensei.resultpipeline.graph-inputs/does-not-exist"
	if _, err := resolveGraphInputs(dir, snap, byteMap); err == nil || !strings.Contains(err.Error(), "graph_input_policy_unknown") {
		t.Fatalf("unknown policy must be refused, got %v", err)
	}
}

// §14: a configured source root missing from the materialized result fails; it is
// never reinterpreted as an empty graph source.
func TestGraphInputSourceRootMissing(t *testing.T) {
	dir := t.TempDir() // no docs/awareness present
	snap := graphbuild.GraphInputSnapshot{
		SchemaVersion:    graphbuild.GraphInputSnapshotSchemaVersion,
		PolicyID:         GraphInputPolicyV1,
		RepositoryDomain: e2eDomain,
		SourceRoots:      []graphbuild.LogicalSourceRoot{{LogicalPath: "docs/awareness"}},
	}
	if _, err := resolveGraphInputs(dir, snap, nil); err == nil || !strings.Contains(err.Error(), "graph_input_source_root_missing") {
		t.Fatalf("missing source root must fail, got %v", err)
	}
}

// §14: a declared supplemental graph whose bytes are absent fails.
func TestGraphInputSupplementalMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	nt, _ := seedmeta.AppendMarker([]byte("<https://x/s> <https://x/p> <https://x/o> .\n"))
	snap, _ := snapshotForRoot(t, dir, []graphbuild.SupplementalGraph{{ID: "pack.a", Version: "v1", NTriples: nt}})
	// Resolve with NO supplemental bytes provided.
	if _, err := resolveGraphInputs(dir, snap, map[string][]byte{}); err == nil || !strings.Contains(err.Error(), "supplemental_graph_artifact_missing") {
		t.Fatalf("missing supplemental bytes must fail, got %v", err)
	}
}

// §14: correct snapshot with WRONG supplemental bytes fails the digest check.
func TestGraphInputSupplementalDigestMismatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	nt, _ := seedmeta.AppendMarker([]byte("<https://x/s> <https://x/p> <https://x/o> .\n"))
	snap, _ := snapshotForRoot(t, dir, []graphbuild.SupplementalGraph{{ID: "pack.a", Version: "v1", NTriples: nt}})
	other, _ := seedmeta.AppendMarker([]byte("<https://x/s> <https://x/p> <https://x/DIFFERENT> .\n"))
	if _, err := resolveGraphInputs(dir, snap, map[string][]byte{"supplemental_graph.pack.a": other}); err == nil {
		t.Fatal("wrong supplemental bytes must fail the marker digest check")
	}
}

// §13 one-verified-supplemental case: the full pipeline builds end-to-end when the
// snapshot binds a supplemental graph, reproducing the base graph and validating.
func TestBuildEndToEndWithSupplemental(t *testing.T) {
	sup, _ := seedmeta.AppendMarker([]byte("<https://globular.io/awareness#pack/a> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#Component> .\n"))
	repo, taskDir := e2eSeedVariant(t, "package src\n\nfunc Publish() {}\n\nfunc Revoke() {}\n",
		[]graphbuild.SupplementalGraph{{ID: "governance.pack", Version: "v1", NTriples: sup}})
	res, err := Build(context.Background(), BuildRequest{
		RepositoryRoot: repo, TaskDirectory: taskDir,
		ResultMode: resulttransition.ResultModeWorktree, RepositoryDomain: e2eDomain,
	})
	if err != nil {
		t.Fatalf("Build with supplemental graph: %v", err)
	}
	if len(res.StageArtifacts) != 10 {
		t.Fatalf("got %d stages", len(res.StageArtifacts))
	}
	if err := closureprotocol.ValidateResultTransitionReceipt(e2eCandidateReceipt(res)); err != nil {
		t.Fatalf("candidate transition invalid: %v", err)
	}
}
