// SPDX-License-Identifier: AGPL-3.0-only

package main

import awarenesspb "github.com/globulario/sensei/golang/pb"

const (
	maxSurfaceNodesPerClass     = 12
	maxSurfaceArchitectureNodes = 12
	maxBriefingReferencedIDs    = 32
	maxBriefingCodeSymbols      = 8
	maxCodeSymbolSectionEntries = 8
	maxResolveRelatedIDs        = 24
	maxResolveFacts             = 24
)

type briefingSurfaceProfile struct {
	impactNodes       int
	architectureNodes int
	referencedIDs     int
	codeSymbols       int
	codeSectionItems  int
	patterns          int
}

var (
	compactBriefingProfile = briefingSurfaceProfile{
		impactNodes:       6,
		architectureNodes: 6,
		referencedIDs:     20,
		codeSymbols:       5,
		codeSectionItems:  5,
		patterns:          2,
	}
	standardBriefingProfile = briefingSurfaceProfile{
		impactNodes:       maxSurfaceNodesPerClass,
		architectureNodes: maxSurfaceArchitectureNodes,
		referencedIDs:     maxBriefingReferencedIDs,
		codeSymbols:       maxBriefingCodeSymbols,
		codeSectionItems:  maxCodeSymbolSectionEntries,
		patterns:          maxPatternsPerBriefing,
	}
	deepBriefingProfile = briefingSurfaceProfile{
		impactNodes:       24,
		architectureNodes: 24,
		referencedIDs:     96,
		codeSymbols:       16,
		codeSectionItems:  16,
		patterns:          maxPatternsPerBriefing,
	}
	agentCompactBriefingProfile = briefingSurfaceProfile{
		impactNodes:       4,
		architectureNodes: 4,
		referencedIDs:     12,
		codeSymbols:       3,
		codeSectionItems:  3,
		patterns:          1,
	}
)

func normalizeBriefingDepth(depth string) string {
	switch depth {
	case "agent_compact":
		return "agent_compact"
	case "compact":
		return "compact"
	case "deep":
		return "deep"
	case "", "standard":
		return "standard"
	default:
		return "standard"
	}
}

func briefingProfileForDepth(depth string) briefingSurfaceProfile {
	switch normalizeBriefingDepth(depth) {
	case "agent_compact":
		return agentCompactBriefingProfile
	case "compact":
		return compactBriefingProfile
	case "deep":
		return deepBriefingProfile
	default:
		return standardBriefingProfile
	}
}

func limitImpactResponseWithProfile(resp *awarenesspb.ImpactResponse, profile briefingSurfaceProfile) *awarenesspb.ImpactResponse {
	return limitImpactResponseWithCaps(resp, profile.impactNodes, profile.architectureNodes)
}

func limitImpactResponseWithCaps(resp *awarenesspb.ImpactResponse, impactCap, architectureCap int) *awarenesspb.ImpactResponse {
	if resp == nil {
		return nil
	}
	resp.DirectInvariants = capNodes(resp.DirectInvariants, impactCap)
	resp.DirectFailureModes = capNodes(resp.DirectFailureModes, impactCap)
	resp.DirectIncidentPatterns = capNodes(resp.DirectIncidentPatterns, impactCap)
	resp.DirectIntents = capNodes(resp.DirectIntents, impactCap)
	resp.ForbiddenFixes = capNodes(resp.ForbiddenFixes, impactCap)
	resp.RequiredTests = capNodes(resp.RequiredTests, impactCap)
	resp.DirectArchitecture = capNodes(resp.DirectArchitecture, architectureCap)
	resp.InferredInvariants = capNodes(resp.InferredInvariants, impactCap)
	resp.InferredFailureModes = capNodes(resp.InferredFailureModes, impactCap)
	resp.InferredIncidentPatterns = capNodes(resp.InferredIncidentPatterns, impactCap)
	resp.InferredIntents = capNodes(resp.InferredIntents, impactCap)
	return resp
}

func compactReferencedIDs(ids []string) []string {
	return compactReferencedIDsWithCap(ids, maxBriefingReferencedIDs)
}

func compactReferencedIDsWithCap(ids []string, cap int) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, min(len(ids), cap))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) >= cap {
			break
		}
	}
	return out
}

func limitCodeSymbols(syms []codeSymbol) []codeSymbol {
	return limitCodeSymbolsWithCap(syms, maxBriefingCodeSymbols)
}

func limitCodeSymbolsWithCap(syms []codeSymbol, cap int) []codeSymbol {
	if len(syms) <= cap {
		return syms
	}
	return syms[:cap]
}

func capStrings(items []string, n int) []string {
	if len(items) <= n {
		return items
	}
	return items[:n]
}

func appendUniqueCapped(dst []string, v string, cap int) []string {
	if v == "" || len(dst) >= cap {
		return dst
	}
	for _, existing := range dst {
		if existing == v {
			return dst
		}
	}
	return append(dst, v)
}

func appendFactCapped(dst []*awarenesspb.NodeFact, pred, value string, cap int) []*awarenesspb.NodeFact {
	if pred == "" || value == "" || len(dst) >= cap {
		return dst
	}
	for _, fact := range dst {
		if fact.GetPredicate() == pred && fact.GetValue() == value {
			return dst
		}
	}
	return append(dst, &awarenesspb.NodeFact{Predicate: pred, Value: value})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
