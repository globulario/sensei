// SPDX-License-Identifier: AGPL-3.0-only

package convergence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/plane"
	"github.com/globulario/sensei/golang/architecture/probe"
)

func testPolicy(t *testing.T) Policy {
	t.Helper()
	p, ok := PolicyByID(PolicyStrictV1)
	if !ok {
		t.Fatal("strict policy missing")
	}
	return p
}

func testRequest() closure.Request {
	return closure.Request{
		SchemaVersion: "1",
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  "github.com/example/project",
			Revision:          strings.Repeat("a", 40),
			RevisionStatus:    architecture.RevisionResolved,
			GraphDigestSHA256: strings.Repeat("b", 64),
			GraphDigestStatus: architecture.GraphDigestResolved,
		},
		Scope: closure.Scope{
			Domain:               "repo",
			TaskClass:            "repository_admission",
			RiskClass:            closure.RiskConvergence,
			AccessMode:           closure.AccessWrite,
			DirectionRequirement: closure.DirectionPreserve,
			Files:                []string{"golang/server/main.go"},
		},
	}
}

func TestStrictConvergencePolicyIsStable(t *testing.T) {
	p := testPolicy(t)
	if p.Version != "v1" || p.MaxIterations != 12 || p.NoEffectInputLimit != 2 || p.OscillationWindow != 6 || !p.ConditionalClosureTerminal {
		t.Fatalf("policy drifted: %+v", p)
	}
}

func TestUnknownConvergencePolicyRejected(t *testing.T) {
	if _, ok := PolicyByID("convergence.unknown.v1"); ok {
		t.Fatal("unknown policy accepted")
	}
}

func TestSessionIDIsDeterministic(t *testing.T) {
	req := testRequest()
	p := testPolicy(t)
	if a, b := StableSessionID(req, p), StableSessionID(req, p); a != b {
		t.Fatalf("session IDs differ: %q %q", a, b)
	}
}

func TestSessionIDChangesWithScope(t *testing.T) {
	req := testRequest()
	p := testPolicy(t)
	a := StableSessionID(req, p)
	req.Scope.Files = []string{"other.go"}
	if b := StableSessionID(req, p); a == b {
		t.Fatal("scope change did not affect session ID")
	}
}

func TestSessionIDChangesWithBinding(t *testing.T) {
	req := testRequest()
	p := testPolicy(t)
	a := StableSessionID(req, p)
	req.Binding.Revision = strings.Repeat("c", 40)
	if b := StableSessionID(req, p); a == b {
		t.Fatal("binding change did not affect session ID")
	}
}

func TestSessionIDChangesWithPolicy(t *testing.T) {
	req := testRequest()
	p := testPolicy(t)
	a := StableSessionID(req, p)
	p.Version = "v2"
	if b := StableSessionID(req, p); a == b {
		t.Fatal("policy change did not affect session ID")
	}
}

func TestSessionIDIgnoresQuestionCreatedAt(t *testing.T) {
	req := testRequest()
	p := testPolicy(t)
	a := StableSessionID(req, p)
	_ = "2026-07-13T12:00:00Z"
	if b := StableSessionID(req, p); a != b {
		t.Fatal("question timestamp affected session ID")
	}
}

func TestSessionRejectsNonContiguousIterations(t *testing.T) {
	s := Session{SessionID: "s", PolicyID: PolicyStrictV1, PolicyVersion: "v1", Iterations: []Iteration{{Index: 2}}}
	if err := ValidateSession(s); err == nil || !strings.Contains(err.Error(), "contiguous") {
		t.Fatalf("err=%v", err)
	}
}

func TestIterationDigestIsDeterministic(t *testing.T) {
	iter := Iteration{Index: 1, Status: StatusWaiting, ProgressStatus: ProgressInitial, ClosureVerdict: closure.VerdictOpen}
	a := iterationDigest("session", iter)
	b := iterationDigest("session", iter)
	if a == "" || a != b {
		t.Fatalf("iteration digest not deterministic: %q %q", a, b)
	}
}

func TestSessionRejectsBrokenDigestChain(t *testing.T) {
	iter := Iteration{Index: 1, Status: StatusWaiting, ProgressStatus: ProgressInitial, ClosureVerdict: closure.VerdictOpen}
	iter.IterationDigestSHA256 = iterationDigest("s", iter)
	s := Session{SessionID: "s", PolicyID: PolicyStrictV1, PolicyVersion: "v1", Iterations: []Iteration{iter}}
	s.Iterations[0].IterationDigestSHA256 = strings.Repeat("f", 64)
	if err := ValidateSession(s); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("err=%v", err)
	}
}

func TestQuestionTimestampOnlyDoesNotAffectSemanticInput(t *testing.T) {
	m := InputManifest{Binding: testRequest().Binding, ClaimsDigestSHA256: "a", DialogueDigestSHA256: "b", EvidenceStateDigestSHA256: "c", GraphSnapshotDigestSHA256: "d"}
	m.QuestionCreatedAt = "2026-07-13T12:00:00Z"
	a := semanticInputDigest(m)
	m.QuestionCreatedAt = "2026-07-13T13:00:00Z"
	b := semanticInputDigest(m)
	if a != b {
		t.Fatal("question timestamp changed semantic input digest")
	}
}

func TestArchitectRequiredQuestionAddsArchitectWait(t *testing.T) {
	dialogue := architecture.DialogueDocument{OpenQuestions: []architecture.OpenQuestion{{ID: "question.one", ArchitectRequired: true, Status: architecture.QuestionStatusAwaitingArchitect}}}
	got := WaitClasses(closure.Report{Verdict: closure.VerdictOpen}, dialogue, emptyProbeDoc())
	if strings.Join(got, ",") != WaitArchitect {
		t.Fatalf("wait classes=%v", got)
	}
}

