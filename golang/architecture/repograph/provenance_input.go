// SPDX-License-Identifier: Apache-2.0

package repograph

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"

	"github.com/globulario/sensei/golang/architecture/graphbuild"
	"github.com/globulario/sensei/golang/seedmeta"
)

// VerifyProvenance independently reloads the persisted repository graph and proves
// the supplemental provenance chain is present (and conflict-free). It is the
// reload primitive the promotion transaction reuses at commit time.
func VerifyProvenance(ctx context.Context, root string, p *PromotionProvenance) error {
	if p == nil {
		return nil
	}
	return proveChain(ctx, filepath.Join(root, filepath.FromSlash(GraphRelPath)), p)
}

// ProvenanceEdge is one directed graph edge (all IRIs) that a supplemental
// provenance input must contribute to the persisted graph.
type ProvenanceEdge struct {
	Subject   string
	Predicate string
	Object    string
}

// PromotionProvenance is a TYPED, digest-bound supplemental graph input. It is
// deliberately narrow — repograph never accepts arbitrary RDF bytes. The owner
// supplies the canonical provenance triples, the expected stamped semantic
// digest, and the exact expected chain edges; repograph re-derives the digest,
// binds it into the build-input identity, and independently proves every edge is
// present (and free of conflicting promotion links) after reload.
type PromotionProvenance struct {
	ID                                string // stable supplemental id, e.g. "promotion.<lineage>"
	Version                           string
	NTriples                          []byte // canonical (unstamped) provenance triples
	ExpectedGraphSemanticDigestSHA256 string // expected marker digest of the stamped provenance
	ExpectedEdges                     []ProvenanceEdge
	// ConflictGuardSubjectPredicate constrains an edge that must be unique: the
	// (Subject,Predicate) may point only at the listed Object (e.g. the governed
	// node's promotedVia edge). Empty entries are ignored.
	ConflictGuards []ProvenanceEdge
}

// stampedSupplemental verifies and stamps the provenance into a graphbuild
// supplemental graph, re-deriving the marker digest and requiring it to equal the
// caller's expected digest (reject malformed / mismatched).
func (p *PromotionProvenance) stampedSupplemental() (graphbuild.SupplementalGraph, string, error) {
	if len(p.NTriples) == 0 {
		return graphbuild.SupplementalGraph{}, "", &InvalidRequestError{Detail: "supplemental provenance has empty triples"}
	}
	// AppendMarker canonicalizes (strips any marker, dedups blanks, sorts) and
	// stamps, so identity is order- and whitespace-independent.
	stamped, marker := seedmeta.AppendMarker(p.NTriples)
	if p.ExpectedGraphSemanticDigestSHA256 != "" && marker.Digest != p.ExpectedGraphSemanticDigestSHA256 {
		return graphbuild.SupplementalGraph{}, "", &InvalidRequestError{
			Detail: fmt.Sprintf("supplemental provenance digest mismatch (marker %s, expected %s)", marker.Digest, p.ExpectedGraphSemanticDigestSHA256),
		}
	}
	// Parse validity is a hard requirement — reject malformed triples.
	if _, err := ReadGraph(bytes.NewReader(stamped)); err != nil {
		return graphbuild.SupplementalGraph{}, "", &InvalidRequestError{Detail: "supplemental provenance is not valid N-Triples: " + err.Error()}
	}
	return graphbuild.SupplementalGraph{
		ID:                           p.ID,
		Version:                      p.Version,
		NTriples:                     stamped,
		ExpectedSemanticDigestSHA256: marker.Digest,
	}, marker.Digest, nil
}

// proveChain reopens the persisted graph and proves every expected provenance
// edge is present, and that each conflict-guarded (subject,predicate) points only
// at its expected object.
func proveChain(ctx context.Context, graphPath string, p *PromotionProvenance) error {
	g, err := LoadGraph(graphPath)
	if err != nil {
		return &ReloadVerifyError{Aspect: "provenance", Detail: err.Error()}
	}
	for _, e := range p.ExpectedEdges {
		if !hasEdge(ctx, g, e) {
			return &ReloadVerifyError{Aspect: "provenance", Detail: fmt.Sprintf("missing provenance edge %s -%s-> %s", e.Subject, e.Predicate, e.Object)}
		}
	}
	for _, guard := range p.ConflictGuards {
		out, _ := g.Describe(ctx, guard.Subject)
		for _, tr := range out {
			if tr.Predicate == guard.Predicate && tr.ObjectIsIRI && tr.Object != guard.Object {
				return &ReloadVerifyError{Aspect: "provenance", Detail: fmt.Sprintf("conflicting %s edge from %s: %s", guard.Predicate, guard.Subject, tr.Object)}
			}
		}
	}
	return nil
}

func hasEdge(ctx context.Context, g *Graph, e ProvenanceEdge) bool {
	out, _ := g.Describe(ctx, e.Subject)
	for _, tr := range out {
		if tr.Predicate == e.Predicate && tr.ObjectIsIRI && tr.Object == e.Object {
			return true
		}
	}
	return false
}
