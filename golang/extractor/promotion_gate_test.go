// SPDX-License-Identifier: AGPL-3.0-only

package extractor

// Unit tests for the candidate promotion quality gate. The validators are pure
// functions over the parsed YAML structs, so these tests use synthetic nodes —
// asserting the gate flags the shapes that would dilute the graph and passes
// the ones that earn promotion.

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/adoption"
)

func ruleSet(vs []PromotionViolation) map[string]bool {
	out := map[string]bool{}
	for _, v := range vs {
		out[v.Rule] = true
	}
	return out
}

// A candidate intent that reaches `active` without a related link or a trigger
// fails validation.
func TestIntentPromotion_ActiveMissingLinksFails(t *testing.T) {
	i := yamlIntent{
		ID:      "x.candidate",
		Level:   "principle",
		Title:   "Some principle",
		Receipt: adoption.Receipt{Status: "active"},
		// no activation_triggers, no bad_smells, no related links
	}
	got := ruleSet(validateIntentPromotion(i, "docs/intent/x.yaml"))
	if !got["missing_trigger_or_smell"] {
		t.Error("expected missing_trigger_or_smell violation")
	}
	if !got["missing_related_link"] {
		t.Error("expected missing_related_link violation")
	}
}

// A well-formed active intent passes.
func TestIntentPromotion_WellFormedActivePasses(t *testing.T) {
	i := yamlIntent{
		ID:                 "x.good",
		Level:              "principle",
		Title:              "A grounded principle",
		Receipt:            adoption.Receipt{Status: "active"},
		ActivationTriggers: []string{"editing the thing"},
		RelatedInvariants:  []string{"some.invariant"},
	}
	if vs := validateIntentPromotion(i, "docs/intent/x.yaml"); len(vs) != 0 {
		t.Errorf("well-formed active intent should pass, got %v", vs)
	}
}

// A bad_smell satisfies the trigger requirement; a zooms_out_to edge satisfies
// the related-link requirement.
func TestIntentPromotion_BadSmellAndZoomEdgeSatisfy(t *testing.T) {
	i := yamlIntent{
		ID:         "x.smell",
		Level:      "principle",
		Title:      "Smell-driven",
		Receipt:    adoption.Receipt{Status: "accepted"},
		BadSmells:  []string{"the bad thing happens"},
		ZoomsOutTo: []string{"x.parent"},
	}
	if vs := validateIntentPromotion(i, "p.yaml"); len(vs) != 0 {
		t.Errorf("bad_smell + zoom edge should satisfy the gate, got %v", vs)
	}
}

// Candidate / seed status is not gated — those are deliberately un-promoted.
func TestIntentPromotion_CandidateStatusNotGated(t *testing.T) {
	for _, st := range []string{"extracted_candidate", "seed", "proposed", "learned_from_incident"} {
		i := yamlIntent{ID: "x", Level: "principle", Title: "t", Receipt: adoption.Receipt{Status: st}}
		if vs := validateIntentPromotion(i, "p.yaml"); len(vs) != 0 {
			t.Errorf("status %q must not be gated, got %v", st, vs)
		}
	}
}

// An active implementation pattern without an activation trigger fails.
func TestImplementationPatternPromotion_NoTriggerFails(t *testing.T) {
	p := yamlImplementationPattern{
		ID:     "globular.pattern.x",
		Class:  "ImplementationPattern",
		Label:  "X",
		Status: "active",
		// no when_to_use
		MustFollow:     []string{"do the thing"},
		ReferenceFiles: []yamlImplementationPatternReference{{Path: "a.go", Role: "ref"}},
	}
	got := ruleSet(validateImplementationPatternPromotion(p, "p.yaml"))
	if !got["missing_activation_trigger"] {
		t.Errorf("expected missing_activation_trigger, got %v", got)
	}
}

// An active pattern missing must_follow and reference fails on both.
func TestImplementationPatternPromotion_MissingMustFollowAndReference(t *testing.T) {
	p := yamlImplementationPattern{
		ID:        "globular.pattern.y",
		Class:     "ImplementationPattern",
		Label:     "Y",
		Status:    "active",
		WhenToUse: []string{"when y"},
		// no must_follow, no reference, no rationale
	}
	got := ruleSet(validateImplementationPatternPromotion(p, "p.yaml"))
	if !got["missing_must_follow"] {
		t.Error("expected missing_must_follow")
	}
	if !got["missing_reference"] {
		t.Error("expected missing_reference")
	}
}

// A rationale stands in for a missing reference file (explicit reason).
func TestImplementationPatternPromotion_RationaleSubstitutesForReference(t *testing.T) {
	p := yamlImplementationPattern{
		ID:         "globular.pattern.z",
		Class:      "ImplementationPattern",
		Label:      "Z",
		Status:     "active",
		WhenToUse:  []string{"when z"},
		MustFollow: []string{"do z"},
		Rationale:  "no reference file exists yet; this is a novel pattern",
	}
	got := ruleSet(validateImplementationPatternPromotion(p, "p.yaml"))
	if got["missing_reference"] {
		t.Errorf("rationale should satisfy the reference requirement, got %v", got)
	}
}

