// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=server.knowledge_scoring
// @awareness file_role=preflight_trust_scoring
// @awareness risk=low
package main

// knowledge_scoring.go — Phase 2D. As the graph grows, agents must know which
// knowledge is trusted, stale, candidate, deprecated, or accepted. This re-ranks
// the surfaced direct anchors so accepted/active lead, drops deprecated/
// superseded out of primary guidance (with a caution that points at the
// replacement), and flags low-confidence knowledge as a caution. Pure ranking
// plus a bounded Describe per surfaced node for the scoring properties that are
// not carried on the KnowledgeNode proto.

import (
	"context"
	"sort"
	"strings"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
)

// nodeScore holds the trust signals for one node, read from its own facts.
type nodeScore struct {
	Confidence      string
	Freshness       string
	SourceKind      string
	PromotionStatus string
	SupersededBy    string // bare id of the replacement
}

// effectiveStatus prefers the explicit promotionStatus, falling back to the
// node's rdfs status.
func effectiveStatus(nodeStatus string, sc nodeScore) string {
	if sc.PromotionStatus != "" {
		return strings.ToLower(strings.TrimSpace(sc.PromotionStatus))
	}
	return strings.ToLower(strings.TrimSpace(nodeStatus))
}

// trustRank orders knowledge by how much it should be trusted as primary
// guidance. Higher is better.
func trustRank(status string, sc nodeScore) int {
	switch effectiveStatus(status, sc) {
	case "active", "accepted":
		base := 5
		switch strings.ToLower(sc.SourceKind) {
		case "manual", "incident":
			base = 6
		}
		if strings.ToLower(sc.Confidence) == "low" || strings.ToLower(sc.Confidence) == "unknown" {
			base = 3
		}
		return base
	case "proposed", "extracted_candidate", "candidate", "seed", "learned_from_incident":
		return 2
	case "deprecated", "superseded", "retired":
		return 0
	default:
		return 4
	}
}

func isPrimaryStatus(status string, sc nodeScore) bool {
	switch effectiveStatus(status, sc) {
	case "deprecated", "superseded", "retired":
		return false
	}
	return true
}

func isLowConfidence(sc nodeScore) bool {
	c := strings.ToLower(strings.TrimSpace(sc.Confidence))
	return c == "low" || c == "unknown"
}

// scoreNode reads the scoring facts for a single node IRI. Best-effort: a store
// error yields an empty score (node keeps its rdfs status).
func (s *server) scoreNode(ctx context.Context, iri string) nodeScore {
	if s.store == nil {
		return nodeScore{}
	}
	triples, err := s.store.Describe(ctx, iri)
	if err != nil {
		return nodeScore{}
	}
	var sc nodeScore
	for _, t := range triples {
		switch t.Predicate {
		case rdf.PropConfidence:
			sc.Confidence = t.Object
		case rdf.PropFreshness:
			sc.Freshness = t.Object
		case rdf.PropSourceKind:
			sc.SourceKind = t.Object
		case rdf.PropPromotionStatus:
			sc.PromotionStatus = t.Object
		case rdf.PropSupersededBy:
			if t.ObjectIsIRI {
				sc.SupersededBy = bareIDFromIRI(t.Object)
			} else {
				sc.SupersededBy = t.Object
			}
		}
	}
	return sc
}

// applyTrustScoring re-ranks the response's direct anchor lists by trust,
// removes deprecated/superseded nodes from primary guidance (adding a caution
// blind_spot that names the replacement when known), and flags low-confidence
// survivors. Returns the blind_spot cautions to append.
func (s *server) applyTrustScoring(ctx context.Context, resp *awarenesspb.PreflightResponse) []string {
	var cautions []string

	rerank := func(nodes []*awarenesspb.KnowledgeNode) []*awarenesspb.KnowledgeNode {
		if len(nodes) == 0 {
			return nodes
		}
		scores := make(map[string]nodeScore, len(nodes))
		for _, n := range nodes {
			scores[n.GetIri()] = s.scoreNode(ctx, n.GetIri())
		}
		var primary []*awarenesspb.KnowledgeNode
		for _, n := range nodes {
			sc := scores[n.GetIri()]
			if !isPrimaryStatus(n.GetStatus(), sc) {
				c := "[" + effectiveStatus(n.GetStatus(), sc) + "] " + n.GetId() + " — not primary guidance"
				if sc.SupersededBy != "" {
					c += "; prefer " + sc.SupersededBy
				}
				cautions = append(cautions, c)
				continue
			}
			if isLowConfidence(sc) {
				cautions = append(cautions, "[low-confidence] "+n.GetId()+" — treat as caution, not settled guidance")
			}
			primary = append(primary, n)
		}
		// Stable sort by trust desc, then keep existing (severity) order.
		sort.SliceStable(primary, func(i, j int) bool {
			return trustRank(primary[i].GetStatus(), scores[primary[i].GetIri()]) >
				trustRank(primary[j].GetStatus(), scores[primary[j].GetIri()])
		})
		return primary
	}

	resp.DirectInvariants = rerank(resp.DirectInvariants)
	resp.DirectFailureModes = rerank(resp.DirectFailureModes)
	resp.DirectIntents = rerank(resp.DirectIntents)
	resp.DirectForbiddenFixes = rerank(resp.DirectForbiddenFixes)
	return cautions
}
