// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/rdf"
)

type ArchitectureClaimReferenceError struct {
	ClaimID string
	Reason  string
}

func (e ArchitectureClaimReferenceError) Error() string {
	if e.ClaimID == "" {
		return "architecture claim reference error: " + e.Reason
	}
	return fmt.Sprintf("architecture claim %s: %s", e.ClaimID, e.Reason)
}

func ValidateArchitectureClaimReferences(r io.Reader) ([]ArchitectureClaimReferenceError, error) {
	type claimState struct {
		props              map[string][]string
		supportingEvidence []string
		refutingEvidence   []string
		dependsOn          []string
		conflictsWith      []string
		supersededBy       []string
		premiseFacts       []string
	}
	claims := map[string]*claimState{}
	definedEvidence := map[string]bool{}
	definedClaims := map[string]bool{}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.HasSuffix(line, ".") {
			continue
		}
		toks := tokenize(strings.TrimSpace(strings.TrimSuffix(line, ".")))
		if len(toks) != 3 {
			continue
		}
		subj, pred, obj := toks[0], stripAngleBrackets(toks[1]), toks[2]
		subjIRI := stripAngleBrackets(subj)
		objIRI := stripAngleBrackets(obj)
		if pred == rdf.PropType {
			switch objIRI {
			case rdf.ClassArchitectureClaim:
				id := extractIDFromIRI(subjIRI, rdf.ClassArchitectureClaim)
				definedClaims[subjIRI] = true
				if claims[subjIRI] == nil {
					claims[subjIRI] = &claimState{props: map[string][]string{}}
				}
				claims[subjIRI].props["id"] = []string{id}
			case rdf.ClassEvidence:
				definedEvidence[subjIRI] = false
			}
			continue
		}
		if pred == rdf.PropAuthoredIn && matchesClassSubject(subjIRI, rdf.ClassEvidence) {
			definedEvidence[subjIRI] = true
		}
		if !matchesClassSubject(subjIRI, rdf.ClassArchitectureClaim) {
			continue
		}
		st := claims[subjIRI]
		if st == nil {
			st = &claimState{props: map[string][]string{}}
			claims[subjIRI] = st
		}
		st.props[pred] = append(st.props[pred], obj)
		switch pred {
		case rdf.PropSupportedByEvidence:
			st.supportingEvidence = append(st.supportingEvidence, objIRI)
		case rdf.PropRefutedByEvidence:
			st.refutingEvidence = append(st.refutingEvidence, objIRI)
		case rdf.PropDependsOnClaim:
			st.dependsOn = append(st.dependsOn, objIRI)
		case rdf.PropConflictsWith:
			st.conflictsWith = append(st.conflictsWith, objIRI)
		case rdf.PropSupersededBy:
			st.supersededBy = append(st.supersededBy, objIRI)
		case rdf.PropDerivedFromFact:
			st.premiseFacts = append(st.premiseFacts, obj)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	var errs []ArchitectureClaimReferenceError
	for iri, st := range claims {
		id := extractIDFromIRI(iri, rdf.ClassArchitectureClaim)
		require := func(prop, label string) {
			if len(st.props[prop]) == 0 {
				errs = append(errs, ArchitectureClaimReferenceError{id, "missing required property " + label})
			}
		}
		require(rdf.PropClaimSubject, "claimSubject")
		require(rdf.PropClaimPredicate, "claimPredicate")
		require(rdf.PropClaimObject, "claimObject")
		require(rdf.PropArchitecturalPlane, "architecturalPlane")
		require(rdf.PropAssertionOrigin, "assertionOrigin")
		require(rdf.PropEpistemicStatus, "epistemicStatus")
		require(rdf.PropPromotionStatus, "promotionStatus")
		require(rdf.PropSourceKind, "sourceKind")
		require(rdf.PropHumanReviewRequired, "humanReviewRequired")
		if !hasLiteral(st.props[rdf.PropPromotionStatus], "candidate") {
			errs = append(errs, ArchitectureClaimReferenceError{id, "promotionStatus must be candidate"})
		}
		if !hasLiteral(st.props[rdf.PropAssertionOrigin], "derived") {
			errs = append(errs, ArchitectureClaimReferenceError{id, "assertionOrigin must be derived"})
		}
		if !hasLiteral(st.props[rdf.PropSourceKind], "generated_candidate") {
			errs = append(errs, ArchitectureClaimReferenceError{id, "sourceKind must be generated_candidate"})
		}
		if !hasLiteral(st.props[rdf.PropHumanReviewRequired], "true") {
			errs = append(errs, ArchitectureClaimReferenceError{id, "humanReviewRequired must be true"})
		}
		if len(st.premiseFacts)+len(st.supportingEvidence)+len(st.refutingEvidence)+len(st.dependsOn) == 0 {
			errs = append(errs, ArchitectureClaimReferenceError{id, "claim requires premise fact, evidence, or claim dependency"})
		}
		for _, ev := range append(append([]string{}, st.supportingEvidence...), st.refutingEvidence...) {
			if !definedEvidence[ev] {
				errs = append(errs, ArchitectureClaimReferenceError{id, "evidence reference is not defined: " + extractIDFromIRI(ev, rdf.ClassEvidence)})
			}
		}
		for _, dep := range append(append([]string{}, st.dependsOn...), append(st.conflictsWith, st.supersededBy...)...) {
			if !definedClaims[dep] {
				errs = append(errs, ArchitectureClaimReferenceError{id, "claim reference is not defined: " + extractIDFromIRI(dep, rdf.ClassArchitectureClaim)})
			}
		}
	}
	sort.Slice(errs, func(i, j int) bool {
		if errs[i].ClaimID != errs[j].ClaimID {
			return errs[i].ClaimID < errs[j].ClaimID
		}
		return errs[i].Reason < errs[j].Reason
	})
	return errs, nil
}

func hasLiteral(values []string, want string) bool {
	quoted := rdf.Lit(want)
	for _, v := range values {
		if v == quoted {
			return true
		}
	}
	return false
}
