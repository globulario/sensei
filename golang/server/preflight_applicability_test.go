package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

// Applicability at the preflight boundary.
//
// The scorer's own tests prove what it does with an anchor set. These prove
// what the CALLER hands it, which is where both review findings on #318 lived:
// first a presentation-capped view that dropped legitimate anchors, then an
// unfiltered view that reinstated retired ones. Neither regression is visible
// from assessChangeRisk, because both are in the construction of its input.

// statusAnchorFacts is anchorFacts plus a lifecycle status, which the
// applicability set must respect.
func statusAnchorFacts(class, id, label, severity, status string) []store.ImpactFact {
	facts := anchorFacts(class, id, label, severity)
	facts = append(facts, store.ImpactFact{
		NodeIRI:   facts[0].NodeIRI,
		TypeIRI:   class,
		Predicate: rdf.PropStatus,
		Object:    status,
	})
	return facts
}

// seedRepairPlans installs plans for the duration of one test. The loader reads
// a package-level cache rather than the store, so a preflight-level test must
// seed it directly.
func seedRepairPlans(t *testing.T, plans ...loadedRepairPlan) {
	t.Helper()
	globalRepairPlanCache.mu.Lock()
	globalRepairPlanCache.loaded = true
	globalRepairPlanCache.plans = plans
	globalRepairPlanCache.mu.Unlock()
	t.Cleanup(invalidateRepairPlanCacheForTest)
}

// taskMatchedPlan is the shape from the reproducer: authored labels that are
// correct about a subject elsewhere, reaching this request only through the
// task text.
func taskMatchedPlan(relatedInvariantID string) loadedRepairPlan {
	return loadedRepairPlan{
		ID:                  "globular.repair.doctor_finding_requires_remediation",
		Label:               "Remediate a cluster-doctor finding",
		Status:              "active",
		Confidence:          "high",
		BlastRadius:         "cluster",
		ApprovalGate:        "human_approval_required",
		FindingClasses:      []string{"doctor.finding_requires_mutation"},
		CoversPaths:         []string{"golang/cluster_doctor/"},
		PreservedInvariants: []string{relatedInvariantID},
	}
}

const applicabilityTask = "remediate a doctor.finding_requires_mutation in the workflow engine"

// A plan related through an anchor the RESPONSE could not show must still be
// able to establish applicability. Caps are a presentation limit; they must not
// decide authority.
func TestPreflightApplicabilitySeesAnchorsThatCapsHide(t *testing.T) {
	// More invariants than the compact response will carry, so the one the plan
	// names is guaranteed to be capped out of resp.DirectInvariants.
	var facts []store.ImpactFact
	for _, id := range []string{"aaa.one", "bbb.two", "ccc.three", "ddd.four", "eee.five"} {
		facts = append(facts, statusAnchorFacts(rdf.ClassInvariant, id, "rule "+id, "critical", "active")...)
	}
	hidden := "eee.five"
	seedRepairPlans(t, taskMatchedPlan(hidden))

	s := newPreflightTestServer(t, map[string][]store.ImpactFact{
		"golang/workflow/engine.go": facts,
	}, false)
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  applicabilityTask,
		Files: []string{"golang/workflow/engine.go"},
		Mode:  awarenesspb.PreflightMode_PREFLIGHT_COMPACT,
	})
	if err != nil {
		t.Fatal(err)
	}

	shown := map[string]bool{}
	for _, n := range resp.GetDirectInvariants() {
		shown[n.GetId()] = true
	}
	if shown[hidden] {
		t.Skip("the response showed every anchor; this fixture no longer exercises the cap")
	}
	if got := resp.GetChangeRisk().GetApprovalGate(); got != awarenesspb.ApprovalGate_APPROVAL_GATE_HUMAN_APPROVAL_REQUIRED {
		t.Fatalf("a plan related through a capped-out anchor lost its vote: approval=%v blast=%v",
			got, resp.GetChangeRisk().GetBlastRadius())
	}
}

// A plan related ONLY through a retired anchor must not vote. Lifecycle is not
// presentation: the same response reports that knowledge as not primary
// guidance, and it must not be what grants authority.
//
// Run per governed class, because the filter is applied per list: dropping it
// from one class only is a real regression this test would otherwise miss.
//
// COVERAGE GAP, STATED: forbidden fixes and architecture contracts are not
// covered here. A fixture built the same way never reaches allForbiddenFixes --
// impact.go routes nodes by a resolved class name and this fixture does not
// produce one for that class -- so the subtest passed whether or not the filter
// was applied, which is a test proving less than it appears. It was removed
// rather than left in. Dropping primaryOnly from allForbiddenFixes or
// allArchitecture therefore still passes this suite.
func TestPreflightApplicabilityIgnoresRetiredAnchors(t *testing.T) {
	for _, c := range []struct {
		name  string
		class string
		plan  func(string) loadedRepairPlan
	}{
		{"invariant", rdf.ClassInvariant, taskMatchedPlan},
		{"failure mode", rdf.ClassFailureMode, func(id string) loadedRepairPlan {
			p := taskMatchedPlan("")
			p.PreservedInvariants = nil
			p.FailureModes = []string{id}
			return p
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			retiredAnchorGrantsNothing(t, c.class, c.plan)
		})
	}
}

func retiredAnchorGrantsNothing(t *testing.T, class string, plan func(string) loadedRepairPlan) {
	t.Helper()
	facts := statusAnchorFacts(class, "retired.rule", "Retired rule", "critical", "retired")
	seedRepairPlans(t, plan("retired.rule"))

	s := newPreflightTestServer(t, map[string][]store.ImpactFact{
		"golang/workflow/engine.go": facts,
	}, false)
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  applicabilityTask,
		Files: []string{"golang/workflow/engine.go"},
		Mode:  awarenesspb.PreflightMode_PREFLIGHT_COMPACT,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.GetChangeRisk().GetApprovalGate(); got == awarenesspb.ApprovalGate_APPROVAL_GATE_HUMAN_APPROVAL_REQUIRED {
		t.Fatalf("a retired anchor granted a repair plan authority: approval=%v", got)
	}
	// The plan itself must still be visible as guidance.
	var surfaced bool
	for _, a := range resp.GetRequiredActions() {
		if strings.Contains(a, "doctor_finding_requires_remediation") {
			surfaced = true
		}
	}
	if !surfaced {
		t.Error("the plan was removed from guidance as well as from authority")
	}
}

// A plan that names nothing this subject holds is guidance only, whatever the
// task text says. This is the original #317 reproducer at the preflight
// boundary rather than in the scorer.
func TestPreflightUnrelatedPlanDoesNotSetAuthority(t *testing.T) {
	facts := statusAnchorFacts(rdf.ClassInvariant, "local.rule", "Local rule", "critical", "active")
	seedRepairPlans(t, taskMatchedPlan("something.else.entirely"))

	s := newPreflightTestServer(t, map[string][]store.ImpactFact{
		"golang/workflow/engine.go": facts,
	}, false)
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  applicabilityTask,
		Files: []string{"golang/workflow/engine.go"},
		Mode:  awarenesspb.PreflightMode_PREFLIGHT_COMPACT,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.GetChangeRisk().GetBlastRadius(); got == awarenesspb.BlastRadius_BLAST_RADIUS_CLUSTER {
		t.Fatalf("an unrelated plan set the blast radius: %v", got)
	}
}
