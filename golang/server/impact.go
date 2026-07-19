// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=server.impact
// @awareness file_role=grpc_rpc_handler
// @awareness implements=globular.awareness_graph:intent.awareness.impact_distinguishes_direct_and_inferred
package main

import (
	"context"
	"errors"
	"sort"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

// Impact returns all awareness nodes whose anchor includes the given file.
// All returned nodes are directly anchored (aw:implements edge from the source file).
// Store errors surface as codes.Unavailable — never as an empty result set.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=server.impact
// @awareness implements=globular.awareness_graph:intent.awareness.impact_distinguishes_direct_and_inferred
// @awareness enforces=globular.awareness_graph:invariant.awareness.store_unavailable_explicit
// @awareness protects=globular.awareness_graph:failure_mode.awareness.empty_graph_silently_treated_as_no_awareness
// @awareness tested_by=golang/server/impact_test.go:TestImpactStoreNil
// @awareness risk=medium
func (s *server) Impact(ctx context.Context, req *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
	file := strings.TrimSpace(req.GetFile())
	if file == "" {
		return nil, status.Error(codes.InvalidArgument, "file is required")
	}
	if s.store == nil {
		return nil, status.Error(codes.Unavailable, "store is unavailable")
	}
	if err := s.requireCurrentGraphAuthority(ctx, "impact"); err != nil {
		return nil, err
	}
	if err := s.requireDomainWhenAmbiguous(ctx, strings.TrimSpace(req.GetDomain())); err != nil {
		return nil, err
	}
	resp, _, _, err := s.collectImpact(ctx, file, strings.TrimSpace(req.GetDomain()))
	if err != nil {
		// Preserve an already-coded status (e.g. FailedPrecondition for an
		// ambiguous domain scope); only an uncoded error is a backend failure.
		if _, ok := status.FromError(err); ok && status.Code(err) != codes.Unknown {
			return nil, err
		}
		return nil, status.Errorf(codes.Unavailable, "backend query failed: %v", err)
	}
	resp.Authority = s.graphAuthority(ctx)
	return resp, nil
}

// collectImpact returns the file's in-scope impact, per-node provenance, and the
// RESOLVED domain scope for this query. The resolved scope is exported so other
// briefing sections (implementation patterns, intent triggers) can be filtered to
// the SAME domain rather than leaking foreign-repo rules — see briefing.go.
func (s *server) collectImpact(ctx context.Context, file, requestedDomain string) (*awarenesspb.ImpactResponse, map[string]nodeProvenance, string, error) {
	requestedDomain = strings.TrimSpace(requestedDomain)
	if err := s.validateRequestedDomain(ctx, requestedDomain); err != nil {
		return nil, nil, "", err
	}

	fileIRI := mintedIRI(rdf.ClassSourceFile, file)
	facts, err := s.store.ImpactForFile(ctx, fileIRI)
	if err != nil {
		return nil, nil, "", err
	}

	resp := &awarenesspb.ImpactResponse{}
	nodes := map[string]*awarenesspb.KnowledgeNode{}
	nodeClass := map[string]string{}
	nodeDomain := map[string]string{}        // node IRI → resolved domain key
	nodeProv := map[string]*nodeProvenance{} // node IRI → provenance receipt
	for _, f := range facts {
		class, ok := classFromTypeIRI(f.TypeIRI)
		if !ok {
			continue
		}
		n, exists := nodes[f.NodeIRI]
		if !exists {
			id, ok := awarenessIDFromIRI(f.NodeIRI)
			if !ok {
				continue
			}
			n = &awarenesspb.KnowledgeNode{Iri: f.NodeIRI, Id: id, Class: class}
			nodes[f.NodeIRI] = n
			nodeClass[f.NodeIRI] = class
			nodeDomain[f.NodeIRI] = s.homeDomain // default until a domain/repo fact says otherwise
			nodeProv[f.NodeIRI] = &nodeProvenance{}
		}
		// Capture domain scope from the node's own facts. aw:repo names a repo
		// domain; aw:domain=shared marks a portable meta-principle; untagged
		// nodes keep the home domain assigned above.
		switch f.Predicate {
		case rdf.PropRepo:
			if f.Object != "" {
				nodeDomain[f.NodeIRI] = f.Object
			}
		case rdf.PropDomain:
			if f.Object == rdf.DomainShared {
				nodeDomain[f.NodeIRI] = rdf.DomainShared
			}
		}
		// Capture provenance facts so the briefing can explain a promoted rule's
		// chain of custody. Untagged nodes accumulate nothing.
		applyProvenanceFact(nodeProv[f.NodeIRI], f.Predicate, f.Object)
		applyNodeFact(n, store.Triple{
			Predicate:   f.Predicate,
			Object:      f.Object,
			ObjectIsIRI: f.ObjectIsIRI,
		})
	}

	// Domain scoping: never return a result set that mixes domains. The
	// selectable domains are exactly those present among these nodes (shared is
	// always admissible and never counts as selectable). ResolveScope applies
	// the agreed policy: explicit request wins; a single domain resolves
	// trivially (host project's existing single-domain briefings are unchanged);
	// 2+ domains with no request fails closed rather than mixing. See scope.go.
	available := make([]string, 0, len(nodeDomain))
	for _, d := range nodeDomain {
		available = append(available, d)
	}
	resolved, scopeErr := ResolveScope(available, requestedDomain)
	if scopeErr != nil {
		var ae *AmbiguousScopeError
		if errors.As(scopeErr, &ae) {
			return nil, nil, "", status.Errorf(codes.FailedPrecondition, "%s", ae.Error())
		}
		return nil, nil, "", scopeErr
	}
	for iri := range nodes {
		if !InScope(nodeDomain[iri], resolved) {
			delete(nodes, iri)
			delete(nodeClass, iri)
			delete(nodeProv, iri)
		}
	}

	iris := make([]string, 0, len(nodes))
	for iri := range nodes {
		iris = append(iris, iri)
	}
	sort.Strings(iris)
	for _, iri := range iris {
		n := nodes[iri]
		switch nodeClass[iri] {
		case "invariant":
			resp.DirectInvariants = append(resp.DirectInvariants, n)
		case "failure_mode":
			resp.DirectFailureModes = append(resp.DirectFailureModes, n)
		case "incident_pattern":
			resp.DirectIncidentPatterns = append(resp.DirectIncidentPatterns, n)
		case "intent":
			resp.DirectIntents = append(resp.DirectIntents, n)
		case "forbidden_fix":
			resp.ForbiddenFixes = append(resp.ForbiddenFixes, n)
		case "test":
			resp.RequiredTests = append(resp.RequiredTests, n)
		case "component", "boundary", "contract", "decision", "evidence",
			"design_pattern", "implementation_pattern", "pattern_misuse":
			resp.DirectArchitecture = append(resp.DirectArchitecture, n)
		}
	}
	sortKnowledgeNodes(resp.DirectInvariants)
	sortKnowledgeNodes(resp.DirectFailureModes)
	sortKnowledgeNodes(resp.DirectIncidentPatterns)
	sortKnowledgeNodes(resp.DirectIntents)
	sortKnowledgeNodes(resp.ForbiddenFixes)
	sortKnowledgeNodes(resp.RequiredTests)
	// Architecture nodes have no severity; group them by class, then id.
	sort.SliceStable(resp.DirectArchitecture, func(i, j int) bool {
		a, b := resp.DirectArchitecture[i], resp.DirectArchitecture[j]
		if a.GetClass() != b.GetClass() {
			return a.GetClass() < b.GetClass()
		}
		return a.GetId() < b.GetId()
	})
	// Inferred fields (InferredInvariants, InferredFailureModes,
	// InferredIncidentPatterns, InferredIntents) are reserved for a future
	// phase. Populating them requires Component IRI nodes in the graph and a
	// 2-hop SPARQL query; neither exists yet. They remain nil intentionally.
	// See docs/awareness/decisions/inference-v0-direct-anchors-only.md.

	// Provenance, keyed by bare node id (the key briefing/edit-check use), for
	// the in-scope nodes only.
	provByID := make(map[string]nodeProvenance, len(nodeProv))
	for iri, p := range nodeProv {
		if id, ok := awarenessIDFromIRI(iri); ok {
			provByID[id] = *p
		}
	}

	// Symbol-level context: functions/methods defined in the file plus the
	// symbols they reference (from an ingested SCIP index). Additive — a failure
	// here must not sink the architectural answer, so the error is dropped.
	if syms, symErr := collectCodeSymbols(ctx, s.store, fileIRI); symErr == nil {
		for _, cs := range syms {
			resp.Symbols = append(resp.Symbols, &awarenesspb.CodeSymbolNode{
				Id:         strings.ReplaceAll(cs.id, "%2F", "/"),
				Label:      cs.label,
				File:       file,
				Language:   cs.language,
				References: cs.references,
			})
		}
	}
	return resp, provByID, resolved, nil
}

func classFromTypeIRI(typeIRI string) (string, bool) {
	switch typeIRI {
	case rdf.ClassInvariant:
		return "invariant", true
	case rdf.ClassFailureMode:
		return "failure_mode", true
	case rdf.ClassIncidentPattern:
		return "incident_pattern", true
	case rdf.ClassIntent:
		return "intent", true
	case rdf.ClassForbiddenFix:
		return "forbidden_fix", true
	case rdf.ClassTest:
		return "test", true
	// Architectural spine + design-pattern nodes (the "what architecture governs
	// this file" context). MetaPrinciple is intentionally absent: its nodes are
	// dual-typed meta.* invariants and surface via the invariant partition.
	case rdf.ClassComponent:
		return "component", true
	case rdf.ClassBoundary:
		return "boundary", true
	case rdf.ClassContract:
		return "contract", true
	case rdf.ClassDecision:
		return "decision", true
	case rdf.ClassEvidence:
		return "evidence", true
	case rdf.ClassDesignPattern:
		return "design_pattern", true
	case rdf.ClassImplementationPattern:
		return "implementation_pattern", true
	case rdf.ClassPatternMisuse:
		return "pattern_misuse", true
	default:
		return "", false
	}
}

func sortKnowledgeNodes(nodes []*awarenesspb.KnowledgeNode) {
	sort.Slice(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if severityRank(a.GetSeverity()) != severityRank(b.GetSeverity()) {
			return severityRank(a.GetSeverity()) < severityRank(b.GetSeverity())
		}
		if a.GetId() != b.GetId() {
			return a.GetId() < b.GetId()
		}
		return a.GetLabel() < b.GetLabel()
	})
}

func severityRank(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	case "warning":
		return 3
	case "info":
		return 4
	case "degraded":
		return 5
	case "":
		return 6
	default:
		return 7
	}
}
