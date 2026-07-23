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
// entry with the same stable ID and matching kind. An attentionItem requires
// one only when its own Selectable flag is true. Duplicate or missing focus
// records for a selectable element are a projection-integrity failure the
// producer must catch — the frontend must never fabricate a fallback.
func validateFocusIntegrity(p Projection) []ValidationError {
	var errs []ValidationError

	var selectable []selectableElement
	for _, r := range p.Regions {
		_ = r // regions are not individually Focus-selectable in v1 (no per-element opt-out field); include per schema's element_kind enum.
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
	for _, a := range p.Attention {
		if a.Selectable {
			selectable = append(selectable, selectableElement{a.ID, "attention"})
		}
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

// validateReferences confirms every *_refs value resolves to a known element
// ID. It does not attempt to check the referenced kind is the semantically
// "right" one everywhere (JSON Schema and this producer both leave that to
// authoring discipline) — it only catches a reference to an ID that does not
// exist anywhere in the projection at all, which is unambiguously a defect.
func validateReferences(p Projection) []ValidationError {
	known := map[string]bool{}
	for _, r := range p.Regions {
		known[r.ID] = true
	}
	for _, c := range p.Components {
		known[c.ID] = true
	}
	for _, b := range p.Boundaries {
		known[b.ID] = true
	}
	for _, c := range p.Contracts {
		known[c.ID] = true
	}
	for _, f := range p.Flows {
		known[f.ID] = true
	}
	for _, a := range p.Attention {
		known[a.ID] = true
	}

	var errs []ValidationError
	check := func(owner string, refs []string) {
		for _, ref := range refs {
			if !known[ref] {
				errs = append(errs, ValidationError{
					Rule:   "unresolved_reference",
					Detail: fmt.Sprintf("%s references unknown id %q", owner, ref),
				})
			}
		}
	}

	for _, r := range p.Regions {
		check(fmt.Sprintf("region %q component_refs", r.ID), r.ComponentRefs)
	}
	for _, c := range p.Components {
		check(fmt.Sprintf("component %q authority_refs", c.ID), c.AuthorityRefs)
	}
	for _, b := range p.Boundaries {
		check(fmt.Sprintf("boundary %q member_refs", b.ID), b.MemberRefs)
	}
	for _, c := range p.Contracts {
		if c.SourceRef != "" && !known[c.SourceRef] {
			errs = append(errs, ValidationError{Rule: "unresolved_reference", Detail: fmt.Sprintf("contract %q source_ref references unknown id %q", c.ID, c.SourceRef)})
		}
		if c.TargetRef != "" && !known[c.TargetRef] {
			errs = append(errs, ValidationError{Rule: "unresolved_reference", Detail: fmt.Sprintf("contract %q target_ref references unknown id %q", c.ID, c.TargetRef)})
		}
		check(fmt.Sprintf("contract %q boundary_refs", c.ID), c.BoundaryRefs)
	}
	for _, f := range p.Flows {
		for _, step := range f.Steps {
			if !known[step.ElementRef] {
				errs = append(errs, ValidationError{Rule: "unresolved_reference", Detail: fmt.Sprintf("flow %q step %d element_ref references unknown id %q", f.ID, step.Order, step.ElementRef)})
			}
			if step.ContractRef != nil && !known[*step.ContractRef] {
				errs = append(errs, ValidationError{Rule: "unresolved_reference", Detail: fmt.Sprintf("flow %q step %d contract_ref references unknown id %q", f.ID, step.Order, *step.ContractRef)})
			}
		}
	}
	for _, a := range p.Attention {
		check(fmt.Sprintf("attention %q element_refs", a.ID), a.ElementRefs)
	}
	for _, fr := range p.FocusRecords {
		check(fmt.Sprintf("focus_records %q owner_refs", fr.ElementRef), fr.OwnerRefs)
		check(fmt.Sprintf("focus_records %q owned_refs", fr.ElementRef), fr.OwnedRefs)
		check(fmt.Sprintf("focus_records %q contract_refs", fr.ElementRef), fr.ContractRefs)
		check(fmt.Sprintf("focus_records %q flow_refs", fr.ElementRef), fr.FlowRefs)
		check(fmt.Sprintf("focus_records %q attention_refs", fr.ElementRef), fr.AttentionRefs)
	}

	return errs
}

// ValidatePublicRedaction enforces the public-static-snapshot redaction rule
// from the adopted contract (architecture-dashboard-v1.md §11, §14 and the
// projection schema's activeContext $comment): 'session' context must never
// be published, and 'task'/'pull_request' context is published only when it
// carries a public URL. This producer treats "bound to a public URL" (a
// non-nil ActiveContext.URL) as the operative, checkable form of "every
// included field is explicitly public" — the schema has no separate
// public-marker field; a future schema revision could add one if a stronger
// signal is needed. This is a producer policy, not a data validity check, so
// it is a separate function from Validate rather than folded into it.
func ValidatePublicRedaction(p Projection) []ValidationError {
	if p.ActiveContext == nil {
		return nil
	}
	var errs []ValidationError
	switch p.ActiveContext.Kind {
	case ContextSession:
		errs = append(errs, ValidationError{
			Rule:   "session_context_in_public_snapshot",
			Detail: "active_context.kind = session must never appear in a public static snapshot",
		})
	case ContextTask, ContextPullRequest:
		if p.ActiveContext.URL == nil {
			errs = append(errs, ValidationError{
				Rule:   "unpublic_context_in_public_snapshot",
				Detail: fmt.Sprintf("active_context.kind = %s has no public url; it must be omitted (active_context: null) unless bound to a public URL", p.ActiveContext.Kind),
			})
		}
	}
	return errs
}
