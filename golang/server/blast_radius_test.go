// SPDX-License-Identifier: AGPL-3.0-only

package main

// Phase 2F tests: Preflight gives a clear blast-radius + approval-gate signal.
// Real-corpus cases run against the embedded seed; the unknown-authority case
// uses an empty store on a high-risk path.

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/store"
)

func changeRiskLine(resp *awarenesspb.PreflightResponse) string {
	for _, a := range resp.GetRequiredActions() {
		if strings.HasPrefix(a, "Change risk:") {
			return a
		}
	}
	return ""
}

func preflightFile(t *testing.T, st store.Store, task, file string) *awarenesspb.PreflightResponse {
	t.Helper()
	invalidateImplementationPatternCacheForTest()
	invalidateAuthorityDomainCacheForTest()
	invalidateIntentTriggerCacheForTest()
	invalidateRepairPlanCacheForTest()
	invalidateRuntimeEvidenceCacheForTest()
	s := newTestServer(st)
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task: task, Files: []string{file}, Mode: awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	return resp
}

func TestBlastRadiusForRepositoryPublishIsClusterOrService(t *testing.T) {
	resp := preflightFile(t, newEmbeddedSeedStore(),
		"change repository publish workflow installability behavior",
		"golang/repository/repository_server/publish_workflow.go")
	line := changeRiskLine(resp)
	if !strings.Contains(line, "blast=cluster") && !strings.Contains(line, "blast=service") {
		t.Errorf("repository publish change should be cluster/service blast; got %q", line)
	}
}

func TestRBACChangeRequiresHumanApproval(t *testing.T) {
	resp := preflightFile(t, newEmbeddedSeedStore(),
		"change RBAC access validation",
		"golang/rbac/rbac_server/rbac_access.go")
	line := changeRiskLine(resp)
	if !strings.Contains(line, "approval=human_approval_required") &&
		!strings.Contains(line, "approval=multi_step_approval_required") &&
		!strings.Contains(line, "approval=manual_only") {
		t.Errorf("RBAC change should require human approval; got %q", line)
	}
}

func TestLowRiskHelperDoesNotRequireApproval(t *testing.T) {
	resp := preflightFile(t, newEmbeddedSeedStore(),
		"tweak echo helper logging",
		"golang/echo/echo_server/echo.go")
	line := changeRiskLine(resp)
	if !strings.Contains(line, "approval=none") {
		t.Errorf("low-risk helper should need no approval; got %q", line)
	}
}

func TestUnknownAuthorityEscalatesApproval(t *testing.T) {
	// nopStore: no anchors, no authority domains — but the file is under a
	// high-risk directory (mcp) not covered by any authority domain.
	resp := preflightFile(t, nopStore{}, "edit an mcp tool",
		"golang/mcp/mcp_server/tools.go")
	line := changeRiskLine(resp)
	if strings.Contains(line, "approval=none") {
		t.Errorf("unknown authority on a high-risk file should escalate approval; got %q", line)
	}
}

// ---------------------------------------------------------------------------
// Repair-plan applicability.
//
// A matched repair plan is authored and trustworthy ABOUT ITSELF. Whether it
// applies to the subject being asked about is a different question, and a match
// -- which is partly made from task prose -- does not answer it. These tests
// pin the difference, because bump() only ever escalates: an unrelated plan
// that votes has the last word on an authority it knows nothing about.
// ---------------------------------------------------------------------------

// inv builds a class-scoped anchor set holding only invariants, which is what
// most of these cases need.
func inv(ids ...string) subjectAnchors { return newSubjectAnchors(ids, nil, nil, nil) }

// clusterPlan is the shape that caused the defect: authored labels that are
// perfectly correct about a subject somewhere else.
func clusterPlan(preserves ...string) loadedRepairPlan {
	return loadedRepairPlan{
		ID:                  "globular.repair.doctor_finding_requires_remediation",
		BlastRadius:         "cluster",
		ApprovalGate:        "human_approval_required",
		Confidence:          "high",
		PreservedInvariants: preserves,
	}
}

