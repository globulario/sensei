// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=server.resolve
// @awareness file_role=grpc_rpc_handler
// @awareness implements=globular.awareness_graph:intent.awareness.resolve_returns_precise_node_by_class_and_id
package main

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/store"
)

// Resolve fetches a single awareness node by class and bare ID.
// Returns Found=false (not an error) when the node is absent from the store.
// Class must be one of the whitelisted values in resolveIRIForClassAndID.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=server.resolve
// @awareness implements=globular.awareness_graph:intent.awareness.resolve_returns_precise_node_by_class_and_id
// @awareness enforces=globular.awareness_graph:invariant.awareness.store_unavailable_explicit
// @awareness tested_by=golang/server/resolve_test.go:TestResolveNotFound
// @awareness risk=low
func (s *server) Resolve(ctx context.Context, req *awarenesspb.ResolveRequest) (*awarenesspb.ResolveResponse, error) {
	if strings.TrimSpace(req.GetId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if strings.TrimSpace(req.GetClass()) == "" {
		return nil, status.Error(codes.InvalidArgument, "class is required")
	}
	if s.store == nil {
		return nil, status.Error(codes.Unavailable, "store is unavailable")
	}
	if err := s.requireCurrentGraphAuthority(ctx, "resolve"); err != nil {
		return nil, err
	}

	iri, canonicalClass, err := resolveIRIForClassAndID(req.GetClass(), req.GetId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	triples, err := s.store.Describe(ctx, iri)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "backend query failed: %v", err)
	}
	if len(triples) == 0 {
		out := &awarenesspb.ResolveResponse{Found: false, Authority: s.graphAuthority(ctx)}
		s.logResolveUsage(req, out)
		return out, nil
	}

	node := &awarenesspb.KnowledgeNode{
		Iri:   iri,
		Id:    req.GetId(),
		Class: canonicalClass,
	}
	for _, t := range triples {
		applyNodeFact(node, t)
	}

	// Optional domain scope. Resolve targets one explicit node, so there is no
	// ambiguity to fail closed on — but if the caller named a domain, a node
	// that belongs to another repo must not be returned (it is invisible in
	// that scope). The node's domain comes from its own aw:repo/aw:domain
	// facts; untagged nodes default to the home domain.
	if requested := strings.TrimSpace(req.GetDomain()); requested != "" {
		if !InScope(nodeDomainFromTriples(triples, s.homeDomain), requested) {
			out := &awarenesspb.ResolveResponse{Found: false, Authority: s.graphAuthority(ctx)}
			s.logResolveUsage(req, out)
			return out, nil
		}
	}
	out := &awarenesspb.ResolveResponse{Found: true, Node: node, Authority: s.graphAuthority(ctx)}
	s.logResolveUsage(req, out)
	return out, nil
}

// nodeDomainFromTriples derives a node's domain key from its facts: aw:repo
// names a repo domain, aw:domain=shared marks a portable meta-principle, and an
// untagged node falls back to the home domain.
func nodeDomainFromTriples(triples []store.Triple, homeDomain string) string {
	domain := homeDomain
	for _, t := range triples {
		switch t.Predicate {
		case rdf.PropRepo:
			if t.Object != "" {
				domain = t.Object
			}
		case rdf.PropDomain:
			if t.Object == rdf.DomainShared {
				return rdf.DomainShared
			}
		}
	}
	return domain
}

