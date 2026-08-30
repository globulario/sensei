// SPDX-License-Identifier: AGPL-3.0-only

package publication

import (
	"fmt"
	"sort"
	"strings"
)

// TermKind is the RDF term kind of a stored object.
//
// It exists because "not an IRI" is not a value. Plain literals, typed
// literals, language-tagged literals and blank nodes were all collapsed into
// one boolean, so a validator could not prove the term it hashed was the term
// the store held.
type TermKind string

const (
	TermIRI     TermKind = "IRI"
	TermLiteral TermKind = "LITERAL"
	TermBlank   TermKind = "BLANK"
	// TermUnknown is what a reader must report when the transport could not
	// tell it which kind was stored. It never satisfies a schema.
	TermUnknown TermKind = "UNKNOWN"
)

// Term is a stored RDF object, losslessly.
type Term struct {
	Kind     TermKind
	Value    string
	Datatype string
	Language string
}

func (t Term) String() string {
	switch {
	case t.Kind != TermLiteral:
		return fmt.Sprintf("%s<%s>", t.Kind, t.Value)
	case t.Language != "":
		return fmt.Sprintf("%q@%s", t.Value, t.Language)
	case t.Datatype != "":
		return fmt.Sprintf("%q^^<%s>", t.Value, t.Datatype)
	}
	return fmt.Sprintf("%q", t.Value)
}

// RDFStatement is one predicate/object pair describing a subject.
type RDFStatement struct {
	Predicate string
	Object    Term
}

// FieldSpec is the complete legal shape of ONE publication predicate under one
// receipt version.
//
// Everything a validator needs is stated here rather than spread across ad-hoc
// checks, so "which states are legal" is a value that can be enumerated,
// generated from, and reasoned about -- instead of the residue of whichever
// counterexamples were reported.
type FieldSpec struct {
	// MinCount/MaxCount are the cardinality. MinCount 1 makes the field
	// required; MaxCount 0 FORBIDS it, which is how v1 forbids the version
	// predicate it cannot authenticate.
	MinCount int
	MaxCount int
	// TermKind is the exact RDF term kind the field must be stored as.
	TermKind TermKind
	// Datatype, when set, is the only datatype the literal may carry. Empty
	// means a plain literal with NO datatype and NO language.
	Datatype string
	// ValidateLexical rejects a syntactically wrong value.
	ValidateLexical func(string) error
	// IdentityBearing records whether this version's identity algorithm hashes
	// the field. A field that is legal but NOT identity-bearing may be stored
	// and must never be projected as trusted evidence.
	IdentityBearing bool
}

// ReceiptSchema is the complete legal state space of a receipt version.
type ReceiptSchema struct {
	Version             ReceiptVersion
	Fields              map[string]FieldSpec
	ValidateCrossFields func(Receipt) error
}

func hexOfLength(n int) func(string) error {
	return func(v string) error {
		if len(v) != n {
			return fmt.Errorf("expected %d hex characters, got %d", n, len(v))
		}
		for _, c := range v {
			if !strings.ContainsRune("0123456789abcdef", c) {
				return fmt.Errorf("contains non-lowercase-hex character %q", c)
			}
		}
		return nil
	}
}

func nonEmpty(v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("must not be empty")
	}
	return nil
}

func oneOfStates(v string) error {
	if !SourceState(v).Valid() {
		return fmt.Errorf("%q is not a source state this schema defines", v)
	}
	return nil
}

// relativePath rejects an absolute or traversing path. The whole point of
// SourcePath is that it is durable and repository-relative; an absolute path
// would be the operational SourceRoot wearing the durable field's name.
func relativePath(v string) error {
	if err := nonEmpty(v); err != nil {
		return err
	}
	if strings.HasPrefix(v, "/") || strings.Contains(v, "..") {
		return fmt.Errorf("%q is not a repository-relative path", v)
	}
	return nil
}

// crossFieldRules is one model of the state/field relationship, shared by the
// writer and the verifier so their semantics cannot drift apart.
//
// UNKNOWN means the revision was never established, so it must be absent.
// CLEAN_EXACT and DIRTY both mean a HEAD was resolved, so it must be present.
func crossFieldRules(r Receipt) error {
	switch r.State {
	case Unknown:
		if r.Revision != "" || r.Tree != "" {
			return fmt.Errorf(
				"state UNKNOWN carries revision %q and tree %q: an unestablished revision must be absent, not stated",
				r.Revision, r.Tree)
		}
	case CleanExact, Dirty:
		if r.Revision == "" {
			return fmt.Errorf("state %s requires a source revision, which is absent", r.State)
		}
	}
	if r.Domain == "" {
		return fmt.Errorf("a receipt with no domain attests nothing")
	}
	return nil
}

var schemas = map[ReceiptVersion]ReceiptSchema{
	ReceiptV1: {
		Version: ReceiptV1,
		Fields: map[string]FieldSpec{
			pDomain:    {1, 1, TermLiteral, "", nonEmpty, true},
			pRevision:  {0, 1, TermLiteral, "", nonEmpty, true},
			pTree:      {0, 1, TermLiteral, "", nonEmpty, true},
			pState:     {1, 1, TermLiteral, "", oneOfStates, true},
			pSourceDig: {1, 1, TermLiteral, "", hexOfLength(64), true},
			// v1's operational root: legal historical baggage, NOT hashed, and
			// therefore never projected as trusted evidence. A1's receipts
			// carry it and must keep verifying; repainting history is not a
			// repair.
			pRoot: {0, 1, TermLiteral, "", nonEmpty, false},
			// v1 predates versioning, so the version predicate is FORBIDDEN:
			// an explicitly injected "v1" would be present and unhashed, which
			// is the defect this whole schema exists to remove.
			pVersion: {0, 0, TermLiteral, "", nonEmpty, false},
			pPath:    {0, 0, TermLiteral, "", relativePath, false},
		},
		ValidateCrossFields: crossFieldRules,
	},
	ReceiptV2: {
		Version: ReceiptV2,
		Fields: map[string]FieldSpec{
			pVersion:   {1, 1, TermLiteral, "", nonEmpty, true},
			pDomain:    {1, 1, TermLiteral, "", nonEmpty, true},
			pRevision:  {0, 1, TermLiteral, "", nonEmpty, true},
			pTree:      {0, 1, TermLiteral, "", nonEmpty, true},
			pState:     {1, 1, TermLiteral, "", oneOfStates, true},
			pPath:      {1, 1, TermLiteral, "", relativePath, true},
			pSourceDig: {1, 1, TermLiteral, "", hexOfLength(64), true},
			// v2 replaced the operational root; carrying one is a v1 shape in a
			// v2 record.
			pRoot: {0, 0, TermLiteral, "", nonEmpty, false},
		},
		ValidateCrossFields: crossFieldRules,
	},
}

// SchemaFor returns the schema for a version, or false when the version is not
// one this reader defines. An unknown version is never treated permissively.
func SchemaFor(v ReceiptVersion) (ReceiptSchema, bool) {
	if v == "" {
		v = ReceiptV1
	}
	s, ok := schemas[v]
	return s, ok
}

// KnownVersions lists the versions this reader defines, for generators.
func KnownVersions() []ReceiptVersion {
	out := make([]ReceiptVersion, 0, len(schemas))
	for v := range schemas {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