// The reproducer from sensei#317: same files, same anchors, an unrelated plan
// matched from task prose. It must not decide how far this change reaches.
func TestUnrelatedRepairPlanDoesNotDecideAuthority(t *testing.T) {
	got := assessChangeRisk(
		[]string{"internal/workflow/engine.go", "internal/workflow/engine_test.go"},
		nil,
		[]loadedRepairPlan{clusterPlan("globular.cluster_doctor.leader_elects_before_remediation")},
		awarenesspb.RiskClass_UNKNOWN_IMPACT,
		true, true,
		inv("invariant:sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary"),
	)
	if got.BlastRadius != "local" || got.ApprovalGate != "none" {
		t.Fatalf("an unrelated repair plan set this change's authority: blast=%s approval=%s reasons=%v",
			got.BlastRadius, got.ApprovalGate, got.Reasons)
	}
}

// The positive control: a plan that names an anchor this subject actually
// produced is applicable, and must keep its vote.
func TestApplicableRepairPlanStillEscalates(t *testing.T) {
	got := assessChangeRisk(
		[]string{"internal/workflow/engine.go"},
		nil,
		[]loadedRepairPlan{clusterPlan("sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary")},
		awarenesspb.RiskClass_UNKNOWN_IMPACT,
		true, true,
		inv("invariant:sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary"),
	)
	if got.BlastRadius != "cluster" || got.ApprovalGate != "human_approval_required" {
		t.Fatalf("an applicable repair plan lost its vote: blast=%s approval=%s", got.BlastRadius, got.ApprovalGate)
	}
}

// The mutation pair, expressed as a test: the ONLY difference between these two
// calls is whether the relationship exists. Everything else -- files, plan,
// labels, coverage -- is identical.
func TestAuthorityFollowsTheRelationshipNotTheMatch(t *testing.T) {
	subject := inv("invariant:sensei_code.candidate.disposition_is_decided_and_evidence_outlives_removal")
	files := []string{"internal/workflow/engine.go"}

	related := assessChangeRisk(files, nil,
		[]loadedRepairPlan{clusterPlan("sensei_code.candidate.disposition_is_decided_and_evidence_outlives_removal")},
		awarenesspb.RiskClass_UNKNOWN_IMPACT, true, true, subject)
	unrelated := assessChangeRisk(files, nil,
		[]loadedRepairPlan{clusterPlan("globular.something.else_entirely")},
		awarenesspb.RiskClass_UNKNOWN_IMPACT, true, true, subject)

	if related.ApprovalGate != "human_approval_required" {
		t.Fatalf("adding the relationship did not restore the vote: %+v", related)
	}
	if unrelated.ApprovalGate != "none" {
		t.Fatalf("removing the relationship did not remove the vote: %+v", unrelated)
	}
}

// Insufficient coverage cannot bootstrap authority from an anchor coincidence.
// Thin coverage is already represented conservatively elsewhere, on its own
// terms; it must not be upgraded here into a borrowed cluster label.
func TestInsufficientCoverageGivesNoRepairPlanVote(t *testing.T) {
	got := assessChangeRisk(
		[]string{"internal/workflow/engine.go"},
		nil,
		[]loadedRepairPlan{clusterPlan("sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary")},
		awarenesspb.RiskClass_UNKNOWN_IMPACT,
		false, // coverage NOT sufficient
		true,
		inv("invariant:sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary"),
	)
	if got.ApprovalGate == "human_approval_required" || got.BlastRadius == "cluster" {
		t.Fatalf("a coincidental anchor bootstrapped authority on thin coverage: %+v", got)
	}
}

// A subject with no anchors of its own gives no plan anything to be applicable
// to, whatever the plan says.
func TestNoSubjectAnchorsGivesNoRepairPlanVote(t *testing.T) {
	got := assessChangeRisk(
		[]string{"internal/workflow/engine.go"},
		nil,
		[]loadedRepairPlan{clusterPlan("sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary")},
		awarenesspb.RiskClass_UNKNOWN_IMPACT,
		true, false,
		subjectAnchors{},
	)
	if got.ApprovalGate == "human_approval_required" {
		t.Fatalf("a plan applied itself to a subject with no anchors: %+v", got)
	}
}