// resolveIRIForClassAndID maps a caller-supplied class name to its canonical IRI.
// The switch is a closed whitelist — unknown classes return InvalidArgument, never a guess.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=server.resolve
// @awareness enforces=globular.awareness_graph:invariant.awareness.query.no_arbitrary_sparql
// @awareness protects=globular.awareness_graph:failure_mode.awareness.raw_sparql_exposed_to_agent
func resolveIRIForClassAndID(class, id string) (iri string, canonicalClass string, err error) {
	// Query rows carry the IRI's path segment verbatim, which is already
	// EncodeIRIPath-encoded (a SourceFile id like "cmd%2Floadnt%2Fmain.go").
	// MintIRI encodes again, so without decoding here a round-trip would
	// double-encode ("%252F") and never match the stored node — which is why
	// SourceFile/CodeSymbol related- and resolve-queries returned nothing.
	// Decoding is a no-op for slash-free ids (invariants, tests), so it is safe
	// to apply uniformly and idempotent for the round-trip.
	id = rdf.DecodeIRIPath(id)
	switch strings.ToLower(strings.TrimSpace(class)) {
	case "invariant":
		return mintedIRI(rdf.ClassInvariant, id), "invariant", nil
	case "failure_mode":
		return mintedIRI(rdf.ClassFailureMode, id), "failure_mode", nil
	case "incident_pattern":
		return mintedIRI(rdf.ClassIncidentPattern, id), "incident_pattern", nil
	case "symbol":
		return mintedIRI(rdf.ClassSymbol, id), "symbol", nil
	case "source_file":
		return mintedIRI(rdf.ClassSourceFile, id), "source_file", nil
	case "intent":
		return mintedIRI(rdf.ClassIntent, id), "intent", nil
	case "code_symbol":
		return mintedIRI(rdf.ClassCodeSymbol, id), "code_symbol", nil
	case "forbidden_fix":
		return mintedIRI(rdf.ClassForbiddenFix, id), "forbidden_fix", nil
	case "test":
		return mintedIRI(rdf.ClassTest, id), "test", nil
	// Architectural spine (Stage A).
	case "meta_principle":
		// Meta-principles are dual-typed meta.* invariants — the node lives at
		// the invariant IRI, so resolve against ClassInvariant.
		return mintedIRI(rdf.ClassInvariant, id), "meta_principle", nil
	case "component":
		return mintedIRI(rdf.ClassComponent, id), "component", nil
	case "boundary":
		return mintedIRI(rdf.ClassBoundary, id), "boundary", nil
	case "contract":
		return mintedIRI(rdf.ClassContract, id), "contract", nil
	case "decision":
		return mintedIRI(rdf.ClassDecision, id), "decision", nil
	case "evidence":
		return mintedIRI(rdf.ClassEvidence, id), "evidence", nil
	case "proof_obligation":
		return mintedIRI(rdf.ClassProofObligation, id), "proof_obligation", nil
	case "proof_slot":
		return mintedIRI(rdf.ClassProofSlot, id), "proof_slot", nil
	// Design-pattern awareness.
	case "design_pattern":
		return mintedIRI(rdf.ClassDesignPattern, id), "design_pattern", nil
	case "implementation_pattern":
		return mintedIRI(rdf.ClassImplementationPattern, id), "implementation_pattern", nil
	case "pattern_misuse":
		return mintedIRI(rdf.ClassPatternMisuse, id), "pattern_misuse", nil
	default:
		return "", "", fmt.Errorf("unsupported class %q", class)
	}
}

func mintedIRI(classIRI, id string) string {
	return strings.TrimSuffix(strings.TrimPrefix(rdf.MintIRI(classIRI, id), "<"), ">")
}

