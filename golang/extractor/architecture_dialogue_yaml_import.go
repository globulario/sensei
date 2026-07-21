// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import (
	"fmt"
	"os"
	"strconv"

	yaml "gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/rdf"
)

type architectureDialogueEnvelope struct {
	ArchitectureDialogue architecture.DialogueDocument `yaml:"architecture_dialogue"`
}

func importArchitectureDialogue(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var env architectureDialogueEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	doc, err := architecture.NormalizeDialogueDocument(env.ArchitectureDialogue)
	if err != nil {
		return fmt.Errorf("validate architecture_dialogue: %w", err)
	}
	for _, q := range doc.OpenQuestions {
		emitOpenQuestion(e, path, doc.Binding, q)
	}
	for _, a := range doc.Answers {
		emitArchitectAnswer(e, path, doc.Binding, a)
	}
	return nil
}

func emitOpenQuestion(e *rdf.Emitter, path string, binding architecture.ClaimDocumentBinding, q architecture.OpenQuestion) {
	subj := rdf.MintIRI(rdf.ClassOpenQuestion, q.ID)
	e.Typed(subj, rdf.ClassOpenQuestion)
	e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(q.Label, q.ID)))
	e.Triple(subj, rdf.IRI(rdf.PropQuestionText), rdf.Lit(q.QuestionText))
	e.Triple(subj, rdf.IRI(rdf.PropBlocksClosureDimension), rdf.Lit(q.BlocksClosureDimension))
	for _, id := range q.BlocksClaims {
		e.Triple(subj, rdf.IRI(rdf.PropBlocksClaim), rdf.MintIRI(rdf.ClassArchitectureClaim, id))
	}
	for _, ref := range q.BlocksNodes {
		if iri, ok := claimReferenceIRI(ref); ok {
			e.Triple(subj, rdf.IRI(rdf.PropBlocksNode), iri)
		}
	}
	for _, id := range q.BlocksClosureBlockers {
		e.Triple(subj, rdf.IRI(rdf.PropBlocksClosureBlocker), rdf.Lit(id))
	}
	emitOptLit(e, subj, rdf.PropQuestionTemplateID, q.QuestionTemplateID)
	emitOptLit(e, subj, rdf.PropQuestionTemplateVersion, q.QuestionTemplateVersion)
	emitOptLit(e, subj, rdf.PropSourceClosureAssessmentDigest, q.SourceClosureAssessmentDigestSHA256)
	for _, typ := range q.AcceptedAnswerTypes {
		e.Triple(subj, rdf.IRI(rdf.PropAcceptedAnswerType), rdf.Lit(typ))
	}
	for _, reason := range q.ReasonsOpen {
		e.Triple(subj, rdf.IRI(rdf.PropReasonOpen), rdf.Lit(reason))
	}
	for _, id := range q.KnownFactIDs {
		e.Triple(subj, rdf.IRI(rdf.PropKnownFact), rdf.Lit(id))
	}
	for _, ref := range q.KnownEvidence {
		e.Triple(subj, rdf.IRI(rdf.PropGroundedByEvidence), evidenceRefIRI(ref))
	}
	for _, hyp := range q.CompetingHypotheses {
		e.Triple(subj, rdf.IRI(rdf.PropCompetingHypothesis), rdf.Lit(hyp.ID+": "+hyp.Statement))
	}
	for _, missing := range q.MissingEvidence {
		e.Triple(subj, rdf.IRI(rdf.PropMissingEvidence), rdf.Lit(missing))
	}
	e.Triple(subj, rdf.IRI(rdf.PropQuestionPriority), rdf.Lit(q.Priority))
	e.Triple(subj, rdf.IRI(rdf.PropRiskIfUnresolved), rdf.Lit(q.RiskIfUnresolved))
	e.Triple(subj, rdf.IRI(rdf.PropArchitectRequired), rdf.Lit(strconv.FormatBool(q.ArchitectRequired)))
	e.Triple(subj, rdf.IRI(rdf.PropQuestionStatus), rdf.Lit(q.Status))
	for _, id := range q.ResolvedByAnswers {
		e.Triple(subj, rdf.IRI(rdf.PropResolvedByAnswer), rdf.MintIRI(rdf.ClassArchitectAnswer, id))
	}
	if q.SupersededByQuestion != "" {
		e.Triple(subj, rdf.IRI(rdf.PropSupersededBy), rdf.MintIRI(rdf.ClassOpenQuestion, q.SupersededByQuestion))
	}
	e.Triple(subj, rdf.IRI(rdf.PropCreatedAt), rdf.Lit(q.CreatedAt))
	emitOptLit(e, subj, rdf.PropLastReviewedAt, q.LastReviewedAt)
	emitDialogueBinding(e, subj, binding)
	e.Triple(subj, rdf.IRI(rdf.PropSourceKind), rdf.Lit("generated_candidate"))
	e.Triple(subj, rdf.IRI(rdf.PropSourcePath), rdf.Lit(e.NormPath(path)))
	e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
	emitDialogueScope(e, subj, binding, q.Scope)
	emitDialogueAnchors(e, subj, q.Scope.Files, q.Scope.Symbols)
}

