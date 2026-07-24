// SPDX-License-Identifier: AGPL-3.0-only

package dashboardprojection

import (
	"strconv"
	"testing"
)

func minimalValidProjection() Projection {
	prov := Provenance{EvidenceRefs: []string{}}
	return Projection{
		SchemaVersion: SchemaVersion,
		Identity: Identity{
			ProjectionID: "proj-1",
			Repository:   Repository{Key: "github.com/globulario/sensei", DisplayName: "Sensei"},
			Revision:     Revision{ID: "rev-1"},
			GraphAuthority: GraphAuthority{
				Observed: TriYes, Current: TriYes, Identity: nil, Summary: "ok",
			},
			GeneratedAt: "2026-07-23T00:00:00Z",
		},
		Availability: Availability{State: Available, Summary: "ok", Limitations: []string{}, Sources: []SourceState{}},
		Assessments: Assessments{
			ArchitectureHealth:  Assessment{State: StateUnknown, Label: "Health", Summary: "not yet evaluated", Severity: SeverityNotApplicable, Provenance: prov},
			ProjectionIntegrity: Assessment{State: StateHealthy, Label: "Integrity", Summary: "current", Severity: SeverityNotApplicable, Provenance: prov},
			ObservationCompleteness: ObservationAssessment{
				State: StateAttention, Label: "Coverage", Summary: "partial", Severity: SeverityMedium,
				Coverage: Coverage{Unit: "authored architectural elements"}, Provenance: prov,
			},
		},
		ActiveContext: nil,
		Briefing:      []Briefing{},
		Regions:       []Region{},
		Components: []Component{
			{ID: "component.a", Name: "A", RegionRef: "", Responsibility: "does a", State: StateOpen, AuthorityRefs: []string{}, VisualAnchor: VisualAnchor{Order: 0}, Provenance: prov},
		},
		Boundaries: []Boundary{},
		Contracts:  []Contract{},
		Flows:      []Flow{},
		Attention:  []AttentionItem{},
		Evolution:  Evolution{Availability: Available, BaseRevision: nil, HeadRevision: "rev-1", Changes: []Change{}},
		FocusRecords: []FocusRecord{
			{ElementRef: "component.a", ElementKind: "component", Name: "A", Responsibility: "does a", State: StateOpen,
				OwnerRefs: []string{}, ContractRefs: []string{}, FlowRefs: []string{}, AttentionRefs: []string{}, DecisionRefs: []string{}, Provenance: prov},
		},
	}
}

func TestValidateAcceptsMinimalHonestProjection(t *testing.T) {
	p := minimalValidProjection()
	if errs := Validate(p); len(errs) != 0 {
		t.Fatalf("expected 0 errors, got %v", errs)
	}
}

func TestValidateRejectsUnsupportedSchemaVersion(t *testing.T) {
	p := minimalValidProjection()
	p.SchemaVersion = "sensei.dashboard.projection.v0"
	errs := Validate(p)
	if !hasRule(errs, "unsupported_schema_version") {
		t.Fatalf("expected unsupported_schema_version, got %v", errs)
	}
}

func TestValidateRejectsMissingFocusRecord(t *testing.T) {
	p := minimalValidProjection()
	p.FocusRecords = nil
	errs := Validate(p)
	if !hasRule(errs, "missing_focus_record") {
		t.Fatalf("expected missing_focus_record, got %v", errs)
	}
}

func TestValidateRejectsDuplicateFocusRecord(t *testing.T) {
	p := minimalValidProjection()
	p.FocusRecords = append(p.FocusRecords, p.FocusRecords[0])
	errs := Validate(p)
	if !hasRule(errs, "duplicate_focus_record") {
		t.Fatalf("expected duplicate_focus_record, got %v", errs)
	}
}

func TestValidateRejectsOrphanFocusRecord(t *testing.T) {
	p := minimalValidProjection()
	prov := Provenance{EvidenceRefs: []string{}}
	p.FocusRecords = append(p.FocusRecords, FocusRecord{
		ElementRef: "component.ghost", ElementKind: "component", Name: "Ghost", Responsibility: "?", State: StateUnknown,
		OwnerRefs: []string{}, ContractRefs: []string{}, FlowRefs: []string{}, AttentionRefs: []string{}, DecisionRefs: []string{}, Provenance: prov,
	})
	errs := Validate(p)
	if !hasRule(errs, "orphan_focus_record") {
		t.Fatalf("expected orphan_focus_record, got %v", errs)
	}
}

