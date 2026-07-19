// SPDX-License-Identifier: AGPL-3.0-only

package taskcontrol

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/probe"
)

func controlFixture() Inputs {
	binding := architecture.ClaimDocumentBinding{RepositoryDomain: "github.com/example/repo", Revision: "abc", RevisionStatus: architecture.RevisionResolved, GraphDigestSHA256: "graph", GraphDigestStatus: architecture.GraphDigestResolved}
	claim := architecture.Claim{ID: "claim.one", Statement: architecture.ClaimStatement{Subject: "router", Predicate: "uses", Object: "tree"}, Scope: architecture.ClaimScope{Files: []string{"router.go"}}, ArchitecturalPlane: architecture.PlaneObserved}
	q := architecture.OpenQuestion{ID: "question.one", QuestionText: "Which evidence proves the route?", BlocksClosureDimension: closure.DimensionEvidence, BlocksClaims: []string{"claim.one"}, BlocksClosureBlockers: []string{"blocker.evidence.aaaaaaaaaaaa"}, AcceptedAnswerTypes: []string{architecture.AnswerTypeEvidencePointer}, Priority: architecture.QuestionPriorityHigh, Status: architecture.QuestionStatusAwaitingEvidence}
	p := probe.EvidenceProbe{ID: "probe.one", QuestionID: q.ID, ClosureBlockerIDs: q.BlocksClosureBlockers, ProbeKind: probe.KindSourceReceiptVerification, SafetyClass: probe.SafetyStaticRead, ApprovalGate: probe.GateNone, AutomaticExecutionAllowed: true, Status: probe.StatusProposed}
	return Inputs{
		TaskID: "task.one", Binding: binding, BindingHealthy: true, Permission: PermissionSummary{Inspect: "admitted", Modify: "waiting"},
		Claims:   architecture.ClaimDocument{Binding: binding, Claims: []architecture.Claim{claim}},
		Dialogue: architecture.DialogueDocument{Binding: binding, OpenQuestions: []architecture.OpenQuestion{q}},
		Probes:   probe.ProbeDocument{Binding: binding, Probes: []probe.EvidenceProbe{p}},
		Closure:  closure.Report{Dimensions: []closure.DimensionAssessment{{Dimension: closure.DimensionEvidence, Required: true, Applicable: true, State: closure.StateOpen}}, Blockers: []closure.Blocker{{ID: "blocker.evidence.aaaaaaaaaaaa", Dimension: closure.DimensionEvidence, Severity: "high", Code: "closure.evidence.missing", Summary: "route evidence is missing", ClaimIDs: []string{"claim.one"}, QuestionIDs: []string{q.ID}, RequiredNextAction: "add_evidence"}}},
	}
}

func TestStaticProbeAnswerableQuestionIsClassified(t *testing.T) {
	state, err := Project(controlFixture())
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Questions[0].ResolutionClass; got != ClassStaticProbeAnswerable {
		t.Fatalf("class=%s", got)
	}
	if state.NextAction.Kind != ActionRunStaticEvidence {
		t.Fatalf("next=%s", state.NextAction.Kind)
	}
}

func TestCompletedProbeIsNotOfferedForAutomaticExecution(t *testing.T) {
	in := controlFixture()
	in.Results = &probe.ResultDocument{Binding: in.Binding, Results: []probe.ProbeResult{{ID: "probe-result.one", ProbeID: "probe.one", QuestionID: "question.one", ResultStatus: probe.ResultCompleted}}}
	state, err := Project(in)
	if err != nil {
		t.Fatal(err)
	}
	if state.Questions[0].ResolutionClass == ClassStaticProbeAnswerable || state.Evidence.Eligible != 0 {
		t.Fatalf("completed probe remained eligible: question=%+v evidence=%+v", state.Questions[0], state.Evidence)
	}
}

func TestMechanicallyAnswerableQuestionIsNotSentToArchitect(t *testing.T) {
	in := controlFixture()
	in.Dialogue.OpenQuestions[0].Status = architecture.QuestionStatusResolved
	in.Probes.Probes = nil
	state, err := Project(in)
	if err != nil {
		t.Fatal(err)
	}
	if state.Questions[0].RequiredActor == "architect" || state.Questions[0].ResolutionClass != ClassMechanicallyAnswerable {
		t.Fatalf("question=%+v", state.Questions[0])
	}
}

