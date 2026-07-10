// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=server.reference_sites
// @awareness file_role=grpc_rpc_handler
package main

import (
	"context"
	"sort"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"github.com/globulario/awareness-graph/golang/rdf"
)

// ReferenceSites returns, for each requested code-symbol id, the OTHER code
// symbols that reference it (inbound aw:references edges). This is the
// completeness primitive behind the gate's sibling-site check: given the
// symbols a diff touches, it answers "N sites share this reference; which did
// the change miss?".
//
// It reads INBOUND edges directly (DescribeInbound), so it is directed and
// complete — a target with many OUTGOING references never crowds out its
// referencing sites the way a bidirectional related-walk would. External
// targets ("external:<name>") have no in-repo definition and are skipped.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=server.reference_sites
// @awareness enforces=globular.awareness_graph:invariant.awareness.store_unavailable_explicit
// @awareness tested_by=golang/server/reference_sites_test.go:TestReferenceSites
// @awareness risk=low
func (s *server) ReferenceSites(ctx context.Context, req *awarenesspb.ReferenceSitesRequest) (*awarenesspb.ReferenceSitesResponse, error) {
	if s.store == nil {
		return nil, status.Error(codes.Unavailable, "store is unavailable")
	}
	if err := s.requireCurrentGraphAuthority(ctx, "reference-sites"); err != nil {
		return nil, err
	}

	resp := &awarenesspb.ReferenceSitesResponse{}
	seen := map[string]bool{}
	for _, raw := range req.GetSymbolIds() {
		id := strings.TrimSpace(raw)
		// Skip empties, dups, and externals (no in-repo definition to be
		// complete about — flagging "you touched M of 900 fmt.Sprintf sites"
		// is noise, not a convention).
		if id == "" || seen[id] || strings.HasPrefix(id, "external:") {
			continue
		}
		seen[id] = true

		sites, err := s.referencingSites(ctx, id)
		if err != nil {
			return nil, err
		}
		resp.Families = append(resp.Families, &awarenesspb.ReferenceFamily{
			SymbolId: id,
			SiteIds:  sites,
		})
	}
	resp.Authority = s.graphAuthority(ctx)
	return resp, nil
}

// referencingSites returns the code-symbol ids whose aw:references edge points
// at symbolID. The target itself is excluded (a self-reference is not a sibling
// site). Result is sorted for determinism.
func (s *server) referencingSites(ctx context.Context, symbolID string) ([]string, error) {
	iri := mintedIRI(rdf.ClassCodeSymbol, symbolID)
	inbound, err := s.store.DescribeInbound(ctx, iri)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "backend query failed: %v", err)
	}
	out := make([]string, 0, len(inbound))
	seen := map[string]bool{}
	for _, t := range inbound {
		if t.Predicate != rdf.PropReferences {
			continue
		}
		siteID, ok := codeSymbolIDFromIRI(t.Subject)
		if !ok || siteID == symbolID || seen[siteID] {
			continue
		}
		seen[siteID] = true
		out = append(out, siteID)
	}
	sort.Strings(out)
	return out, nil
}

// codeSymbolIDFromIRI turns a CodeSymbol IRI back into its raw "file:symbol" id.
// awarenessRelatedID yields a class-qualified, still-encoded id; strip the class
// prefix and decode the path so callers get the same ids Impact returns.
func codeSymbolIDFromIRI(iri string) (string, bool) {
	qualified, ok := awarenessRelatedID(iri)
	if !ok {
		return "", false
	}
	const prefix = "code_symbol:"
	if !strings.HasPrefix(qualified, prefix) {
		return "", false
	}
	return rdf.DecodeIRIPath(strings.TrimPrefix(qualified, prefix)), true
}
