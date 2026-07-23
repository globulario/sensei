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

func TestValidatePublicRedactionRejectsSessionContext(t *testing.T) {
	p := minimalValidProjection()
	p.ActiveContext = &ActiveContext{Kind: ContextSession, ID: "session-1", Label: "s"}
	errs := ValidatePublicRedaction(p)
	if !hasRule(errs, "session_context_in_public_snapshot") {
		t.Fatalf("expected session_context_in_public_snapshot, got %v", errs)
	}
}

func TestValidatePublicRedactionRejectsTaskContextWithoutURL(t *testing.T) {
	p := minimalValidProjection()
	p.ActiveContext = &ActiveContext{Kind: ContextTask, ID: "task-1", Label: "t"}
	errs := ValidatePublicRedaction(p)
	if !hasRule(errs, "unpublic_context_in_public_snapshot") {
		t.Fatalf("expected unpublic_context_in_public_snapshot, got %v", errs)
	}
}

func TestValidatePublicRedactionAcceptsTaskContextWithURL(t *testing.T) {
	p := minimalValidProjection()
	url := "https://github.com/globulario/sensei/pull/1"
	p.ActiveContext = &ActiveContext{Kind: ContextTask, ID: "task-1", Label: "t", URL: &url}
	if errs := ValidatePublicRedaction(p); len(errs) != 0 {
		t.Fatalf("expected 0 errors, got %v", errs)
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