// Applicability is per candidate, not per file: one touched file genuinely
// governed by the plan is enough for the whole change.
func TestOneApplicableFileEscalatesTheCandidate(t *testing.T) {
	got := assessChangeRisk(
		[]string{"internal/report/report.go", "internal/workflow/engine.go"},
		nil,
		[]loadedRepairPlan{clusterPlan("sensei_code.derived.future_only_a_question_cannot_authorize_the_run_that_wrote_it")},
		awarenesspb.RiskClass_UNKNOWN_IMPACT,
		true, true,
		inv("invariant:sensei_code.report.states_what_it_does_not_establish",
			"invariant:sensei_code.derived.future_only_a_question_cannot_authorize_the_run_that_wrote_it"),
	)
	if got.ApprovalGate != "human_approval_required" {
		t.Fatalf("one genuinely governed file did not escalate the candidate: %+v", got)
	}
}

// The two sides store identities differently -- plans keep bare ids, knowledge
// nodes carry a class prefix. Comparing them unnormalised would make every
// intersection empty, which looks exactly like a working fix while silently
// disabling repair-plan authority altogether.
func TestApplicabilitySurvivesTheTwoIdentityForms(t *testing.T) {
	for _, subject := range []string{
		"invariant:sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary",
		"sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary",
		"<https://globular.io/awareness#invariant/sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary>",
	} {
		got := assessChangeRisk(
			[]string{"internal/workflow/engine.go"}, nil,
			[]loadedRepairPlan{clusterPlan("sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary")},
			awarenesspb.RiskClass_UNKNOWN_IMPACT, true, true, inv(subject))
		if got.ApprovalGate != "human_approval_required" {
			t.Fatalf("subject form %q did not match the plan's bare id: %+v", subject, got)
		}
	}
}

// The prose line and the structured fields must keep coming from ONE verdict:
// the file's own invariant says a risk verdict an agent must act on is
// published as structured fields, not only as prose.
func TestProseAndStructuredComeFromTheSameVerdict(t *testing.T) {
	got := assessChangeRisk(
		[]string{"internal/workflow/engine.go"}, nil,
		[]loadedRepairPlan{clusterPlan("globular.something.else_entirely")},
		awarenesspb.RiskClass_UNKNOWN_IMPACT, true, true,
		inv("invariant:sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary"))

	line := changeAssessmentAction(got)
	if !strings.Contains(line, "blast="+got.BlastRadius) || !strings.Contains(line, "approval="+got.ApprovalGate) {
		t.Fatalf("prose line and verdict disagree: %q vs %+v", line, got)
	}
	proto := changeRiskProto(got)
	if proto == nil {
		t.Fatal("structured change_risk was not published")
	}
}

// A plan may be related to the subject through any governed class it can name,
// not only through invariants. The scorer must not discriminate by class, and
// the caller must not hand it a view that has dropped one: response caps are a
// presentation limit, and an anchor capped out of the response is still an
// anchor this subject has.
func TestApplicabilityHoldsThroughEveryGovernedClass(t *testing.T) {
	for _, c := range []struct {
		name    string
		plan    loadedRepairPlan
		subject subjectAnchors
	}{
		{"forbidden fix", loadedRepairPlan{
			ID: "p", BlastRadius: "cluster", ApprovalGate: "human_approval_required",
			ForbiddenFixes: []string{"sensei_code.adapter.reconstructs_upstream_classification"}},
			newSubjectAnchors(nil, nil, []string{"forbidden_fix:sensei_code.adapter.reconstructs_upstream_classification"}, nil)},
		{"governed contract", loadedRepairPlan{
			ID: "p", BlastRadius: "cluster", ApprovalGate: "human_approval_required",
			GovernedContracts: []string{"sensei_code.contract.admission_chain"}},
			newSubjectAnchors(nil, nil, nil, []string{"contract:sensei_code.contract.admission_chain"})},
		{"failure mode", loadedRepairPlan{
			ID: "p", BlastRadius: "cluster", ApprovalGate: "human_approval_required",
			FailureModes: []string{"sensei_code.stale_lane_failure_reported_as_current_outcome"}},
			newSubjectAnchors(nil, []string{"failure:sensei_code.stale_lane_failure_reported_as_current_outcome"}, nil, nil)},
	} {
		got := assessChangeRisk([]string{"internal/workflow/engine.go"}, nil,
			[]loadedRepairPlan{c.plan}, awarenesspb.RiskClass_UNKNOWN_IMPACT, true, true, c.subject)
		if got.ApprovalGate != "human_approval_required" {
			t.Fatalf("%s: a plan related through this class lost its vote: %+v", c.name, got)
		}
	}
}

