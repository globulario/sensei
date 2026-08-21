// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"context"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/convergence"
	"github.com/globulario/sensei/golang/architecture/dispositionsemantics"
	"github.com/globulario/sensei/golang/architecture/identity"
	"github.com/globulario/sensei/golang/architecture/probe"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
	"github.com/globulario/sensei/golang/architecture/taskcontrol"
)

// TestNoProjectionAdvertisesADemandTheAuthorityTerminated is the executable
// form of the law behind #230.
//
// Three projections consume the same governed decision and each has its own
// vocabulary for it — a task-control action, a convergence wait class, a
// convergence next action. All three read the dialogue document's local status,
// and all three failed the same way: they advertised an evidence demand that an
// architect had already terminated on the ledger.
//
// The property is one-directional, not output equality. The projections have
// different jobs and should not agree on their outputs. They must agree that
// NO decision-bearing projection may advertise an evidence demand the
// authoritative question disposition has ended.
//
// The decision here is a real recorded dismissal — seeded transition, governed
// policy, enrolled identity, Prepare + RecordDisposition — not a struct built
// to match the assertion.
func TestNoProjectionAdvertisesADemandTheAuthorityTerminated(t *testing.T) {
	env := seedDismissedQuestion(t)

	dialogue := architecture.DialogueDocument{OpenQuestions: []architecture.OpenQuestion{{
		ID: env.questionID, QuestionText: "which evidence proves this?",
		BlocksClosureDimension: closure.DimensionEvidence,
		BlocksClaims:           []string{"claim.one"},
		BlocksClosureBlockers:  []string{"blocker.evidence.aaaaaaaaaaaa"},
		AcceptedAnswerTypes:    []string{architecture.AnswerTypeEvidencePointer},
		Priority:               architecture.QuestionPriorityHigh,
		Status:                 architecture.QuestionStatusAwaitingEvidence,
	}}}

	// The fold every consumer shares, taken from the verified ledger.
	decisions := governedDispositions(env.taskDir, dialogue)
	decided := decisions[env.questionID]
	if !decided.DismissesEvidenceDemand() {
		t.Fatalf("the recorded dismissal did not read as one: %+v", decided)
	}
	receipt := decided.DecisionReceipt()
	if receipt == "" {
		t.Fatal("a governed decision must carry the receipt behind it")
	}

	// 1. Task control: no evidence demand, and the blocker still stands.
	state, err := taskcontrol.Project(controlInputsFor(dialogue, decisions))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if state.NextAction.Kind == taskcontrol.ActionProvideExternalEvidence {
		t.Fatalf("task control still demands evidence: %+v", state.NextAction)
	}
	if state.Summary.ActiveRootBlockers == 0 {
		t.Fatal("dismissing the question silently cleared the blocker it was about")
	}
	if basis := strings.Join(state.Questions[0].AnswerabilityBasis, " "); !strings.Contains(basis, receipt) {
		t.Fatalf("task control suppressed a demand without naming the receipt: %q", basis)
	}

	// 2. Convergence wait classes: no evidence wait.
	report := closure.Report{Verdict: closure.VerdictOpen}
	for _, w := range convergence.WaitClasses(report, dialogue, probe.ProbeDocument{}, decisions) {
		if w == convergence.WaitEvidence {
			t.Fatal("convergence still waits on evidence for a dismissed question")
		}
	}

	// 3. Convergence next actions: no evidence request.
	for _, a := range convergence.NextActions(report, dialogue, probe.ProbeDocument{}, decisions) {
		if a.Class == "provide_evidence" {
			t.Fatalf("convergence still offers provide_evidence: %+v", a)
		}
	}

	// 4. The cached control projection predates the receipt, so it may not be
	// served: a correct projection returned from an older snapshot lies just as
	// effectively as a wrong one.
	if !taskHasGovernedDisposition(env.taskDir) {
		t.Fatal("a recorded decision left the cached control projection servable")
	}

	// The baseline: with no decision, every one of them demands evidence again.
	// Without this the test would pass on projections that never demand
	// anything.
	base, err := taskcontrol.Project(controlInputsFor(dialogue, nil))
	if err != nil {
		t.Fatalf("project baseline: %v", err)
	}
	if base.NextAction.Kind != taskcontrol.ActionProvideExternalEvidence {
		t.Fatalf("undisposed baseline does not demand evidence: %+v", base.NextAction)
	}
	if got := convergence.WaitClasses(report, dialogue, probe.ProbeDocument{}, nil); len(got) == 0 {
		t.Fatal("undisposed baseline does not wait on anything")
	}
}

func controlInputsFor(dialogue architecture.DialogueDocument, decisions map[string]dispositionsemantics.Decision) taskcontrol.Inputs {
	binding := architecture.ClaimDocumentBinding{
		RepositoryDomain: "github.com/example/repo", Revision: "abc", RevisionStatus: architecture.RevisionResolved,
		GraphDigestSHA256: "graph", GraphDigestStatus: architecture.GraphDigestResolved,
	}
	claim := architecture.Claim{
		ID: "claim.one", Statement: architecture.ClaimStatement{Subject: "router", Predicate: "uses", Object: "tree"},
		Scope: architecture.ClaimScope{Files: []string{"router.go"}}, ArchitecturalPlane: architecture.PlaneObserved,
	}
	dialogue.Binding = binding
	return taskcontrol.Inputs{
		TaskID: "task.one", Binding: binding, BindingHealthy: true,
		Permission: taskcontrol.PermissionSummary{Inspect: "admitted", Modify: "waiting"},
		Claims:     architecture.ClaimDocument{Binding: binding, Claims: []architecture.Claim{claim}},
		Dialogue:   dialogue,
		Probes:     probe.ProbeDocument{Binding: binding},
		Closure: closure.Report{
			Dimensions: []closure.DimensionAssessment{{Dimension: closure.DimensionEvidence, Required: true, Applicable: true, State: closure.StateOpen}},
			Blockers: []closure.Blocker{{
				ID: "blocker.evidence.aaaaaaaaaaaa", Dimension: closure.DimensionEvidence, Severity: "high",
				Code: "closure.evidence.missing", Summary: "route evidence is missing", ClaimIDs: []string{"claim.one"},
				QuestionIDs: []string{dialogue.OpenQuestions[0].ID}, RequiredNextAction: "add_evidence",
			}},
		},
		Dispositions: decisions,
	}
}

type dismissedEnv struct {
	taskDir    string
	questionID string
}

func seedDismissedQuestion(t *testing.T) dismissedEnv {
	t.Helper()
	seeded, questions := seedTaskWithArchitectQuestion(t)
	cand, err := qd.Prepare(qd.PrepareRequest{
		TaskDirectory: seeded.TaskDir, RepositoryRoot: seeded.Repo, IdentityRoot: identity.Root(seeded.Repo),
		QuestionID: questions[0].QuestionID, Disposition: qd.DispositionDismissed, Reusability: qd.ReusabilityNone,
		Rationale: "the architect decided no evidence will be sought for this question",
	})
	if err != nil {
		t.Fatalf("prepare dismissal: %v", err)
	}
	if _, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: seeded.TaskDir, Candidate: cand}); err != nil {
		t.Fatalf("record dismissal: %v", err)
	}
	return dismissedEnv{taskDir: seeded.TaskDir, questionID: questions[0].QuestionID}
}
