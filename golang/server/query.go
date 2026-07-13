// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=server.query
// @awareness file_role=grpc_rpc_handler
// @awareness implements=globular.awareness_graph:intent.awareness.query_does_not_expose_arbitrary_sparql
// @awareness implements=globular.platform:intent.awareness.graph_is_compiled_context_not_authority
package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

const (
	defaultQueryLimit = 20
	maxQueryLimit     = 100
)

// Query browses the awareness graph in one of four typed modes.
// Arbitrary SPARQL is never exposed — the mode enum is a closed whitelist.
// Unknown modes return codes.InvalidArgument, never a passthrough.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=server.query
// @awareness implements=globular.awareness_graph:intent.awareness.query_does_not_expose_arbitrary_sparql
// @awareness enforces=globular.awareness_graph:invariant.awareness.query.no_arbitrary_sparql
// @awareness enforces=globular.awareness_graph:invariant.awareness.store_unavailable_explicit
// @awareness protects=globular.awareness_graph:failure_mode.awareness.raw_sparql_exposed_to_agent
// @awareness tested_by=golang/server/query_test.go:TestQueryUnknownMode
// @awareness risk=high
func (s *server) Query(ctx context.Context, req *awarenesspb.QueryRequest) (*awarenesspb.QueryResponse, error) {
	if s.store == nil {
		return nil, status.Error(codes.Unavailable, "store is unavailable")
	}
	if err := s.requireCurrentGraphAuthority(ctx, "query"); err != nil {
		return nil, err
	}
	start := time.Now()
	limit := normalizeQueryLimit(int(req.GetLimit()))

	var rows []*awarenesspb.QueryRow
	var err error
	switch req.GetMode() {
	case awarenesspb.QueryMode_QUERY_MODE_BY_FILE:
		rows, err = s.queryByFile(ctx, strings.TrimSpace(req.GetFile()), strings.TrimSpace(req.GetDomain()))
	case awarenesspb.QueryMode_QUERY_MODE_BY_ID:
		rows, err = s.queryByID(ctx, strings.TrimSpace(req.GetId()))
	case awarenesspb.QueryMode_QUERY_MODE_BY_CLASS:
		rows, err = s.queryByClass(ctx, req.GetClass(), limit, strings.TrimSpace(req.GetDomain()))
	case awarenesspb.QueryMode_QUERY_MODE_RELATED:
		rows, err = s.queryRelated(ctx, strings.TrimSpace(req.GetId()), limit)
	default:
		return nil, status.Error(codes.InvalidArgument, "unsupported query mode")
	}
	if err != nil {
		return nil, err
	}
	sortQueryRows(rows)
	return &awarenesspb.QueryResponse{
		Rows:          rows,
		GeneratedInMs: time.Since(start).Milliseconds(),
		Authority:     s.graphAuthority(ctx),
	}, nil
}

func (s *server) queryByFile(ctx context.Context, file, domain string) ([]*awarenesspb.QueryRow, error) {
	if file == "" {
		return nil, status.Error(codes.InvalidArgument, "file is required for by_file")
	}
	impact, _, _, err := s.collectImpact(ctx, file, domain)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "backend query failed: %v", err)
	}
	rows := make([]*awarenesspb.QueryRow, 0)
	rows = appendRowsFromNodes(rows, impact.GetDirectInvariants(), "")
	rows = appendRowsFromNodes(rows, impact.GetDirectFailureModes(), "")
	rows = appendRowsFromNodes(rows, impact.GetDirectIncidentPatterns(), "")
	rows = appendRowsFromNodes(rows, impact.GetDirectIntents(), "")
	rows = appendRowsFromNodes(rows, impact.GetDirectArchitecture(), "")
	return rows, nil
}

func (s *server) queryByID(ctx context.Context, qualifiedID string) ([]*awarenesspb.QueryRow, error) {
	class, id, err := parseQualifiedID(qualifiedID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	iri, canonicalClass, err := resolveIRIForClassAndID(class, id)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	triples, err := s.store.Describe(ctx, iri)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "backend query failed: %v", err)
	}
	if len(triples) == 0 {
		return nil, nil
	}
	n := &awarenesspb.KnowledgeNode{Iri: iri, Id: id, Class: canonicalClass}
	for _, t := range triples {
		applyNodeFact(n, t)
	}
	return []*awarenesspb.QueryRow{rowFromNode(n, "")}, nil
}

