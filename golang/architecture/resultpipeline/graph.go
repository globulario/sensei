// SPDX-License-Identifier: AGPL-3.0-only

package resultpipeline

import (
	"bytes"
	"context"
	"fmt"

	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/graphbuild"
	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/architecture/plane"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

// compiledGraph is the shared intermediate: one canonical compilation (stage 1),
// its stamped architecture-graph artifact (stage 3), and the in-memory graph
// indexes every downstream engine reads. Nothing here touches a live graph store.
type compiledGraph struct {
	snapshot    graphbuild.GraphInputSnapshot
	compilation graphbuild.Compilation
	artifact    graphbuild.Artifact
	triples     []graphsnapshot.Triple
	planeIndex  plane.GraphIndex
	closIndex   closure.GraphIndex
}

// compileGovernedGraph runs stage 1 (governed source manifest) and stage 3
// (architecture graph) over a materialized root using ONLY the graph inputs the
// closed policy resolved from the immutable snapshot — never hardcoded roots and
// never unbound caller-supplied supplemental graphs. It builds the in-memory
// indexes. Stage 2 (generated repository artifacts) is verified separately.
func compileGovernedGraph(ctx context.Context, repoRoot string, inputs resolvedGraphInputs) (compiledGraph, error) {
	if len(inputs.SourceRoots) == 0 {
		return compiledGraph{}, fmt.Errorf("resultpipeline: no governed source roots resolved for %s", repoRoot)
	}
	comp, err := graphbuild.Compile(ctx, graphbuild.CompileRequest{
		RepositoryRoot: repoRoot,
		Sources:        inputs.SourceRoots,
		Policy:         graphbuild.ClosureStrictPolicy(),
	})
	if err != nil {
		return compiledGraph{}, fmt.Errorf("resultpipeline: compile governed sources: %w", err)
	}
	artifact, err := graphbuild.Finalize(ctx, graphbuild.FinalizeRequest{
		Compilation:        comp,
		SupplementalGraphs: inputs.SupplementalGraphs,
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
		snapshot:    inputs.Snapshot,
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
