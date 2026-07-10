// SPDX-License-Identifier: AGPL-3.0-only

// Package store defines the RDF backend abstraction the awareness-graph
// server depends on.
//
// Scope (Phase 2 step 4 — backend wire-up only):
//
//   - Store: a small interface describing connection lifecycle + health.
//
//   - No SPARQL query primitives yet. They land in the next phase step
//     when Resolve is wired and a concrete caller drives the shape — at
//     that point we know whether we need Query (SELECT bindings),
//     Describe (CONSTRUCT subject), Update (SPARQL Update), or all
//     three. Adding the methods now, without a consumer to pin their
//     return shapes, would force premature API decisions.
//
// The package is deliberately thin. Concrete backends (Oxigraph today,
// possibly others later) live in sub-packages like store/oxigraph and
// import this package. The reverse arrow never happens — store/ stays
// free of any backend-specific imports.
package store

import "context"

// Triple is a single RDF statement where subject is implied by Describe.
// The object is either an IRI (when ObjectIsIRI=true) or a literal.
type Triple struct {
	Predicate   string
	Object      string
	ObjectIsIRI bool
}

// InboundTriple is a single RDF statement where the OBJECT is implied by
// DescribeInbound (the queried IRI). Subject is the node pointing at it. The
// subject is always an IRI — literal subjects do not exist in RDF.
type InboundTriple struct {
	Subject   string
	Predicate string
}

// ImpactFact is one row from the fixed direct-impact query rooted at a source file.
type ImpactFact struct {
	NodeIRI     string
	TypeIRI     string
	Predicate   string
	Object      string
	ObjectIsIRI bool
}

// Store is the abstract handle to the RDF backend.
//
// Implementations must be safe for concurrent use; the server holds one
// Store value and uses it across all RPC goroutines.
type Store interface {
	// Close releases backend resources. Idempotent — safe to call
	// multiple times. Implementations that hold no resources may return
	// nil unconditionally; the method exists so a future migration
	// (e.g. to a native binding with a connection pool) does not have
	// to change the interface.
	Close() error

	// Health verifies the backend is reachable and responsive. Returns
	// nil on success, a wrapped error on failure. Implementations
	// SHOULD use ctx for cancellation; the server passes a bounded
	// timeout context for startup checks.
	//
	// Health is intentionally cheap. It is called once at startup and
	// MAY be called periodically by future readiness probes; it must
	// not perform expensive queries or open new connections beyond
	// what's needed to verify the endpoint answers.
	Health(ctx context.Context) error

	// Describe returns all outgoing triples for one subject IRI.
	//
	// The subject itself is not repeated in each row because the caller
	// already knows it. If the subject does not exist, implementations
	// return an empty slice and nil error.
	Describe(ctx context.Context, iri string) ([]Triple, error)

	// DescribeInbound returns all triples whose OBJECT is the given IRI — the
	// inverse of Describe. It lets callers traverse a relationship from the
	// pointed-at side: a Test that an Invariant `requiresTest`, or a SourceFile
	// that a CodeSymbol is `definedInFile`. Without it, nodes that are primarily
	// edge objects (tests, files) appear unlinked even though edges exist.
	// Returns an empty slice (not an error) when nothing points at the IRI.
	DescribeInbound(ctx context.Context, iri string) ([]InboundTriple, error)

	// ImpactForFile returns direct node facts for:
	//   <sourceFileIRI> aw:implements ?node
	// including each node's rdf:type and direct facts.
	ImpactForFile(ctx context.Context, sourceFileIRI string) ([]ImpactFact, error)

	// ClassFacts returns direct facts for nodes with rdf:type = classIRI.
	// Implementations must enforce a safe upper bound on limit.
	ClassFacts(ctx context.Context, classIRI string, limit int) ([]ImpactFact, error)

	// CodeSymbolFacts returns all triples for CodeSymbol nodes anchored to
	// the given source-file IRI via aw:definedInFile. NodeIRI in each fact
	// is the symbol IRI; TypeIRI is always the CodeSymbol class IRI.
	// Returns an empty slice (not an error) when no symbols are found.
	CodeSymbolFacts(ctx context.Context, sourceFileIRI string) ([]ImpactFact, error)

	// RenderingGroupsForFile returns rendering groups that the given
	// source file belongs to (via aw:memberOfGroup edges).
	RenderingGroupsForFile(ctx context.Context, sourceFileIRI string) ([]RenderingGroupInfo, error)

	// DetectFacts returns all direct facts for every node that carries a
	// detect block (aw:detectForbiddenPattern OR aw:detectRequiredPattern).
	// Each row carries the node IRI, its rdf:type, and one predicate/object —
	// the same flat shape as ImpactForFile — so callers can reify the detect
	// patterns plus the node's domain/provenance facts in one pass. Returns an
	// empty slice (not an error) when no detect rules exist.
	DetectFacts(ctx context.Context) ([]ImpactFact, error)
}

// RenderingGroupInfo holds one rendering group with its label and contract.
type RenderingGroupInfo struct {
	IRI      string
	ID       string
	Label    string
	Contract string
}
