// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
)

// realized_contracts.go gives the contract spine a voice. When a briefing touches
// a file/endpoint/RPC that anchors a contract, it follows the authoritative
// realizesContract edge UP to the architectural guarantee the surface must
// honor, and that contract's constraining invariant + required test — rendered
// as a repair instruction. candidateRealizesContract is surfaced SEPARATELY and
// clearly marked review-only; candidates are never presented as authority.

type spineLink struct {
	impl       string // implementation contract id
	implLabel  string // human label (e.g. "HTTP /api/save-config")
	arch       string // architectural contract id
	archLabel  string // human label / description of the guarantee
	invariants []string
	tests      []string
	provenance spineProvenance
	candidate  bool
}

type spineProvenance struct {
	evidenceID      string
	sourceKind      string
	confidence      string
	promotionStatus string
	summary         string
}

// realizedContractSpineSection extracts the contract nodes from a file's impact,
// follows their realized-contract spine, and renders the prose section + the ids
// to add to referenced_ids. Returns ("", nil) when the file anchors no contract
// (so existing briefings are unchanged).
func (s *server) realizedContractSpineSection(ctx context.Context, impact *awarenesspb.ImpactResponse) (string, []string) {
	if s.store == nil || impact == nil {
		return "", nil
	}
	var contractIRIs []string
	for _, n := range impact.GetDirectArchitecture() {
		if strings.Contains(n.GetIri(), "#contract/") {
			contractIRIs = append(contractIRIs, n.GetIri())
		}
	}
	if len(contractIRIs) == 0 {
		return "", nil
	}
	auth, cand := s.realizedContractSpine(ctx, contractIRIs)
	if len(auth) == 0 && len(cand) == 0 {
		return "", nil
	}

	var b strings.Builder
	var refs []string
	if len(auth) > 0 {
		b.WriteString("\n\nRealized architectural contracts (AUTHORITY — respect or do not claim resolution):")
		for _, l := range auth {
			implName := l.implLabel
			if implName == "" {
				implName = l.impl
			}
			fmt.Fprintf(&b, "\n- %s realizes %s", implName, l.arch)
			if l.archLabel != "" {
				fmt.Fprintf(&b, "\n  - The contract requires: %s", l.archLabel)
			}
			if len(l.invariants) > 0 {
				fmt.Fprintf(&b, "\n  - Constrained by: %s", strings.Join(l.invariants, ", "))
			}
			if len(l.tests) > 0 {
				fmt.Fprintf(&b, "\n  - Required proof: %s", strings.Join(l.tests, ", "))
			}
			if line := formatSpineProvenance(l.provenance); line != "" {
				fmt.Fprintf(&b, "\n  - Realization provenance: %s", line)
			}
			b.WriteString("\n  - Do not claim resolution if this contract is bypassed, weakened, or left untested.")
			refs = append(refs, "contract:"+l.arch)
			for _, inv := range l.invariants {
				refs = append(refs, "invariant:"+inv)
			}
			for _, t := range l.tests {
				refs = append(refs, "test:"+t)
			}
			if l.provenance.evidenceID != "" {
				refs = append(refs, "evidence:"+l.provenance.evidenceID)
			}
		}
	}
	if len(cand) > 0 {
		b.WriteString("\n\nCandidate realized contracts (REVIEW-ONLY — not authority; promote with `awg promote-realization`):")
		for _, l := range cand {
			implName := l.implLabel
			if implName == "" {
				implName = l.impl
			}
			fmt.Fprintf(&b, "\n- %s ~candidate~> %s (unverified — do not treat as a guarantee)", implName, l.arch)
			if line := formatSpineProvenance(l.provenance); line != "" {
				fmt.Fprintf(&b, "\n  - Candidate provenance: %s", line)
			}
		}
	}
	return b.String(), refs
}

// realizedContractSpine follows realizesContract / realizedByContract (authority)
// and candidateRealizesContract (review-only) from each contract IRI, gathering
// each architectural contract's invariants + required tests.
func (s *server) realizedContractSpine(ctx context.Context, contractIRIs []string) (auth, cand []spineLink) {
	seen := map[string]bool{}
	add := func(k string) bool {
		if seen[k] {
			return false
		}
		seen[k] = true
		return true
	}
	for _, iri := range dedupSortedStrings(contractIRIs) {
		triples, err := s.store.Describe(ctx, iri)
		if err != nil {
			continue
		}
		for _, t := range triples {
			if !t.ObjectIsIRI {
				continue
			}
			switch t.Predicate {
			case rdf.PropRealizesContract: // this node is the impl, object is the arch
				if add(iri + "|" + t.Object) {
					auth = append(auth, s.buildSpineLink(ctx, iri, t.Object, false))
				}
			case rdf.PropRealizedByContract: // this node is the arch, object is the impl
				if add(t.Object + "|" + iri) {
					auth = append(auth, s.buildSpineLink(ctx, t.Object, iri, false))
				}
			case rdf.PropCandidateRealizesContract: // candidate impl -> arch
				if add("c|" + iri + "|" + t.Object) {
					cand = append(cand, s.buildSpineLink(ctx, iri, t.Object, true))
				}
			}
		}
	}
	sortSpine(auth)
	sortSpine(cand)
	return auth, cand
}