func TestValidateRejectsDuplicateIDAcrossKinds(t *testing.T) {
	p := minimalValidProjection()
	prov := Provenance{EvidenceRefs: []string{}}
	p.Boundaries = append(p.Boundaries, Boundary{
		ID: "component.a", Name: "Collides", Kind: "domain", MemberRefs: []string{}, State: StateOpen, Summary: "x", Provenance: prov,
	})
	errs := Validate(p)
	if !hasRule(errs, "duplicate_id_across_incompatible_kinds") {
		t.Fatalf("expected duplicate_id_across_incompatible_kinds, got %v", errs)
	}
}

func TestValidateRejectsUnresolvedReference(t *testing.T) {
	p := minimalValidProjection()
	p.Components[0].AuthorityRefs = []string{"boundary.does_not_exist"}
	errs := Validate(p)
	if !hasRule(errs, "unresolved_reference") {
		t.Fatalf("expected unresolved_reference, got %v", errs)
	}
}

// --- adversarial reference-kind tests: an ID that exists but is the wrong
// kind must be rejected, not silently accepted because it resolves. ---

func projectionWithOneOfEachKind() Projection {
	prov := Provenance{EvidenceRefs: []string{}}
	return Projection{
		SchemaVersion: SchemaVersion,
		Identity: Identity{
			ProjectionID:   "proj-1",
			Repository:     Repository{Key: "github.com/globulario/sensei", DisplayName: "Sensei"},
			Revision:       Revision{ID: "rev-1"},
			GraphAuthority: GraphAuthority{Observed: TriYes, Current: TriYes, Summary: "ok"},
			GeneratedAt:    "2026-07-23T00:00:00Z",
		},
		Availability: Availability{State: Partial, Summary: "fixture", Limitations: []string{}, Sources: []SourceState{}},
		Assessments: Assessments{
			ArchitectureHealth:      Assessment{State: StateUnknown, Label: "h", Summary: "s", Severity: SeverityNotApplicable, Provenance: prov},
			ProjectionIntegrity:     Assessment{State: StateHealthy, Label: "i", Summary: "s", Severity: SeverityNotApplicable, Provenance: prov},
			ObservationCompleteness: ObservationAssessment{State: StateAttention, Label: "o", Summary: "s", Severity: SeverityMedium, Coverage: Coverage{Unit: "elements"}, Provenance: prov},
		},
		Regions: []Region{
			{ID: "region.r1", Name: "R1", Responsibility: "r", State: StateUnknown, ComponentRefs: []string{"component.c1"}, VisualAnchor: VisualAnchor{Order: 0}, Provenance: prov},
		},
		Components: []Component{
			{ID: "component.c1", Name: "C1", RegionRef: "region.r1", Responsibility: "c", State: StateOpen, AuthorityRefs: []string{"boundary.b1"}, VisualAnchor: VisualAnchor{Order: 0}, Provenance: prov},
		},
		Boundaries: []Boundary{
			{ID: "boundary.b1", Name: "B1", Kind: "domain", MemberRefs: []string{"component.c1"}, State: StateHealthy, Summary: "b", Provenance: prov},
		},
		Contracts: []Contract{
			{ID: "contract.k1", Name: "K1", SourceRef: "component.c1", TargetRef: "component.c1", Kind: "grpc", Direction: "undirected", State: StateOpen, Summary: "k", Provenance: prov},
		},
		Flows: []Flow{
			{ID: "flow.f1", Name: "F1", Kind: "command", State: StateOpen, Summary: "f",
				Steps: []FlowStep{{Order: 0, ElementRef: "component.c1", ContractRef: strp("contract.k1")}}, Provenance: prov},
		},
		Attention: []AttentionItem{{ID: "attention.a1", Kind: "question", Title: "A1", Summary: "a", Severity: SeverityLow, State: StateOpen, ElementRefs: []string{"component.c1"}, Provenance: prov}},
		Evolution: Evolution{Availability: Available, HeadRevision: "rev-1", Changes: []Change{}},
		FocusRecords: []FocusRecord{
			{ElementRef: "region.r1", ElementKind: "region", Name: "R1", Responsibility: "r", State: StateUnknown, OwnerRefs: []string{}, OwnedRefs: []string{"component.c1"}, ContractRefs: []string{}, FlowRefs: []string{}, AttentionRefs: []string{}, DecisionRefs: []string{}, Provenance: prov},
			{ElementRef: "component.c1", ElementKind: "component", Name: "C1", Responsibility: "c", State: StateOpen, OwnerRefs: []string{}, ContractRefs: []string{"contract.k1"}, FlowRefs: []string{"flow.f1"}, AttentionRefs: []string{"attention.a1"}, DecisionRefs: []string{}, Provenance: prov},
			{ElementRef: "boundary.b1", ElementKind: "boundary", Name: "B1", Responsibility: "b", State: StateHealthy, OwnerRefs: []string{}, ContractRefs: []string{}, FlowRefs: []string{}, AttentionRefs: []string{}, DecisionRefs: []string{}, Provenance: prov},
			{ElementRef: "contract.k1", ElementKind: "contract", Name: "K1", Responsibility: "k", State: StateOpen, OwnerRefs: []string{}, ContractRefs: []string{}, FlowRefs: []string{}, AttentionRefs: []string{}, DecisionRefs: []string{}, Provenance: prov},
			{ElementRef: "flow.f1", ElementKind: "flow", Name: "F1", Responsibility: "f", State: StateOpen, OwnerRefs: []string{}, ContractRefs: []string{}, FlowRefs: []string{}, AttentionRefs: []string{}, DecisionRefs: []string{}, Provenance: prov},
		},
	}
}

