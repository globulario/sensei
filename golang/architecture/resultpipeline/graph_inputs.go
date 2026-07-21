// SPDX-License-Identifier: AGPL-3.0-only

package resultpipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/graphbuild"
	"gopkg.in/yaml.v3"
)

// GraphInputPolicyV1 is the frozen graph-input policy the result pipeline uses to
// resolve a recorded graph-input snapshot into concrete graphbuild inputs.
const GraphInputPolicyV1 = "sensei.resultpipeline.graph-inputs/v1"

// recordedGraphInputs is the verified, immutable graph-input truth loaded from a
// task's task_prepared event.
type recordedGraphInputs struct {
	Snapshot          graphbuild.GraphInputSnapshot
	SnapshotDigest    string
	SupplementalBytes map[string][]byte
}

// resolvedGraphInputs is a graph-input snapshot resolved against a materialized
// repository root into concrete graphbuild source roots and verified supplemental
// graphs.
type resolvedGraphInputs struct {
	Snapshot           graphbuild.GraphInputSnapshot
	SourceRoots        []graphbuild.SourceRoot
	SupplementalGraphs []graphbuild.SupplementalGraph
}

// graphInputPolicy resolves a snapshot into concrete graph inputs against a
// materialized root. A policy never inspects the current active governance-pack
// pointer, the live worktree, environment variables, Git HEAD, or a mutable task
// projection.
type graphInputPolicy interface {
	ID() string
	Resolve(materializedRoot string, snapshot graphbuild.GraphInputSnapshot, supplementalBytes map[string][]byte) (resolvedGraphInputs, error)
}

// graphInputPolicies is the closed registry. An unknown policy id never falls
// back to a default.
var graphInputPolicies = map[string]graphInputPolicy{
	GraphInputPolicyV1: graphInputPolicyV1{},
}

// resolveGraphInputs looks up the snapshot's policy in the closed registry and
// resolves the inputs against the materialized root.
func resolveGraphInputs(root string, snapshot graphbuild.GraphInputSnapshot, supplementalBytes map[string][]byte) (resolvedGraphInputs, error) {
	policy, ok := graphInputPolicies[strings.TrimSpace(snapshot.PolicyID)]
	if !ok {
		return resolvedGraphInputs{}, fmt.Errorf("resultpipeline: graph_input_policy_unknown: %q", snapshot.PolicyID)
	}
	return policy.Resolve(root, snapshot, supplementalBytes)
}

// loadRecordedGraphInputs loads and verifies the immutable graph-input snapshot
// and its supplemental graph bytes from the task's task_prepared event. It never
// reads an active pointer, a mutable projection, or an environment-selected
// graph; the required truth is the content-addressed ledger artifacts.
func loadRecordedGraphInputs(taskDir, expectedDomain string) (recordedGraphInputs, error) {
	data, found, err := admission.LoadLatestArtifactBytes(taskDir, closureprotocol.LedgerEventTaskPrepared, "graph_input_snapshot")
	if err != nil {
		return recordedGraphInputs{}, fmt.Errorf("resultpipeline: graph_input_snapshot_unavailable: %w", err)
	}
	if !found {
		return recordedGraphInputs{}, fmt.Errorf("resultpipeline: graph_input_snapshot_unavailable: task_prepared has no graph_input_snapshot artifact")
	}
	var snap graphbuild.GraphInputSnapshot
	if err := yaml.Unmarshal(data, &snap); err != nil {
		return recordedGraphInputs{}, fmt.Errorf("resultpipeline: graph_input_snapshot_unavailable: %w", err)
	}
	if err := graphbuild.ValidateBoundGraphInputSnapshot(snap); err != nil {
		return recordedGraphInputs{}, err
	}
	if strings.TrimSpace(snap.RepositoryDomain) != strings.TrimSpace(expectedDomain) {
		return recordedGraphInputs{}, fmt.Errorf("resultpipeline: graph-input snapshot domain %q does not match task domain %q", snap.RepositoryDomain, expectedDomain)
	}
	supplemental := map[string][]byte{}
	for _, s := range snap.SupplementalGraphs {
		b, ok, err := admission.LoadLatestArtifactBytes(taskDir, closureprotocol.LedgerEventTaskPrepared, s.ArtifactKey)
		if err != nil {
			return recordedGraphInputs{}, err
		}
		if !ok {
			return recordedGraphInputs{}, fmt.Errorf("resultpipeline: supplemental_graph_artifact_missing: %s", s.ArtifactKey)
		}
		supplemental[s.ArtifactKey] = b
	}
	return recordedGraphInputs{Snapshot: snap, SnapshotDigest: snap.SnapshotDigestSHA256, SupplementalBytes: supplemental}, nil
}

