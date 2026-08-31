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
		true,
		[]string{"invariant:sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary"},
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
		true,
		[]string{"invariant:sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary"},
	)
	if got.BlastRadius != "cluster" || got.ApprovalGate != "human_approval_required" {
		t.Fatalf("an applicable repair plan lost its vote: blast=%s approval=%s", got.BlastRadius, got.ApprovalGate)
	}
}

// The mutation pair, expressed as a test: the ONLY difference between these two
// calls is whether the relationship exists. Everything else -- files, plan,
// labels, coverage -- is identical.
func TestAuthorityFollowsTheRelationshipNotTheMatch(t *testing.T) {
	subject := []string{"invariant:sensei_code.candidate.disposition_is_decided_and_evidence_outlives_removal"}
	files := []string{"internal/workflow/engine.go"}

	related := assessChangeRisk(files, nil,
		[]loadedRepairPlan{clusterPlan("sensei_code.candidate.disposition_is_decided_and_evidence_outlives_removal")},
		awarenesspb.RiskClass_UNKNOWN_IMPACT, true, subject)
	unrelated := assessChangeRisk(files, nil,
		[]loadedRepairPlan{clusterPlan("globular.something.else_entirely")},
		awarenesspb.RiskClass_UNKNOWN_IMPACT, true, subject)

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
		[]string{"invariant:sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary"},
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
		true,
		nil,
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
		true,
		[]string{
			"invariant:sensei_code.report.states_what_it_does_not_establish",
			"invariant:sensei_code.derived.future_only_a_question_cannot_authorize_the_run_that_wrote_it",
		},
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
			awarenesspb.RiskClass_UNKNOWN_IMPACT, true, []string{subject})
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
		awarenesspb.RiskClass_UNKNOWN_IMPACT, true,
		[]string{"invariant:sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary"})

	line := changeAssessmentAction(got)
	if !strings.Contains(line, "blast="+got.BlastRadius) || !strings.Contains(line, "approval="+got.ApprovalGate) {
		t.Fatalf("prose line and verdict disagree: %q vs %+v", line, got)
	}
	proto := changeRiskProto(got)
	if proto == nil {
		t.Fatal("structured change_risk was not published")
	}
}
