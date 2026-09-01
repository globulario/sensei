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
		inv("sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary"),
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
		inv("sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary"),
	)
	if got.BlastRadius != "cluster" || got.ApprovalGate != "human_approval_required" {
		t.Fatalf("an applicable repair plan lost its vote: blast=%s approval=%s", got.BlastRadius, got.ApprovalGate)
	}
}

// The mutation pair, expressed as a test: the ONLY difference between these two
// calls is whether the relationship exists. Everything else -- files, plan,
// labels, coverage -- is identical.
func TestAuthorityFollowsTheRelationshipNotTheMatch(t *testing.T) {
	subject := inv("sensei_code.candidate.disposition_is_decided_and_evidence_outlives_removal")
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
		inv("sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary"),
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
		inv("sensei_code.report.states_what_it_does_not_establish",
			"sensei_code.derived.future_only_a_question_cannot_authorize_the_run_that_wrote_it"),
	)
	if got.ApprovalGate != "human_approval_required" {
		t.Fatalf("one genuinely governed file did not escalate the candidate: %+v", got)
	}
}

// Identity normalisation happens at the PRODUCER, once, so this comparison has
// nothing to reconcile. These pin the two ways the old downstream normaliser
// was wrong.

// A governed id may legitimately contain a colon after a simple head. The old
// heuristic read that head as a class and stripped it, so a plan naming
// "alpha:x" was authorised by a subject anchored at the distinct "beta:x".
func TestGovernedIDsContainingAColonStayDistinct(t *testing.T) {
	plan := loadedRepairPlan{ID: "p", BlastRadius: "cluster", ApprovalGate: "human_approval_required",
		PreservedInvariants: []string{"alpha:x"}}
	got := assessChangeRisk([]string{"internal/workflow/engine.go"}, nil,
		[]loadedRepairPlan{plan}, awarenesspb.RiskClass_UNKNOWN_IMPACT, true, true, inv("beta:x"))
	if got.ApprovalGate == "human_approval_required" {
		t.Fatalf("\"beta:x\" authorised a plan naming \"alpha:x\": %+v", got)
	}
	if got = assessChangeRisk([]string{"internal/workflow/engine.go"}, nil,
		[]loadedRepairPlan{plan}, awarenesspb.RiskClass_UNKNOWN_IMPACT, true, true,
		inv("alpha:x")); got.ApprovalGate != "human_approval_required" {
		t.Fatalf("the identical id did not authorise the plan: %+v", got)
	}
}

// The subject side arrives already decoded. Decoding again here turned a real
// "%2F" in an id into "/", collapsing two different identities.
func TestAnchorIDsAreNotDecodedTwice(t *testing.T) {
	if got := bareAnchorID("scope%2Fa"); got != "scope%2Fa" {
		t.Fatalf("bareAnchorID decoded an already-normalised id: %q", got)
	}
	plan := loadedRepairPlan{ID: "p", BlastRadius: "cluster", ApprovalGate: "human_approval_required",
		PreservedInvariants: []string{"scope%2Fa"}}
	got := assessChangeRisk([]string{"internal/workflow/engine.go"}, nil,
		[]loadedRepairPlan{plan}, awarenesspb.RiskClass_UNKNOWN_IMPACT, true, true, inv("scope/a"))
	if got.ApprovalGate == "human_approval_required" {
		t.Fatalf("an id whose real value is %%2F was matched against one whose real value is /: %+v", got)
	}
}