func TestArchitectJudgementRequiresNormativeGap(t *testing.T) {
	in := controlFixture()
	in.Probes.Probes = nil
	in.Dialogue.OpenQuestions[0].ArchitectRequired = true
	in.Dialogue.OpenQuestions[0].AcceptedAnswerTypes = []string{architecture.AnswerTypeDesiredDirection}
	state, err := Project(in)
	if err != nil {
		t.Fatal(err)
	}
	if state.Questions[0].ResolutionClass != ClassArchitectJudgementRequired {
		t.Fatalf("question=%+v", state.Questions[0])
	}
}

func TestNonBlockingUnknownDoesNotBlockAdmission(t *testing.T) {
	in := controlFixture()
	in.Probes.Probes = nil
	in.Closure.Dimensions[0].Required = false
	state, err := Project(in)
	if err != nil {
		t.Fatal(err)
	}
	if state.Blockers[0].Disposition != ClassNonBlockingUnknown || state.Summary.ActiveRootBlockers != 0 {
		t.Fatalf("state=%+v", state.Summary)
	}
}

func TestUncertifiableClassificationRemainsVisible(t *testing.T) {
	in := controlFixture()
	in.BindingHealthy = false
	in.BindingErrors = []string{"task.binding.graph_digest_mismatch"}
	state, err := Project(in)
	if err != nil {
		t.Fatal(err)
	}
	if state.Blockers[0].Disposition != ClassUncertifiable || len(state.Blockers) != 1 {
		t.Fatalf("blockers=%+v", state.Blockers)
	}
}

func TestDuplicateBlockersMergeReceipts(t *testing.T) {
	in := controlFixture()
	dup := in.Closure.Blockers[0]
	dup.ID = "blocker.evidence.bbbbbbbbbbbb"
	in.Closure.Blockers = append(in.Closure.Blockers, dup)
	state, err := Project(in)
	if err != nil {
		t.Fatal(err)
	}
	if state.Summary.Duplicate != 1 || state.Summary.TotalBlockers != 2 {
		t.Fatalf("summary=%+v", state.Summary)
	}
}

func TestActiveRootCountUsesStableGroups(t *testing.T) {
	in := controlFixture()
	other := in.Closure.Blockers[0]
	other.ID = "blocker.evidence.bbbbbbbbbbbb"
	other.Files = []string{"other.go"}
	in.Closure.Blockers = append(in.Closure.Blockers, other)
	state, err := Project(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Groups) != 1 || state.Summary.ActiveRootBlockers != 1 {
		t.Fatalf("groups=%d roots=%d", len(state.Groups), state.Summary.ActiveRootBlockers)
	}
}

func TestDifferentScopesAreNotDuplicates(t *testing.T) {
	in := controlFixture()
	other := in.Closure.Blockers[0]
	other.ID = "blocker.evidence.bbbbbbbbbbbb"
	other.Files = []string{"other.go"}
	in.Closure.Blockers = append(in.Closure.Blockers, other)
	state, err := Project(in)
	if err != nil {
		t.Fatal(err)
	}
	if state.Summary.Duplicate != 0 {
		t.Fatalf("summary=%+v", state.Summary)
	}
}

