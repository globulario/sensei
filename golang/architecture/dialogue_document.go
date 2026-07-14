// SPDX-License-Identifier: AGPL-3.0-only

package architecture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type DialogueDocument struct {
	SchemaVersion string               `json:"schema_version" yaml:"schema_version"`
	CompiledBy    string               `json:"compiled_by" yaml:"compiled_by"`
	Binding       ClaimDocumentBinding `json:"binding" yaml:"binding"`
	OpenQuestions []OpenQuestion       `json:"open_questions" yaml:"open_questions"`
	Answers       []ArchitectAnswer    `json:"architect_answers,omitempty" yaml:"architect_answers,omitempty"`
}

type dialogueDocumentEnvelope struct {
	ArchitectureDialogue DialogueDocument `json:"architecture_dialogue" yaml:"architecture_dialogue"`
}

func NormalizeDialogueDocument(in DialogueDocument) (DialogueDocument, error) {
	doc := canonicalizeDialogueDocument(in)
	questions, err := NormalizeOpenQuestions(doc.OpenQuestions)
	if err != nil {
		return DialogueDocument{}, err
	}
	answers, err := NormalizeArchitectAnswers(doc.Answers)
	if err != nil {
		return DialogueDocument{}, err
	}
	doc.OpenQuestions = questions
	doc.Answers = answers
	if err := ValidateDialogueDocument(doc); err != nil {
		return DialogueDocument{}, err
	}
	return doc, nil
}