// maxScopedListFetch is how many class facts to pull before domain-filtering a
// by_class list — the store's ClassFacts cap. A scoped list is complete for
// classes with up to this many nodes; larger classes render a (still in-scope)
// prefix.
const maxScopedListFetch = 300

func (s *server) queryByClass(ctx context.Context, queryClass awarenesspb.QueryClass, limit int, domain string) ([]*awarenesspb.QueryRow, error) {
	className, classIRI, ok := queryClassSpec(queryClass)
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "invalid class for by_class")
	}
	// Domain-scoped: prefer a store that applies the domain FILTER inside its
	// selection LIMIT (ClassFactsScoped), so the LIMIT lands on in-scope nodes
	// rather than an arbitrary page that may contain none. Fall back to
	// fetch-then-filter (capped) only if the store lacks it.
	var facts []store.ImpactFact
	var keep map[string]bool
	if domain != "" {
		if sc, ok := s.store.(classFactsScoper); ok {
			var err error
			if facts, err = sc.ClassFactsScoped(ctx, classIRI, domain, s.homeDomain, limit); err != nil {
				return nil, status.Errorf(codes.Unavailable, "backend query failed: %v", err)
			}
		} else {
			var err error
			if facts, err = s.store.ClassFacts(ctx, classIRI, maxScopedListFetch); err != nil {
				return nil, status.Errorf(codes.Unavailable, "backend query failed: %v", err)
			}
			keep = keepIRIsInScope(facts, s.homeDomain, domain)
		}
	} else {
		var err error
		if facts, err = s.store.ClassFacts(ctx, classIRI, limit); err != nil {
			return nil, status.Errorf(codes.Unavailable, "backend query failed: %v", err)
		}
	}

	nodes := nodesFromFacts(facts, className)
	rows := make([]*awarenesspb.QueryRow, 0, len(nodes))
	for _, n := range nodes {
		if keep != nil && !keep[n.GetIri()] {
			continue // fallback path: node belongs to another domain
		}
		rows = append(rows, rowFromNode(n, ""))
		if limit > 0 && len(rows) >= limit {
			break
		}
	}
	return rows, nil
}

// queryRelated returns the neighbours of a node in BOTH directions. Outgoing
// edges (<iri> ?p ?o) and inbound edges (?s ?p <iri>) are both traversed, so a
// node that is primarily an edge *object* — a Test an Invariant `requiresTest`,
// a SourceFile a CodeSymbol is `definedInFile` — is no longer reported as
// unlinked. Inbound edges are labelled from the queried node's perspective
// (requiresTest → verifies, definedInFile → defines). Outgoing is traversed
// first; results are de-duplicated by related IRI and capped at limit.
func (s *server) queryRelated(ctx context.Context, qualifiedID string, limit int) ([]*awarenesspb.QueryRow, error) {
	class, id, err := parseQualifiedID(qualifiedID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	iri, _, err := resolveIRIForClassAndID(class, id)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	rows := make([]*awarenesspb.QueryRow, 0)
	seen := map[string]bool{} // related IRI -> added (dedup across both directions)

	// addRelated resolves one neighbour IRI, builds its node, and appends a row.
	// Returns cont=false when the limit is reached.
	addRelated := func(neighbourIRI, relation string) (cont bool, err error) {
		relatedID, ok := awarenessRelatedID(neighbourIRI)
		if !ok {
			return true, nil
		}
		relClass, relBareID, perr := parseQualifiedID(relatedID)
		if perr != nil {
			return true, nil
		}
		relIRI, relCanonicalClass, rerr := resolveIRIForClassAndID(relClass, relBareID)
		if rerr != nil {
			return true, nil
		}
		if relIRI == iri || seen[relIRI] {
			return true, nil // skip self-loops and nodes already surfaced
		}
		seen[relIRI] = true
		relTriples, derr := s.store.Describe(ctx, relIRI)
		if derr != nil {
			return false, status.Errorf(codes.Unavailable, "backend query failed: %v", derr)
		}
		n := &awarenesspb.KnowledgeNode{Iri: relIRI, Id: relBareID, Class: relCanonicalClass}
		for _, rt := range relTriples {
			applyNodeFact(n, rt)
		}
		rows = append(rows, rowFromNode(n, relation))
		return len(rows) < limit, nil
	}

	// Outgoing: <iri> ?p ?o.
	out, err := s.store.Describe(ctx, iri)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "backend query failed: %v", err)
	}
	for _, t := range out {
		if !t.ObjectIsIRI {
			continue
		}
		cont, aerr := addRelated(t.Object, relationShortName(t.Predicate))
		if aerr != nil {
			return nil, aerr
		}
		if !cont {
			return rows, nil
		}
	}

	// Inbound: ?s ?p <iri>. rdf:type is excluded — class membership is not a
	// related-node relationship (and only matters when iri is a class anyway).
	in, err := s.store.DescribeInbound(ctx, iri)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "backend query failed: %v", err)
	}
	for _, t := range in {
		if t.Predicate == rdf.PropType {
			continue
		}
		cont, aerr := addRelated(t.Subject, inboundRelationName(t.Predicate))
		if aerr != nil {
			return nil, aerr
		}
		if !cont {
			return rows, nil
		}
	}
	return rows, nil
}

