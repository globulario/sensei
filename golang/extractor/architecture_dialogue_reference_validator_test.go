// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

func dialogueValidNT() string {
	claim := strings.Trim(rdf.MintIRI(rdf.ClassArchitectureClaim, "claim.config_writer"), "<>")
	component := strings.Trim(rdf.MintIRI(rdf.ClassComponent, "component.config"), "<>")
	q := strings.Trim(rdf.MintIRI(rdf.ClassOpenQuestion, "question.config_writer"), "<>")
	a := strings.Trim(rdf.MintIRI(rdf.ClassArchitectAnswer, "answer.config_writer"), "<>")
	ev := strings.Trim(rdf.MintIRI(rdf.ClassEvidence, "evidence.config.writer"), "<>")
	return strings.Join([]string{
		nt(component, rdf.PropType, rdf.ClassComponent, true),
		nt(claim, rdf.PropType, rdf.ClassArchitectureClaim, true),
		nt(claim, rdf.PropDerivedFromFact, "fact.config.writer", false),
		nt(ev, rdf.PropType, rdf.ClassEvidence, true),
		nt(ev, rdf.PropAuthoredIn, "docs/evidence.yaml", false),
		nt(q, rdf.PropType, rdf.ClassOpenQuestion, true),
		nt(q, rdf.PropLabel, "Config writer", false),
		nt(q, rdf.PropQuestionText, "Who writes config?", false),
		nt(q, rdf.PropBlocksClosureDimension, "authority", false),
		nt(q, rdf.PropBlocksClaim, claim, true),
		nt(q, rdf.PropBlocksNode, component, true),
		nt(q, rdf.PropBlocksClosureBlocker, "blocker.authority.abcdef012345", false),
		nt(q, rdf.PropQuestionTemplateID, "question.authority_definition.v1", false),
		nt(q, rdf.PropQuestionTemplateVersion, "v1", false),
		nt(q, rdf.PropSourceClosureAssessmentDigest, strings.Repeat("a", 64), false),
		nt(q, rdf.PropAcceptedAnswerType, "intent_statement", false),
		nt(q, rdf.PropReasonOpen, "Two writers observed.", false),
		nt(q, rdf.PropKnownFact, "fact.config.writer", false),
		nt(q, rdf.PropGroundedByEvidence, ev, true),
		nt(q, rdf.PropCompetingHypothesis, "hypothesis.owner_a: Component A owns it.", false),
		nt(q, rdf.PropQuestionPriority, "high", false),
		nt(q, rdf.PropRiskIfUnresolved, "Authority split persists.", false),
		nt(q, rdf.PropArchitectRequired, "true", false),
		nt(q, rdf.PropQuestionStatus, "resolved", false),
		nt(q, rdf.PropResolvedByAnswer, a, true),
		nt(q, rdf.PropCreatedAt, "2026-07-13T12:00:00Z", false),
		nt(q, rdf.PropValidForCommit, "0123456789abcdef", false),
		nt(q, rdf.PropValidForGraphDigest, "abcdef0123456789", false),
		nt(q, rdf.PropSourceKind, "generated_candidate", false),
		nt(a, rdf.PropType, rdf.ClassArchitectAnswer, true),
		nt(a, rdf.PropLabel, "Config answer", false),
		nt(a, rdf.PropAnswersQuestion, q, true),
		nt(a, rdf.PropAuthorRole, "project_architect", false),
		nt(a, rdf.PropAnswerStatement, "Component A is the intended writer.", false),
		nt(a, rdf.PropAnswerClassification, "intent_statement", false),
		nt(a, rdf.PropCitesEvidence, ev, true),
		nt(a, rdf.PropSelectedHypothesis, "question.config_writer:hypothesis.owner_a", false),
		nt(a, rdf.PropRecordedAt, "2026-07-13T12:15:00Z", false),
		nt(a, rdf.PropAnswerGovernanceStatus, "accepted_for_question", false),
		nt(a, rdf.PropValidForCommit, "0123456789abcdef", false),
		nt(a, rdf.PropValidForGraphDigest, "abcdef0123456789", false),
		nt(a, rdf.PropSourceKind, "architect_dialogue", false),
	}, "\n") + "\n"
}

func nt(s, p, o string, objectIRI bool) string {
	if objectIRI {
		return "<" + s + "> <" + p + "> <" + o + "> ."
	}
	return "<" + s + "> <" + p + "> " + rdf.Lit(o) + " ."
}

func assertDialogueRefInvalid(t *testing.T, nt string) {
	t.Helper()
	errs, err := ValidateArchitectureDialogueReferences(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateArchitectureDialogueReferences: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("expected dialogue reference errors")
	}
}

