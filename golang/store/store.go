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

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

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

// ValidateQueryIRI is the ONE shared lexical validator for node IRIs that reach a SPARQL query.
// It accepts canonical project IRI forms (aw:contract/example, invariant:example,
// urn:sensei:example, https://globular.io/awareness#...) and rejects anything that could
// terminate or escape a SPARQL <...> IRIREF token, look like a filesystem path, or smuggle
// whitespace/control characters. Callers apply it at the RPC boundary; store implementations
// apply it AGAIN before interpolation as defense in depth — an unvalidated caller value must
// never reach SPARQL text.
func ValidateQueryIRI(iri string) error {
	if iri == "" {
		return fmt.Errorf("iri is empty")
	}
	if len(iri) > 2048 {
		return fmt.Errorf("iri exceeds the maximum length")
	}
	if iri != strings.TrimSpace(iri) {
		return fmt.Errorf("iri is padded")
	}
	for _, r := range iri {
		// ALL Unicode whitespace and control characters (C0, C1 incl. U+0085/U+009F, U+00A0,
		// and every other space separator) are forbidden — SPARQL IRIREF admits none of them.
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("iri contains whitespace or a control character")
		}
		switch r {
		case '<', '>', '{', '}', '"', '\'', '`', '\\', '|', '^':
			return fmt.Errorf("iri contains a character forbidden in a SPARQL IRIREF")
		}
	}
	// Filesystem-path shapes are never node IRIs.
	if strings.HasPrefix(iri, "/") {
		return fmt.Errorf("iri is an absolute filesystem path")
	}
	// A scheme is required: ALPHA *(ALPHA / DIGIT / "+" / "-" / ".") ":".
	colon := strings.IndexByte(iri, ':')
	if colon <= 0 {
		return fmt.Errorf("iri has no scheme")
	}
	scheme := iri[:colon]
	for i := 0; i < len(scheme); i++ {
		c := scheme[i]
		alpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		digit := c >= '0' && c <= '9'
		if i == 0 && !alpha {
			return fmt.Errorf("iri scheme must start with a letter")
		}
		if !alpha && !digit && c != '+' && c != '-' && c != '.' {
			return fmt.Errorf("iri scheme contains an invalid character")
		}
	}
	// A single-letter scheme followed by a slash is a Windows drive path, not an IRI.
	if len(scheme) == 1 && colon+1 < len(iri) && iri[colon+1] == '/' {
		return fmt.Errorf("iri is a drive-letter filesystem path")
	}
	if colon == len(iri)-1 {
		return fmt.Errorf("iri has an empty body")
	}
	return nil
}
