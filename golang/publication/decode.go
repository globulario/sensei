// SPDX-License-Identifier: AGPL-3.0-only

package publication

import (
	"fmt"
	"sort"
	"strings"
)

// DecodeStoredReceipt turns stored RDF terms into a verified Receipt, or
// refuses.
//
// ONE OPERATION, IN ONE ORDER. The previous pipeline simplified terms, then ran
// ad-hoc checks, then built a struct, then verified an identity computed from
// that struct. Every defect this package has had came from that shape: a fact
// present in storage was discarded, normalised or reinterpreted somewhere
// before the identity was computed, so the digest attested a value the store
// did not hold.
//
// The rule this enforces is: no authority-bearing fact present in storage may
// be discarded, normalised, reinterpreted or projected unless the selected
// schema explicitly defines and authenticates it.
//
// The version is determined FROM RAW STORAGE before a schema is selected,
// because the schema decides what is legal and cannot be chosen by a value the
// schema has not yet validated.
func DecodeStoredReceipt(subject string, terms []RDFStatement) (Receipt, error) {
	byPred := map[string][]Term{}
	for _, st := range terms {
		byPred[st.Predicate] = append(byPred[st.Predicate], st.Object)
	}

	version, err := versionFromStorage(byPred)
	if err != nil {
		return Receipt{}, err
	}
	schema, ok := SchemaFor(version)
	if !ok {
		return Receipt{}, fmt.Errorf("receipt version %q is not one this reader defines", version)
	}

	// Unknown publication predicates are REFUSED, never ignored. "I do not
	// understand it, therefore it does not count" is how a present fact becomes
	// an unauthenticated one.
	var undefined []string
	for pred := range byPred {
		if !strings.HasPrefix(pred, PublicationFieldPrefix) {
			continue // rdf:type, labels: metadata, not attested content
		}
		if _, defined := schema.Fields[pred]; !defined {
			undefined = append(undefined, pred)
		}
	}
	if len(undefined) != 0 {
		sort.Strings(undefined)
		return Receipt{}, fmt.Errorf(
			"schema %s does not define publication field(s) %v, so they are present but unauthenticated",
			version, undefined)
	}

	for _, pred := range sortedFieldNames(schema.Fields) {
		spec := schema.Fields[pred]
		got := byPred[pred]
		if len(got) < spec.MinCount {
			return Receipt{}, fmt.Errorf("schema %s requires %s at least %d time(s), found %d",
				version, pred, spec.MinCount, len(got))
		}
		if len(got) > spec.MaxCount {
			if spec.MaxCount == 0 {
				return Receipt{}, fmt.Errorf("schema %s forbids %s, but it is present", version, pred)
			}
			return Receipt{}, fmt.Errorf("schema %s allows %s at most %d time(s), found %d",
				version, pred, spec.MaxCount, len(got))
		}
		for _, term := range got {
			if term.Kind != spec.TermKind {
				return Receipt{}, fmt.Errorf("%s is stored as %s, but schema %s requires %s",
					pred, term.Kind, version, spec.TermKind)
			}
			if term.Datatype != spec.Datatype {
				return Receipt{}, fmt.Errorf("%s carries datatype %q, but schema %s requires %q",
					pred, term.Datatype, version, spec.Datatype)
			}
			if term.Language != "" {
				return Receipt{}, fmt.Errorf("%s carries language tag %q, which schema %s does not allow",
					pred, term.Language, version)
			}
			if spec.ValidateLexical != nil {
				if err := spec.ValidateLexical(term.Value); err != nil {
					return Receipt{}, fmt.Errorf("%s value %q is invalid: %w", pred, term.Value, err)
				}
			}
		}
	}

	r := Receipt{Version: version}
	single := func(pred string) string {
		if v := byPred[pred]; len(v) == 1 {
			return v[0].Value
		}
		return ""
	}
	r.Domain = single(pDomain)
	r.Revision = single(pRevision)
	r.Tree = single(pTree)
	r.State = SourceState(single(pState))
	r.SourceDigest = single(pSourceDig)
	r.SourcePath = single(pPath)
	r.SourceRoot = single(pRoot)
	if version == ReceiptV1 {
		// v1 receipts have no stated version; carrying one in the struct would
		// change the identity algorithm the historical record was written under.
		r.Version = ""
	}

	if schema.ValidateCrossFields != nil {
		if err := schema.ValidateCrossFields(r); err != nil {
			return Receipt{}, err
		}
	}
	if subject != "" && r.IRI() != subject {
		return Receipt{}, fmt.Errorf(
			"the receipt stored as %s recomputes to %s: its fields have changed since it was published",
			subject, r.IRI())
	}
	return r, nil
}

// versionFromStorage reads the version predicate before any schema is selected.
func versionFromStorage(byPred map[string][]Term) (ReceiptVersion, error) {
	got := byPred[pVersion]
	switch len(got) {
	case 0:
		// No stated version is v1: that is what every receipt published before
		// versioning existed actually is. A migration fact, not a guess.
		return ReceiptV1, nil
	case 1:
		v := ReceiptVersion(got[0].Value)
		if !v.Valid() {
			return "", fmt.Errorf("receipt version %q is not one this reader defines", got[0].Value)
		}
		return v, nil
	default:
		return "", fmt.Errorf("the receipt states %d versions, so its schema is ambiguous", len(got))
	}
}

func sortedFieldNames(m map[string]FieldSpec) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ValidateForPublication refuses to EMIT a receipt this reader would later
// refuse.
//
// The publisher used to build a Receipt and render it straight to triples, so
// Sensei could produce a record its own verifier rejects. Emission and
// verification now share one schema.
func ValidateForPublication(r Receipt) error {
	terms, err := statementsOf(r)
	if err != nil {
		return err
	}
	if _, err := DecodeStoredReceipt(r.IRI(), terms); err != nil {
		return fmt.Errorf("this receipt would be refused by its own reader: %w", err)
	}
	return nil
}

// statementsOf renders a receipt as the terms a store would return for it.
func statementsOf(r Receipt) ([]RDFStatement, error) {
	var out []RDFStatement
	for _, line := range strings.Split(string(r.Triples()), "\n") {
		if !strings.HasPrefix(line, "<"+r.IRI()+">") {
			continue
		}
		_, pred, obj, ok := splitTriple(line)
		if !ok {
			return nil, fmt.Errorf("a rendered receipt line is unparseable: %q", line)
		}
		if !strings.HasPrefix(pred, PublicationFieldPrefix) {
			continue
		}
		out = append(out, RDFStatement{Predicate: pred, Object: Term{Kind: TermLiteral, Value: obj}})
	}
	return out, nil
}
