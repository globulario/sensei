// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=server.metadata
// @awareness file_role=grpc_rpc_handler
// @awareness implements=globular.awareness_graph:intent.awg.graph_is_compiled_context_not_authority
// @awareness risk=medium
package main

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/seedmeta"
)

// Metadata returns graph-level coverage and freshness signals. Agents
// call this at session start to interpret EMPTY briefings: a healthy
// graph + empty briefing means "no rules apply here"; a low-coverage
// graph + empty briefing means "this domain is unannotated."
//
// Counts come from live SPARQL queries (bounded, cheap). Build provenance
// comes from ldflags set at compile time. A nil store returns Unavailable
// — never a silent zero response that an agent could misread as a healthy
// empty graph.
func (s *server) Metadata(ctx context.Context, req *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
	if s.store == nil {
		return nil, status.Error(codes.Unavailable, "store is unavailable")
	}

	start := time.Now()
	resp := &awarenesspb.MetadataResponse{
		GraphBuildCommit:   BuildCommit,
		SourceRepoCommit:   SourceCommit,
		GraphBuildTimeUnix: parseUnixStamp(BuildTimeUnix),
		ServerVersion:      Version,
		ServerStartedUnix:  serverStartedUnix,
	}
	local := collectLocalMetadata()
	resp.CandidateQueueState = local.candidateState
	resp.LocalCandidateFileCount = local.candidateFileCount
	resp.LocalCandidateEntryCount = local.candidateEntryCount
	resp.BenchmarkState = local.benchmarkState
	resp.BenchmarkContractCount = local.benchmarkContracts
	resp.BenchmarkLearningEventCount = local.benchmarkEvents
	resp.BenchmarkLatestLearningEventUnix = local.benchmarkLatestUnix
	resp.BenchmarkLatestTaskId = local.benchmarkLatestTask
	resp.BenchmarkLatestScore = local.benchmarkLatestScore
	resp.BenchmarkLatestCertificationStatus = local.benchmarkLatestCert
	resp.GovernancePackState = local.governancePackState
	resp.GovernancePackId = local.governancePackID
	resp.GovernancePackVersion = local.governancePackVer
	resp.GovernancePackDigestSha256 = local.governancePackDigest
	resp.GovernancePackPublisher = local.governancePublisher
	resp.CombinedGraphDigestSha256 = local.combinedGraphDigest
	resp.CombinedGraphTripleCount = local.combinedGraphTriples
	freshness := snapshotGraphFreshness(ctx, s)
	resp.EmbeddedSeedDigestSha256 = freshness.verification.Expected.Digest
	resp.EmbeddedSeedMarkerIri = freshness.verification.Expected.IRI
	resp.LiveStoreContainsEmbeddedSeedMarker = freshness.verification.MarkerPresent
	resp.LiveStoreGraphDigestSha256 = freshness.verification.Live.Digest
	resp.LiveStoreGraphTripleCount = freshness.verification.LiveTripleCount
	resp.GraphFreshnessState = graphFreshnessStateProto(freshness.verification.State)
	resp.GraphFreshnessDetail = freshness.verification.Detail
	if resp.GetGovernancePackState() == awarenesspb.GovernancePackState_GOVERNANCE_PACK_STATE_CURRENT &&
		freshness.verification.State != seedmeta.FreshnessCurrent {
		resp.GovernancePackState = awarenesspb.GovernancePackState_GOVERNANCE_PACK_STATE_STALE
	}
	if resp.GetCombinedGraphDigestSha256() == "" && freshness.verification.Expected.Digest != "" {
		resp.CombinedGraphDigestSha256 = freshness.verification.Expected.Digest
		resp.CombinedGraphTripleCount = freshness.verification.Expected.TripleCount
	}
	stamp := seedmeta.ParseTransactionStamp(seedTransactionStamp)
	stamp, txMode, txPath, txReadErr := transactionStampForGraph(s)
	resp.EmbeddedTransactionStampPresent = stamp.Present
	resp.CertifiedAwarenessGraphCommit = stamp.AwarenessGraphCommit
	resp.CertifiedServicesRepoCommit = stamp.ServicesCommit
	resp.EmbeddedTransactionMatchesSeed, resp.EmbeddedTransactionDetail = evaluateTransactionForGraph(
		freshness.verification.Expected,
		stamp,
		txMode,
		txPath,
		txReadErr,
	)
	usage := s.surfaceUsage.snapshot()
	resp.BriefingCallCount = usage.briefingCalls
	resp.BriefingAgentCompactCount = usage.briefingAgentCompact
	resp.ResolveCallCount = usage.resolveCalls
	resp.ResolveFoundCount = usage.resolveFound
	resp.ResolveMissCount = usage.resolveMiss

	// CountTriples + per-class counts. Each query is independent — one
	// failure shouldn't blank the whole response, so we let the helpers
	// return 0 on error rather than propagating.
	type counter interface {
		CountTriples(context.Context) (int64, error)
		CountByClass(context.Context, string) (int64, error)
	}
	c, ok := s.store.(counter)
	if !ok {
		// Backend doesn't expose counts — return build provenance only.
		resp.BuildProvenanceState = classifyBuildProvenance(resp)
		resp.SeedState = graphFreshnessSeedState(freshness.verification)
		resp.GeneratedInMs = time.Since(start).Milliseconds()
		return resp, nil
	}

	// The selectable domains, so a client can offer a filter.
	resp.AvailableDomains = s.availableDomains(ctx)

	// Per-class counts. Graph-wide by default (fast COUNT); when a domain is
	// requested, count only nodes visible to it (reusing InScope over the facts
	// ClassFacts already returns) so a multi-domain graph reports this project's
	// totals. triple_count stays the raw store size — triples are not cleanly
	// domain-attributable — so the client labels it graph-wide.
	domain := strings.TrimSpace(req.GetDomain())
	countClass := func(classIRI string) int64 {
		if domain == "" {
			n, _ := c.CountByClass(ctx, classIRI)
			return n
		}
		// Prefer the uncapped domain count; fall back to the (capped) facts path
		// only if the store can't enumerate class node domains.
		if n, ok := s.countClassInScopeUncapped(ctx, classIRI, s.homeDomain, domain); ok {
			return n
		}
		facts, err := s.store.ClassFacts(ctx, classIRI, 0)
		if err != nil {
			return 0
		}
		return countClassInScope(facts, s.homeDomain, domain)
	}

	// triple_count: graph-wide by default; when a domain is requested and the
	// store can attribute triples, count only that domain's (each triple by its
	// subject's domain — repo/shared/home).
	if domain != "" {
		if tc, ok := s.store.(tripleDomainCounter); ok {
			if n, err := tc.CountTriplesInDomain(ctx, domain, s.homeDomain); err == nil {
				resp.TripleCount = n
			}
		} else if n, err := c.CountTriples(ctx); err == nil {
			resp.TripleCount = n
		}
	} else if n, err := c.CountTriples(ctx); err == nil {
		resp.TripleCount = n
	}
	resp.InvariantCount = countClass(rdf.ClassInvariant)
	resp.FailureModeCount = countClass(rdf.ClassFailureMode)
	resp.IncidentPatternCount = countClass(rdf.ClassIncidentPattern)
	resp.IntentCount = countClass(rdf.ClassIntent)
	resp.ForbiddenFixCount = countClass(rdf.ClassForbiddenFix)
	resp.RequiredTestCount = countClass(rdf.ClassTest)
	resp.SourceFileCount = countClass(rdf.ClassSourceFile)
	resp.CodeSymbolCount = countClass(rdf.ClassCodeSymbol)

	// Architectural-spine counts (Stage A). meta_principle_count counts the
	// dual-typed meta.* invariants (also included in invariant_count).
	resp.MetaPrincipleCount = countClass(rdf.ClassMetaPrinciple)
	resp.ComponentCount = countClass(rdf.ClassComponent)
	resp.BoundaryCount = countClass(rdf.ClassBoundary)
	resp.ContractCount = countClass(rdf.ClassContract)
	resp.DecisionCount = countClass(rdf.ClassDecision)
	resp.EvidenceCount = countClass(rdf.ClassEvidence)
	resp.DesignPatternCount = countClass(rdf.ClassDesignPattern)
	resp.ImplementationPatternCount = countClass(rdf.ClassImplementationPattern)
	resp.PatternMisuseCount = countClass(rdf.ClassPatternMisuse)

	resp.BuildProvenanceState = classifyBuildProvenance(resp)
	resp.CoverageState = classifyCoverage(resp)
	resp.SeedState = graphFreshnessSeedState(freshness.verification)
	resp.GeneratedInMs = time.Since(start).Milliseconds()
	return resp, nil
}