// graphInputPolicyV1 resolves logical source roots and supplemental graphs
// strictly from the snapshot. It adds no root the snapshot did not name and reads
// no mutable state.
type graphInputPolicyV1 struct{}

func (graphInputPolicyV1) ID() string { return GraphInputPolicyV1 }

func (graphInputPolicyV1) Resolve(materializedRoot string, snapshot graphbuild.GraphInputSnapshot, supplementalBytes map[string][]byte) (resolvedGraphInputs, error) {
	root, err := filepath.Abs(materializedRoot)
	if err != nil {
		return resolvedGraphInputs{}, err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return resolvedGraphInputs{}, err
	}
	roots := make([]graphbuild.SourceRoot, 0, len(snapshot.SourceRoots))
	for _, r := range snapshot.SourceRoots {
		if strings.HasPrefix(r.LogicalPath, "/") || escapesLogicalRoot(r.LogicalPath) {
			return resolvedGraphInputs{}, fmt.Errorf("resultpipeline: graph-input source root %q is not repository-relative", r.LogicalPath)
		}
		abs := filepath.Join(root, filepath.FromSlash(r.LogicalPath))
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return resolvedGraphInputs{}, fmt.Errorf("resultpipeline: graph_input_source_root_missing: %s", r.LogicalPath)
		}
		if rel, err := filepath.Rel(realRoot, real); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return resolvedGraphInputs{}, fmt.Errorf("resultpipeline: graph-input source root %q escapes the materialized root via symlink", r.LogicalPath)
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			return resolvedGraphInputs{}, fmt.Errorf("resultpipeline: graph_input_source_root_missing: %s", r.LogicalPath)
		}
		roots = append(roots, graphbuild.SourceRoot{
			FilesystemPath:      abs,
			IdentityRoot:        root,
			StripPathPrefixes:   []string{root},
			RepositoryDomain:    snapshot.RepositoryDomain,
			DefaultDomain:       firstNonEmptyStr(r.DefaultDomain, snapshot.RepositoryDomain),
			DefaultSourceSet:    r.DefaultSourceSet,
			SkipNestedGenerated: r.SkipNestedGenerated,
		})
	}
	if len(roots) == 0 {
		return resolvedGraphInputs{}, fmt.Errorf("resultpipeline: graph-input snapshot resolved no source roots")
	}
	sups := make([]graphbuild.SupplementalGraph, 0, len(snapshot.SupplementalGraphs))
	for _, b := range snapshot.SupplementalGraphs {
		data, ok := supplementalBytes[b.ArtifactKey]
		if !ok {
			return resolvedGraphInputs{}, fmt.Errorf("resultpipeline: supplemental_graph_artifact_missing: %s", b.ArtifactKey)
		}
		sg, err := graphbuild.VerifySupplementalGraph(b, data)
		if err != nil {
			return resolvedGraphInputs{}, fmt.Errorf("resultpipeline: supplemental_graph_invalid: %w", err)
		}
		sups = append(sups, sg)
	}
	return resolvedGraphInputs{Snapshot: snapshot, SourceRoots: roots, SupplementalGraphs: sups}, nil
}

func escapesLogicalRoot(rel string) bool {
	rel = filepath.ToSlash(rel)
	return rel == ".." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") || strings.HasSuffix(rel, "/..")
}

func firstNonEmptyStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
