// SPDX-License-Identifier: AGPL-3.0-only

package dashboardprojection

import (
	"fmt"
	"sort"
)

// ValidationError is one violation of the producer-side contract in
// globulario/sensei/issue-115 ("Required producer validation"). JSON Schema
// alone cannot express these — they are cross-record and policy invariants.
type ValidationError struct {
	Rule   string
	Detail string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Rule, e.Detail)
}

// selectableElement is any element kind the dashboard schema allows a user to
// select and expect a Focus explanation for.
type selectableElement struct {
	id   string
	kind string
}

// Validate runs every producer-side check issue #115 requires beyond what
// dashboard-projection-v1.schema.json itself can express. It returns every
// violation found (not just the first) so a single run reports the full
// picture.
func Validate(p Projection) []ValidationError {
	var errs []ValidationError

	errs = append(errs, validateSchemaVersion(p)...)
	errs = append(errs, validateDuplicateIDs(p)...)
	errs = append(errs, validateFocusIntegrity(p)...)
	errs = append(errs, validateReferences(p)...)

	return errs
}

func validateSchemaVersion(p Projection) []ValidationError {
	if p.SchemaVersion != SchemaVersion {
		return []ValidationError{{
			Rule:   "unsupported_schema_version",
			Detail: fmt.Sprintf("got %q, only %q is accepted; an unrecognized schema_version must be rejected, never partially interpreted", p.SchemaVersion, SchemaVersion),
		}}
	}
	return nil
}

// validateDuplicateIDs rejects a stable ID reused across incompatible element
// kinds, where two different kinds sharing one ID would make a *_refs
// reference ambiguous (which kind does the ID resolve to?).
func validateDuplicateIDs(p Projection) []ValidationError {
	kindsByID := map[string]map[string]bool{}
	record := func(id, kind string) {
		if kindsByID[id] == nil {
			kindsByID[id] = map[string]bool{}
		}
		kindsByID[id][kind] = true
	}

	for _, r := range p.Regions {
		record(r.ID, "region")
	}
	for _, c := range p.Components {
		record(c.ID, "component")
	}
	for _, b := range p.Boundaries {
		record(b.ID, "boundary")
	}
	for _, c := range p.Contracts {
		record(c.ID, "contract")
	}
	for _, f := range p.Flows {
		record(f.ID, "flow")
	}
	for _, a := range p.Attention {
		record(a.ID, "attention")
	}

	var errs []ValidationError
	ids := make([]string, 0, len(kindsByID))
	for id := range kindsByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		kinds := kindsByID[id]
		if len(kinds) > 1 {
			list := make([]string, 0, len(kinds))
			for k := range kinds {
				list = append(list, k)
			}
			sort.Strings(list)
			errs = append(errs, ValidationError{
				Rule:   "duplicate_id_across_incompatible_kinds",
				Detail: fmt.Sprintf("id %q is used by more than one element kind (%v); a *_refs reference to it would be ambiguous", id, list),
			})
		}
	}
	return errs
}

