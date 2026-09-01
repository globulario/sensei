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

// Risk classification must see the complete live anchor set.
//
// classifyRisk builds a keyword haystack over the anchors it is given and
// returns DATA_LOSS_RISK or SECURITY_RISK from it, and assessChangeRisk turns
// those into human_approval_required. Classifying from the capped response view
// therefore let a presentation limit drop a security classification: severity
// sorting keeps the most severe anchors, but the keyword rules are not
// severity-driven, so a lower-severity security anchor behind more severe ones
// was never examined.
func TestRiskClassificationSeesAnchorsThatCapsHide(t *testing.T) {
	var facts []store.ImpactFact
	// Three critical anchors with no risk keywords, which will fill the cap.
	for _, id := range []string{"aaa.plain", "bbb.plain", "ccc.plain"} {
		facts = append(facts, statusAnchorFacts(rdf.ClassInvariant, id, "plain "+id, "critical", "active")...)
	}
	// One lower-severity anchor that IS a security concern.
	facts = append(facts, statusAnchorFacts(rdf.ClassInvariant,
		"zzz.rbac.enforcement", "rbac enforcement rule", "high", "active")...)

	s := newPreflightTestServer(t, map[string][]store.ImpactFact{
		"golang/workflow/engine.go": facts,
	}, false)
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "adjust the workflow engine",
		Files: []string{"golang/workflow/engine.go"},
		Mode:  awarenesspb.PreflightMode_PREFLIGHT_COMPACT,
	})
	if err != nil {
		t.Fatal(err)
	}

	var shown bool
	for _, n := range resp.GetDirectInvariants() {
		if n.GetId() == "zzz.rbac.enforcement" {
			shown = true
		}
	}
	if shown {
		t.Skip("the response showed the security anchor; this fixture no longer exercises the cap")
	}
	if resp.GetRiskClass() != awarenesspb.RiskClass_SECURITY_RISK {
		t.Fatalf("a security anchor hidden by the display cap did not classify the risk: %v", resp.GetRiskClass())
	}
}

// Coverage and risk classification must see every matched pattern, not the one
// the compact response shows.
//
// caps.patterns is 1 in compact mode, and both computePreflightCoverage and
// classifyRisk read the pattern list: coverage counts a "strong" match as
// sufficient, and classifyRisk's anyStrongOrMediumPattern feeds the verdict. A
// strong pattern ranked second was therefore invisible to both.
func TestPatternDrivenSignalsSeeEveryMatchedPattern(t *testing.T) {
	weak := &awarenesspb.MatchedImplementationPattern{Id: "weak.one", MatchStrength: "weak"}
	strong := &awarenesspb.MatchedImplementationPattern{Id: "strong.two", MatchStrength: "strong"}
	all := []*awarenesspb.MatchedImplementationPattern{weak, strong}

	// The display cap keeps only the first.
	shown := all
	if len(shown) > 1 {
		shown = shown[:1]
	}
	if len(shown) != 1 || shown[0].GetId() != "weak.one" {
		t.Fatalf("fixture no longer models the cap: %+v", shown)
	}

	if anyStrongOrMediumPattern(shown) {
		t.Fatal("fixture is wrong: the shown subset already contains a strong match")
	}
	if !anyStrongOrMediumPattern(all) {
		t.Fatal("the complete set does not contain a strong match")
	}

	// Coverage reports a strong match as guidance rather than as coverage --
	// which is this same law, already correctly applied there -- so what changes
	// is the note, not sufficiency. The note must still be able to see it.
	if note := computePreflightCoverage([]string{"a.go"}, 0, nil, nil, nil, all).GetNote(); !strings.Contains(note, "strong-tier") {
		t.Fatalf("a strong pattern beyond the display cap was invisible to coverage: %q", note)
	}
	if note := computePreflightCoverage([]string{"a.go"}, 0, nil, nil, nil, shown).GetNote(); strings.Contains(note, "strong-tier") {
		t.Fatalf("fixture is wrong: the shown subset already reports a strong tier: %q", note)
	}
}

// Confidence is a decision, and it was reading the projection.
//
// computeConfidence returns HIGH at three or more direct anchors. Compact mode
// caps failure modes at TWO. So a subject holding three live failure-mode
// anchors was HIGH by the truth and MEDIUM by the response view -- a governance
// signal degraded by a display constant, found by review while the branch that
// records that very law was in flight (#318 review).
func TestConfidenceCountsEveryLiveAnchorNotTheShownOnes(t *testing.T) {
	var facts []store.ImpactFact
	for _, id := range []string{"fm.one", "fm.two", "fm.three"} {
		facts = append(facts, anchorFacts(rdf.ClassFailureMode, id, "Live failure mode", "high")...)
	}
	s := newPreflightTestServer(t, map[string][]store.ImpactFact{
		"golang/workflow/engine.go": facts,
	}, false)
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "adjust workflow resume behaviour",
		Files: []string{"golang/workflow/engine.go"},
		Mode:  awarenesspb.PreflightMode_PREFLIGHT_COMPACT,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Guard: if the cap is not actually biting, the test proves nothing.
	if len(resp.GetDirectFailureModes()) >= 3 {
		t.Fatalf("the display cap did not bind (%d shown), so this asserts nothing",
			len(resp.GetDirectFailureModes()))
	}
	if got := resp.GetConfidence(); got != awarenesspb.Confidence_CONFIDENCE_HIGH {
		t.Fatalf("three live anchors, %d shown: confidence %v -- a display cap decided a governance signal",
			len(resp.GetDirectFailureModes()), got)
	}
}
