// SPDX-License-Identifier: Apache-2.0

package prereview

import "context"

// GenerateRequest parameterizes advisory report generation.
type GenerateRequest struct {
	RepoRoot  string
	Base      string
	Head      string
	Purpose   string
	PolicyIDs []string
	Display   *DisplayMetadata
	// Strict fails generation when graph context cannot be collected, instead of
	// degrading to a diff-only advisory report.
	Strict bool
}

// AdvisoryInputs carries the collected inputs for advisory assembly.
type AdvisoryInputs struct {
	Diff      BoundDiff
	Graph     GraphContext
	Purpose   string
	PolicyIDs []string
	Display   *DisplayMetadata
}

// advisoryUnavailable lists the surfaces that advisory coverage inherently
// cannot report; they are always named so the report is honest about its limits.
var advisoryUnavailable = []string{
	"authority", "admission", "capability", "scope",
	"proof", "certification", "result_architecture", "completion",
}

// AssembleAdvisory builds and finalizes an advisory-coverage report from a bound
// diff and graph context. It populates only what advisory evidence supports:
// the diff, architectural impact, applicable protection rules, and reviewer
// concerns. Governance, proof, result, and certification stay empty — those
// require higher coverage — and their absence is named, never hidden.
func AssembleAdvisory(in AdvisoryInputs) (PreReviewReport, error) {
	r := PreReviewReport{
		Binding: ReviewBinding{
			RepositoryDomain:     in.Diff.RepositoryDomain,
			BaseRevision:         in.Diff.BaseRevision,
			BaseTreeDigestSHA256: in.Diff.BaseTreeDigestSHA256,
			HeadRevision:         in.Diff.HeadRevision,
			HeadTreeDigestSHA256: in.Diff.HeadTreeDigestSHA256,
			MergeBaseRevision:    in.Diff.MergeBaseRevision,
			DiffDigestSHA256:     in.Diff.DiffDigestSHA256,
			PolicyIDs:            in.PolicyIDs,
		},
		Coverage: CoverageSummary{
			Level:       CoverageAdvisory,
			Available:   sortedUnique(append([]string{"diff"}, in.Graph.Available...)),
			Unavailable: sortedUnique(append(append([]string{}, advisoryUnavailable...), in.Graph.Unavailable...)),
		},
		Summary: ExecutiveSummary{Purpose: in.Purpose},
		Change: ChangeSummary{
			FilesCreated:  in.Diff.FilesCreated,
			FilesModified: in.Diff.FilesModified,
			FilesDeleted:  in.Diff.FilesDeleted,
			FilesRenamed:  in.Diff.FilesRenamed,
		},
		Impact: ArchitecturalImpact{
			RiskClass:          in.Graph.RiskClass,
			AffectedComponents: in.Graph.AffectedComponents,
			ChangedBoundaries:  in.Graph.ChangedBoundaries,
			AffectedContracts:  in.Graph.AffectedContracts,
		},
		Protection: ProtectionSummary{
			Invariants:     in.Graph.Invariants,
			FailureModes:   in.Graph.FailureModes,
			ForbiddenFixes: in.Graph.ForbiddenFixes,
			RequiredTests:  in.Graph.RequiredTests,
		},
		ReviewerAttention: in.Graph.ReviewerConcerns,
		// Result architecture is unavailable until a verified result transition
		// exists; advisory coverage never populates it.
		Result:      ResultArchitectureSummary{Available: false},
		Limitations: advisoryLimitations(in.Graph.Degraded),
		Display:     in.Display,
	}
	return Finalize(r)
}

// GenerateAdvisory orchestrates advisory generation: resolve the diff, collect
// graph context, and assemble. When graph collection fails and Strict is not
// set, it degrades to a diff-only advisory report that names the gap rather than
// failing.
func GenerateAdvisory(ctx context.Context, diffSrc DiffSource, graphSrc GraphSource, req GenerateRequest) (PreReviewReport, error) {
	diff, err := diffSrc.ResolveReviewDiff(ctx, DiffRequest{RepoRoot: req.RepoRoot, Base: req.Base, Head: req.Head})
	if err != nil {
		return PreReviewReport{}, err
	}

	var gc GraphContext
	switch {
	case graphSrc == nil:
		gc = GraphContext{Degraded: []string{"graph context source unavailable"}}
	default:
		gc, err = graphSrc.CollectArchitecturalContext(ctx, GraphRequest{
			RepositoryDomain: diff.RepositoryDomain,
			ChangedPaths:     diff.ChangedPaths(),
		})
		if err != nil {
			if req.Strict {
				return PreReviewReport{}, err
			}
			gc = GraphContext{Degraded: []string{"graph context could not be collected: " + err.Error()}}
		}
	}

	return AssembleAdvisory(AdvisoryInputs{
		Diff:      diff,
		Graph:     gc,
		Purpose:   req.Purpose,
		PolicyIDs: req.PolicyIDs,
		Display:   req.Display,
	})
}

// advisoryLimitations states that advisory coverage cannot establish
// authorization or correctness, then appends any degraded-context notes.
func advisoryLimitations(degraded []string) []string {
	base := []string{
		"advisory coverage: authorization and mutation scope are not established",
		"advisory coverage: correctness is not certified",
	}
	return append(base, degraded...)
}