// validateFocusIntegrity enforces: every selectable region, component,
// boundary, contract, and flow must resolve to exactly one focus_records
// entry with the same stable ID and matching kind. Attention items are never
// independently Focus-selectable in V1 (see AttentionItem's doc comment) —
// they may only be reached via ElementRefs from a selectable element's
// Focus, never carry their own focus_records entry. Duplicate or missing
// focus records for a selectable element are a projection-integrity failure
// the producer must catch — the frontend must never fabricate a fallback.
func validateFocusIntegrity(p Projection) []ValidationError {
	var errs []ValidationError

	var selectable []selectableElement
	for _, r := range p.Regions {
		selectable = append(selectable, selectableElement{r.ID, "region"})
	}
	for _, c := range p.Components {
		selectable = append(selectable, selectableElement{c.ID, "component"})
	}
	for _, b := range p.Boundaries {
		selectable = append(selectable, selectableElement{b.ID, "boundary"})
	}
	for _, c := range p.Contracts {
		selectable = append(selectable, selectableElement{c.ID, "contract"})
	}
	for _, f := range p.Flows {
		selectable = append(selectable, selectableElement{f.ID, "flow"})
	}

	focusCount := map[selectableElement]int{}
	for _, fr := range p.FocusRecords {
		focusCount[selectableElement{fr.ElementRef, fr.ElementKind}]++
	}

	for _, el := range selectable {
		switch focusCount[el] {
		case 0:
			errs = append(errs, ValidationError{
				Rule:   "missing_focus_record",
				Detail: fmt.Sprintf("selectable %s %q has no matching focus_records entry", el.kind, el.id),
			})
		case 1:
			// satisfied
		default:
			errs = append(errs, ValidationError{
				Rule:   "duplicate_focus_record",
				Detail: fmt.Sprintf("selectable %s %q has %d focus_records entries; exactly one is required", el.kind, el.id, focusCount[el]),
			})
		}
	}

	// A focus record whose (id, kind) matches nothing selectable is also an
	// integrity problem: it claims to explain an element the map never offers.
	selectableSet := map[selectableElement]bool{}
	for _, el := range selectable {
		selectableSet[el] = true
	}
	for _, fr := range p.FocusRecords {
		key := selectableElement{fr.ElementRef, fr.ElementKind}
		if !selectableSet[key] {
			errs = append(errs, ValidationError{
				Rule:   "orphan_focus_record",
				Detail: fmt.Sprintf("focus_records entry %q (%s) does not match any selectable element", fr.ElementRef, fr.ElementKind),
			})
		}
	}

	return errs
}

// Element kind tokens, matching the schema's element_kind/*_ref vocabularies.
const (
	kindRegion    = "region"
	kindComponent = "component"
	kindBoundary  = "boundary"
	kindContract  = "contract"
	kindFlow      = "flow"
	kindAttention = "attention"
)