func (s *server) buildSpineLink(ctx context.Context, implIRI, archIRI string, candidate bool) spineLink {
	link := spineLink{candidate: candidate}
	link.impl, _ = awarenessIDFromIRI(implIRI)
	link.arch, _ = awarenessIDFromIRI(archIRI)
	link.implLabel = s.labelOf(ctx, implIRI)
	if at, err := s.store.Describe(ctx, archIRI); err == nil {
		for _, t := range at {
			switch {
			case t.Predicate == rdf.PropLabel && !t.ObjectIsIRI:
				link.archLabel = t.Object
			case t.Predicate == rdf.PropConstrainedByInvariant && t.ObjectIsIRI:
				if id, ok := awarenessIDFromIRI(t.Object); ok {
					link.invariants = append(link.invariants, id)
				}
			case t.Predicate == rdf.PropRequiresTest && t.ObjectIsIRI:
				if id, ok := awarenessIDFromIRI(t.Object); ok {
					// test ids embed a file path, URL-encoded in the IRI segment.
					link.tests = append(link.tests, strings.ReplaceAll(id, "%2F", "/"))
				}
			}
		}
	}
	link.provenance = s.realizationProvenance(ctx, implIRI, archIRI)
	sort.Strings(link.invariants)
	sort.Strings(link.tests)
	return link
}

func (s *server) realizationProvenance(ctx context.Context, implIRI, archIRI string) spineProvenance {
	seen := map[string]bool{}
	for _, iri := range []string{implIRI, archIRI} {
		triples, err := s.store.Describe(ctx, iri)
		if err != nil {
			continue
		}
		for _, t := range triples {
			if t.Predicate != rdf.PropSupportedByEvidence || !t.ObjectIsIRI || seen[t.Object] {
				continue
			}
			seen[t.Object] = true
			p := s.describeSpineEvidence(ctx, t.Object)
			if p.evidenceID != "" {
				return p
			}
		}
	}
	return spineProvenance{}
}

func (s *server) describeSpineEvidence(ctx context.Context, evidenceIRI string) spineProvenance {
	triples, err := s.store.Describe(ctx, evidenceIRI)
	if err != nil {
		return spineProvenance{}
	}
	var p spineProvenance
	p.evidenceID, _ = awarenessIDFromIRI(evidenceIRI)
	for _, t := range triples {
		switch {
		case t.Predicate == rdf.PropSourceKind && !t.ObjectIsIRI:
			p.sourceKind = t.Object
		case t.Predicate == rdf.PropConfidence && !t.ObjectIsIRI:
			p.confidence = t.Object
		case t.Predicate == rdf.PropPromotionStatus && !t.ObjectIsIRI:
			p.promotionStatus = t.Object
		case t.Predicate == rdf.PropComment && !t.ObjectIsIRI && p.summary == "":
			p.summary = strings.TrimSpace(t.Object)
		}
	}
	return p
}

func formatSpineProvenance(p spineProvenance) string {
	if p.evidenceID == "" {
		return ""
	}
	var parts []string
	if p.sourceKind != "" {
		parts = append(parts, "source="+p.sourceKind)
	}
	if p.confidence != "" {
		parts = append(parts, "confidence="+p.confidence)
	}
	if p.promotionStatus != "" {
		parts = append(parts, "status="+p.promotionStatus)
	}
	if p.summary != "" {
		parts = append(parts, p.summary)
	}
	if len(parts) == 0 {
		return p.evidenceID
	}
	return strings.Join(parts, "; ")
}

func (s *server) labelOf(ctx context.Context, iri string) string {
	triples, err := s.store.Describe(ctx, iri)
	if err != nil {
		return ""
	}
	for _, t := range triples {
		if t.Predicate == rdf.PropLabel && !t.ObjectIsIRI {
			return t.Object
		}
	}
	return ""
}

func sortSpine(s []spineLink) {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].impl != s[j].impl {
			return s[i].impl < s[j].impl
		}
		return s[i].arch < s[j].arch
	})
}

func dedupSortedStrings(in []string) []string {
	set := map[string]bool{}
	for _, x := range in {
		set[x] = true
	}
	out := make([]string, 0, len(set))
	for x := range set {
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}