func ValidateDialogueDocument(doc DialogueDocument) error {
	var errs []string
	doc = canonicalizeDialogueDocument(doc)
	if doc.Binding.RevisionStatus == "" {
		errs = append(errs, "binding revision_status is required")
	}
	if doc.Binding.GraphDigestStatus == "" {
		errs = append(errs, "binding graph_digest_status is required")
	}
	if doc.Binding.RevisionStatus != "" && !oneOf(doc.Binding.RevisionStatus, RevisionResolved, RevisionUnavailable, RevisionNotGit, RevisionNotRequested) {
		errs = append(errs, "binding revision_status is invalid")
	}
	if doc.Binding.GraphDigestStatus != "" && !oneOf(doc.Binding.GraphDigestStatus, GraphDigestResolved, GraphDigestUnavailable, GraphDigestNotRequested) {
		errs = append(errs, "binding graph_digest_status is invalid")
	}

	questions := map[string]OpenQuestion{}
	for _, q := range doc.OpenQuestions {
		if _, ok := questions[q.ID]; ok {
			errs = append(errs, fmt.Sprintf("duplicate question id %s", q.ID))
		}
		questions[q.ID] = q
		if err := ValidateOpenQuestion(q); err != nil {
			errs = append(errs, fmt.Sprintf("question %s: %v", q.ID, err))
		}
		if !bindingCanResolveDialogue(doc.Binding) && !oneOf(q.Status, QuestionStatusOpen, QuestionStatusAwaitingArchitect, QuestionStatusAwaitingEvidence, QuestionStatusSuperseded) {
			errs = append(errs, fmt.Sprintf("question %s: status %s requires resolved revision and graph digest", q.ID, q.Status))
		}
		if err := validateDialogueScopeBinding(doc.Binding, q.Scope, "question "+q.ID); err != nil {
			errs = append(errs, err.Error())
		}
	}

	answers := map[string]ArchitectAnswer{}
	for _, a := range doc.Answers {
		if _, ok := answers[a.ID]; ok {
			errs = append(errs, fmt.Sprintf("duplicate answer id %s", a.ID))
		}
		answers[a.ID] = a
		if err := ValidateArchitectAnswer(a); err != nil {
			errs = append(errs, fmt.Sprintf("answer %s: %v", a.ID, err))
		}
		if !bindingCanResolveDialogue(doc.Binding) && a.GovernanceStatus == AnswerGovernanceAcceptedForQuestion {
			errs = append(errs, fmt.Sprintf("answer %s: accepted_for_question requires resolved revision and graph digest", a.ID))
		}
		if err := validateDialogueScopeBinding(doc.Binding, a.Scope, "answer "+a.ID); err != nil {
			errs = append(errs, err.Error())
		}
	}

	for _, a := range doc.Answers {
		answeredScope := ClaimScope{}
		for _, qid := range a.AnswersQuestions {
			q, ok := questions[qid]
			if !ok {
				errs = append(errs, fmt.Sprintf("answer %s: missing question %s", a.ID, qid))
				continue
			}
			if !classificationsAccepted(a.Classifications, q.AcceptedAnswerTypes) {
				errs = append(errs, fmt.Sprintf("answer %s: classification is not accepted by question %s", a.ID, q.ID))
			}
			answeredScope = mergeClaimScopes(answeredScope, q.Scope)
		}
		if !contains(a.Classifications, AnswerTypeScopeClarification) && !claimScopeSubset(a.Scope, answeredScope) {
			errs = append(errs, fmt.Sprintf("answer %s: scope broadens answered question scope", a.ID))
		}
		for _, sel := range a.SelectedHypotheses {
			q, ok := questions[sel.QuestionID]
			if !ok {
				errs = append(errs, fmt.Sprintf("answer %s: selected hypothesis references missing question %s", a.ID, sel.QuestionID))
				continue
			}
			if !contains(a.AnswersQuestions, sel.QuestionID) {
				errs = append(errs, fmt.Sprintf("answer %s: selected hypothesis question is not answered by this answer", a.ID))
			}
			if !questionHasHypothesis(q, sel.HypothesisID) {
				errs = append(errs, fmt.Sprintf("answer %s: selected hypothesis %s does not exist on question %s", a.ID, sel.HypothesisID, sel.QuestionID))
			}
		}
		if a.GovernanceStatus == AnswerGovernanceSuperseded {
			if _, ok := answers[a.SupersededByAnswer]; !ok {
				errs = append(errs, fmt.Sprintf("answer %s: missing superseding answer %s", a.ID, a.SupersededByAnswer))
			}
		}
	}

	resolutionUses := map[string]bool{}
	for _, q := range doc.OpenQuestions {
		pointedAnswers := answersForQuestion(doc.Answers, q.ID)
		switch q.Status {
		case QuestionStatusAnswered:
			if len(pointedAnswers) == 0 {
				errs = append(errs, fmt.Sprintf("question %s: answered status requires at least one answer", q.ID))
			}
			for _, aid := range q.ResolvedByAnswers {
				if a, ok := answers[aid]; ok && a.GovernanceStatus == AnswerGovernanceAcceptedForQuestion {
					errs = append(errs, fmt.Sprintf("question %s: answered status must not list accepted resolution answer", q.ID))
				}
			}
		case QuestionStatusResolved, QuestionStatusAcceptedUnknown:
			for _, aid := range q.ResolvedByAnswers {
				a, ok := answers[aid]
				if !ok {
					errs = append(errs, fmt.Sprintf("question %s: missing resolution answer %s", q.ID, aid))
					continue
				}
				resolutionUses[aid] = true
				if !contains(a.AnswersQuestions, q.ID) {
					errs = append(errs, fmt.Sprintf("question %s: resolution answer %s does not answer this question", q.ID, aid))
				}
				if a.GovernanceStatus != AnswerGovernanceAcceptedForQuestion {
					errs = append(errs, fmt.Sprintf("question %s: resolution answer %s must be accepted_for_question", q.ID, aid))
				}
				if !classificationsAccepted(a.Classifications, q.AcceptedAnswerTypes) {
					errs = append(errs, fmt.Sprintf("question %s: resolution answer %s classification is not accepted", q.ID, aid))
				}
				if q.Status == QuestionStatusAcceptedUnknown && !(len(a.Classifications) == 1 && a.Classifications[0] == AnswerTypeUnknownAcknowledgement) {
					errs = append(errs, fmt.Sprintf("question %s: accepted_unknown requires unknown_acknowledgement resolution", q.ID))
				}
			}
		case QuestionStatusSuperseded:
			if _, ok := questions[q.SupersededByQuestion]; !ok {
				errs = append(errs, fmt.Sprintf("question %s: missing superseding question %s", q.ID, q.SupersededByQuestion))
			}
			for _, aid := range q.ResolvedByAnswers {
				a, ok := answers[aid]
				if !ok {
					errs = append(errs, fmt.Sprintf("question %s: missing supersession answer %s", q.ID, aid))
					continue
				}
				resolutionUses[aid] = true
				if !contains(a.AnswersQuestions, q.ID) {
					errs = append(errs, fmt.Sprintf("question %s: supersession answer %s does not answer this question", q.ID, aid))
				}
				if a.GovernanceStatus != AnswerGovernanceAcceptedForQuestion {
					errs = append(errs, fmt.Sprintf("question %s: supersession answer %s must be accepted_for_question", q.ID, aid))
				}
			}
		}
	}
	for _, a := range doc.Answers {
		if a.GovernanceStatus == AnswerGovernanceAcceptedForQuestion && !resolutionUses[a.ID] {
			errs = append(errs, fmt.Sprintf("answer %s: accepted_for_question must be referenced by a question resolution", a.ID))
		}
		if a.GovernanceStatus == AnswerGovernanceRejected && resolutionUses[a.ID] {
			errs = append(errs, fmt.Sprintf("answer %s: rejected answer must not be used as resolution", a.ID))
		}
	}
	if cycle := questionSupersessionCycle(doc.OpenQuestions); cycle != "" {
		errs = append(errs, "question supersession cycle: "+cycle)
	}
	if cycle := answerSupersessionCycle(doc.Answers); cycle != "" {
		errs = append(errs, "answer supersession cycle: "+cycle)
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func MarshalCanonicalDialogueDocument(doc DialogueDocument) ([]byte, error) {
	doc, err := NormalizeDialogueDocument(doc)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func MarshalCanonicalDialogueDocumentYAML(doc DialogueDocument) ([]byte, error) {
	doc, err := NormalizeDialogueDocument(doc)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(dialogueDocumentEnvelope{ArchitectureDialogue: doc})
}

func UnmarshalDialogueDocumentYAML(data []byte) (DialogueDocument, error) {
	var env dialogueDocumentEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return DialogueDocument{}, err
	}
	if env.ArchitectureDialogue.SchemaVersion == "" && env.ArchitectureDialogue.CompiledBy == "" && len(env.ArchitectureDialogue.OpenQuestions) == 0 {
		return DialogueDocument{}, errors.New("missing architecture_dialogue document")
	}
	return NormalizeDialogueDocument(env.ArchitectureDialogue)
}

func LoadDialogueDocument(path string) (DialogueDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DialogueDocument{}, err
	}
	return UnmarshalDialogueDocumentYAML(data)
}

func canonicalizeDialogueDocument(in DialogueDocument) DialogueDocument {
	doc := in
	doc.SchemaVersion = strings.TrimSpace(doc.SchemaVersion)
	doc.CompiledBy = strings.TrimSpace(doc.CompiledBy)
	doc.Binding.RepositoryDomain = strings.TrimSpace(doc.Binding.RepositoryDomain)
	doc.Binding.Revision = strings.TrimSpace(doc.Binding.Revision)
	doc.Binding.RevisionStatus = strings.TrimSpace(doc.Binding.RevisionStatus)
	doc.Binding.GraphDigestSHA256 = strings.TrimSpace(doc.Binding.GraphDigestSHA256)
	doc.Binding.GraphDigestStatus = strings.TrimSpace(doc.Binding.GraphDigestStatus)
	return doc
}

func bindingCanResolveDialogue(b ClaimDocumentBinding) bool {
	return b.RevisionStatus == RevisionResolved && b.GraphDigestStatus == GraphDigestResolved
}

func validateDialogueScopeBinding(binding ClaimDocumentBinding, scope ClaimScope, label string) error {
	if binding.RepositoryDomain == "" {
		return nil
	}
	for _, repo := range []string{scope.Repository, scope.Repo} {
		if repo != "" && repo != binding.RepositoryDomain {
			return fmt.Errorf("%s: scope repository %s does not match document binding", label, repo)
		}
	}
	return nil
}

func classificationsAccepted(classes, accepted []string) bool {
	for _, cls := range classes {
		if !contains(accepted, cls) {
			return false
		}
	}
	return true
}

func answersForQuestion(answers []ArchitectAnswer, qid string) []ArchitectAnswer {
	var out []ArchitectAnswer
	for _, a := range answers {
		if contains(a.AnswersQuestions, qid) {
			out = append(out, a)
		}
	}
	return out
}

func questionHasHypothesis(q OpenQuestion, id string) bool {
	for _, h := range q.CompetingHypotheses {
		if h.ID == id {
			return true
		}
	}
	return false
}

func mergeClaimScopes(a, b ClaimScope) ClaimScope {
	a.Repository = coalesceNonEmpty(a.Repository, b.Repository)
	a.Repo = coalesceNonEmpty(a.Repo, b.Repo)
	a.Domain = coalesceNonEmpty(a.Domain, b.Domain)
	a.SourceSet = coalesceNonEmpty(a.SourceSet, b.SourceSet)
	a.Files = cleanStringList(append(a.Files, b.Files...), true)
	a.Symbols = cleanStringList(append(a.Symbols, b.Symbols...), false)
	a.Components = cleanStringList(append(a.Components, b.Components...), false)
	return a
}

func claimScopeSubset(sub, allowed ClaimScope) bool {
	if sub.Repository != "" && allowed.Repository != "" && sub.Repository != allowed.Repository {
		return false
	}
	if sub.Repo != "" && allowed.Repo != "" && sub.Repo != allowed.Repo {
		return false
	}
	if sub.Domain != "" && allowed.Domain != "" && sub.Domain != allowed.Domain {
		return false
	}
	return stringSliceSubset(sub.Files, allowed.Files) &&
		stringSliceSubset(sub.Symbols, allowed.Symbols) &&
		stringSliceSubset(sub.Components, allowed.Components)
}

func stringSliceSubset(sub, allowed []string) bool {
	if len(sub) == 0 {
		return true
	}
	allow := map[string]bool{}
	for _, item := range allowed {
		allow[item] = true
	}
	for _, item := range sub {
		if !allow[item] {
			return false
		}
	}
	return true
}

func questionSupersessionCycle(questions []OpenQuestion) string {
	next := map[string]string{}
	for _, q := range questions {
		if q.SupersededByQuestion != "" {
			next[q.ID] = q.SupersededByQuestion
		}
	}
	return supersessionCycle(next)
}

func answerSupersessionCycle(answers []ArchitectAnswer) string {
	next := map[string]string{}
	for _, a := range answers {
		if a.SupersededByAnswer != "" {
			next[a.ID] = a.SupersededByAnswer
		}
	}
	return supersessionCycle(next)
}

func supersessionCycle(next map[string]string) string {
	keys := make([]string, 0, len(next))
	for k := range next {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, start := range keys {
		seen := map[string]bool{}
		for cur := start; cur != ""; cur = next[cur] {
			if seen[cur] {
				return cur
			}
			seen[cur] = true
		}
	}
	return ""
}

func coalesceNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
