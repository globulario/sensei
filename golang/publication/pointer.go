// SPDX-License-Identifier: AGPL-3.0-only

package publication

import (
	"fmt"
	"sort"
	"strings"
)

// PointerOutcome is how a current-publication lookup ended.
type PointerOutcome int

const (
	// PointerNone: no pointer subject exists at all.
	PointerNone PointerOutcome = iota
	// PointerBroken: a pointer exists and cannot be read as naming exactly one
	// receipt.
	PointerBroken
	// PointerOK: exactly one well-formed target.
	PointerOK
)

// pointer field predicates, and their closed schema.
var pointerFields = map[string]FieldSpec{
	pCurrent: {MinCount: 1, MaxCount: 1, TermKind: TermIRI, ValidateLexical: nonEmpty},
	pRepo:    {MinCount: 1, MaxCount: 1, TermKind: TermLiteral, ValidateLexical: domainName},
	pDomain:  {MinCount: 1, MaxCount: 1, TermKind: TermLiteral, ValidateLexical: domainName},
}

// DecodePointer reads a current-publication pointer under the SAME law as the
// receipt body.
//
// The pointer was the last thing outside the lossless rule: it was read through
// the simplified transport, which drops empty lexical objects, so a
// currentPublication "" could vanish and the missing edge be reported as
// ABSENT -- corrupt state becoming "never published", which is the one answer a
// start gate may treat as benign.
//
// It also has a SCHEMA rather than a single check. The writer emits type, repo,
// domain and target; validating only the target trusted the rest, and the
// pointer's own domain metadata is authority-bearing: a pointer whose repo or
// domain disagrees with the domain being asked about is not a pointer with a
// usable target.
func DecodePointer(domain string, terms []RDFStatement) (target string, outcome PointerOutcome, err error) {
	byPred := map[string][]Term{}
	present := false
	for _, st := range terms {
		if strings.HasPrefix(st.Predicate, PublicationFieldPrefix) || st.Predicate == pCurrent || st.Predicate == pRepo {
			present = true
		}
		byPred[st.Predicate] = append(byPred[st.Predicate], st.Object)
	}
	if len(terms) == 0 || !present {
		return "", PointerNone, nil
	}

	for _, pred := range sortedFieldNames(pointerFields) {
		spec := pointerFields[pred]
		got := byPred[pred]
		if len(got) < spec.MinCount {
			return "", PointerBroken, fmt.Errorf(
				"the pointer for %q states %s %d time(s); exactly %d is required",
				domain, shortPredicate(pred), len(got), spec.MinCount)
		}
		if len(got) > spec.MaxCount {
			return "", PointerBroken, fmt.Errorf(
				"the pointer for %q states %s %d times; exactly %d is required",
				domain, shortPredicate(pred), len(got), spec.MaxCount)
		}
		for _, term := range got {
			if term.Kind != spec.TermKind {
				return "", PointerBroken, fmt.Errorf(
					"the pointer's %s is stored as %s, but %s is required",
					shortPredicate(pred), term.Kind, spec.TermKind)
			}
			if term.Language != "" || term.Datatype != "" {
				return "", PointerBroken, fmt.Errorf(
					"the pointer's %s carries a datatype or language tag, which its schema does not allow",
					shortPredicate(pred))
			}
			if spec.ValidateLexical != nil {
				if err := spec.ValidateLexical(term.Value); err != nil {
					return "", PointerBroken, fmt.Errorf(
						"the pointer's %s value %q is invalid: %w", shortPredicate(pred), term.Value, err)
				}
			}
		}
	}

	// The pointer's own metadata must agree with the domain being asked about.
	for _, pred := range []string{pRepo, pDomain} {
		if v := byPred[pred][0].Value; v != domain {
			return "", PointerBroken, fmt.Errorf(
				"the pointer resolved for %q declares %s %q: it describes a different domain",
				domain, shortPredicate(pred), v)
		}
	}
	// Unknown publication predicates on the pointer are refused for the same
	// reason they are on the receipt.
	var undefined []string
	for pred := range byPred {
		if !strings.HasPrefix(pred, PublicationFieldPrefix) {
			continue
		}
		if _, ok := pointerFields[pred]; !ok {
			undefined = append(undefined, pred)
		}
	}
	if len(undefined) != 0 {
		sort.Strings(undefined)
		return "", PointerBroken, fmt.Errorf(
			"the pointer carries publication field(s) %v that its schema does not define", undefined)
	}
	return byPred[pCurrent][0].Value, PointerOK, nil
}

func shortPredicate(p string) string {
	if i := strings.LastIndex(p, "#"); i >= 0 {
		return p[i+1:]
	}
	return p
}