func strp(s string) *string { return &s }

func TestValidateAcceptsOneOfEachKindFixture(t *testing.T) {
	p := projectionWithOneOfEachKind()
	if errs := Validate(p); len(errs) != 0 {
		t.Fatalf("expected 0 errors, got %v", errs)
	}
}

func TestValidateRejectsComponentRegionRefOfWrongKind(t *testing.T) {
	p := projectionWithOneOfEachKind()
	p.Components[0].RegionRef = "component.c1" // a component, not a region
	errs := Validate(p)
	if !hasRule(errs, "reference_kind_mismatch") {
		t.Fatalf("expected reference_kind_mismatch, got %v", errs)
	}
}

func TestValidateRejectsRegionComponentRefOfWrongKind(t *testing.T) {
	p := projectionWithOneOfEachKind()
	p.Regions[0].ComponentRefs = []string{"boundary.b1"} // a boundary, not a component
	errs := Validate(p)
	if !hasRule(errs, "reference_kind_mismatch") {
		t.Fatalf("expected reference_kind_mismatch, got %v", errs)
	}
}

func TestValidateRejectsComponentAuthorityRefOfWrongKind(t *testing.T) {
	p := projectionWithOneOfEachKind()
	p.Components[0].AuthorityRefs = []string{"component.c1"} // a component, not a boundary
	errs := Validate(p)
	if !hasRule(errs, "reference_kind_mismatch") {
		t.Fatalf("expected reference_kind_mismatch, got %v", errs)
	}
}

func TestValidateRejectsFlowContractRefOfWrongKind(t *testing.T) {
	p := projectionWithOneOfEachKind()
	wrong := "component.c1" // a component, not a contract
	p.Flows[0].Steps[0].ContractRef = &wrong
	errs := Validate(p)
	if !hasRule(errs, "reference_kind_mismatch") {
		t.Fatalf("expected reference_kind_mismatch, got %v", errs)
	}
}

func TestValidateRejectsFocusRecordContractRefsOfWrongKind(t *testing.T) {
	p := projectionWithOneOfEachKind()
	p.FocusRecords[1].ContractRefs = []string{"flow.f1"} // a flow, not a contract
	errs := Validate(p)
	if !hasRule(errs, "reference_kind_mismatch") {
		t.Fatalf("expected reference_kind_mismatch, got %v", errs)
	}
}

func TestValidateRejectsFocusRecordFlowRefsOfWrongKind(t *testing.T) {
	p := projectionWithOneOfEachKind()
	p.FocusRecords[1].FlowRefs = []string{"contract.k1"} // a contract, not a flow
	errs := Validate(p)
	if !hasRule(errs, "reference_kind_mismatch") {
		t.Fatalf("expected reference_kind_mismatch, got %v", errs)
	}
}

func TestValidateRejectsFocusRecordAttentionRefsOfWrongKind(t *testing.T) {
	p := projectionWithOneOfEachKind()
	p.FocusRecords[1].AttentionRefs = []string{"component.c1"} // a component, not attention
	errs := Validate(p)
	if !hasRule(errs, "reference_kind_mismatch") {
		t.Fatalf("expected reference_kind_mismatch, got %v", errs)
	}
}

