// SPDX-License-Identifier: Apache-2.0

package maintenance

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

func TestEvidenceStateRequiresExplicitBindingStatuses(t *testing.T) {
	doc := validEvidenceState("rev", "graph")
	doc.Binding.RevisionStatus = ""
	if err := ValidateEvidenceStateDocument(doc, nil); err == nil {
		t.Fatal("expected missing revision status")
	}
}

func TestEvidenceStateRejectsUnknownStatus(t *testing.T) {
	doc := validEvidenceState("rev", "graph")
	doc.Evidence[0].Status = "ok"
	if err := ValidateEvidenceStateDocument(doc, nil); err == nil {
		t.Fatal("expected unknown status rejection")
	}
}

func TestEvidenceStateRejectsUnknownFreshness(t *testing.T) {
	doc := validEvidenceState("rev", "graph")
	doc.Evidence[0].Freshness = "fresh"
	if err := ValidateEvidenceStateDocument(doc, nil); err == nil {
		t.Fatal("expected unknown freshness rejection")
	}
}

func TestEvidenceStateRejectsDuplicateID(t *testing.T) {
	doc := validEvidenceState("rev", "graph")
	doc.Evidence = append(doc.Evidence, doc.Evidence[0])
	if err := ValidateEvidenceStateDocument(doc, nil); err == nil {
		t.Fatal("expected duplicate id rejection")
	}
}

func TestEvidenceStateRejectsInvalidObservedAt(t *testing.T) {
	doc := validEvidenceState("rev", "graph")
	doc.Evidence[0].ObservedAt = "today"
	if err := ValidateEvidenceStateDocument(doc, nil); err == nil {
		t.Fatal("expected observed_at rejection")
	}
}

func TestEvidenceStateRejectsBindingMismatch(t *testing.T) {
	doc := validEvidenceState("rev", "graph")
	expected := doc.Binding
	expected.GraphDigestSHA256 = "other"
	if err := ValidateEvidenceStateDocument(doc, &expected); err == nil {
		t.Fatal("expected binding mismatch")
	}
}

func TestEvidencePassCurrentIsActive(t *testing.T) {
	doc := validEvidenceState("rev", "graph")
	if !EvidenceIsActive(doc.Evidence[0], doc.Binding, doc.Binding) {
		t.Fatal("pass/current should be active")
	}
}

func TestEvidenceFailIsNotOppositeProof(t *testing.T) {
	doc := validEvidenceState("rev", "graph")
	doc.Evidence[0].Status = EvidenceStatusFail
	if EvidenceIsActive(doc.Evidence[0], doc.Binding, doc.Binding) {
		t.Fatal("fail should not be active")
	}
}

func TestEvidenceStaleIsNotActive(t *testing.T) {
	doc := validEvidenceState("rev", "graph")
	doc.Evidence[0].Freshness = EvidenceFreshnessStale
	if EvidenceIsActive(doc.Evidence[0], doc.Binding, doc.Binding) {
		t.Fatal("stale evidence should not be active")
	}
}

func TestSourceReceiptCurrentWhenDigestMatches(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	if lane := VerifySourceReceipt(root, doc.FactReceipts[0]); lane.State != LaneCurrent {
		t.Fatalf("lane=%#v", lane)
	}
}

