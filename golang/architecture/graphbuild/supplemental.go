// SPDX-License-Identifier: Apache-2.0

package graphbuild

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/seedmeta"
)

// VerifySupplementalGraph is the single, shared verification of a supplemental
// graph's exact bytes against its immutable binding, used by BOTH the task
// preparation producer and the result pipeline consumer so their rules can never
// drift. It rejects empty bytes, invalid N-Triples, and a missing seed marker,
// and requires the marker's semantic digest to equal the binding's declared
// digest. It returns a canonical SupplementalGraph carrying the exact bytes.
func VerifySupplementalGraph(binding SupplementalGraphBinding, data []byte) (SupplementalGraph, error) {
	if len(data) == 0 {
		return SupplementalGraph{}, fmt.Errorf("graphbuild: supplemental graph %q has empty bytes", binding.ID)
	}
	if errs := extractor.ValidateNTriples(bytes.NewReader(data)); len(errs) > 0 {
		return SupplementalGraph{}, fmt.Errorf("graphbuild: supplemental graph %q is not valid N-Triples: %v", binding.ID, errs[0])
	}
	marker, ok := seedmeta.ParseMarker(data)
	if !ok {
		return SupplementalGraph{}, fmt.Errorf("graphbuild: supplemental graph %q carries no seed marker", binding.ID)
	}
	want := strings.TrimSpace(binding.SemanticDigestSHA256)
	if want == "" || marker.Digest != want {
		return SupplementalGraph{}, fmt.Errorf("graphbuild: supplemental graph %q marker digest mismatch (marker %s, expected %s)", binding.ID, marker.Digest, want)
	}
	return SupplementalGraph{
		ID:                           binding.ID,
		Version:                      binding.Version,
		NTriples:                     data,
		ExpectedSemanticDigestSHA256: marker.Digest,
	}, nil
}

// SnapshotFromBuildInputs constructs a canonical, stamped, validated graph-input
// snapshot from the exact inputs a build used, plus the supplemental bytes keyed
// by artifact key. It converts each source root's filesystem path into a
// repository-relative logical path (rejecting external roots), derives safe
// supplemental artifact keys, and computes each supplemental's semantic digest
// from its seed marker. No caller re-describes inputs by hand after building.
func SnapshotFromBuildInputs(policyID, repositoryRoot, repositoryDomain string, sources []SourceRoot, supplemental []SupplementalGraph) (GraphInputSnapshot, map[string][]byte, error) {
	root := filepath.Clean(strings.TrimSpace(repositoryRoot))
	if root == "" {
		return GraphInputSnapshot{}, nil, fmt.Errorf("graphbuild: snapshot needs a repository root")
	}
	roots := make([]LogicalSourceRoot, 0, len(sources))
	for _, s := range sources {
		rel, err := filepath.Rel(root, filepath.Clean(s.FilesystemPath))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return GraphInputSnapshot{}, nil, fmt.Errorf("graphbuild: source root %q escapes repository root %q", s.FilesystemPath, root)
		}
		roots = append(roots, LogicalSourceRoot{
			LogicalPath:         filepath.ToSlash(rel),
			DefaultDomain:       s.DefaultDomain,
			DefaultSourceSet:    s.DefaultSourceSet,
			SkipNestedGenerated: s.SkipNestedGenerated,
		})
	}

	supplementalBytes := map[string][]byte{}
	bindings := make([]SupplementalGraphBinding, 0, len(supplemental))
	for _, sup := range supplemental {
		marker, ok := seedmeta.ParseMarker(sup.NTriples)
		if !ok {
			return GraphInputSnapshot{}, nil, fmt.Errorf("graphbuild: supplemental graph %q carries no seed marker", sup.ID)
		}
		key, err := SupplementalGraphArtifactKey(sup.ID)
		if err != nil {
			return GraphInputSnapshot{}, nil, err
		}
		if _, dup := supplementalBytes[key]; dup {
			return GraphInputSnapshot{}, nil, fmt.Errorf("graphbuild: duplicate supplemental graph artifact key %q", key)
		}
		bindings = append(bindings, SupplementalGraphBinding{
			ID:                   sup.ID,
			Version:              sup.Version,
			SemanticDigestSHA256: marker.Digest,
			ArtifactKey:          key,
		})
		supplementalBytes[key] = sup.NTriples
	}

	snap := GraphInputSnapshot{
		SchemaVersion:      GraphInputSnapshotSchemaVersion,
		PolicyID:           strings.TrimSpace(policyID),
		RepositoryDomain:   strings.TrimSpace(repositoryDomain),
		SourceRoots:        roots,
		SupplementalGraphs: bindings,
	}
	canon, err := CanonicalizeGraphInputSnapshot(snap)
	if err != nil {
		return GraphInputSnapshot{}, nil, err
	}
	digest, err := GraphInputSnapshotDigest(canon)
	if err != nil {
		return GraphInputSnapshot{}, nil, err
	}
	canon.SnapshotDigestSHA256 = digest
	if err := ValidateBoundGraphInputSnapshot(canon); err != nil {
		return GraphInputSnapshot{}, nil, err
	}
	return canon, supplementalBytes, nil
}