func TestDominanceTransitiveRootIsStable(t *testing.T) {
	in := controlFixture()
	b := in.Closure.Blockers[0]
	b.ID = "blocker.evidence.bbbbbbbbbbbb"
	b.Code = "b"
	c := in.Closure.Blockers[0]
	c.ID = "blocker.evidence.cccccccccccc"
	c.Code = "c"
	in.Closure.Blockers = append(in.Closure.Blockers, b, c)
	in.DominanceEdges = []DominanceEdge{{DominatorID: in.Closure.Blockers[0].ID, DominatedID: b.ID, ReasonCode: "explicit"}, {DominatorID: b.ID, DominatedID: c.ID, ReasonCode: "explicit"}}
	state, err := Project(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, blocker := range state.Blockers {
		if blocker.ID == c.ID && blocker.DominatorID != in.Closure.Blockers[0].ID {
			t.Fatalf("dominator=%s", blocker.DominatorID)
		}
	}
}

func TestDominatedQuestionReferencesDominantQuestion(t *testing.T) {
	in := controlFixture()
	root := in.Closure.Blockers[0]
	root.QuestionIDs = []string{"question.root"}
	in.Dialogue.OpenQuestions = append(in.Dialogue.OpenQuestions, architecture.OpenQuestion{
		ID: "question.root", QuestionText: "root", BlocksClosureBlockers: []string{root.ID}, Priority: architecture.QuestionPriorityHigh,
	})
	dominated := root
	dominated.ID = "blocker.evidence.bbbbbbbbbbbb"
	dominated.Code = "dependent"
	dominated.QuestionIDs = []string{"question.dependent"}
	in.Closure.Blockers = []closure.Blocker{root, dominated}
	in.Dialogue.OpenQuestions = append(in.Dialogue.OpenQuestions, architecture.OpenQuestion{
		ID: "question.dependent", QuestionText: "dependent", BlocksClosureBlockers: []string{dominated.ID}, Priority: architecture.QuestionPriorityHigh,
	})
	in.DominanceEdges = []DominanceEdge{{DominatorID: root.ID, DominatedID: dominated.ID, ReasonCode: "explicit"}}
	state, err := Project(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, question := range state.Questions {
		if question.ID == "question.dependent" && (question.ResolutionClass != ClassDominated || question.DominantQuestionID != "question.root") {
			t.Fatalf("question=%+v", question)
		}
	}
}

func TestDominanceCycleMakesControlUncertifiable(t *testing.T) {
	in := controlFixture()
	b := in.Closure.Blockers[0]
	b.ID = "blocker.evidence.bbbbbbbbbbbb"
	b.Code = "b"
	in.Closure.Blockers = append(in.Closure.Blockers, b)
	in.DominanceEdges = []DominanceEdge{{DominatorID: in.Closure.Blockers[0].ID, DominatedID: b.ID, ReasonCode: "explicit"}, {DominatorID: b.ID, DominatedID: in.Closure.Blockers[0].ID, ReasonCode: "explicit"}}
	state, err := Project(in)
	if err != nil {
		t.Fatal(err)
	}
	if state.Summary.Uncertifiable != 2 || len(state.Limitations) == 0 {
		t.Fatalf("state=%+v", state)
	}
}

func TestCompressionAccountingBalances(t *testing.T) {
	state, err := Project(controlFixture())
	if err != nil {
		t.Fatal(err)
	}
	if err := validateAccounting(state); err != nil {
		t.Fatal(err)
	}
}

func TestExactlyOnePrimaryNextAction(t *testing.T) {
	state, err := Project(controlFixture())
	if err != nil {
		t.Fatal(err)
	}
	if state.NextAction.Kind == "" {
		t.Fatal("missing primary action")
	}
}

func TestTaskControlYAMLRoundTripPreservesDigest(t *testing.T) {
	state, err := Project(controlFixture())
	if err != nil {
		t.Fatal(err)
	}
	data, err := MarshalYAML(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnmarshalYAML(data); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyTaskControlYAMLRoundTripPreservesDigest(t *testing.T) {
	state := TaskControlState{SchemaVersion: SchemaVersion, GeneratedBy: GeneratedBy, TaskID: "task.empty", NextAction: NextAction{Kind: ActionCompleteTask, Summary: "complete"}}
	data, err := MarshalYAML(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnmarshalYAML(data); err != nil {
		t.Fatal(err)
	}
}

func TestRepairBindingHasHighestPriority(t *testing.T) {
	in := controlFixture()
	in.BindingHealthy = false
	state, err := Project(in)
	if err != nil {
		t.Fatal(err)
	}
	if state.NextAction.Kind != ActionRepairBinding {
		t.Fatalf("next=%s", state.NextAction.Kind)
	}
}