func TestDialogueReferenceValidatorAcceptsDefinedClaimQuestionAnswerAndEvidence(t *testing.T) {
	errs, err := ValidateArchitectureDialogueReferences(strings.NewReader(dialogueValidNT()))
	if err != nil {
		t.Fatalf("ValidateArchitectureDialogueReferences: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("errs=%v", errs)
	}
}

func TestDialogueReferenceValidatorRejectsFabricatedBlockedClaim(t *testing.T) {
	assertDialogueRefInvalid(t, strings.Replace(dialogueValidNT(), "architectureClaim/claim.config_writer", "architectureClaim/claim.missing", 1))
}

func TestDialogueReferenceValidatorRejectsFabricatedBlockedNode(t *testing.T) {
	assertDialogueRefInvalid(t, strings.Replace(dialogueValidNT(), "component/component.config", "component/component.missing", 1))
}

func TestDialogueReferenceValidatorRejectsMalformedClosureBlockerID(t *testing.T) {
	assertDialogueRefInvalid(t, strings.Replace(dialogueValidNT(), "blocker.authority.abcdef012345", "blocker.authority.bad", 1))
}

func TestDialogueReferenceValidatorRejectsPartialGeneratedMetadata(t *testing.T) {
	line := nt(strings.Trim(rdf.MintIRI(rdf.ClassOpenQuestion, "question.config_writer"), "<>"), rdf.PropQuestionTemplateVersion, "v1", false) + "\n"
	assertDialogueRefInvalid(t, strings.Replace(dialogueValidNT(), line, "", 1))
}

func TestDialogueReferenceValidatorRejectsFabricatedGroundingEvidence(t *testing.T) {
	assertDialogueRefInvalid(t, strings.Replace(dialogueValidNT(), "evidence/evidence.config.writer", "evidence/evidence.missing", 2))
}

func TestDialogueReferenceValidatorRejectsUnknownKnownFact(t *testing.T) {
	valid := dialogueValidNT()
	old := nt(strings.Trim(rdf.MintIRI(rdf.ClassOpenQuestion, "question.config_writer"), "<>"), rdf.PropKnownFact, "fact.config.writer", false)
	newLine := nt(strings.Trim(rdf.MintIRI(rdf.ClassOpenQuestion, "question.config_writer"), "<>"), rdf.PropKnownFact, "fact.missing", false)
	assertDialogueRefInvalid(t, strings.Replace(valid, old, newLine, 1))
}

func TestDialogueReferenceValidatorRejectsMissingAnsweredQuestion(t *testing.T) {
	assertDialogueRefInvalid(t, strings.Replace(dialogueValidNT(), "openQuestion/question.config_writer", "openQuestion/question.missing", 2))
}

func TestDialogueReferenceValidatorRejectsMissingResolutionAnswer(t *testing.T) {
	assertDialogueRefInvalid(t, strings.Replace(dialogueValidNT(), "architectAnswer/answer.config_writer", "architectAnswer/answer.missing", 1))
}

func TestDialogueReferenceValidatorRejectsMissingSupersedingQuestion(t *testing.T) {
	q := strings.Trim(rdf.MintIRI(rdf.ClassOpenQuestion, "question.config_writer"), "<>")
	missing := strings.Trim(rdf.MintIRI(rdf.ClassOpenQuestion, "question.missing"), "<>")
	assertDialogueRefInvalid(t, dialogueValidNT()+nt(q, rdf.PropSupersededBy, missing, true)+"\n")
}

func TestDialogueReferenceValidatorRejectsMissingSupersedingAnswer(t *testing.T) {
	a := strings.Trim(rdf.MintIRI(rdf.ClassArchitectAnswer, "answer.config_writer"), "<>")
	missing := strings.Trim(rdf.MintIRI(rdf.ClassArchitectAnswer, "answer.missing"), "<>")
	assertDialogueRefInvalid(t, dialogueValidNT()+nt(a, rdf.PropSupersededBy, missing, true)+"\n")
}

func TestDialogueReferenceValidatorRejectsUnacceptedClassification(t *testing.T) {
	assertDialogueRefInvalid(t, strings.Replace(dialogueValidNT(), `"intent_statement"`, `"historical_context"`, 1))
}

func TestDialogueReferenceValidatorRejectsUnknownSelectedHypothesis(t *testing.T) {
	assertDialogueRefInvalid(t, strings.Replace(dialogueValidNT(), "question.config_writer:hypothesis.owner_a", "question.config_writer:hypothesis.missing", 1))
}

func TestDialogueReferenceValidatorRejectsRejectedResolution(t *testing.T) {
	assertDialogueRefInvalid(t, strings.Replace(dialogueValidNT(), `"accepted_for_question"`, `"rejected"`, 1))
}

func TestDialogueReferenceValidatorDoesNotChangeLegacyResultsWithoutDialogue(t *testing.T) {
	errs, err := ValidateArchitectureDialogueReferences(strings.NewReader(nt(strings.Trim(rdf.MintIRI(rdf.ClassInvariant, "invariant.x"), "<>"), rdf.PropType, rdf.ClassInvariant, true)))
	if err != nil {
		t.Fatalf("ValidateArchitectureDialogueReferences: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("errs=%v", errs)
	}
}
