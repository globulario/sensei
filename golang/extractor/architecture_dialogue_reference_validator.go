// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/rdf"
)

var dialogueClosureBlockerIDRE = regexp.MustCompile(`^blocker\.(structural|authority|contract|behavioral|evidence|contradiction|direction|agent)\.[a-f0-9]{12}$`)
var dialogueSHA256RE = regexp.MustCompile(`^[a-f0-9]{64}$`)

type ArchitectureDialogueReferenceError struct {
	NodeID string
	Reason string
}

type dialogueNTNode struct {
	class string
	props map[string][]string
}

func (e ArchitectureDialogueReferenceError) Error() string {
	if e.NodeID == "" {
		return "architecture dialogue reference error: " + e.Reason
	}
	return fmt.Sprintf("architecture dialogue %s: %s", e.NodeID, e.Reason)
}

func ValidateArchitectureDialogueReferences(r io.Reader) ([]ArchitectureDialogueReferenceError, error) {
	nodes := map[string]*dialogueNTNode{}
	definedClaims := map[string]bool{}
	definedEvidence := map[string]bool{}
	definedGraphNodes := map[string]bool{}
	claimFacts := map[string]map[string]bool{}

	get := func(iri string) *dialogueNTNode {
		st := nodes[iri]
		if st == nil {
			st = &dialogueNTNode{props: map[string][]string{}}
			nodes[iri] = st
		}
		return st
	}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.HasSuffix(line, ".") {
			continue
		}
		toks := tokenize(strings.TrimSpace(strings.TrimSuffix(line, ".")))
		if len(toks) != 3 {
			continue
		}
		subjIRI := stripAngleBrackets(toks[0])
		pred := stripAngleBrackets(toks[1])
		obj := toks[2]
		objIRI := stripAngleBrackets(obj)
		if pred == rdf.PropType {
			definedGraphNodes[subjIRI] = true
			switch objIRI {
			case rdf.ClassOpenQuestion:
				get(subjIRI).class = "open_question"
			case rdf.ClassArchitectAnswer:
				get(subjIRI).class = "architect_answer"
			case rdf.ClassArchitectureClaim:
				definedClaims[subjIRI] = true
				if claimFacts[subjIRI] == nil {
					claimFacts[subjIRI] = map[string]bool{}
				}
			case rdf.ClassEvidence:
				definedEvidence[subjIRI] = false
			}
			continue
		}
		if pred == rdf.PropAuthoredIn && matchesClassSubject(subjIRI, rdf.ClassEvidence) {
			definedEvidence[subjIRI] = true
		}
		if pred == rdf.PropDerivedFromFact && matchesClassSubject(subjIRI, rdf.ClassArchitectureClaim) {
			if claimFacts[subjIRI] == nil {
				claimFacts[subjIRI] = map[string]bool{}
			}
			claimFacts[subjIRI][unquoteNTLiteral(obj)] = true
		}
		if matchesClassSubject(subjIRI, rdf.ClassOpenQuestion) || matchesClassSubject(subjIRI, rdf.ClassArchitectAnswer) {
			get(subjIRI).props[pred] = append(get(subjIRI).props[pred], obj)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	var errs []ArchitectureDialogueReferenceError
	questions := map[string]*dialogueNTNode{}
	answers := map[string]*dialogueNTNode{}
	for iri, st := range nodes {
		switch st.class {
		case "open_question":
			questions[iri] = st
		case "architect_answer":
			answers[iri] = st
		}
	}
	for iri, st := range questions {
		id := "open_question:" + extractIDFromIRI(iri, rdf.ClassOpenQuestion)
		requireDialogueProp(&errs, id, st, rdf.PropLabel, "label")
		requireDialogueProp(&errs, id, st, rdf.PropQuestionText, "questionText")
		requireDialogueProp(&errs, id, st, rdf.PropBlocksClosureDimension, "blocksClosureDimension")
		requireDialogueProp(&errs, id, st, rdf.PropAcceptedAnswerType, "acceptedAnswerType")
		requireDialogueProp(&errs, id, st, rdf.PropReasonOpen, "reasonOpen")
		requireDialogueProp(&errs, id, st, rdf.PropQuestionPriority, "questionPriority")
		requireDialogueProp(&errs, id, st, rdf.PropRiskIfUnresolved, "riskIfUnresolved")
		requireDialogueProp(&errs, id, st, rdf.PropArchitectRequired, "architectRequired")
		requireDialogueProp(&errs, id, st, rdf.PropQuestionStatus, "questionStatus")
		requireDialogueProp(&errs, id, st, rdf.PropCreatedAt, "createdAt")
		requireDialogueProp(&errs, id, st, rdf.PropSourceKind, "sourceKind")
		requireDialogueProp(&errs, id, st, rdf.PropValidForCommit, "validForCommit")
		requireDialogueProp(&errs, id, st, rdf.PropValidForGraphDigest, "validForGraphDigest")
		if !hasLiteral(st.props[rdf.PropSourceKind], "generated_candidate") {
			errs = append(errs, ArchitectureDialogueReferenceError{id, "sourceKind must be generated_candidate"})
		}
		if len(st.props[rdf.PropBlocksClaim])+len(st.props[rdf.PropBlocksNode])+len(st.props[rdf.PropBlocksClosureBlocker]) == 0 {
			errs = append(errs, ArchitectureDialogueReferenceError{id, "at least one claim, node, or closure blocker grounding is required"})
		}
		generatedFields := 0
		if len(st.props[rdf.PropQuestionTemplateID]) > 0 {
			generatedFields++
		}
		if len(st.props[rdf.PropQuestionTemplateVersion]) > 0 {
			generatedFields++
		}
		if len(st.props[rdf.PropSourceClosureAssessmentDigest]) > 0 {
			generatedFields++
		}
		if generatedFields != 0 {
			if generatedFields != 3 {
				errs = append(errs, ArchitectureDialogueReferenceError{id, "generated metadata must be all-or-none"})
			}
			if len(st.props[rdf.PropBlocksClosureBlocker]) == 0 {
				errs = append(errs, ArchitectureDialogueReferenceError{id, "generated question requires closure blocker grounding"})
			}
			for _, v := range st.props[rdf.PropSourceClosureAssessmentDigest] {
				if !dialogueSHA256RE.MatchString(unquoteNTLiteral(v)) {
					errs = append(errs, ArchitectureDialogueReferenceError{id, "source closure assessment digest must be lowercase SHA-256"})
				}
			}
		}
		for _, status := range st.props[rdf.PropQuestionStatus] {
			if !isQuestionStatusLiteral(status) {
				errs = append(errs, ArchitectureDialogueReferenceError{id, "invalid question status"})
			}
		}
		for _, obj := range st.props[rdf.PropBlocksNode] {
			nodeIRI := stripAngleBrackets(obj)
			if !definedGraphNodes[nodeIRI] {
				errs = append(errs, ArchitectureDialogueReferenceError{id, "blocked node is not defined: " + nodeIRI})
			}
		}
		for _, obj := range st.props[rdf.PropBlocksClosureBlocker] {
			blockerID := unquoteNTLiteral(obj)
			if !dialogueClosureBlockerIDRE.MatchString(blockerID) {
				errs = append(errs, ArchitectureDialogueReferenceError{id, "blocked closure blocker ID is malformed: " + blockerID})
			}
		}
		blockedClaims := map[string]bool{}
		for _, obj := range st.props[rdf.PropBlocksClaim] {
			claimIRI := stripAngleBrackets(obj)
			blockedClaims[claimIRI] = true
			if !definedClaims[claimIRI] {
				errs = append(errs, ArchitectureDialogueReferenceError{id, "blocked claim is not defined: " + extractIDFromIRI(claimIRI, rdf.ClassArchitectureClaim)})
			}
		}
		for _, obj := range st.props[rdf.PropGroundedByEvidence] {
			evIRI := stripAngleBrackets(obj)
			if !definedEvidence[evIRI] {
				errs = append(errs, ArchitectureDialogueReferenceError{id, "grounding evidence is not defined: " + extractIDFromIRI(evIRI, rdf.ClassEvidence)})
			}
		}
		for _, obj := range st.props[rdf.PropResolvedByAnswer] {
			answerIRI := stripAngleBrackets(obj)
			if answers[answerIRI] == nil {
				errs = append(errs, ArchitectureDialogueReferenceError{id, "resolution answer is not defined: " + extractIDFromIRI(answerIRI, rdf.ClassArchitectAnswer)})
			}
		}
		for _, obj := range st.props[rdf.PropSupersededBy] {
			qIRI := stripAngleBrackets(obj)
			if questions[qIRI] == nil {
				errs = append(errs, ArchitectureDialogueReferenceError{id, "superseding question is not defined: " + extractIDFromIRI(qIRI, rdf.ClassOpenQuestion)})
			}
		}
		for _, fact := range st.props[rdf.PropKnownFact] {
			factID := unquoteNTLiteral(fact)
			found := false
			for claimIRI := range blockedClaims {
				if claimFacts[claimIRI][factID] {
					found = true
					break
				}
			}
			if !found {
				errs = append(errs, ArchitectureDialogueReferenceError{id, "known fact is absent from blocked claim premises: " + factID})
			}
		}
	}

	acceptedForQuestion := map[string]bool{}
	usedResolution := map[string]bool{}
	for iri, st := range answers {
		id := "architect_answer:" + extractIDFromIRI(iri, rdf.ClassArchitectAnswer)
		requireDialogueProp(&errs, id, st, rdf.PropLabel, "label")
		requireDialogueProp(&errs, id, st, rdf.PropAnswersQuestion, "answersQuestion")
		requireDialogueProp(&errs, id, st, rdf.PropAuthorRole, "authorRole")
		requireDialogueProp(&errs, id, st, rdf.PropAnswerStatement, "answerStatement")
		requireDialogueProp(&errs, id, st, rdf.PropAnswerClassification, "answerClassification")
		requireDialogueProp(&errs, id, st, rdf.PropRecordedAt, "recordedAt")
		requireDialogueProp(&errs, id, st, rdf.PropAnswerGovernanceStatus, "answerGovernanceStatus")
		requireDialogueProp(&errs, id, st, rdf.PropSourceKind, "sourceKind")
		requireDialogueProp(&errs, id, st, rdf.PropValidForCommit, "validForCommit")
		requireDialogueProp(&errs, id, st, rdf.PropValidForGraphDigest, "validForGraphDigest")
		if !hasLiteral(st.props[rdf.PropSourceKind], "architect_dialogue") {
			errs = append(errs, ArchitectureDialogueReferenceError{id, "sourceKind must be architect_dialogue"})
		}
		for _, status := range st.props[rdf.PropAnswerGovernanceStatus] {
			if !isAnswerGovernanceLiteral(status) {
				errs = append(errs, ArchitectureDialogueReferenceError{id, "invalid answer governance status"})
			}
			if status == rdf.Lit(architecture.AnswerGovernanceAcceptedForQuestion) {
				acceptedForQuestion[iri] = true
			}
		}
		for _, obj := range st.props[rdf.PropAnswersQuestion] {
			qIRI := stripAngleBrackets(obj)
			q := questions[qIRI]
			if q == nil {
				errs = append(errs, ArchitectureDialogueReferenceError{id, "answered question is not defined: " + extractIDFromIRI(qIRI, rdf.ClassOpenQuestion)})
				continue
			}
			if !answerClassesAcceptedByQuestion(st, q) {
				errs = append(errs, ArchitectureDialogueReferenceError{id, "classification is not accepted by answered question " + extractIDFromIRI(qIRI, rdf.ClassOpenQuestion)})
			}
		}
		for _, obj := range st.props[rdf.PropCitesEvidence] {
			evIRI := stripAngleBrackets(obj)
			if !definedEvidence[evIRI] {
				errs = append(errs, ArchitectureDialogueReferenceError{id, "cited evidence is not defined: " + extractIDFromIRI(evIRI, rdf.ClassEvidence)})
			}
		}
		for _, obj := range st.props[rdf.PropSupersededBy] {
			answerIRI := stripAngleBrackets(obj)
			if answers[answerIRI] == nil {
				errs = append(errs, ArchitectureDialogueReferenceError{id, "superseding answer is not defined: " + extractIDFromIRI(answerIRI, rdf.ClassArchitectAnswer)})
			}
		}
		for _, lit := range st.props[rdf.PropSelectedHypothesis] {
			qid, hid, ok := strings.Cut(unquoteNTLiteral(lit), ":")
			if !ok || qid == "" || hid == "" {
				errs = append(errs, ArchitectureDialogueReferenceError{id, "selected hypothesis must be question_id:hypothesis_id"})
				continue
			}
			qIRI := stripAngleBrackets(rdf.MintIRI(rdf.ClassOpenQuestion, qid))
			q := questions[qIRI]
			if q == nil || !questionHasHypothesisLiteral(q, hid) {
				errs = append(errs, ArchitectureDialogueReferenceError{id, "selected hypothesis is not declared: " + qid + ":" + hid})
			}
		}
	}

	for iri, q := range questions {
		qid := "open_question:" + extractIDFromIRI(iri, rdf.ClassOpenQuestion)
		statuses := literalValues(q.props[rdf.PropQuestionStatus])
		for _, obj := range q.props[rdf.PropResolvedByAnswer] {
			answerIRI := stripAngleBrackets(obj)
			usedResolution[answerIRI] = true
			answer := answers[answerIRI]
			if answer == nil {
				continue
			}
			if !hasLiteral(answer.props[rdf.PropAnswerGovernanceStatus], architecture.AnswerGovernanceAcceptedForQuestion) {
				errs = append(errs, ArchitectureDialogueReferenceError{qid, "resolution answer must be accepted_for_question"})
			}
			if hasLiteral(answer.props[rdf.PropAnswerGovernanceStatus], architecture.AnswerGovernanceRejected) {
				errs = append(errs, ArchitectureDialogueReferenceError{qid, "rejected answer must not be used as resolution"})
			}
		}
		if dialogueStringContains(statuses, architecture.QuestionStatusResolved) && len(q.props[rdf.PropResolvedByAnswer]) == 0 {
			errs = append(errs, ArchitectureDialogueReferenceError{qid, "resolved question requires resolvedByAnswer"})
		}
		if dialogueStringContains(statuses, architecture.QuestionStatusAcceptedUnknown) && !hasAcceptedUnknownResolution(q, answers) {
			errs = append(errs, ArchitectureDialogueReferenceError{qid, "accepted_unknown requires unknown_acknowledgement resolution"})
		}
	}
	for iri := range acceptedForQuestion {
		if !usedResolution[iri] {
			errs = append(errs, ArchitectureDialogueReferenceError{"architect_answer:" + extractIDFromIRI(iri, rdf.ClassArchitectAnswer), "accepted_for_question answer is not used by a question resolution"})
		}
	}

	sort.Slice(errs, func(i, j int) bool {
		if errs[i].NodeID != errs[j].NodeID {
			return errs[i].NodeID < errs[j].NodeID
		}
		return errs[i].Reason < errs[j].Reason
	})
	return errs, nil
}

func requireDialogueProp(errs *[]ArchitectureDialogueReferenceError, id string, st *dialogueNTNode, prop, label string) {
	if len(st.props[prop]) == 0 {
		*errs = append(*errs, ArchitectureDialogueReferenceError{id, "missing required property " + label})
	}
}

func isQuestionStatusLiteral(v string) bool {
	switch unquoteNTLiteral(v) {
	case architecture.QuestionStatusOpen,
		architecture.QuestionStatusAwaitingArchitect,
		architecture.QuestionStatusAwaitingEvidence,
		architecture.QuestionStatusAnswered,
		architecture.QuestionStatusResolved,
		architecture.QuestionStatusAcceptedUnknown,
		architecture.QuestionStatusSuperseded:
		return true
	default:
		return false
	}
}

func isAnswerGovernanceLiteral(v string) bool {
	switch unquoteNTLiteral(v) {
	case architecture.AnswerGovernanceRecorded,
		architecture.AnswerGovernanceAwaitingEvidence,
		architecture.AnswerGovernanceAwaitingGovernance,
		architecture.AnswerGovernanceAcceptedForQuestion,
		architecture.AnswerGovernanceRejected,
		architecture.AnswerGovernanceSuperseded:
		return true
	default:
		return false
	}
}

func answerClassesAcceptedByQuestion(answer, question *dialogueNTNode) bool {
	accepted := map[string]bool{}
	for _, v := range question.props[rdf.PropAcceptedAnswerType] {
		accepted[unquoteNTLiteral(v)] = true
	}
	for _, v := range answer.props[rdf.PropAnswerClassification] {
		if !accepted[unquoteNTLiteral(v)] {
			return false
		}
	}
	return true
}

func questionHasHypothesisLiteral(question *dialogueNTNode, hypothesisID string) bool {
	prefix := hypothesisID + ":"
	for _, v := range question.props[rdf.PropCompetingHypothesis] {
		if strings.HasPrefix(unquoteNTLiteral(v), prefix) {
			return true
		}
	}
	return false
}

func literalValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, unquoteNTLiteral(v))
	}
	return out
}

func hasAcceptedUnknownResolution(question *dialogueNTNode, answers map[string]*dialogueNTNode) bool {
	for _, obj := range question.props[rdf.PropResolvedByAnswer] {
		answer := answers[stripAngleBrackets(obj)]
		if answer == nil {
			continue
		}
		classes := literalValues(answer.props[rdf.PropAnswerClassification])
		if len(classes) == 1 && classes[0] == architecture.AnswerTypeUnknownAcknowledgement &&
			hasLiteral(answer.props[rdf.PropAnswerGovernanceStatus], architecture.AnswerGovernanceAcceptedForQuestion) {
			return true
		}
	}
	return false
}

func dialogueStringContains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
