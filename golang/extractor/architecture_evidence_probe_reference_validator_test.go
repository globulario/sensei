// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

func evidenceProbeValidNT() string {
	probe := strings.Trim(rdf.MintIRI(rdf.ClassEvidenceProbe, "probe.config_writer_test"), "<>")
	question := strings.Trim(rdf.MintIRI(rdf.ClassOpenQuestion, "question.config_writer"), "<>")
	claim := strings.Trim(rdf.MintIRI(rdf.ClassArchitectureClaim, "claim.config_writer"), "<>")
	ev := strings.Trim(rdf.MintIRI(rdf.ClassEvidence, "evidence.config.writer"), "<>")
	test := strings.Trim(rdf.MintIRI(rdf.ClassTest, "golang/server/config_test.go:TestConfigWriter"), "<>")
	return strings.Join([]string{
		nt(question, rdf.PropType, rdf.ClassOpenQuestion, true),
		nt(claim, rdf.PropType, rdf.ClassArchitectureClaim, true),
		nt(ev, rdf.PropType, rdf.ClassEvidence, true),
		nt(ev, rdf.PropAuthoredIn, "docs/awareness/evidence.yaml", false),
		nt(test, rdf.PropType, rdf.ClassTest, true),
		nt(claim, rdf.PropSupportedByEvidence, ev, true),
		nt(probe, rdf.PropType, rdf.ClassEvidenceProbe, true),
		nt(probe, rdf.PropLabel, "Config writer test probe", false),
		nt(probe, rdf.PropStatus, "proposed", false),
		nt(probe, rdf.PropProbeForQuestion, question, true),
		nt(probe, rdf.PropAddressesClosureBlocker, "blocker.evidence.abcdef012345", false),
		nt(probe, rdf.PropTargetsClaim, claim, true),
		nt(probe, rdf.PropProducesEvidence, ev, true),
		nt(probe, rdf.PropProbeTemplateID, "probe.existing_test_execution.v1", false),
		nt(probe, rdf.PropProbeTemplateVersion, "v1", false),
		nt(probe, rdf.PropProbeKind, "test_execution", false),
		nt(probe, rdf.PropHasEvidenceLane, "test", false),
		nt(probe, rdf.PropEvidenceRole, "supporting", false),
		nt(probe, rdf.PropProbeForTest, test, true),
		nt(probe, rdf.PropSafetyClass, "local_test", false),
		nt(probe, rdf.PropRequiresApprovalGate, "review_required", false),
		nt(probe, rdf.PropAutomaticExecutionAllowed, "false", false),
		nt(probe, rdf.PropHasProbeStep, "001|run_existing_test|golang/server/config_test.go:TestConfigWriter||Run the test", false),
		nt(probe, rdf.PropSourceClosureAssessmentDigest, strings.Repeat("a", 64), false),
		nt(probe, rdf.PropSourceDialogueDigest, strings.Repeat("b", 64), false),
		nt(probe, rdf.PropSourceClaimDocumentDigest, strings.Repeat("c", 64), false),
		nt(probe, rdf.PropSourceKind, "generated_candidate", false),
	}, "\n") + "\n"
}

func assertProbeRefInvalid(t *testing.T, nt string) {
	t.Helper()
	errs, err := ValidateArchitectureEvidenceProbeReferences(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateArchitectureEvidenceProbeReferences: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("expected evidence probe reference errors")
	}
}

func TestEvidenceProbeReferenceValidatorAcceptsDefinedTargets(t *testing.T) {
	errs, err := ValidateArchitectureEvidenceProbeReferences(strings.NewReader(evidenceProbeValidNT()))
	if err != nil {
		t.Fatalf("ValidateArchitectureEvidenceProbeReferences: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("errs=%v", errs)
	}
}

func TestEvidenceProbeReferenceValidatorRejectsMissingQuestion(t *testing.T) {
	assertProbeRefInvalid(t, strings.Replace(evidenceProbeValidNT(), "openQuestion/question.config_writer", "openQuestion/question.missing", 1))
}

func TestEvidenceProbeReferenceValidatorRejectsEvidenceNotLinkedToClaim(t *testing.T) {
	line := nt(strings.Trim(rdf.MintIRI(rdf.ClassArchitectureClaim, "claim.config_writer"), "<>"), rdf.PropSupportedByEvidence, strings.Trim(rdf.MintIRI(rdf.ClassEvidence, "evidence.config.writer"), "<>"), true) + "\n"
	assertProbeRefInvalid(t, strings.Replace(evidenceProbeValidNT(), line, "", 1))
}

func TestEvidenceProbeReferenceValidatorRejectsWeakerApprovalGate(t *testing.T) {
	assertProbeRefInvalid(t, strings.Replace(evidenceProbeValidNT(), rdf.Lit("review_required"), rdf.Lit("none"), 1))
}

func TestEvidenceProbeReferenceValidatorRejectsAutomaticRuntimeExecution(t *testing.T) {
	nt := strings.Replace(evidenceProbeValidNT(), rdf.Lit("local_test"), rdf.Lit("runtime_read"), 1)
	nt = strings.Replace(nt, rdf.Lit("false"), rdf.Lit("true"), 1)
	assertProbeRefInvalid(t, nt)
}