// An identity in the WRONG class must not authorise a plan. The graph's ids are
// class-scoped, so failure mode "x" is not invariant "x", and a component named
// like a contract is not that contract.
func TestAnchorInOneClassDoesNotAuthoriseAPlanNamingAnother(t *testing.T) {
	plan := loadedRepairPlan{ID: "p", BlastRadius: "cluster", ApprovalGate: "human_approval_required",
		PreservedInvariants: []string{"shared.identity"}}
	// The subject holds "shared.identity" -- but as a FAILURE MODE.
	got := assessChangeRisk([]string{"internal/workflow/engine.go"}, nil,
		[]loadedRepairPlan{plan}, awarenesspb.RiskClass_UNKNOWN_IMPACT, true, true,
		newSubjectAnchors(nil, []string{"failure:shared.identity"}, nil, nil))
	if got.ApprovalGate == "human_approval_required" {
		t.Fatalf("a failure mode authorised a plan that preserves an invariant of the same name: %+v", got)
	}
	// Same id, right class: the vote applies.
	got = assessChangeRisk([]string{"internal/workflow/engine.go"}, nil,
		[]loadedRepairPlan{plan}, awarenesspb.RiskClass_UNKNOWN_IMPACT, true, true,
		inv("invariant:shared.identity"))
	if got.ApprovalGate != "human_approval_required" {
		t.Fatalf("the correctly-classed anchor did not authorise the plan: %+v", got)
	}
}

// A plan matched through its OWN authored scope -- covers_paths, expressed_by,
// or authority-domain membership -- has already established that it is about
// these files. Requiring an anchor intersection on top of that discards the
// plan's declared scope, and can downgrade a manual_only path to the generic
// fallback because the file happens to hold none of its anchor classes.
func TestAuthoredSubjectMatchDoesNotNeedAnAnchorIntersection(t *testing.T) {
	plan := loadedRepairPlan{ID: "p", BlastRadius: "cluster", ApprovalGate: "manual_only",
		SubjectMatched: true, PreservedInvariants: []string{"names.nothing.this.file.holds"}}
	got := assessChangeRisk([]string{"golang/cluster_doctor/server.go"}, nil,
		[]loadedRepairPlan{plan}, awarenesspb.RiskClass_UNKNOWN_IMPACT, true, true, subjectAnchors{})
	if got.ApprovalGate != "manual_only" {
		t.Fatalf("an authored covers_paths match lost its authority: %+v", got)
	}

	// The same plan reaching the request through task text only does need one.
	plan.SubjectMatched = false
	got = assessChangeRisk([]string{"golang/cluster_doctor/server.go"}, nil,
		[]loadedRepairPlan{plan}, awarenesspb.RiskClass_UNKNOWN_IMPACT, true, true, subjectAnchors{})
	if got.ApprovalGate == "manual_only" {
		t.Fatalf("a task-text-only match set authority with no anchor intersection: %+v", got)
	}
}

// subjectAnchorIDs must collect across every list it is given and drop none of
// them: the applicability decision is only as complete as its input.
func TestSubjectAnchorIDsCollectsEveryList(t *testing.T) {
	node := func(id string) *awarenesspb.KnowledgeNode { return &awarenesspb.KnowledgeNode{Id: id} }
	got := subjectAnchorIDs(
		[]*awarenesspb.KnowledgeNode{node("invariant:a")},
		[]*awarenesspb.KnowledgeNode{node("failure:b")},
		[]*awarenesspb.KnowledgeNode{node("forbidden_fix:c"), node("")},
		[]*awarenesspb.KnowledgeNode{node("contract:d")},
	)
	if len(got) != 4 {
		t.Fatalf("collected %d ids, want 4 (empties dropped, nothing else): %v", len(got), got)
	}
	for _, want := range []string{"invariant:a", "failure:b", "forbidden_fix:c", "contract:d"} {
		if !strings.Contains(strings.Join(got, " "), want) {
			t.Fatalf("%q missing from %v", want, got)
		}
	}
}