// inboundRelationLabels names an inbound edge from the queried node's
// perspective: <subject> <predicate> <queried> reads as "queried <label> subject".
// Curated for the common predicates; uncurated ones fall back to the short
// predicate name so the relationship is still legible.
var inboundRelationLabels = map[string]string{
	"requiresTest":  "verifies",    // invariant requiresTest test   → test verifies invariant
	"protects":      "protectedBy", // invariant protects file       → file protectedBy invariant
	"enforces":      "enforcedBy",
	"definedInFile": "defines",       // symbol definedInFile file     → file defines symbol
	"anchoredIn":    "anchors",       // node anchoredIn file          → file anchors node
	"implements":    "implementedBy", // file implements component     → component implementedBy file
	"expressedBy":   "expresses",
	"configures":    "configuredBy",
	"affects":       "affectedBy",
	"memberOfGroup": "hasMember",
}

func inboundRelationName(predicate string) string {
	short := relationShortName(predicate)
	if inv, ok := inboundRelationLabels[short]; ok {
		return inv
	}
	return short
}

func appendRowsFromNodes(dst []*awarenesspb.QueryRow, nodes []*awarenesspb.KnowledgeNode, relation string) []*awarenesspb.QueryRow {
	for _, n := range nodes {
		dst = append(dst, rowFromNode(n, relation))
	}
	return dst
}

func rowFromNode(n *awarenesspb.KnowledgeNode, relation string) *awarenesspb.QueryRow {
	if n == nil {
		return &awarenesspb.QueryRow{}
	}
	sourceYAML := ""
	if n.GetAnchor() != nil {
		sourceYAML = n.GetAnchor().GetSourceYaml()
	}
	return &awarenesspb.QueryRow{
		Id:            n.GetClass() + ":" + n.GetId(),
		Class:         n.GetClass(),
		Label:         n.GetLabel(),
		Severity:      n.GetSeverity(),
		Status:        n.GetStatus(),
		Relation:      relation,
		SourceFile:    sourceYAML,
		UmlKind:       n.GetUmlKind(),
		UmlStereotype: n.GetUmlStereotype(),
		UmlView:       n.GetUmlView(),
	}
}

func normalizeQueryLimit(limit int) int {
	if limit <= 0 {
		return defaultQueryLimit
	}
	if limit > maxQueryLimit {
		return maxQueryLimit
	}
	return limit
}