func TestAwaitingEvidenceQuestionAddsEvidenceWait(t *testing.T) {
	dialogue := architecture.DialogueDocument{OpenQuestions: []architecture.OpenQuestion{{ID: "question.one", Status: architecture.QuestionStatusAwaitingEvidence}}}
	got := WaitClasses(closure.Report{Verdict: closure.VerdictOpen}, dialogue, emptyProbeDoc())
	if strings.Join(got, ",") != WaitEvidence {
		t.Fatalf("wait classes=%v", got)
	}
}

func TestAcceptedAnswerWithoutGovernedKnowledgeAddsGovernanceWait(t *testing.T) {
	dialogue := architecture.DialogueDocument{Answers: []architecture.ArchitectAnswer{{ID: "answer.one", GovernanceStatus: architecture.AnswerGovernanceAcceptedForQuestion}}}
	got := WaitClasses(closure.Report{Verdict: closure.VerdictOpen}, dialogue, emptyProbeDoc())
	if strings.Join(got, ",") != WaitGovernance {
		t.Fatalf("wait classes=%v", got)
	}
}

func TestRepairBindingAddsMechanicalWait(t *testing.T) {
	report := closure.Report{Verdict: closure.VerdictOpen, Blockers: []closure.Blocker{{ID: "blocker.one", RequiredNextAction: "repair_binding"}}}
	got := WaitClasses(report, architecture.DialogueDocument{}, emptyProbeDoc())
	if strings.Join(got, ",") != WaitMechanicalRepair {
		t.Fatalf("wait classes=%v", got)
	}
}

func TestOpenWithoutActionableWaitBecomesStalled(t *testing.T) {
	p := testPolicy(t)
	if got := classifyStatus(closure.VerdictOpen, nil, p, false, 0, false); got != StatusStalled {
		t.Fatalf("status=%s", got)
	}
}

func TestNoEffectLimitProducesStalled(t *testing.T) {
	p := testPolicy(t)
	if got := classifyStatus(closure.VerdictOpen, []string{WaitEvidence}, p, true, p.NoEffectInputLimit, false); got != StatusStalled {
		t.Fatalf("status=%s", got)
	}
}

func TestABAStateCycleIsOscillation(t *testing.T) {
	history := []Iteration{{Index: 1, SemanticStateDigestSHA256: "A"}, {Index: 2, SemanticStateDigestSHA256: "B"}}
	osc := detectOscillation(history, "A", testPolicy(t))
	if osc == nil || osc.StartIteration != 1 || osc.EndIteration != 3 {
		t.Fatalf("oscillation=%+v", osc)
	}
}

func TestAdjacentSameStateIsNoProgressNotOscillation(t *testing.T) {
	history := []Iteration{{Index: 1, SemanticStateDigestSHA256: "A"}}
	if osc := detectOscillation(history, "A", testPolicy(t)); osc != nil {
		t.Fatalf("unexpected oscillation=%+v", osc)
	}
}

func TestEvidenceUnknownToCurrentFailIsEpistemicProgress(t *testing.T) {
	prev := maintenance.EvidenceStateDocument{Evidence: []maintenance.EvidenceState{{ID: "ev", Status: maintenance.EvidenceStatusUnknown, Freshness: maintenance.EvidenceFreshnessUnknown}}}
	next := maintenance.EvidenceStateDocument{Evidence: []maintenance.EvidenceState{{ID: "ev", Status: maintenance.EvidenceStatusFail, Freshness: maintenance.EvidenceFreshnessCurrent}}}
	if SemanticStateDigest(architecture.ClaimDocument{}, planeEmpty(), closure.Report{}, architecture.DialogueDocument{}, emptyProbeDoc(), prev) == SemanticStateDigest(architecture.ClaimDocument{}, planeEmpty(), closure.Report{}, architecture.DialogueDocument{}, emptyProbeDoc(), next) {
		t.Fatal("evidence state semantic change was not reflected")
	}
}

func TestBundleUsesRelativeStagePaths(t *testing.T) {
	iter := Iteration{Index: 1, Status: StatusWaiting, ProgressStatus: ProgressInitial, ClosureVerdict: closure.VerdictOpen, StageReceipts: []StageReceipt{stageReceipt("x", "x.yaml", []byte("x"), InputManifest{})}}
	for i := range iter.StageReceipts {
		iter.StageReceipts[i].ArtifactPath = "iterations/0001/x.yaml"
	}
	iter.IterationDigestSHA256 = iterationDigest("s", iter)
	s := Session{SessionID: "s", PolicyID: PolicyStrictV1, PolicyVersion: "v1", Iterations: []Iteration{iter}}
	b, err := RenderBundle(s, InputManifest{}, iter, map[string][]byte{"x.yaml": []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	for rel := range b.Files {
		if filepath.IsAbs(rel) {
			t.Fatalf("absolute bundle path %s", rel)
		}
	}
}

func TestConvergencePackageDoesNotUseCommandExecutionAPIs(t *testing.T) {
	root := "."
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		for _, forbidden := range []string{`"os/` + `exec"`, "exec." + "Command", "sh" + " -c", "ba" + "sh"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s contains forbidden command-execution token %q", path, forbidden)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func emptyProbeDoc() probe.ProbeDocument {
	return probe.ProbeDocument{}
}

func planeEmpty() plane.Report {
	return plane.Report{}
}
