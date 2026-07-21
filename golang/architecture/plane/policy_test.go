// SPDX-License-Identifier: AGPL-3.0-only

package plane

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

func TestDefaultPlanePoliciesAreStableAndComplete(t *testing.T) {
	policies, err := DefaultPolicies()
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 5 {
		t.Fatalf("policy count=%d", len(policies))
	}
	for _, want := range PlaneOrder {
		if p, ok := PolicyFor(want); !ok || p.Plane != want {
			t.Fatalf("missing policy for %s", want)
		}
	}
}

func TestPlanePolicyKeepsTruthLayerSeparate(t *testing.T) {
	for _, p := range mustPolicies(t) {
		if !p.TruthLayerIsSeparateAxis {
			t.Fatalf("%s policy collapsed truth layer into plane", p.Plane)
		}
	}
}

func TestObservedPolicyRejectsAuthoredProseOnly(t *testing.T) {
	report := assessOne(t, architecture.PlaneObserved, nil, graphNT(t, "intent", "i.current", "active", ""))
	if report.ClaimAssessments[0].PlaneState != StateInvalid {
		t.Fatalf("state=%s", report.ClaimAssessments[0].PlaneState)
	}
	requireReason(t, report, "plane.observed.authored_prose_rejected")
}

func TestEnforcedPolicyRejectsTestNameOnly(t *testing.T) {
	report := assessOneWithAbout(t, architecture.PlaneEnforced, []string{"test:TestThing"}, nil, graphNT(t, "test", "TestThing", "", ""))
	if report.ClaimAssessments[0].PlaneState != StateInvalid {
		t.Fatalf("state=%s", report.ClaimAssessments[0].PlaneState)
	}
	requireReason(t, report, "plane.enforced.test_name_only_rejected")
}

func TestIntendedPolicyRequiresGovernedNode(t *testing.T) {
	report := assessOneWithAbout(t, architecture.PlaneIntended, []string{"invariant:missing"}, nil, "")
	if report.ClaimAssessments[0].PlaneState != StateUnknown {
		t.Fatalf("state=%s", report.ClaimAssessments[0].PlaneState)
	}
	requireReason(t, report, "plane.intended.missing_governed_node")
}

func TestHistoricalPolicyAcceptsHistoricalRemovalFact(t *testing.T) {
	report := assessOne(t, architecture.PlaneHistorical, []architecture.Fact{fact("fact.hist", "historical_removal", "removed")}, "")
	if report.ClaimAssessments[0].PlaneState != StateJustified {
		t.Fatalf("state=%s", report.ClaimAssessments[0].PlaneState)
	}
	requireReason(t, report, "plane.historical.removal_fact_basis")
}

func TestDesiredPolicyRequiresExplicitAnnotation(t *testing.T) {
	report := assessOneWithAbout(t, architecture.PlaneDesired, []string{"intent:i.current"}, nil, graphNT(t, "intent", "i.current", "active", ""))
	if report.ClaimAssessments[0].PlaneState != StateInvalid {
		t.Fatalf("state=%s", report.ClaimAssessments[0].PlaneState)
	}
	requireReason(t, report, "plane.desired.missing_explicit_annotation")
}

func TestDesiredPolicyRejectsActiveIntentWithoutExplicitDesired(t *testing.T) {
	report := assessOneWithAbout(t, architecture.PlaneDesired, []string{"intent:i.current"}, nil, graphNT(t, "intent", "i.current", "active", ""))
	requireReason(t, report, "plane.desired.implicit_intent_rejected")
}

func TestInvariantAcceptsIntendedPlane(t *testing.T) {
	if err := ValidateGovernedPlaneAnnotation("invariant", "active", architecture.PlaneIntended, nil); err != nil {
		t.Fatal(err)
	}
}

func TestInvariantRejectsDesiredPlane(t *testing.T) {
	if err := ValidateGovernedPlaneAnnotation("invariant", "active", architecture.PlaneDesired, nil); err == nil {
		t.Fatal("expected desired invariant rejection")
	}
}

func TestContractAcceptsDesiredPlane(t *testing.T) {
	if err := ValidateGovernedPlaneAnnotation("contract", "active", architecture.PlaneDesired, nil); err != nil {
		t.Fatal(err)
	}
}

func TestDecisionAcceptsDesiredPlane(t *testing.T) {
	if err := ValidateGovernedPlaneAnnotation("decision", "active", architecture.PlaneDesired, nil); err != nil {
		t.Fatal(err)
	}
}

func TestIntentAcceptsDesiredPlane(t *testing.T) {
	if err := ValidateGovernedPlaneAnnotation("intent", "active", architecture.PlaneDesired, nil); err != nil {
		t.Fatal(err)
	}
}

func TestGovernedNodeRejectsObservedPlane(t *testing.T) {
	if err := ValidateGovernedPlaneAnnotation("intent", "active", architecture.PlaneObserved, nil); err == nil {
		t.Fatal("expected observed rejection")
	}
}

func TestGovernedNodeRejectsEnforcedPlane(t *testing.T) {
	if err := ValidateGovernedPlaneAnnotation("contract", "active", architecture.PlaneEnforced, nil); err == nil {
		t.Fatal("expected enforced rejection")
	}
}

func TestHistoricalPlaneRequiresHistoricalStatusOrSupersession(t *testing.T) {
	if err := ValidateGovernedPlaneAnnotation("decision", "active", architecture.PlaneHistorical, nil); err == nil {
		t.Fatal("expected active historical annotation rejection")
	}
	if err := ValidateGovernedPlaneAnnotation("decision", "active", architecture.PlaneHistorical, []string{"decision:new"}); err != nil {
		t.Fatal(err)
	}
}

func TestIntendedPlaneRejectsRetiredNode(t *testing.T) {
	if err := ValidateGovernedPlaneAnnotation("contract", "retired", architecture.PlaneIntended, nil); err == nil {
		t.Fatal("expected retired intended rejection")
	}
}

func TestDesiredPlaneRejectsSupersededNode(t *testing.T) {
	if err := ValidateGovernedPlaneAnnotation("intent", "active", architecture.PlaneDesired, []string{"intent:new"}); err == nil {
		t.Fatal("expected superseded desired rejection")
	}
}

func mustPolicies(t *testing.T) []Policy {
	t.Helper()
	policies, err := DefaultPolicies()
	if err != nil {
		t.Fatal(err)
	}
	return policies
}