func TestSourceReceiptStaleWhenDigestChanges(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	if err := os.WriteFile(filepath.Join(root, "state.go"), []byte("package fixture\nvar Changed = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if lane := VerifySourceReceipt(root, doc.FactReceipts[0]); lane.State != LaneStale {
		t.Fatalf("lane=%#v", lane)
	}
}

func TestSourceReceiptStaleWhenFileDisappears(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	if err := os.Remove(filepath.Join(root, "state.go")); err != nil {
		t.Fatal(err)
	}
	if lane := VerifySourceReceipt(root, doc.FactReceipts[0]); lane.State != LaneStale {
		t.Fatalf("lane=%#v", lane)
	}
}

func TestNonSourceFactDoesNotInventFilesystemCheck(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	doc.FactReceipts[0].Provenance.SourceKind = "git_history"
	if lane := VerifySourceReceipt(root, doc.FactReceipts[0]); lane.State != LaneAbsent {
		t.Fatalf("lane=%#v", lane)
	}
}

func TestRepositoryRevisionCurrentWhenEqual(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	if lane := VerifyRepositoryRevision(root, doc.Binding); lane.State != LaneCurrent {
		t.Fatalf("lane=%#v", lane)
	}
}

func TestRepositoryRevisionStaleWhenDifferent(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	doc.Binding.Revision = "other"
	if lane := VerifyRepositoryRevision(root, doc.Binding); lane.State != LaneStale {
		t.Fatalf("lane=%#v", lane)
	}
}

func TestGraphDigestCurrentWhenEqual(t *testing.T) {
	b := architecture.ClaimDocumentBinding{GraphDigestSHA256: "graph", GraphDigestStatus: architecture.GraphDigestResolved}
	if lane := VerifyGraphDigest(b, b); lane.State != LaneCurrent {
		t.Fatalf("lane=%#v", lane)
	}
}

func TestGraphDigestStaleWhenDifferent(t *testing.T) {
	doc := architecture.ClaimDocumentBinding{GraphDigestSHA256: "graph", GraphDigestStatus: architecture.GraphDigestResolved}
	obs := architecture.ClaimDocumentBinding{GraphDigestSHA256: "other", GraphDigestStatus: architecture.GraphDigestResolved}
	if lane := VerifyGraphDigest(doc, obs); lane.State != LaneStale {
		t.Fatalf("lane=%#v", lane)
	}
}

func TestGraphDigestUnknownWhenNotRequested(t *testing.T) {
	doc := architecture.ClaimDocumentBinding{GraphDigestStatus: architecture.GraphDigestResolved, GraphDigestSHA256: "graph"}
	obs := architecture.ClaimDocumentBinding{GraphDigestStatus: architecture.GraphDigestNotRequested}
	if lane := VerifyGraphDigest(doc, obs); lane.State != LaneUnknown {
		t.Fatalf("lane=%#v", lane)
	}
}

func TestCurrentPremiseFactsProduceSupported(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	result := evaluateTest(t, root, doc, nil, nil, nil, "graph")
	if got := result.Document.Claims[0].EpistemicStatus; got != architecture.StatusSupported {
		t.Fatalf("status=%s", got)
	}
}

func TestMissingGraphDigestProducesUnknown(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	result := evaluateTest(t, root, doc, nil, nil, nil, "")
	if got := result.Document.Claims[0].EpistemicStatus; got != architecture.StatusUnknown {
		t.Fatalf("status=%s", got)
	}
}

func TestGraphDigestMismatchProducesStale(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	result := evaluateTest(t, root, doc, nil, nil, nil, "other")
	if got := result.Document.Claims[0].EpistemicStatus; got != architecture.StatusStale {
		t.Fatalf("status=%s", got)
	}
}

func TestActiveSupportAndActiveRefutationProduceContested(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	doc.Claims[0].RefutingEvidence = []string{"evidence:counter"}
	doc.Claims[0].EpistemicStatus = architecture.StatusUnknown
	doc.Claims[0].Unknowns = []string{"refutation unresolved"}
	ev := validEvidenceState(rev, "graph")
	ev.Evidence = append(ev.Evidence, EvidenceState{ID: "counter", Status: EvidenceStatusPass, Freshness: EvidenceFreshnessCurrent})
	result := evaluateTest(t, root, doc, nil, &ev, nil, "graph")
	if got := result.Document.Claims[0].EpistemicStatus; got != architecture.StatusContested {
		t.Fatalf("status=%s", got)
	}
}

func TestActiveRefutationWithoutCurrentSupportProducesRefuted(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	doc.Claims[0].PremiseFacts = nil
	doc.Claims[0].RefutingEvidence = []string{"evidence:counter"}
	doc.Claims[0].EpistemicStatus = architecture.StatusUnknown
	doc.Claims[0].Unknowns = []string{"no premise"}
	ev := validEvidenceState(rev, "graph")
	ev.Evidence = []EvidenceState{{ID: "counter", Status: EvidenceStatusPass, Freshness: EvidenceFreshnessCurrent}}
	result := evaluateTest(t, root, doc, nil, &ev, nil, "graph")
	if got := result.Document.Claims[0].EpistemicStatus; got != architecture.StatusRefuted {
		t.Fatalf("status=%s", got)
	}
}

func TestMissingRefutingEvidenceStateProducesUnknown(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	doc.Claims[0].RefutingEvidence = []string{"evidence:counter"}
	doc.Claims[0].EpistemicStatus = architecture.StatusUnknown
	doc.Claims[0].Unknowns = []string{"refutation unresolved"}
	result := evaluateTest(t, root, doc, nil, nil, nil, "graph")
	if got := result.Document.Claims[0].EpistemicStatus; got != architecture.StatusUnknown {
		t.Fatalf("status=%s", got)
	}
}

func TestExplicitSupersessionProducesSuperseded(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	replacement := doc.Claims[0]
	replacement.ID = "claim.replacement"
	replacement.Statement.Object = "replacement"
	doc.Claims[0].SupersededBy = replacement.ID
	doc.Claims[0].EpistemicStatus = architecture.StatusSuperseded
	doc.Claims = append(doc.Claims, replacement)
	result := evaluateTest(t, root, doc, nil, nil, nil, "graph")
	if got := findClaim(result.Document, "claim.current").EpistemicStatus; got != architecture.StatusSuperseded {
		t.Fatalf("status=%s", got)
	}
}

func TestSupportedDependencyAllowsDependentSupport(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	dep := doc.Claims[0]
	dep.ID = "claim.dependent"
	dep.PremiseFacts = nil
	dep.DependsOnClaims = []string{"claim.current"}
	doc.Claims = append(doc.Claims, dep)
	result := evaluateTest(t, root, doc, nil, nil, nil, "graph")
	if got := findClaim(result.Document, "claim.dependent").EpistemicStatus; got != architecture.StatusSupported {
		t.Fatalf("status=%s", got)
	}
}

func TestStaleDependencyMakesDependentStale(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	dep := doc.Claims[0]
	dep.ID = "claim.dependent"
	dep.PremiseFacts = nil
	dep.DependsOnClaims = []string{"claim.current"}
	doc.Claims = append(doc.Claims, dep)
	if err := os.WriteFile(filepath.Join(root, "state.go"), []byte("package fixture\nvar Changed = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := evaluateTest(t, root, doc, nil, nil, nil, "graph")
	if got := findClaim(result.Document, "claim.dependent").EpistemicStatus; got != architecture.StatusStale {
		t.Fatalf("status=%s", got)
	}
}

func TestPreviousSupportedDoesNotOverrideCurrentUnknown(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	prev := doc
	doc.Binding.GraphDigestStatus = architecture.GraphDigestNotRequested
	doc.Binding.GraphDigestSHA256 = ""
	doc.Claims[0].EpistemicStatus = architecture.StatusUnknown
	doc.Claims[0].Unknowns = []string{"graph unavailable"}
	result := evaluateTest(t, root, doc, &prev, nil, nil, "")
	if got := result.Document.Claims[0].EpistemicStatus; got != architecture.StatusUnknown {
		t.Fatalf("status=%s", got)
	}
}

func TestAbsentCurrentClaimIsReportedRetiredAndStale(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	prev := doc
	doc.Claims = nil
	result := evaluateTest(t, root, doc, &prev, nil, nil, "graph")
	if len(result.Report.RetiredClaims) != 1 || result.Report.RetiredClaims[0].EvaluatedStatus != architecture.StatusStale {
		t.Fatalf("retired=%#v", result.Report.RetiredClaims)
	}
}

func TestSameIDSemanticDivergenceFails(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	prev, err := architecture.NormalizeClaimDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	doc.Claims[0].Statement.Object = "other"
	if _, err := Evaluate(Context{RepositoryRoot: root, Current: doc, Previous: &prev, ObservedBinding: observedBinding(rev, "graph")}); err == nil {
		t.Fatal("expected semantic divergence error")
	}
}

func TestMaintenanceReportIsDeterministic(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	a := evaluateTest(t, root, doc, nil, nil, nil, "graph")
	b := evaluateTest(t, root, doc, nil, nil, nil, "graph")
	ay, err := MarshalCanonicalReportYAML(a.Report)
	if err != nil {
		t.Fatal(err)
	}
	by, err := MarshalCanonicalReportYAML(b.Report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ay, by) || !bytes.Contains(ay, []byte("claim_truth_maintenance:")) {
		t.Fatalf("nondeterministic report:\n%s\n---\n%s", ay, by)
	}
}

func TestArchitectAnswerDoesNotSupportClaim(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	doc.Claims[0].PremiseFacts = nil
	doc.Claims[0].SupportingEvidence = []string{"evidence:missing"}
	doc.Claims[0].EpistemicStatus = architecture.StatusUnknown
	doc.Claims[0].Unknowns = []string{"no premise"}
	dialogue := dialogueForClaim(rev, "graph", doc.Claims[0].ID, architecture.QuestionStatusResolved, architecture.AnswerTypeIntentStatement)
	result := evaluateTest(t, root, doc, nil, nil, &dialogue, "graph")
	if got := result.Document.Claims[0].EpistemicStatus; got == architecture.StatusSupported {
		t.Fatal("architect answer supported claim")
	}
	if len(result.Report.ClaimEvaluations[0].OpenQuestions) == 0 {
		t.Fatal("dialogue not reported")
	}
}

func TestAcceptedUnknownRemainsVisible(t *testing.T) {
	root, rev := gitFixture(t)
	doc := claimDocForRoot(t, root, rev, "graph")
	dialogue := dialogueForClaim(rev, "graph", doc.Claims[0].ID, architecture.QuestionStatusAcceptedUnknown, architecture.AnswerTypeUnknownAcknowledgement)
	result := evaluateTest(t, root, doc, nil, nil, &dialogue, "graph")
	if !strings.Contains(string(mustReportYAML(t, result.Report)), "dialogue.accepted_unknown") {
		t.Fatal("accepted_unknown not visible")
	}
}

func validEvidenceState(rev, graph string) EvidenceStateDocument {
	return EvidenceStateDocument{
		SchemaVersion: "1",
		GeneratedBy:   "test",
		Binding:       observedBinding(rev, graph),
		Evidence:      []EvidenceState{{ID: "support", Status: EvidenceStatusPass, Freshness: EvidenceFreshnessCurrent, ObservedAt: "2026-07-13T14:00:00Z"}},
	}
}

func gitFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "state.go"), []byte("package fixture\nvar State string\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitMaint(t, root, "init")
	runGitMaint(t, root, "config", "user.email", "test@example.com")
	runGitMaint(t, root, "config", "user.name", "Test User")
	runGitMaint(t, root, "add", ".")
	runGitMaint(t, root, "commit", "-m", "initial")
	out := runGitOutput(t, root, "rev-parse", "HEAD")
	return root, strings.TrimSpace(out)
}

func claimDocForRoot(t *testing.T, root, rev, graph string) architecture.ClaimDocument {
	t.Helper()
	digest, err := architecture.SourceDigestSHA256(root, "state.go")
	if err != nil {
		t.Fatal(err)
	}
	f := architecture.Fact{
		ID:        "fact.current",
		Kind:      "guard",
		Subject:   "fixture.Apply",
		Predicate: "refuses_when",
		Object:    "bad",
		Scope: architecture.Scope{
			Repository: "github.com/example/project",
			Files:      []string{"state.go"},
			Symbols:    []string{"fixture.Apply"},
		},
		Evidence:   architecture.Evidence{SourceFile: "state.go", LineStart: 1, LineEnd: 1},
		Confidence: 0.6,
		Extractor:  "test",
	}
	prov := architecture.Provenance{
		RepositoryDomain:       "github.com/example/project",
		RepositoryDomainStatus: architecture.RepositoryDomainResolved,
		Revision:               rev,
		RevisionStatus:         architecture.RevisionResolved,
		SourceDigest:           digest,
		SourceDigestStatus:     architecture.SourceDigestResolved,
		SourceKind:             "source_file",
	}
	c := architecture.Claim{
		ID:                     "claim.current",
		Label:                  "Current claim",
		Statement:              architecture.ClaimStatement{Subject: "fixture.Apply", Predicate: "refuses_when", Object: "bad"},
		Scope:                  architecture.ClaimScope{Repository: "github.com/example/project", Files: []string{"state.go"}, Symbols: []string{"fixture.Apply"}},
		ArchitecturalPlane:     architecture.PlaneObserved,
		AssertionOrigin:        architecture.OriginDerived,
		EpistemicStatus:        architecture.StatusSupported,
		InferenceRule:          "rule.observed_guard_behavior.v1",
		PremiseFacts:           []string{f.ID},
		InvalidationConditions: []string{"source changes"},
		Confidence:             0.6,
		Freshness:              "current",
		HumanReviewRequired:    true,
		PromotionStatus:        architecture.PromotionCandidate,
	}
	return architecture.ClaimDocument{
		SchemaVersion: "1",
		GeneratedBy:   "test",
		Binding:       observedBinding(rev, graph),
		FactReceipts:  []architecture.ClaimFactReceipt{{Fact: f, Provenance: prov}},
		Claims:        []architecture.Claim{c},
	}
}

func observedBinding(rev, graph string) architecture.ClaimDocumentBinding {
	status := architecture.GraphDigestResolved
	if graph == "" {
		status = architecture.GraphDigestNotRequested
	}
	return architecture.ClaimDocumentBinding{
		RepositoryDomain:  "github.com/example/project",
		Revision:          rev,
		RevisionStatus:    architecture.RevisionResolved,
		GraphDigestSHA256: graph,
		GraphDigestStatus: status,
	}
}

func evaluateTest(t *testing.T, root string, doc architecture.ClaimDocument, prev *architecture.ClaimDocument, ev *EvidenceStateDocument, dialogue *architecture.DialogueDocument, graph string) Result {
	t.Helper()
	rev := doc.Binding.Revision
	result, err := Evaluate(Context{RepositoryRoot: root, Current: doc, Previous: prev, Evidence: ev, Dialogue: dialogue, ObservedBinding: observedBinding(rev, graph)})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func findClaim(doc architecture.ClaimDocument, id string) architecture.Claim {
	for _, c := range doc.Claims {
		if c.ID == id {
			return c
		}
	}
	return architecture.Claim{}
}

func dialogueForClaim(rev, graph, claimID, status, answerType string) architecture.DialogueDocument {
	q := architecture.OpenQuestion{
		ID:                     "question.current",
		QuestionText:           "What is true?",
		BlocksClosureDimension: architecture.ClosureEvidence,
		BlocksClaims:           []string{claimID},
		AcceptedAnswerTypes:    []string{answerType},
		ReasonsOpen:            []string{"missing evidence"},
		Priority:               architecture.QuestionPriorityMedium,
		RiskIfUnresolved:       "unknown",
		ArchitectRequired:      true,
		Status:                 status,
		ResolvedByAnswers:      []string{"answer.current"},
		CreatedAt:              "2026-07-13T14:00:00Z",
	}
	a := architecture.ArchitectAnswer{
		ID:               "answer.current",
		AnswersQuestions: []string{q.ID},
		Author:           architecture.AnswerAuthor{Role: "architect"},
		Statement:        "Unknown",
		Classifications:  []string{answerType},
		RecordedAt:       "2026-07-13T14:01:00Z",
		GovernanceStatus: architecture.AnswerGovernanceAcceptedForQuestion,
	}
	return architecture.DialogueDocument{SchemaVersion: "1", CompiledBy: "test", Binding: observedBinding(rev, graph), OpenQuestions: []architecture.OpenQuestion{q}, Answers: []architecture.ArchitectAnswer{a}}
}

func mustReportYAML(t *testing.T, r Report) []byte {
	t.Helper()
	data, err := MarshalCanonicalReportYAML(r)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func runGitMaint(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}