func emitArchitectAnswer(e *rdf.Emitter, path string, binding architecture.ClaimDocumentBinding, a architecture.ArchitectAnswer) {
	subj := rdf.MintIRI(rdf.ClassArchitectAnswer, a.ID)
	e.Typed(subj, rdf.ClassArchitectAnswer)
	e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(a.Label, a.ID)))
	for _, qid := range a.AnswersQuestions {
		e.Triple(subj, rdf.IRI(rdf.PropAnswersQuestion), rdf.MintIRI(rdf.ClassOpenQuestion, qid))
	}
	e.Triple(subj, rdf.IRI(rdf.PropAuthorRole), rdf.Lit(a.Author.Role))
	emitOptLit(e, subj, rdf.PropAuthorID, a.Author.ID)
	e.Triple(subj, rdf.IRI(rdf.PropAnswerStatement), rdf.Lit(a.Statement))
	for _, cls := range a.Classifications {
		e.Triple(subj, rdf.IRI(rdf.PropAnswerClassification), rdf.Lit(cls))
	}
	for _, condition := range a.Conditions {
		e.Triple(subj, rdf.IRI(rdf.PropAnswerCondition), rdf.Lit(condition))
	}
	for _, ref := range a.EvidenceRefs {
		e.Triple(subj, rdf.IRI(rdf.PropCitesEvidence), evidenceRefIRI(ref))
	}
	for _, pointer := range a.EvidencePointers {
		e.Triple(subj, rdf.IRI(rdf.PropEvidencePointer), rdf.Lit(pointer))
	}
	for _, sel := range a.SelectedHypotheses {
		e.Triple(subj, rdf.IRI(rdf.PropSelectedHypothesis), rdf.Lit(sel.QuestionID+":"+sel.HypothesisID))
	}
	emitOptLit(e, subj, rdf.PropReframedQuestion, a.ReframedQuestion)
	e.Triple(subj, rdf.IRI(rdf.PropRecordedAt), rdf.Lit(a.RecordedAt))
	e.Triple(subj, rdf.IRI(rdf.PropAnswerGovernanceStatus), rdf.Lit(a.GovernanceStatus))
	if a.SupersededByAnswer != "" {
		e.Triple(subj, rdf.IRI(rdf.PropSupersededBy), rdf.MintIRI(rdf.ClassArchitectAnswer, a.SupersededByAnswer))
	}
	emitDialogueBinding(e, subj, binding)
	e.Triple(subj, rdf.IRI(rdf.PropSourceKind), rdf.Lit("architect_dialogue"))
	e.Triple(subj, rdf.IRI(rdf.PropSourcePath), rdf.Lit(e.NormPath(path)))
	e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
	emitDialogueScope(e, subj, binding, a.Scope)
	emitDialogueAnchors(e, subj, a.Scope.Files, a.Scope.Symbols)
}

func emitDialogueBinding(e *rdf.Emitter, subj string, binding architecture.ClaimDocumentBinding) {
	emitOptLit(e, subj, rdf.PropValidForCommit, binding.Revision)
	emitOptLit(e, subj, rdf.PropValidForGraphDigest, binding.GraphDigestSHA256)
}

func emitDialogueScope(e *rdf.Emitter, subj string, binding architecture.ClaimDocumentBinding, scope architecture.ClaimScope) {
	if scope.Domain != "" {
		e.Triple(subj, rdf.IRI(rdf.PropDomain), rdf.Lit(scope.Domain))
	}
	repo := coalesce(scope.Repository, scope.Repo, binding.RepositoryDomain)
	if repo != "" {
		e.Triple(subj, rdf.IRI(rdf.PropRepo), rdf.Lit(repo))
	}
	emitOptLit(e, subj, rdf.PropSourceSet, scope.SourceSet)
}

func emitDialogueAnchors(e *rdf.Emitter, subj string, files, symbols []string) {
	emitClaimAnchors(e, subj, files, symbols)
}
