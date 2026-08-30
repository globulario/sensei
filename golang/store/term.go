// SPDX-License-Identifier: AGPL-3.0-only

package store

// TermKind is the RDF term kind of a stored object.
//
// Triple.ObjectIsIRI answers only "is this an IRI", which collapses plain
// literals, typed literals, language-tagged literals and blank nodes into one
// value. That is adequate for browsing and inadequate for verification.
type TermKind string

const (
	TermIRI     TermKind = "IRI"
	TermLiteral TermKind = "LITERAL"
	TermBlank   TermKind = "BLANK"
	// TermUnknown is what a reader reports when the transport did not tell it
	// which kind was stored. It must never satisfy a schema.
	TermUnknown TermKind = "UNKNOWN"
)

// Term is a stored RDF object, losslessly.
type Term struct {
	Kind     TermKind
	Value    string
	Datatype string
	Language string
}

// Statement is one predicate/object pair describing a subject, preserving the
// object's term kind, datatype and language.
type Statement struct {
	Predicate string
	Object    Term
}
