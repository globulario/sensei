// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

var metadataRPC = func(ctx context.Context, addr, domain string) (*awarenesspb.MetadataResponse, error) {
	c, err := connectAWG(addr)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.MetadataScoped(ctx, domain)
}

func runMetadata(args []string) int {
	fs := flag.NewFlagSet("sensei metadata", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", defaultServiceAddr(), "AWG gRPC server address")
	domain := fs.String("domain", "", "scope per-class counts to a domain/repo (e.g. github.com/globulario/services); empty = graph-wide")
	asJSON := fs.Bool("json", false, "output as JSON")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei metadata [flags]

Shows graph-level coverage, freshness, and build provenance.
Use this to tell whether an EMPTY briefing means "no rules apply"
(graph is well-covered) or "this area is unannotated" (thin coverage).

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := metadataRPC(ctx, *addr, *domain)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei metadata: %s\n", formatReadSurfaceError("metadata", err))
		return 1
	}

	if *asJSON {
		return emitProtoJSON(resp)
	}

	fmt.Printf("Server version:        %s\n", strOrDash(resp.GetServerVersion()))
	if t := resp.GetServerStartedUnix(); t != 0 {
		fmt.Printf("Server started:        %s\n", time.Unix(t, 0).UTC().Format(time.RFC3339))
	} else {
		fmt.Printf("Server started:        (unstamped)\n")
	}
	fmt.Println()
	fmt.Println("Build provenance:")
	fmt.Printf("  Graph build commit:  %s\n", strOrDash(resp.GetGraphBuildCommit()))
	if t := resp.GetGraphBuildTimeUnix(); t != 0 {
		fmt.Printf("  Graph build time:    %s\n", time.Unix(t, 0).UTC().Format(time.RFC3339))
	} else {
		fmt.Printf("  Graph build time:    (unstamped)\n")
	}
	fmt.Printf("  Source repo commit:  %s\n", strOrDash(resp.GetSourceRepoCommit()))
	if resp.GetEmbeddedSeedDigestSha256() != "" {
		fmt.Printf("  Seed digest:         %s\n", resp.GetEmbeddedSeedDigestSha256())
		fmt.Printf("  Live graph digest:   %s\n", strOrDash(resp.GetLiveStoreGraphDigestSha256()))
		fmt.Printf("  Live graph triples:  %d\n", resp.GetLiveStoreGraphTripleCount())
	}
	fmt.Printf("  Provenance state:    %s\n", strings.ToLower(strings.TrimPrefix(resp.GetBuildProvenanceState().String(), "BUILD_PROVENANCE_STATE_")))
	fmt.Printf("  Coverage state:      %s\n", strings.ToLower(strings.TrimPrefix(resp.GetCoverageState().String(), "COVERAGE_STATE_")))
	fmt.Printf("  Seed state:          %s\n", strings.ToLower(strings.TrimPrefix(resp.GetSeedState().String(), "SEED_STATE_")))
	fmt.Printf("  Freshness state:     %s\n", strings.ToLower(strings.TrimPrefix(resp.GetGraphFreshnessState().String(), "GRAPH_FRESHNESS_STATE_")))
	if detail := strings.TrimSpace(resp.GetGraphFreshnessDetail()); detail != "" {
		fmt.Printf("  Freshness detail:    %s\n", detail)
	}
	fmt.Println()
	fmt.Println("Live counts:")
	fmt.Printf("  Triples:             %d\n", resp.GetTripleCount())
	fmt.Printf("  Invariants:          %d\n", resp.GetInvariantCount())
	fmt.Printf("  Failure modes:       %d\n", resp.GetFailureModeCount())
	fmt.Printf("  Incident patterns:   %d\n", resp.GetIncidentPatternCount())
	fmt.Printf("  Intents:             %d\n", resp.GetIntentCount())
	fmt.Printf("  Forbidden fixes:     %d\n", resp.GetForbiddenFixCount())
	fmt.Printf("  Required tests:      %d\n", resp.GetRequiredTestCount())
	fmt.Printf("  Source files:        %d\n", resp.GetSourceFileCount())
	fmt.Printf("  Code symbols:        %d\n", resp.GetCodeSymbolCount())
	fmt.Println()
	fmt.Println("Architectural spine:")
	fmt.Printf("  Meta-principles:     %d\n", resp.GetMetaPrincipleCount())
	fmt.Printf("  Components:          %d\n", resp.GetComponentCount())
	fmt.Printf("  Boundaries:          %d\n", resp.GetBoundaryCount())
	fmt.Printf("  Contracts:           %d\n", resp.GetContractCount())
	fmt.Printf("  Decisions:           %d\n", resp.GetDecisionCount())
	fmt.Printf("  Evidence:            %d\n", resp.GetEvidenceCount())
	fmt.Printf("  Design patterns:     %d\n", resp.GetDesignPatternCount())
	fmt.Printf("  Impl. patterns:      %d\n", resp.GetImplementationPatternCount())
	fmt.Printf("  Pattern misuses:     %d\n", resp.GetPatternMisuseCount())
	fmt.Println()
	fmt.Println("Local review surfaces:")
	fmt.Printf("  Candidate queue:     %s\n", strings.ToLower(strings.TrimPrefix(resp.GetCandidateQueueState().String(), "CANDIDATE_QUEUE_STATE_")))
	fmt.Printf("  Candidate files:     %d\n", resp.GetLocalCandidateFileCount())
	fmt.Printf("  Candidate entries:   %d\n", resp.GetLocalCandidateEntryCount())
	fmt.Printf("  Benchmark state:     %s\n", strings.ToLower(strings.TrimPrefix(resp.GetBenchmarkState().String(), "BENCHMARK_STATE_")))
	fmt.Printf("  Benchmark contracts: %d\n", resp.GetBenchmarkContractCount())
	fmt.Printf("  Learning events:     %d\n", resp.GetBenchmarkLearningEventCount())
	if t := resp.GetBenchmarkLatestLearningEventUnix(); t != 0 {
		fmt.Printf("  Latest benchmark:    %s (%s, score %d, %s)\n",
			time.Unix(t, 0).UTC().Format(time.RFC3339),
			strOrDash(resp.GetBenchmarkLatestTaskId()),
			resp.GetBenchmarkLatestScore(),
			strOrDash(resp.GetBenchmarkLatestCertificationStatus()),
		)
	}
	fmt.Printf("  Governance pack:     %s\n", strings.ToLower(strings.TrimPrefix(resp.GetGovernancePackState().String(), "GOVERNANCE_PACK_STATE_")))
	if resp.GetGovernancePackId() != "" {
		fmt.Printf("  Pack id:             %s\n", resp.GetGovernancePackId())
		fmt.Printf("  Pack version:        %s\n", resp.GetGovernancePackVersion())
		fmt.Printf("  Pack digest:         %s\n", resp.GetGovernancePackDigestSha256())
		fmt.Printf("  Pack publisher:      %s\n", resp.GetGovernancePackPublisher())
	}
	if resp.GetCombinedGraphDigestSha256() != "" {
		fmt.Printf("  Combined digest:     %s\n", resp.GetCombinedGraphDigestSha256())
		fmt.Printf("  Combined triples:    %d\n", resp.GetCombinedGraphTripleCount())
	}
	fmt.Println()
	fmt.Printf("Generated in: %d ms\n", resp.GetGeneratedInMs())
	return 0
}