// validateReferences confirms every *_refs value resolves to a known element
// ID *of the expected kind* (issue #115, required producer validation item
// 3: "All references resolve to known identifiers of the expected kind").
// It builds one id->kind index and checks each field against an explicit,
// documented set of permitted kinds rather than a single flat existence
// check — an ID that exists but belongs to the wrong kind is exactly the
// kind of ambiguous data this producer must catch before it reaches the UI.
//
// provenance.* refs (evidence_refs, decision_refs, source_refs) and
// focus_records[].decision_refs are intentionally not checked here: they
// point into evidence/decision spaces this projection does not itself model
// as elements (there is no top-level "decisions" collection), so there is no
// local id space to resolve them against.
func validateReferences(p Projection) []ValidationError {
	kindOf := map[string]string{}
	record := func(id, kind string) { kindOf[id] = kind }
	for _, r := range p.Regions {
		record(r.ID, kindRegion)
	}
	for _, c := range p.Components {
		record(c.ID, kindComponent)
	}
	for _, b := range p.Boundaries {
		record(b.ID, kindBoundary)
	}
	for _, c := range p.Contracts {
		record(c.ID, kindContract)
	}
	for _, f := range p.Flows {
		record(f.ID, kindFlow)
	}
	for _, a := range p.Attention {
		record(a.ID, kindAttention)
	}

	var errs []ValidationError

	// checkOne validates a single ref against an explicit allowed-kind set.
	checkOne := func(owner, field, ref string, allowed ...string) {
		kind, ok := kindOf[ref]
		if !ok {
			errs = append(errs, ValidationError{
				Rule:   "unresolved_reference",
				Detail: fmt.Sprintf("%s %s references unknown id %q", owner, field, ref),
			})
			return
		}
		for _, k := range allowed {
			if kind == k {
				return
			}
		}
		errs = append(errs, ValidationError{
			Rule:   "reference_kind_mismatch",
			Detail: fmt.Sprintf("%s %s references %q, which is a %s, not one of %v", owner, field, ref, kind, allowed),
		})
	}
	checkMany := func(owner, field string, refs []string, allowed ...string) {
		for _, ref := range refs {
			checkOne(owner, field, ref, allowed...)
		}
	}

	for _, r := range p.Regions {
		owner := fmt.Sprintf("region %q", r.ID)
		checkMany(owner, "component_refs", r.ComponentRefs, kindComponent)
	}
	for _, c := range p.Components {
		owner := fmt.Sprintf("component %q", c.ID)
		if c.RegionRef != "" {
			checkOne(owner, "region_ref", c.RegionRef, kindRegion)
		}
		// authority_refs is populated from boundaries.yaml's protected_by (the
		// boundaries that constrain this component's authority), so the
		// expected kind is boundary.
		checkMany(owner, "authority_refs", c.AuthorityRefs, kindBoundary)
	}
	for _, b := range p.Boundaries {
		owner := fmt.Sprintf("boundary %q", b.ID)
		// A boundary can separate any architecture-map element, not only
		// components (e.g. a trust boundary between two regions) — the
		// permitted set is deliberately the three "placeable" element kinds.
		checkMany(owner, "member_refs", b.MemberRefs, kindRegion, kindComponent, kindBoundary)
	}
	for _, c := range p.Contracts {
		owner := fmt.Sprintf("contract %q", c.ID)
		// Contracts connect components/boundaries in this producer's data
		// (populated from exposed_by); regions do not expose contracts.
		if c.SourceRef != "" {
			checkOne(owner, "source_ref", c.SourceRef, kindComponent, kindBoundary)
		}
		if c.TargetRef != "" {
			checkOne(owner, "target_ref", c.TargetRef, kindComponent, kindBoundary)
		}
		checkMany(owner, "boundary_refs", c.BoundaryRefs, kindBoundary)
	}
	for _, f := range p.Flows {
		for _, step := range f.Steps {
			owner := fmt.Sprintf("flow %q step %d", f.ID, step.Order)
			checkOne(owner, "element_ref", step.ElementRef, kindRegion, kindComponent, kindBoundary, kindContract)
			if step.ContractRef != nil {
				checkOne(owner, "contract_ref", *step.ContractRef, kindContract)
			}
		}
	}
	for _, a := range p.Attention {
		owner := fmt.Sprintf("attention %q", a.ID)
		// An attention item may point at any architecture-map element or flow.
		checkMany(owner, "element_refs", a.ElementRefs, kindRegion, kindComponent, kindBoundary, kindContract, kindFlow)
	}
	for _, fr := range p.FocusRecords {
		owner := fmt.Sprintf("focus_records %q", fr.ElementRef)
		// owner_refs/owned_refs describe ownership relationships, which in
		// this producer's model only hold between region/component/boundary.
		checkMany(owner, "owner_refs", fr.OwnerRefs, kindRegion, kindComponent, kindBoundary)
		checkMany(owner, "owned_refs", fr.OwnedRefs, kindRegion, kindComponent, kindBoundary, kindContract, kindFlow)
		checkMany(owner, "contract_refs", fr.ContractRefs, kindContract)
		checkMany(owner, "flow_refs", fr.FlowRefs, kindFlow)
		checkMany(owner, "attention_refs", fr.AttentionRefs, kindAttention)
	}
	for _, br := range p.Briefing {
		owner := fmt.Sprintf("briefing %q", br.ID)
		checkMany(owner, "element_refs", br.ElementRefs, kindRegion, kindComponent, kindBoundary, kindContract, kindFlow, kindAttention)
	}

	return errs
}

// ValidatePublicRedaction enforces the public-static-snapshot redaction rule
// from the adopted contract (architecture-dashboard-v1.md §11, §14 and the
// projection schema's activeContext $comment). V1 requires active_context:
// null in every public snapshot, full stop — including task, pull_request,
// and change context that carries a URL. A public URL is proof the *context
// object itself* has a public location; it is not proof that its label, id,
// element_refs, or surrounding projection context are safe to publish, and
// the adopted schema defines no per-field public marker that could make that
// determination. Until a future schema version carries an explicit,
// producer-owned publication classification, the only honest, enforceable
// V1 rule is "no active_context at all" in public mode. This is a producer
// policy, not a data validity check, so it is a separate function from
// Validate rather than folded into it.
func ValidatePublicRedaction(p Projection) []ValidationError {
	if p.ActiveContext == nil {
		return nil
	}
	detail := fmt.Sprintf("active_context.kind = %s must be omitted (active_context: null) in a public snapshot; V1 has no per-field public marker that could justify publishing it, regardless of url", p.ActiveContext.Kind)
	if p.ActiveContext.Kind == ContextSession {
		detail = "active_context.kind = session must never appear in a public static snapshot"
	}
	return []ValidationError{{Rule: "active_context_in_public_snapshot", Detail: detail}}
}
