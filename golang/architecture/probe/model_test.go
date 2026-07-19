// SPDX-License-Identifier: AGPL-3.0-only

package probe

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

const (
	testRepo   = "github.com/globulario/sensei"
	testRev    = "0123456789abcdef"
	testGraph  = "abcdef0123456789"
	testSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func testBinding() architecture.ClaimDocumentBinding {
	return architecture.ClaimDocumentBinding{
		RepositoryDomain:  testRepo,
		Revision:          testRev,
		RevisionStatus:    architecture.RevisionResolved,
		GraphDigestSHA256: testGraph,
		GraphDigestStatus: architecture.GraphDigestResolved,
	}
}

func testProbeDocument(probes ...EvidenceProbe) ProbeDocument {
	return ProbeDocument{
		SchemaVersion:                       SchemaVersion,
		GeneratedBy:                         GeneratedBy,
		Binding:                             testBinding(),
		SourceClosureAssessmentDigestSHA256: testSHA256,
		SourceDialogueDigestSHA256:          strings.Repeat("b", 64),
		SourceClaimDocumentDigestSHA256:     strings.Repeat("c", 64),
		Probes:                              probes,
	}
}

func testProbe() EvidenceProbe {
	return EvidenceProbe{
		ID:                "probe.fixed",
		Label:             "Config writer test probe",
		Status:            StatusProposed,
		QuestionID:        "question.config_writer",
		ClosureBlockerIDs: []string{"blocker.evidence.abcdefabcdef"},
		ClaimIDs:          []string{"claim.config_writer"},
		TemplateID:        "probe.existing_test_execution.v1",
		TemplateVersion:   "v1",
		ProbeKind:         KindTestExecution,
		EvidenceLane:      LaneTest,
		EvidenceRole:      RoleSupporting,
		TargetEvidenceID:  "evidence:config_writer_test",
		TestIDs:           []string{"golang/server/config_test.go:TestConfigWriter"},
		SafetyClass:       SafetyLocalTest,
		ApprovalGate:      GateReviewRequired,
		Steps: []ProbeStep{{
			Kind:        StepRunExistingTest,
			Target:      "golang/server/config_test.go:TestConfigWriter",
			Description: "Run the existing config writer test.",
		}},
		ExpectedArtifactKinds: []string{"test_output"},
	}
}

func TestStableProbeIDIsDeterministicAndIgnoresStatus(t *testing.T) {
	p := testProbe()
	p.ID = ""
	first := StableProbeID(p, testRepo)
	p.Status = StatusSuperseded
	p.Description = "changed display text"
	second := StableProbeID(p, testRepo)
	if first == "" || first != second {
		t.Fatalf("stable IDs differ: %q vs %q", first, second)
	}
}

func TestNormalizeProbeDocumentSortsDeduplicatesAndRejectsCollision(t *testing.T) {
	a := testProbe()
	a.ID = ""
	a.ClaimIDs = []string{"claim.b", "claim.a", "claim.b"}
	b := a
	doc, err := NormalizeProbeDocument(testProbeDocument(a, b), nil)
	if err != nil {
		t.Fatalf("NormalizeProbeDocument: %v", err)
	}
	if len(doc.Probes) != 1 {
		t.Fatalf("probe count=%d, want 1", len(doc.Probes))
	}
	if got := strings.Join(doc.Probes[0].ClaimIDs, ","); got != "claim.a,claim.b" {
		t.Fatalf("claim IDs=%q", got)
	}

	c := doc.Probes[0]
	d := c
	d.TargetEvidenceID = "evidence:other"
	if _, err := NormalizeProbeDocument(testProbeDocument(c, d), nil); err == nil || !strings.Contains(err.Error(), "id collision") {
		t.Fatalf("expected collision error, got %v", err)
	}
}

func TestValidateProbeRejectsUnsafeAutomaticExecution(t *testing.T) {
	p := testProbe()
	p.SafetyClass = SafetyRuntimeRead
	p.ApprovalGate = GateReviewRequired
	p.AutomaticExecutionAllowed = true
	err := ValidateProbe(p, testProbeDocument(p), nil)
	if err == nil || !strings.Contains(err.Error(), "automatic execution forbidden") {
		t.Fatalf("err=%v", err)
	}
}

func TestQuestionEligibleOnlyForEvidenceSeekingQuestions(t *testing.T) {
	q := architecture.OpenQuestion{ID: "question.evidence", Status: architecture.QuestionStatusAwaitingEvidence}
	if !QuestionEligible(architecture.DialogueDocument{OpenQuestions: []architecture.OpenQuestion{q}}, q) {
		t.Fatal("awaiting_evidence question should be eligible")
	}
	q.Status = architecture.QuestionStatusAwaitingArchitect
	if QuestionEligible(architecture.DialogueDocument{OpenQuestions: []architecture.OpenQuestion{q}}, q) {
		t.Fatal("awaiting_architect question must not be evidence eligible")
	}
}

func TestCanonicalProbeDocumentHasNoWallClockTime(t *testing.T) {
	p := testProbe()
	out, err := MarshalCanonicalDocumentYAML(testProbeDocument(p), nil)
	if err != nil {
		t.Fatalf("MarshalCanonicalDocumentYAML: %v", err)
	}
	if strings.Contains(string(out), "created_at") || strings.Contains(string(out), "generated_at") || strings.Contains(string(out), "observed_at") {
		t.Fatalf("canonical probe plan contains runtime timestamp field:\n%s", out)
	}
}