// Lifecycle is not presentation. A deprecated, superseded or retired anchor is
// not primary guidance, and must not be the thing that lets a repair plan
// decide how far a change reaches -- otherwise retired knowledge re-enables an
// authority the same response is calling out as superseded.
func TestRetiredAnchorsDoNotEstablishApplicability(t *testing.T) {
	live := &awarenesspb.KnowledgeNode{Id: "invariant:live.one", Iri: "iri:live", Status: "active"}
	retired := &awarenesspb.KnowledgeNode{Id: "invariant:retired.one", Iri: "iri:retired", Status: "retired"}
	isPrimary := func(n *awarenesspb.KnowledgeNode) bool { return n.GetStatus() != "retired" }

	kept := subjectAnchorIDs(primaryOnly([]*awarenesspb.KnowledgeNode{live, retired}, isPrimary))
	if len(kept) != 1 || kept[0] != "invariant:live.one" {
		t.Fatalf("lifecycle filtering did not drop the retired anchor: %v", kept)
	}

	// The plan is related ONLY through the retired anchor, so it must not vote.
	got := assessChangeRisk([]string{"internal/workflow/engine.go"}, nil,
		[]loadedRepairPlan{{ID: "p", BlastRadius: "cluster", ApprovalGate: "human_approval_required",
			PreservedInvariants: []string{"retired.one"}}},
		awarenesspb.RiskClass_UNKNOWN_IMPACT, true, true, inv(kept...))
	if got.ApprovalGate == "human_approval_required" {
		t.Fatalf("a retired anchor re-enabled a repair plan's authority: %+v", got)
	}

	// And a plan related through the live anchor still does.
	got = assessChangeRisk([]string{"internal/workflow/engine.go"}, nil,
		[]loadedRepairPlan{{ID: "p", BlastRadius: "cluster", ApprovalGate: "human_approval_required",
			PreservedInvariants: []string{"live.one"}}},
		awarenesspb.RiskClass_UNKNOWN_IMPACT, true, true, inv(kept...))
	if got.ApprovalGate != "human_approval_required" {
		t.Fatalf("filtering removed a live anchor as well: %+v", got)
	}
}

// primaryOnly with no predicate must not silently drop everything: an absent
// filter means "unfiltered", never "empty".
func TestPrimaryOnlyWithoutAPredicateKeepsEverything(t *testing.T) {
	nodes := []*awarenesspb.KnowledgeNode{{Id: "a"}, {Id: "b"}}
	if got := primaryOnly(nodes, nil); len(got) != 2 {
		t.Fatalf("a nil predicate dropped nodes: %v", got)
	}
}

// The origin must actually be recorded where it is known. matchRepairPlans is
// the only place that can tell an authored relationship from a prose
// resemblance, and it used to flatten both into one slice -- which is how the
// distinction was lost in the first place.
func TestMatchRepairPlansRecordsWhetherTheMatchWasAuthored(t *testing.T) {
	covering := loadedRepairPlan{ID: "covering", CoversPaths: []string{"golang/cluster_doctor/"},
		FindingClasses: []string{"doctor.finding_requires_mutation"}}
	prose := loadedRepairPlan{ID: "prose", FindingClasses: []string{"doctor.finding_requires_mutation"}}

	got := matchRepairPlans("remediate a doctor.finding_requires_mutation",
		[]string{"golang/cluster_doctor/server.go"}, nil,
		[]loadedRepairPlan{covering, prose})

	byID := map[string]loadedRepairPlan{}
	for _, p := range got {
		byID[p.ID] = p
	}
	if len(byID) != 2 {
		t.Fatalf("expected both plans matched, got %d: %+v", len(byID), got)
	}
	if !byID["covering"].SubjectMatched {
		t.Error("a covers_paths match was not recorded as authored")
	}
	if byID["prose"].SubjectMatched {
		t.Error("a task-text-only match was recorded as authored")
	}
}