// A well-formed active pattern passes.
func TestImplementationPatternPromotion_WellFormedPasses(t *testing.T) {
	p := yamlImplementationPattern{
		ID:             "globular.pattern.ok",
		Class:          "ImplementationPattern",
		Label:          "OK",
		Status:         "active",
		WhenToUse:      []string{"when ok"},
		MustFollow:     []string{"follow this"},
		ReferenceFiles: []yamlImplementationPatternReference{{Path: "a.go", Role: "ref"}},
	}
	if vs := validateImplementationPatternPromotion(p, "p.yaml"); len(vs) != 0 {
		t.Errorf("well-formed active pattern should pass, got %v", vs)
	}
}

// Deprecated / superseded nodes are allowed to be incomplete, but a superseded
// node with no note at all is flagged so its retirement is explained.
func TestPromotion_RetiredAllowedIncompleteButNotedWhenEmpty(t *testing.T) {
	// Superseded pattern with no rationale -> flagged (soft) for a note.
	bare := yamlImplementationPattern{ID: "globular.pattern.old", Class: "ImplementationPattern", Status: "superseded"}
	if got := ruleSet(validateImplementationPatternPromotion(bare, "p.yaml")); !got["retired_without_note"] {
		t.Errorf("superseded pattern with no note should be flagged, got %v", got)
	}
	// Superseded pattern WITH a supersession note -> allowed incomplete.
	noted := yamlImplementationPattern{ID: "globular.pattern.old", Class: "ImplementationPattern", Status: "deprecated",
		Rationale: "superseded by globular.pattern.new"}
	if vs := validateImplementationPatternPromotion(noted, "p.yaml"); len(vs) != 0 {
		t.Errorf("deprecated pattern with a note should be allowed incomplete, got %v", vs)
	}
	// Deprecated intent that links a successor is allowed incomplete.
	dep := yamlIntent{ID: "x.old", Level: "principle", Receipt: adoption.Receipt{Status: "deprecated"}, RelatedTo: []string{"x.new"}}
	if vs := validateIntentPromotion(dep, "p.yaml"); len(vs) != 0 {
		t.Errorf("deprecated intent linking a successor should pass, got %v", vs)
	}
}

// An active repair plan must declare applicability, a precondition, a
// verification step, an approval gate (when high blast radius), and a binding.
func TestActiveRepairPlanValidation(t *testing.T) {
	// Bare active plan -> fails on every required field.
	bare := yamlRepairPlan{ID: "globular.repair.bad", Class: "RepairPlan", Label: "Bad", Status: "active", BlastRadius: "cluster"}
	got := ruleSet(validateRepairPlanPromotion(bare, "r.yaml"))
	for _, want := range []string{"missing_applicability", "missing_precondition", "missing_verification", "high_risk_without_gate", "missing_binding"} {
		if !got[want] {
			t.Errorf("bare active plan: expected %q, got %v", want, got)
		}
	}

	// Well-formed active plan -> passes.
	good := yamlRepairPlan{
		ID: "globular.repair.good", Class: "RepairPlan", Label: "Good", Status: "active",
		BlastRadius: "cluster", ApprovalGate: "human_approval_required",
		RepairsFindingClasses:     []string{"x.stuck"},
		Preconditions:             []string{"check the owner"},
		VerificationSteps:         []string{"re-check"},
		AppliesToAuthorityDomains: []string{"authority_domain:authority.x"},
	}
	if vs := validateRepairPlanPromotion(good, "r.yaml"); len(vs) != 0 {
		t.Errorf("well-formed active plan should pass, got %v", vs)
	}

	// A low blast-radius plan does not need an approval gate.
	lowRisk := yamlRepairPlan{
		ID: "globular.repair.low", Class: "RepairPlan", Label: "Low", Status: "active",
		BlastRadius:              "local",
		RepairsFindingClasses:    []string{"x.minor"},
		Preconditions:            []string{"p"},
		VerificationSteps:        []string{"v"},
		MustNotViolateInvariants: []string{"invariant:x"},
	}
	if got := ruleSet(validateRepairPlanPromotion(lowRisk, "r.yaml")); got["high_risk_without_gate"] {
		t.Errorf("local blast-radius plan must not require an approval gate, got %v", got)
	}

	// Candidate status is not gated.
	cand := yamlRepairPlan{ID: "globular.repair.cand", Class: "RepairPlan", Status: "draft"}
	if vs := validateRepairPlanPromotion(cand, "r.yaml"); len(vs) != 0 {
		t.Errorf("draft repair plan must not be gated, got %v", vs)
	}
}

// Empty status defaults to promoted: an authored node with no status is live
// and must meet the bar.
func TestPromotion_EmptyStatusTreatedAsPromoted(t *testing.T) {
	i := yamlIntent{ID: "x.nostatus", Level: "principle", Title: "t"} // no status, no trigger, no link
	if vs := validateIntentPromotion(i, "p.yaml"); len(vs) == 0 {
		t.Error("empty-status intent must be gated (treated as promoted)")
	}
}