// governedID is where the plan side becomes the human spelling, so that the
// comparison above needs no decoding of its own.
func TestGovernedIDDecodesTheMintedSegment(t *testing.T) {
	got := governedID("<https://globular.io/awareness#invariant/scope%2Fa>")
	if got != "scope/a" {
		t.Fatalf("governedID = %q, want the decoded human id", got)
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
		inv("sensei_code.authority.only_an_explicit_answer_satisfies_a_boundary"))

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
			newSubjectAnchors(nil, nil, []string{"sensei_code.adapter.reconstructs_upstream_classification"}, nil)},
		{"governed contract", loadedRepairPlan{
			ID: "p", BlastRadius: "cluster", ApprovalGate: "human_approval_required",
			GovernedContracts: []string{"sensei_code.contract.admission_chain"}},
			newSubjectAnchors(nil, nil, nil, []string{"sensei_code.contract.admission_chain"})},
		{"failure mode", loadedRepairPlan{
			ID: "p", BlastRadius: "cluster", ApprovalGate: "human_approval_required",
			FailureModes: []string{"sensei_code.stale_lane_failure_reported_as_current_outcome"}},
			newSubjectAnchors(nil, []string{"sensei_code.stale_lane_failure_reported_as_current_outcome"}, nil, nil)},
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
		newSubjectAnchors(nil, []string{"shared.identity"}, nil, nil))
	if got.ApprovalGate == "human_approval_required" {
		t.Fatalf("a failure mode authorised a plan that preserves an invariant of the same name: %+v", got)
	}
	// Same id, right class: the vote applies.
	got = assessChangeRisk([]string{"internal/workflow/engine.go"}, nil,
		[]loadedRepairPlan{plan}, awarenesspb.RiskClass_UNKNOWN_IMPACT, true, true,
		inv("shared.identity"))
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
	live := &awarenesspb.KnowledgeNode{Id: "live.one", Iri: "iri:live", Status: "active"}
	retired := &awarenesspb.KnowledgeNode{Id: "retired.one", Iri: "iri:retired", Status: "retired"}
	isPrimary := func(n *awarenesspb.KnowledgeNode) bool { return n.GetStatus() != "retired" }

	kept := subjectAnchorIDs(primaryOnly([]*awarenesspb.KnowledgeNode{live, retired}, isPrimary))
	if len(kept) != 1 || kept[0] != "live.one" {
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

// MintIRI percent-encodes angle brackets, and both decoders decode them, so a
// decoded bare identity may legally contain "<" or ">". Treating them as IRI
// wrappers reduced the id "<x>" to "x" and let a plan naming the distinct "x"
// claim an anchor that was never its own.
func TestAngleBracketsAreNotStrippedFromAnIdentity(t *testing.T) {
	if got := bareAnchorID("<x>"); got != "<x>" {
		t.Fatalf("bareAnchorID stripped brackets that are part of the id: %q", got)
	}
	plan := loadedRepairPlan{ID: "p", BlastRadius: "cluster", ApprovalGate: "human_approval_required",
		PreservedInvariants: []string{"x"}}
	got := assessChangeRisk([]string{"internal/workflow/engine.go"}, nil,
		[]loadedRepairPlan{plan}, awarenesspb.RiskClass_UNKNOWN_IMPACT, true, true, inv("<x>"))
	if got.ApprovalGate == "human_approval_required" {
		t.Fatalf("the identity \"<x>\" authorised a plan naming \"x\": %+v", got)
	}
}

// maxRepairPlansSurfaced is a DISPLAY limit. Applying it before scoring decided
// which plans were allowed to influence authority: with three task matches, the
// two shown could fail the intersection while the third -- naming a live anchor
// and requiring manual_only -- was discarded before anything could score it.
func TestMatchingReturnsEveryPlanAndSurfacingIsWhatCaps(t *testing.T) {
	plans := []loadedRepairPlan{
		{ID: "first", FindingClasses: []string{"doctor.finding_requires_mutation"}},
		{ID: "second", FindingClasses: []string{"doctor.finding_requires_mutation"}},
		{ID: "third", FindingClasses: []string{"doctor.finding_requires_mutation"},
			BlastRadius: "cluster", ApprovalGate: "manual_only",
			PreservedInvariants: []string{"live.anchor"}},
	}
	matched := matchRepairPlans("remediate a doctor.finding_requires_mutation",
		[]string{"internal/workflow/engine.go"}, nil, plans)
	if len(matched) != 3 {
		t.Fatalf("matching capped its own result: %d plans", len(matched))
	}
	if got := len(surfacedRepairPlans(matched)); got != maxRepairPlansSurfaced {
		t.Fatalf("surfacing did not cap: %d", got)
	}

	// The third plan is the one that decides, and it is past the display cap.
	got := assessChangeRisk([]string{"internal/workflow/engine.go"}, nil, matched,
		awarenesspb.RiskClass_UNKNOWN_IMPACT, true, true, inv("live.anchor"))
	if got.ApprovalGate != "manual_only" {
		t.Fatalf("a plan beyond the display cap lost its vote: %+v", got)
	}
}