func classifyBuildProvenance(resp *awarenesspb.MetadataResponse) awarenesspb.BuildProvenanceState {
	if resp.GetServerVersion() == "0.0.0-dev" || strings.TrimSpace(resp.GetGraphBuildCommit()) == "" {
		return awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_DEV
	}
	if strings.TrimSpace(resp.GetSourceRepoCommit()) == "" || resp.GetGraphBuildTimeUnix() == 0 {
		return awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_INCOMPLETE
	}
	return awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED
}

func classifyCoverage(resp *awarenesspb.MetadataResponse) awarenesspb.CoverageState {
	if resp.GetTripleCount() == 0 {
		return awarenesspb.CoverageState_COVERAGE_STATE_EMPTY
	}
	present := 0
	for _, n := range []int64{
		resp.GetInvariantCount(),
		resp.GetFailureModeCount(),
		resp.GetIncidentPatternCount(),
		resp.GetIntentCount(),
		resp.GetForbiddenFixCount(),
		resp.GetRequiredTestCount(),
		resp.GetSourceFileCount(),
		resp.GetComponentCount(),
		resp.GetContractCount(),
		resp.GetDecisionCount(),
	} {
		if n > 0 {
			present++
		}
	}
	if resp.GetSourceFileCount() == 0 || present < 4 {
		return awarenesspb.CoverageState_COVERAGE_STATE_THIN
	}
	return awarenesspb.CoverageState_COVERAGE_STATE_SUFFICIENT
}

