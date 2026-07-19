// SPDX-License-Identifier: AGPL-3.0-only

package proofrequirements

import (
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// composeAuthorityMechanisms projects the required runtime mechanisms from the
// authority resolution. Only operations that resolved valid contribute; an
// invalid operation cannot enlarge or shrink the requirement set. It reports a
// conflict when the same mechanism id is demanded under different selected
// mechanisms, which the caller escalates to uncertifiable.
func composeAuthorityMechanisms(res closureprotocol.AuthorityResolution) ([]Requirement, bool) {
	type acc struct {
		origins  map[string]bool
		sources  map[string]bool
		selected map[string]bool
	}
	byID := map[string]*acc{}
	for _, op := range res.OperationResults {
		if op.Status != closureprotocol.ReceiptValid {
			continue
		}
		for _, mech := range op.RequiredRuntimeMechanismIDs {
			mech = strings.TrimSpace(mech)
			if mech == "" {
				continue
			}
			a := byID[mech]
			if a == nil {
				a = &acc{origins: map[string]bool{}, sources: map[string]bool{}, selected: map[string]bool{}}
				byID[mech] = a
			}
			a.origins[OriginAuthorityResolution] = true
			if op.OperationID != "" {
				a.sources[op.OperationID] = true
			}
			for _, d := range op.AuthorityDomainIDs {
				if d = strings.TrimSpace(d); d != "" {
					a.sources[d] = true
				}
			}
			if sel := strings.TrimSpace(string(op.SelectedMechanism)); sel != "" {
				a.selected[sel] = true
			}
		}
	}
	conflict := false
	out := make([]Requirement, 0, len(byID))
	for id, a := range byID {
		if len(a.selected) > 1 {
			conflict = true
		}
		out = append(out, Requirement{
			Class: "RuntimeMechanism", ID: id, Status: "required",
			Origins:   keysSorted(a.origins),
			SourceIDs: keysSorted(a.sources),
			Detail:    keysSorted(a.selected),
		})
	}
	sortRequirements(out)
	return out, conflict
}

// composeRepositoryObligations reuses the exact verified Stage 2 proof
// obligations output (never a second authority extraction).
func composeRepositoryObligations(out RepositoryProofOutput) ([]ObligationRequirement, error) {
	if len(out.Bytes) == 0 {
		return nil, nil
	}
	doc, err := ParseObligations(out.Bytes)
	if err != nil {
		return nil, err
	}
	if err := ValidateObligations(doc); err != nil {
		return nil, err
	}
	res := make([]ObligationRequirement, 0, len(doc.ProofObligations))
	for _, o := range doc.ProofObligations {
		var slotIDs []string
		for _, s := range o.RequiredSlots {
			if id := strings.TrimSpace(s.ID); id != "" {
				slotIDs = append(slotIDs, id)
			}
		}
		notes := []string(nil)
		if n := strings.TrimSpace(o.Notes); n != "" {
			notes = []string{n}
		}
		res = append(res, ObligationRequirement{
			ID: o.ID, Label: o.Label, EvidenceLane: o.EvidenceLane, TemplateKind: o.TemplateKind,
			RequiredSlotIDs: cleanStrings(slotIDs), Origins: []string{OriginRepositoryAuthoritySurface},
			SourceIDs: cleanStrings(o.AppliesToAuthoritySurfaces), Notes: notes,
		})
	}
	return res, nil
}

// composeAdmission projects the admission floor: required proof slots, evidence
// profiles, and result rebuilds. Admission requirements are never dropped. A slot
// or profile that the result graph / repository defines is marked defined; one
// that nothing downstream represents is retained as unresolved and recorded as a
// requirement change (governance_review_required) — the extraction is then
// incomplete. A result rebuild whose path the Stage 2 verification confirmed is
// satisfied_by_result; one outside the verified set is uncertifiable.
func composeAdmission(in ComposeInput, graph GraphProjection, repo []ObligationRequirement) (slots, evidence, rebuilds []Requirement, changes []RequirementChange, incomplete, uncertifiable bool) {
	dec := in.AdmissionDecision

	defined := map[string]bool{}
	for _, s := range graph.RequiredSlots {
		defined[s.ID] = true
	}
	for _, o := range graph.Obligations {
		defined[o.ID] = true
	}
	for _, o := range repo {
		defined[o.ID] = true
		for _, sid := range o.RequiredSlotIDs {
			defined[sid] = true
		}
	}
	evidenceDefined := map[string]bool{}
	for _, e := range graph.RuntimeEvidenceProfiles {
		evidenceDefined[e.ID] = true
	}

	for _, id := range dec.RequiredProofSlots {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		r := Requirement{Class: "ProofSlot", ID: id, Origins: []string{OriginAdmission}, Status: "required"}
		if defined[id] {
			r.DefinitionStatus = "defined"
		} else {
			r.DefinitionStatus = "unresolved"
			incomplete = true
			changes = append(changes, RequirementChange{
				Class: "ProofSlot", ID: id, Origins: []string{OriginAdmission},
				ResultGraphStatus: "no_longer_represented", Disposition: "governance_review_required",
			})
		}
		slots = append(slots, r)
	}
	for _, id := range dec.RequiredEvidenceProfiles {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		r := Requirement{Class: "RuntimeEvidence", ID: id, Origins: []string{OriginAdmission}, Status: "required"}
		if evidenceDefined[id] {
			r.DefinitionStatus = "defined"
		} else {
			r.DefinitionStatus = "unresolved"
			incomplete = true
			changes = append(changes, RequirementChange{
				Class: "RuntimeEvidence", ID: id, Origins: []string{OriginAdmission},
				ResultGraphStatus: "no_longer_represented", Disposition: "governance_review_required",
			})
		}
		evidence = append(evidence, r)
	}
	verified := map[string]bool{}
	for _, p := range in.GeneratedArtifacts.VerifiedPaths {
		if p = strings.TrimSpace(p); p != "" {
			verified[p] = true
		}
	}
	for _, id := range dec.RequiredResultRebuilds {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		r := Requirement{Class: "ResultRebuild", ID: id, Origins: []string{OriginAdmission}}
		if verified[id] {
			r.Status = "satisfied_by_result"
		} else {
			r.Status = "uncertifiable"
			uncertifiable = true
		}
		rebuilds = append(rebuilds, r)
	}
	sortRequirements(slots)
	sortRequirements(evidence)
	sortRequirements(rebuilds)
	sortChanges(changes)
	return slots, evidence, rebuilds, changes, incomplete, uncertifiable
}

// composeClosureBlockers projects closure blockers and conditions as
// verification requirements. These are NOT forbidden moves.
func composeClosureBlockers(rep closure.Report) []Requirement {
	var out []Requirement
	for _, b := range rep.Blockers {
		out = append(out, Requirement{
			Class: "ClosureBlocker", ID: b.ID, Origins: []string{OriginClosure},
			Status: b.Severity, EvidenceLane: b.Dimension,
			Detail: cleanStrings([]string{b.Code, b.Summary, b.RequiredNextAction}),
		})
	}
	for _, c := range rep.Conditions {
		out = append(out, Requirement{
			Class: "ClosureCondition", ID: c.ID, Origins: []string{OriginClosure},
			Status: "conditional", EvidenceLane: c.Dimension,
			Detail: cleanStrings(append([]string{c.Code, c.Summary, c.RequiredNextAction}, c.RequiredVerification...)),
		})
	}
	sortRequirements(out)
	return out
}

// composeArchitectQuestions projects unresolved architect questions and the
// blocker-accounting anomalies as requirements the human owns.
func composeArchitectQuestions(q QuestionInput) []Requirement {
	var out []Requirement
	add := func(class string, ids []string, status string) {
		for _, id := range ids {
			if id = strings.TrimSpace(id); id != "" {
				out = append(out, Requirement{Class: class, ID: id, Origins: []string{OriginArchitectQuestions}, Status: status})
			}
		}
	}
	add("ArchitectQuestion", q.UnresolvedArchitectQuestionIDs, "unresolved")
	add("UnaccountedBlocker", q.UnaccountedBlockerIDs, "unaccounted")
	add("DuplicateAccounting", q.DuplicateAccountingIDs, "duplicate")
	add("UnsupportedCritical", q.UnsupportedCriticalIDs, "unsupported")
	sortRequirements(out)
	return out
}

// mergeRequirements merges by (class, id): unions origins, source ids, detail,
// slot ids; strengthens definition status (defined wins over unresolved); and
// keeps the strongest status (uncertifiable > satisfied_by_result > required >
// pending). Output is sorted, so the merge is order-independent.
func mergeRequirements(in []Requirement) []Requirement {
	type key struct{ class, id string }
	order := []key{}
	by := map[key]*Requirement{}
	for i := range in {
		r := in[i]
		k := key{r.Class, r.ID}
		cur, ok := by[k]
		if !ok {
			cp := r
			by[k] = &cp
			order = append(order, k)
			continue
		}
		cur.Origins = cleanStrings(append(cur.Origins, r.Origins...))
		cur.SourceIDs = cleanStrings(append(cur.SourceIDs, r.SourceIDs...))
		cur.RequiredSlotIDs = cleanStrings(append(cur.RequiredSlotIDs, r.RequiredSlotIDs...))
		cur.Detail = cleanStrings(append(cur.Detail, r.Detail...))
		if cur.EvidenceLane == "" {
			cur.EvidenceLane = r.EvidenceLane
		}
		cur.DefinitionStatus = strongerDefinition(cur.DefinitionStatus, r.DefinitionStatus)
		cur.Status = strongerStatus(cur.Status, r.Status)
	}
	out := make([]Requirement, 0, len(order))
	for _, k := range order {
		r := by[k]
		r.Origins = cleanStrings(r.Origins)
		out = append(out, *r)
	}
	sortRequirements(out)
	return out
}

func mergeObligations(in []ObligationRequirement) []ObligationRequirement {
	order := []string{}
	by := map[string]*ObligationRequirement{}
	for i := range in {
		o := in[i]
		cur, ok := by[o.ID]
		if !ok {
			cp := o
			by[o.ID] = &cp
			order = append(order, o.ID)
			continue
		}
		cur.Origins = cleanStrings(append(cur.Origins, o.Origins...))
		cur.SourceIDs = cleanStrings(append(cur.SourceIDs, o.SourceIDs...))
		cur.RequiredSlotIDs = cleanStrings(append(cur.RequiredSlotIDs, o.RequiredSlotIDs...))
		cur.Notes = cleanStrings(append(cur.Notes, o.Notes...))
		if cur.Label == "" {
			cur.Label = o.Label
		}
		if cur.EvidenceLane == "" {
			cur.EvidenceLane = o.EvidenceLane
		}
		if cur.TemplateKind == "" {
			cur.TemplateKind = o.TemplateKind
		}
	}
	out := make([]ObligationRequirement, 0, len(order))
	for _, id := range order {
		o := by[id]
		o.Origins = cleanStrings(o.Origins)
		out = append(out, *o)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

var definitionRank = map[string]int{"": 0, "unresolved": 1, "defined": 2}

func strongerDefinition(a, b string) string {
	if definitionRank[b] > definitionRank[a] {
		return b
	}
	return a
}

var statusRank = map[string]int{
	"": 0, "pending": 1, "conditional": 2, "required": 3,
	"satisfied_by_result": 4, "uncertifiable": 5,
}

func strongerStatus(a, b string) string {
	ra, oka := statusRank[a]
	rb, okb := statusRank[b]
	if !okb {
		return a
	}
	if !oka || rb > ra {
		return b
	}
	return a
}

func sortChanges(in []RequirementChange) {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Class != in[j].Class {
			return in[i].Class < in[j].Class
		}
		return in[i].ID < in[j].ID
	})
}

func keysSorted(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