func TestValidateRejectsContractSourceRefOfWrongKind(t *testing.T) {
	p := projectionWithOneOfEachKind()
	p.Contracts[0].SourceRef = "region.r1" // a region, not component/boundary
	errs := Validate(p)
	if !hasRule(errs, "reference_kind_mismatch") {
		t.Fatalf("expected reference_kind_mismatch, got %v", errs)
	}
}

func TestValidateRejectsBoundaryMemberRefOfWrongKind(t *testing.T) {
	p := projectionWithOneOfEachKind()
	p.Boundaries[0].MemberRefs = []string{"contract.k1"} // a contract, not region/component/boundary
	errs := Validate(p)
	if !hasRule(errs, "reference_kind_mismatch") {
		t.Fatalf("expected reference_kind_mismatch, got %v", errs)
	}
}

func TestValidatePublicRedactionRejectsSessionContext(t *testing.T) {
	p := minimalValidProjection()
	p.ActiveContext = &ActiveContext{Kind: ContextSession, ID: "session-1", Label: "s"}
	errs := ValidatePublicRedaction(p)
	if !hasRule(errs, "active_context_in_public_snapshot") {
		t.Fatalf("expected active_context_in_public_snapshot, got %v", errs)
	}
}

func TestValidatePublicRedactionRejectsTaskContextWithoutURL(t *testing.T) {
	p := minimalValidProjection()
	p.ActiveContext = &ActiveContext{Kind: ContextTask, ID: "task-1", Label: "t"}
	errs := ValidatePublicRedaction(p)
	if !hasRule(errs, "active_context_in_public_snapshot") {
		t.Fatalf("expected active_context_in_public_snapshot, got %v", errs)
	}
}

// TestValidatePublicRedactionRejectsTaskContextEvenWithURL is the corrected
// V1 behavior: a public URL on task/pull_request context is not proof every
// included field (label, id, element_refs) is safe to publish, so it must
// still be rejected in public mode. A prior version of this producer treated
// a non-nil URL as sufficient; that was wrong and has been removed.
func TestValidatePublicRedactionRejectsTaskContextEvenWithURL(t *testing.T) {
	p := minimalValidProjection()
	url := "https://github.com/globulario/sensei/pull/1"
	p.ActiveContext = &ActiveContext{Kind: ContextTask, ID: "task-1", Label: "t", URL: &url}
	errs := ValidatePublicRedaction(p)
	if !hasRule(errs, "active_context_in_public_snapshot") {
		t.Fatalf("expected active_context_in_public_snapshot even with a public url, got %v", errs)
	}
}

func TestValidatePublicRedactionRejectsPullRequestAndChangeContext(t *testing.T) {
	for _, kind := range []ActiveContextKind{ContextPullRequest, ContextChange} {
		p := minimalValidProjection()
		p.ActiveContext = &ActiveContext{Kind: kind, ID: "x-1", Label: "x"}
		errs := ValidatePublicRedaction(p)
		if !hasRule(errs, "active_context_in_public_snapshot") {
			t.Fatalf("kind=%s: expected active_context_in_public_snapshot, got %v", kind, errs)
		}
	}
}

func TestValidatePublicRedactionAcceptsNilActiveContext(t *testing.T) {
	p := minimalValidProjection()
	p.ActiveContext = nil
	if errs := ValidatePublicRedaction(p); len(errs) != 0 {
		t.Fatalf("expected 0 errors, got %v", errs)
	}
}

func hasRule(errs []ValidationError, rule string) bool {
	for _, e := range errs {
		if e.Rule == rule {
			return true
		}
	}
	return false
}

func TestDigestIsStableAcrossGeneratedAtOnly(t *testing.T) {
	p1 := minimalValidProjection()
	p2 := minimalValidProjection()
	p2.Identity.GeneratedAt = "2099-01-01T00:00:00Z"

	d1, err := Digest(p1)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := Digest(p2)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatalf("digest changed when only generated_at changed: %s vs %s", d1, d2)
	}

	p3 := minimalValidProjection()
	p3.Components[0].Responsibility = "does something else " + strconv.Itoa(1)
	d3, err := Digest(p3)
	if err != nil {
		t.Fatal(err)
	}
	if d3 == d1 {
		t.Fatalf("digest did not change when architectural content changed")
	}
}
