// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"
	"sync"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
)

type surfaceUsageCounters struct {
	mu sync.Mutex

	briefingCalls        int
	briefingAgentCompact int
	resolveCalls         int
	resolveFound         int
	resolveMiss          int
}

type surfaceUsageSnapshot struct {
	briefingCalls        int64
	briefingAgentCompact int64
	resolveCalls         int64
	resolveFound         int64
	resolveMiss          int64
}

func (c *surfaceUsageCounters) recordBriefing(depth string) (calls int, agentCompact int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.briefingCalls++
	if depth == "agent_compact" {
		c.briefingAgentCompact++
	}
	return c.briefingCalls, c.briefingAgentCompact
}

func (c *surfaceUsageCounters) recordResolve(found bool) (calls int, foundCount int, missCount int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resolveCalls++
	if found {
		c.resolveFound++
	} else {
		c.resolveMiss++
	}
	return c.resolveCalls, c.resolveFound, c.resolveMiss
}

func (c *surfaceUsageCounters) snapshot() surfaceUsageSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return surfaceUsageSnapshot{
		briefingCalls:        int64(c.briefingCalls),
		briefingAgentCompact: int64(c.briefingAgentCompact),
		resolveCalls:         int64(c.resolveCalls),
		resolveFound:         int64(c.resolveFound),
		resolveMiss:          int64(c.resolveMiss),
	}
}

func (s *server) logBriefingUsage(req *awarenesspb.BriefingRequest, resp *awarenesspb.BriefingResponse, profile briefingSurfaceProfile) {
	if s.logger == nil || resp == nil {
		return
	}
	depth := normalizeBriefingDepth(strings.TrimSpace(req.GetDepth()))
	calls, agentCompact := s.surfaceUsage.recordBriefing(depth)
	s.logger.Printf(
		"awareness-graph: briefing_usage depth=%s file=%t task=%t status=%s refs=%d patterns=%d prose_bytes=%d generated_ms=%d cap_nodes=%d cap_arch=%d cap_refs=%d call=%d agent_compact_calls=%d",
		depth,
		strings.TrimSpace(req.GetFile()) != "",
		strings.TrimSpace(req.GetTask()) != "",
		resp.GetStatus().String(),
		len(resp.GetReferencedIds()),
		len(resp.GetImplementationPatterns()),
		len(resp.GetProse()),
		resp.GetGeneratedInMs(),
		profile.impactNodes,
		profile.architectureNodes,
		profile.referencedIDs,
		calls,
		agentCompact,
	)
}

func (s *server) logResolveUsage(req *awarenesspb.ResolveRequest, resp *awarenesspb.ResolveResponse) {
	if s.logger == nil || resp == nil {
		return
	}
	calls, foundCount, missCount := s.surfaceUsage.recordResolve(resp.GetFound())
	s.logger.Printf(
		"awareness-graph: resolve_usage class=%s id=%s found=%t related=%d facts=%d domain_scoped=%t call=%d found_calls=%d miss_calls=%d",
		strings.TrimSpace(req.GetClass()),
		compactLogToken(strings.TrimSpace(req.GetId()), 96),
		resp.GetFound(),
		len(resp.GetNode().GetRelatedIds()),
		len(resp.GetNode().GetFacts()),
		strings.TrimSpace(req.GetDomain()) != "",
		calls,
		foundCount,
		missCount,
	)
}

func compactLogToken(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