func awarenessRelatedID(iri string) (string, bool) {
	if !strings.HasPrefix(iri, rdf.AwNS) {
		return "", false
	}
	rest := strings.TrimPrefix(iri, rdf.AwNS)
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 || slash+1 >= len(rest) {
		return "", false
	}
	classPart, idPart := rest[:slash], rest[slash+1:]
	switch classPart {
	case "invariant":
		return "invariant:" + idPart, true
	case "failureMode":
		return "failure_mode:" + idPart, true
	case "incidentPattern":
		return "incident_pattern:" + idPart, true
	case "symbol":
		return "symbol:" + idPart, true
	case "sourceFile":
		return "source_file:" + idPart, true
	case "intent":
		return "intent:" + idPart, true
	case "codeSymbol":
		return "code_symbol:" + idPart, true
	case "forbiddenFix":
		return "forbidden_fix:" + idPart, true
	case "test":
		return "test:" + idPart, true
	// Architectural spine (Stage A). The IRI path segment is lowerFirst of the
	// class name; for these single-word classes that equals the lowercase form.
	// Meta-principles carry the invariant IRI segment, so they surface as
	// invariant:meta.* related ids (the node is dual-typed).
	case "component":
		return "component:" + idPart, true
	case "boundary":
		return "boundary:" + idPart, true
	case "contract":
		return "contract:" + idPart, true
	case "decision":
		return "decision:" + idPart, true
	case "evidence":
		return "evidence:" + idPart, true
	case "proofObligation":
		return "proof_obligation:" + idPart, true
	case "proofSlot":
		return "proof_slot:" + idPart, true
	// Design-pattern awareness. The reverse aw:relatedPattern edge makes these
	// surface when resolving an invariant/component/boundary/contract.
	case "designPattern":
		return "design_pattern:" + idPart, true
	case "implementationPattern":
		return "implementation_pattern:" + idPart, true
	case "patternMisuse":
		return "pattern_misuse:" + idPart, true
	default:
		return "", false
	}
}

func awarenessIDFromIRI(iri string) (string, bool) {
	if !strings.HasPrefix(iri, rdf.AwNS) {
		return "", false
	}
	rest := strings.TrimPrefix(iri, rdf.AwNS)
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 || slash+1 >= len(rest) {
		return "", false
	}
	return rest[slash+1:], true
}

func applyNodeFact(node *awarenesspb.KnowledgeNode, t store.Triple) {
	switch t.Predicate {
	case rdf.PropLabel:
		if !t.ObjectIsIRI && node.Label == "" {
			node.Label = t.Object
		}
	case rdf.PropSeverity:
		if !t.ObjectIsIRI && node.Severity == "" {
			node.Severity = t.Object
		}
	case rdf.PropStatus:
		if !t.ObjectIsIRI && node.Status == "" {
			node.Status = t.Object
		}
	case rdf.PropComment:
		if !t.ObjectIsIRI && node.Description == "" {
			node.Description = t.Object
		}
	case rdf.PropUmlKind:
		if !t.ObjectIsIRI && node.UmlKind == "" {
			node.UmlKind = t.Object
		}
	case rdf.PropUmlStereotype:
		if !t.ObjectIsIRI && node.UmlStereotype == "" {
			node.UmlStereotype = t.Object
		}
	case rdf.PropUmlView:
		if !t.ObjectIsIRI && node.UmlView == "" {
			node.UmlView = t.Object
		}
	}
	if t.ObjectIsIRI {
		if rel, ok := awarenessRelatedID(t.Object); ok {
			node.RelatedIds = appendUniqueCapped(node.RelatedIds, rel, maxResolveRelatedIDs)
		}
		return
	}
	// Literal-valued pattern rules (an impl/design pattern's requiresCall,
	// mustFollow, … ) are rules-about-code, not edges — so they never show as
	// graph links. Surface them as facts so a pattern node reads as governed,
	// not bare. Curated/prose predicates are handled above and excluded here.
	if lbl, ok := patternRuleLabel[relationShortName(t.Predicate)]; ok {
		node.Facts = appendFactCapped(node.Facts, lbl, t.Object, maxResolveFacts)
	}
}

// patternRuleLabel maps a pattern's literal-rule predicates to a human label.
// Only these become facts — invariants' long prose (title/summary/enforcement)
// is deliberately not chipped.
var patternRuleLabel = map[string]string{
	"requiresCall":      "requires call",
	"forbidsCall":       "forbids call",
	"mustFollow":        "must follow",
	"forbiddenShortcut": "forbidden shortcut",
	"activationTrigger": "trigger",
	"referenceFile":     "reference",
	"requiresPattern":   "requires pattern",
}
