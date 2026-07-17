// SPDX-License-Identifier: Apache-2.0

package resultpipeline

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/graphbuild"
	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/architecture/plane"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

// governedSourceRoots is the explicit, versioned, stable Phase 7 selection of
// governed source trees inside a materialized repository root. It never includes
// candidate output, mutable task state, runtime observations, raw Git history, or
// temporary directories; graphbuild's base exclusions drop candidates/ and
// non-governed extensions, and SkipNestedGenerated drops nested generated/ trees.
func governedSourceRoots(repoRoot, domain string) []graphbuild.SourceRoot {
	candidates := []string{
		filepath.Join(repoRoot, "docs", "awareness"),
		filepath.Join(repoRoot, "eval", "multi-swe-bench", "contracts"),
		filepath.Join(repoRoot, "eval", "multi-swe-bench", "notes", "learning_events"),
	}
	var roots []graphbuild.SourceRoot
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		roots = append(roots, graphbuild.SourceRoot{
			FilesystemPath:      dir,
			IdentityRoot:        repoRoot,
			StripPathPrefixes:   []string{repoRoot},
			RepositoryDomain:    domain,
			DefaultDomain:       domain,
			SkipNestedGenerated: true,
		})
	}
	return roots
}

// compiledGraph is the shared intermediate: one canonical compilation (stage 1),
// its stamped architecture-graph artifact (stage 3), and the in-memory graph
// indexes every downstream engine reads. Nothing here touches a live graph store.
type compiledGraph struct {
	compilation graphbuild.Compilation
	artifact    graphbuild.Artifact
	triples     []graphsnapshot.Triple
	planeIndex  plane.GraphIndex
	closIndex   closure.GraphIndex
}

// compileGovernedGraph runs stage 1 (governed source manifest) and stage 3
// (architecture graph) over the materialized result root, and builds the
// in-memory indexes. Stage 2 (generated repository artifacts) is verified and
// composed separately, before finalization, so its verified artifacts can bind
// into the result binding.
func compileGovernedGraph(ctx context.Context, repoRoot, domain string, supplemental []graphbuild.SupplementalGraph) (compiledGraph, error) {
	roots := governedSourceRoots(repoRoot, domain)
	if len(roots) == 0 {
		return compiledGraph{}, fmt.Errorf("resultpipeline: no governed source roots under %s", repoRoot)
	}
	comp, err := graphbuild.Compile(ctx, graphbuild.CompileRequest{
		RepositoryRoot: repoRoot,
		Sources:        roots,
		Policy:         graphbuild.ClosureStrictPolicy(),
	})
	if err != nil {
		return compiledGraph{}, fmt.Errorf("resultpipeline: compile governed sources: %w", err)
	}
	artifact, err := graphbuild.Finalize(ctx, graphbuild.FinalizeRequest{
		Compilation:        comp,
		SupplementalGraphs: supplemental,
	})
	if err != nil {
		return compiledGraph{}, fmt.Errorf("resultpipeline: finalize architecture graph: %w", err)
	}
	triples, err := graphsnapshot.Read(bytes.NewReader(artifact.NTriples))
	if err != nil {
		return compiledGraph{}, fmt.Errorf("resultpipeline: read result graph: %w", err)
	}
	planeIndex, err := plane.ReadGraphIndex(bytes.NewReader(artifact.NTriples))
	if err != nil {
		return compiledGraph{}, fmt.Errorf("resultpipeline: index result graph (plane): %w", err)
	}
	return compiledGraph{
		compilation: comp,
		artifact:    artifact,
		triples:     triples,
		planeIndex:  planeIndex,
		closIndex:   closure.BuildGraphIndex(triples),
	}, nil
}

// completeResultBinding lifts the typed pre-transition repository result binding
// into the frozen closureprotocol.ResultBinding, once the result graph digest and
// the verified generated repository artifacts are known. It validates the binding
// and returns it with its recomputed self-digest. The binding is never modified
// after this point, because every operational artifact receipt binds it.
func completeResultBinding(rr resulttransition.RepositoryResultBinding, graphDigest string, generated []closureprotocol.ResultArtifact) (closureprotocol.ResultBinding, string, error) {
	rb := closureprotocol.ResultBinding{
		BaseRevision:           rr.BaseRevision,
		PatchDigestSHA256:      rr.PatchDigestSHA256,
		ResultTreeDigestSHA256: rr.ResultTreeDigestSHA256,
		ResultRevision:         rr.ResultRevision,
		GraphDigestSHA256:      graphDigest,
		GeneratedArtifacts:     generated,
	}
	if err := closureprotocol.ValidateResultBinding(rb); err != nil {
		return closureprotocol.ResultBinding{}, "", fmt.Errorf("resultpipeline: result binding invalid: %w", err)
	}
	digest, err := closureprotocol.ResultBindingDigest(rb)
	if err != nil {
		return closureprotocol.ResultBinding{}, "", err
	}
	return rb, digest, nil
}