func parseQualifiedID(qualifiedID string) (class, id string, err error) {
	parts := strings.SplitN(strings.TrimSpace(qualifiedID), ":", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("id must be class-qualified: class:id")
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

func queryClassSpec(queryClass awarenesspb.QueryClass) (className, classIRI string, ok bool) {
	switch queryClass {
	case awarenesspb.QueryClass_QUERY_CLASS_INVARIANT:
		return "invariant", rdf.ClassInvariant, true
	case awarenesspb.QueryClass_QUERY_CLASS_FAILURE_MODE:
		return "failure_mode", rdf.ClassFailureMode, true
	case awarenesspb.QueryClass_QUERY_CLASS_INCIDENT_PATTERN:
		return "incident_pattern", rdf.ClassIncidentPattern, true
	case awarenesspb.QueryClass_QUERY_CLASS_INTENT:
		return "intent", rdf.ClassIntent, true
	case awarenesspb.QueryClass_QUERY_CLASS_SYMBOL:
		return "symbol", rdf.ClassSymbol, true
	case awarenesspb.QueryClass_QUERY_CLASS_SOURCE_FILE:
		return "source_file", rdf.ClassSourceFile, true
	case awarenesspb.QueryClass_QUERY_CLASS_CODE_SYMBOL:
		return "code_symbol", rdf.ClassCodeSymbol, true
	case awarenesspb.QueryClass_QUERY_CLASS_FORBIDDEN_FIX:
		return "forbidden_fix", rdf.ClassForbiddenFix, true
	case awarenesspb.QueryClass_QUERY_CLASS_TEST:
		return "test", rdf.ClassTest, true
	// Architectural spine (Stage A).
	case awarenesspb.QueryClass_QUERY_CLASS_META_PRINCIPLE:
		return "meta_principle", rdf.ClassMetaPrinciple, true
	case awarenesspb.QueryClass_QUERY_CLASS_COMPONENT:
		return "component", rdf.ClassComponent, true
	case awarenesspb.QueryClass_QUERY_CLASS_BOUNDARY:
		return "boundary", rdf.ClassBoundary, true
	case awarenesspb.QueryClass_QUERY_CLASS_CONTRACT:
		return "contract", rdf.ClassContract, true
	case awarenesspb.QueryClass_QUERY_CLASS_DECISION:
		return "decision", rdf.ClassDecision, true
	case awarenesspb.QueryClass_QUERY_CLASS_EVIDENCE:
		return "evidence", rdf.ClassEvidence, true
	// Design-pattern awareness.
	case awarenesspb.QueryClass_QUERY_CLASS_DESIGN_PATTERN:
		return "design_pattern", rdf.ClassDesignPattern, true
	case awarenesspb.QueryClass_QUERY_CLASS_IMPLEMENTATION_PATTERN:
		return "implementation_pattern", rdf.ClassImplementationPattern, true
	case awarenesspb.QueryClass_QUERY_CLASS_PATTERN_MISUSE:
		return "pattern_misuse", rdf.ClassPatternMisuse, true
	default:
		return "", "", false
	}
}

func nodesFromFacts(facts []store.ImpactFact, className string) []*awarenesspb.KnowledgeNode {
	nodes := map[string]*awarenesspb.KnowledgeNode{}
	iris := make([]string, 0)
	for _, f := range facts {
		n, exists := nodes[f.NodeIRI]
		if !exists {
			id, ok := awarenessIDFromIRI(f.NodeIRI)
			if !ok {
				continue
			}
			n = &awarenesspb.KnowledgeNode{Iri: f.NodeIRI, Id: id, Class: className}
			nodes[f.NodeIRI] = n
			iris = append(iris, f.NodeIRI)
		}
		applyNodeFact(n, store.Triple{Predicate: f.Predicate, Object: f.Object, ObjectIsIRI: f.ObjectIsIRI})
	}
	sort.Strings(iris)
	out := make([]*awarenesspb.KnowledgeNode, 0, len(iris))
	for _, iri := range iris {
		out = append(out, nodes[iri])
	}
	sortKnowledgeNodes(out)
	return out
}

func sortQueryRows(rows []*awarenesspb.QueryRow) {
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.GetClass() != b.GetClass() {
			return a.GetClass() < b.GetClass()
		}
		if a.GetSeverity() != b.GetSeverity() {
			return severityRank(a.GetSeverity()) < severityRank(b.GetSeverity())
		}
		if a.GetId() != b.GetId() {
			return a.GetId() < b.GetId()
		}
		if a.GetLabel() != b.GetLabel() {
			return a.GetLabel() < b.GetLabel()
		}
		return a.GetRelation() < b.GetRelation()
	})
}

func relationShortName(predicate string) string {
	if strings.HasPrefix(predicate, rdf.AwNS) {
		return strings.TrimPrefix(predicate, rdf.AwNS)
	}
	if strings.HasPrefix(predicate, rdf.RdfsNS) {
		return strings.TrimPrefix(predicate, rdf.RdfsNS)
	}
	return predicate
}