func transactionStampForGraph(s *server) (seedmeta.TransactionStamp, string, string, error) {
	if s != nil && strings.TrimSpace(s.graphMarkerFile) != "" {
		txPath := seedmeta.RuntimeTransactionPath(s.graphMarkerFile)
		data, err := os.ReadFile(txPath)
		if err != nil {
			return seedmeta.TransactionStamp{}, "runtime", txPath, err
		}
		return seedmeta.ParseTransactionStamp(data), "runtime", txPath, nil
	}
	return seedmeta.ParseTransactionStamp(seedTransactionStamp), "embedded", "", nil
}

func evaluateTransactionForGraph(marker seedmeta.Marker, stamp seedmeta.TransactionStamp, mode, txPath string, readErr error) (bool, string) {
	label := "embedded"
	if strings.TrimSpace(mode) == "runtime" {
		label = "runtime"
	}
	if readErr != nil {
		if os.IsNotExist(readErr) {
			if txPath != "" {
				return false, label + " transaction stamp missing: " + txPath
			}
			return false, label + " transaction stamp missing"
		}
		if txPath != "" {
			return false, "read " + label + " transaction stamp " + txPath + ": " + readErr.Error()
		}
		return false, "read " + label + " transaction stamp: " + readErr.Error()
	}
	if !stamp.Present {
		return false, label + " transaction stamp missing"
	}
	if strings.TrimSpace(marker.Digest) == "" {
		return false, label + " seed digest missing"
	}
	if strings.TrimSpace(stamp.SeedDigest) == "" {
		return false, label + " transaction seed digest missing"
	}
	if stamp.SeedDigest != marker.Digest {
		return false, label + " transaction seed digest does not match expected graph"
	}
	if stamp.SeedTripleCount != "" {
		if n, err := strconv.ParseInt(strings.TrimSpace(stamp.SeedTripleCount), 10, 64); err != nil {
			return false, label + " transaction seed triple count is invalid"
		} else if marker.TripleCount > 0 && n != marker.TripleCount {
			return false, label + " transaction seed triple count does not match expected graph"
		}
	}
	return true, label + " transaction certifies expected graph"
}

// parseUnixStamp tolerates an empty BuildTimeUnix (un-stamped build)
// by returning 0. Anything non-numeric is also treated as 0 — the agent
// should rely on GraphBuildCommit being empty as the "un-stamped" signal,
// not on this field.
func parseUnixStamp(s string) int64 {
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
