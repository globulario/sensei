// SPDX-License-Identifier: AGPL-3.0-only

package questiongen

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigator"
)

const (
	InvestigationQuestionSchemaVersion = "1"
	InvestigationQuestionGeneratedBy   = "sensei generate-investigation-questions"

	TemplateStructuralWhy            = "question.structural_why.v1"
	TemplateMissingContractCandidate = "question.missing_contract_candidate.v1"
	TemplateOwnerCandidate           = "question.owner_candidate.v1"
	TemplateFailureModeCandidate     = "question.failure_mode_candidate.v1"
	TemplateCounterexampleValidation = "question.counterexample_validation.v1"
	TemplateEvidenceRequest          = "question.evidence_request.v1"
	TemplateGovernanceDebt           = "question.governance_debt.v1"
	TemplateRepeatedDeviation        = "question.repeated_deviation.v1"

	InvestigationDispositionGenerated      = "generated"
	InvestigationDispositionExistingCovers = "existing_covers"
	InvestigationDispositionSkipped        = "skipped"
)

// InvestigationQuestionContext binds deterministic investigator output to the
// existing architecture dialogue owner. It does not write or promote knowledge.
type InvestigationQuestionContext struct {
	Investigation investigator.Result
	Existing      *architecture.DialogueDocument
	CreatedAt     string
}

// InvestigationQuestionResult returns the normalized dialogue plus explicit
// disposition accounting for every question-worthy investigator sidecar.
type InvestigationQuestionResult struct {
	Dialogue architecture.DialogueDocument
	Report   InvestigationQuestionReport
}

// InvestigationQuestionReport records exact source binding and deterministic
// question-generation dispositions.
type InvestigationQuestionReport struct {
	SchemaVersion                   string                            `json:"schema_version" yaml:"schema_version"`
	GeneratedBy                     string                            `json:"generated_by" yaml:"generated_by"`
	Binding                         architecture.ClaimDocumentBinding `json:"binding" yaml:"binding"`
	SourceInvestigationDigestSHA256 string                            `json:"source_investigation_digest_sha256" yaml:"source_investigation_digest_sha256"`
	Generated                       []InvestigationQuestionItem       `json:"generated,omitempty" yaml:"generated,omitempty"`
	ExistingCoverage                []InvestigationQuestionItem       `json:"existing_coverage,omitempty" yaml:"existing_coverage,omitempty"`
	Skipped                         []InvestigationQuestionItem       `json:"skipped,omitempty" yaml:"skipped,omitempty"`
}

// InvestigationQuestionItem accounts for one evidence request or challenge.
type InvestigationQuestionItem struct {
	SourceKind  architecture.QuestionSourceKind `json:"source_kind" yaml:"source_kind"`
	SourceID    string                          `json:"source_id" yaml:"source_id"`
	CandidateID string                          `json:"candidate_id,omitempty" yaml:"candidate_id,omitempty"`
	ClaimID     string                          `json:"claim_id,omitempty" yaml:"claim_id,omitempty"`
	Disposition string                          `json:"disposition" yaml:"disposition"`
	TemplateID  string                          `json:"template_id,omitempty" yaml:"template_id,omitempty"`
	QuestionID  string                          `json:"question_id,omitempty" yaml:"question_id,omitempty"`
	ReasonCode  string                          `json:"reason_code" yaml:"reason_code"`
	Detail      string                          `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// GenerateFromInvestigation creates governed open questions from exact,
// receipt-bound investigator output. It appends questions only; answers and
// promotion remain owned by the existing dialogue and proposal paths.
func GenerateFromInvestigation(ctx InvestigationQuestionContext) (InvestigationQuestionResult, error) {
	if strings.TrimSpace(ctx.CreatedAt) == "" {
		return InvestigationQuestionResult{}, errors.New("created_at is required")
	}
	actualDigest, err := investigator.ResultDigest(ctx.Investigation)
	if err != nil {
		return InvestigationQuestionResult{}, fmt.Errorf("investigation result is not receipt-valid: %w", err)
	}
	if ctx.Investigation.Receipt.ExactResultDigestSHA256 == "" || ctx.Investigation.Receipt.ExactResultDigestSHA256 != actualDigest {
		return InvestigationQuestionResult{}, errors.New("investigation exact result digest does not match its receipt")
	}

	binding := ctx.Investigation.Binding.Repository
	dialogue := architecture.DialogueDocument{
		SchemaVersion: "1",
		CompiledBy:    InvestigationQuestionGeneratedBy,
		Binding:       binding,
	}
	if ctx.Existing != nil {
		if !bindingsEqual(ctx.Existing.Binding, binding) {
			return InvestigationQuestionResult{}, errors.New("existing dialogue binding does not match investigation result")
		}
		dialogue = *ctx.Existing
		dialogue.OpenQuestions = append([]architecture.OpenQuestion(nil), ctx.Existing.OpenQuestions...)
		dialogue.Answers = append([]architecture.ArchitectAnswer(nil), ctx.Existing.Answers...)
	}
	report := InvestigationQuestionReport{
		SchemaVersion:                   InvestigationQuestionSchemaVersion,
		GeneratedBy:                     InvestigationQuestionGeneratedBy,
		Binding:                         binding,
		SourceInvestigationDigestSHA256: actualDigest,
	}

	claimsByID := make(map[string]architecture.Claim, len(ctx.Investigation.Document.CandidateClaims))
	for _, claim := range ctx.Investigation.Document.CandidateClaims {
		claimsByID[claim.ID] = claim
	}
	candidatesByID := make(map[string]investigator.CandidateEnvelope, len(ctx.Investigation.Candidates))
	for _, candidate := range ctx.Investigation.Candidates {
		candidatesByID[candidate.CandidateID] = candidate
	}
	questionedCandidateIDs := map[string]bool{}

	requests := append([]investigator.EvidenceRequest(nil), ctx.Investigation.EvidenceRequests...)
	sort.SliceStable(requests, func(i, j int) bool { return requests[i].ID < requests[j].ID })
	for _, request := range requests {
		candidate, ok := candidatesByID[request.CandidateID]
		if !ok {
			return InvestigationQuestionResult{}, fmt.Errorf("evidence request %s references missing candidate %s", request.ID, request.CandidateID)
		}
		claim, ok := claimsByID[candidate.ClaimID]
		if !ok {
			return InvestigationQuestionResult{}, fmt.Errorf("candidate %s references missing claim %s", candidate.CandidateID, candidate.ClaimID)
		}
		question := evidenceRequestQuestion(ctx.CreatedAt, actualDigest, request, candidate, claim)
		disposition, err := appendInvestigationQuestion(&dialogue, question)
		if err != nil {
			return InvestigationQuestionResult{}, err
		}
		item := investigationQuestionItem(
			architecture.SourceEvidenceGap,
			request.ID,
			candidate.CandidateID,
			claim.ID,
			disposition,
			TemplateEvidenceRequest,
			question.ID,
			request.ReasonCode,
			request.Description,
		)
		appendInvestigationDisposition(&report, item)
		questionedCandidateIDs[candidate.CandidateID] = true
	}

	challenges := append([]investigator.ChallengeReceipt(nil), ctx.Investigation.Challenges...)
	sort.SliceStable(challenges, func(i, j int) bool { return challenges[i].ID < challenges[j].ID })
	for _, challenge := range challenges {
		if challenge.Status != investigator.ChallengeContested && challenge.Status != investigator.ChallengeRefuted {
			report.Skipped = append(report.Skipped, investigationQuestionItem(
				architecture.SourceCounterexample,
				challenge.ID,
				challenge.CandidateID,
				"",
				InvestigationDispositionSkipped,
				TemplateCounterexampleValidation,
				"",
				"investigation.challenge_not_question_worthy",
				"only contested or refuted challenges require governed architectural dialogue",
			))
			continue
		}
		candidate, ok := candidatesByID[challenge.CandidateID]
		if !ok {
			return InvestigationQuestionResult{}, fmt.Errorf("challenge %s references missing candidate %s", challenge.ID, challenge.CandidateID)
		}
		claim, ok := claimsByID[candidate.ClaimID]
		if !ok {
			return InvestigationQuestionResult{}, fmt.Errorf("candidate %s references missing claim %s", candidate.CandidateID, candidate.ClaimID)
		}
		question := challengeQuestion(ctx.CreatedAt, actualDigest, challenge, candidate, claim)
		disposition, err := appendInvestigationQuestion(&dialogue, question)
		if err != nil {
			return InvestigationQuestionResult{}, err
		}
		item := investigationQuestionItem(
			architecture.SourceCounterexample,
			challenge.ID,
			candidate.CandidateID,
			claim.ID,
			disposition,
			TemplateCounterexampleValidation,
			question.ID,
			challenge.ReasonCode,
			"challenge requires governed interpretation",
		)
		appendInvestigationDisposition(&report, item)
		questionedCandidateIDs[candidate.CandidateID] = true
	}

	candidates := append([]investigator.CandidateEnvelope(nil), ctx.Investigation.Candidates...)
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].CandidateID < candidates[j].CandidateID })
	for _, candidate := range candidates {
		if questionedCandidateIDs[candidate.CandidateID] {
			report.Skipped = append(report.Skipped, investigationQuestionItem(
				architecture.SourceInvestigationCandidate,
				candidate.CandidateID,
				candidate.CandidateID,
				candidate.ClaimID,
				InvestigationDispositionSkipped,
				"",
				"",
				"investigation.more_specific_question_exists",
				"an evidence-gap or counterexample question already covers this candidate",
			))
			continue
		}
		templateID, ok := candidateQuestionTemplate(candidate.OutputKind)
		if !ok {
			report.Skipped = append(report.Skipped, investigationQuestionItem(
				architecture.SourceInvestigationCandidate,
				candidate.CandidateID,
				candidate.CandidateID,
				candidate.ClaimID,
				InvestigationDispositionSkipped,
				"",
				"",
				"investigation.unsupported_candidate_template",
				"no stable Phase 10.5 question template is registered for this candidate kind",
			))
			continue
		}
		claim, ok := claimsByID[candidate.ClaimID]
		if !ok {
			return InvestigationQuestionResult{}, fmt.Errorf("candidate %s references missing claim %s", candidate.CandidateID, candidate.ClaimID)
		}
		question := candidateQuestion(ctx.CreatedAt, actualDigest, templateID, candidate, claim)
		disposition, err := appendInvestigationQuestion(&dialogue, question)
		if err != nil {
			return InvestigationQuestionResult{}, err
		}
		item := investigationQuestionItem(
			architecture.SourceInvestigationCandidate,
			candidate.CandidateID,
			candidate.CandidateID,
			claim.ID,
			disposition,
			templateID,
			question.ID,
			"investigation.candidate_requires_governed_review",
			"candidate question routed through the canonical architecture dialogue",
		)
		appendInvestigationDisposition(&report, item)
	}

	normalized, err := architecture.NormalizeDialogueDocument(dialogue)
	if err != nil {
		return InvestigationQuestionResult{}, err
	}
	report = normalizeInvestigationQuestionReport(report)
	return InvestigationQuestionResult{Dialogue: normalized, Report: report}, nil
}

func evidenceRequestQuestion(
	createdAt string,
	sourceDigest string,
	request investigator.EvidenceRequest,
	candidate investigator.CandidateEnvelope,
	claim architecture.Claim,
) architecture.OpenQuestion {
	architectRequired := request.ReasonCode == investigator.ReasonOwnerAuthorityUnresolved ||
		request.ReasonCode == investigator.ReasonBoundaryScopeUnresolved ||
		request.ReasonCode == investigator.ReasonHistoricalRationaleUnresolved
	status := architecture.QuestionStatusAwaitingEvidence
	if architectRequired {
		status = architecture.QuestionStatusAwaitingArchitect
	}
	accepted := []string{
		architecture.AnswerTypeEvidencePointer,
		architecture.AnswerTypeHistoricalContext,
		architecture.AnswerTypeScopeClarification,
		architecture.AnswerTypeUnknownAcknowledgement,
		architecture.AnswerTypeQuestionReframing,
	}
	if architectRequired {
		accepted = append(accepted, architecture.AnswerTypeIntentStatement, architecture.AnswerTypeGovernedDecisionCandidate)
	}
	question := architecture.OpenQuestion{
		QuestionText:               evidenceRequestQuestionText(request, claim),
		Scope:                      request.Scope,
		BlocksClosureDimension:     questionDimension(candidate.OutputKind, false),
		BlocksClaims:               []string{claim.ID},
		BlocksNodes:                []string{"architecture_claim:" + claim.ID},
		QuestionTemplateID:         TemplateEvidenceRequest,
		QuestionTemplateVersion:    "v1",
		QuestionSourceKind:         architecture.SourceEvidenceGap,
		SourceArtifactDigestSHA256: sourceDigest,
		SourceReferenceIDs:         []string{request.ID, candidate.CandidateID, claim.ID},
		AcceptedAnswerTypes:        accepted,
		ReasonsOpen:                []string{request.ReasonCode, request.Description},
		KnownFactIDs:               candidate.ObservationRefIDs,
		KnownEvidence:              evidenceReferences(candidate),
		SupportingEvidence:         supportingEvidenceReferences(candidate),
		RefutingEvidence:           refutingEvidenceReferences(candidate),
		FalsificationConditions:    candidate.FalsificationConditions,
		SuggestedAnswerOwner:       evidenceRequestAnswerOwner(request),
		MissingEvidence: []string{
			request.Description,
			"required proof strength: " + string(request.RequiredProofStrength),
			"evidence category: " + string(request.Category),
		},
		Priority:          investigationQuestionPriority(candidate.OutputKind, false),
		RiskIfUnresolved:  "The architectural candidate remains ungrounded at the required proof strength and cannot be promoted safely.",
		ArchitectRequired: architectRequired,
		Status:            status,
		CreatedAt:         strings.TrimSpace(createdAt),
	}
	question.ID = architecture.StableOpenQuestionID(question)
	return question
}

func challengeQuestion(
	createdAt string,
	sourceDigest string,
	challenge investigator.ChallengeReceipt,
	candidate investigator.CandidateEnvelope,
	claim architecture.Claim,
) architecture.OpenQuestion {
	proposition := claimProposition(claim)
	priority := architecture.QuestionPriorityHigh
	if challenge.Status == investigator.ChallengeRefuted {
		priority = architecture.QuestionPriorityCritical
	}
	question := architecture.OpenQuestion{
		QuestionText:               "Bound evidence contests the architectural candidate: " + proposition + ". Which interpretation is current, scoped differently, historical, or unsupported?",
		Scope:                      claim.Scope,
		BlocksClosureDimension:     architecture.ClosureContradiction,
		BlocksClaims:               []string{claim.ID},
		BlocksNodes:                []string{"architecture_claim:" + claim.ID},
		QuestionTemplateID:         TemplateCounterexampleValidation,
		QuestionTemplateVersion:    "v1",
		QuestionSourceKind:         architecture.SourceCounterexample,
		SourceArtifactDigestSHA256: sourceDigest,
		SourceReferenceIDs:         []string{challenge.ID, candidate.CandidateID, claim.ID},
		AcceptedAnswerTypes: []string{
			architecture.AnswerTypeIntentStatement,
			architecture.AnswerTypeGovernedDecisionCandidate,
			architecture.AnswerTypeHistoricalContext,
			architecture.AnswerTypeScopeClarification,
			architecture.AnswerTypeEvidencePointer,
			architecture.AnswerTypeUnknownAcknowledgement,
			architecture.AnswerTypeQuestionReframing,
		},
		ReasonsOpen:             []string{challenge.ReasonCode, "investigation challenge status: " + string(challenge.Status)},
		KnownFactIDs:            candidate.ObservationRefIDs,
		KnownEvidence:           evidenceReferences(candidate),
		SupportingEvidence:      supportingEvidenceReferences(candidate),
		RefutingEvidence:        refutingEvidenceReferences(candidate),
		FalsificationConditions: candidate.FalsificationConditions,
		SuggestedAnswerOwner:    "architect",
		MissingEvidence:         nil,
		CompetingHypotheses: []architecture.QuestionHypothesis{
			{ID: "candidate.current", Statement: "The candidate describes the current intended architecture in this scope."},
			{ID: "candidate.contested", Statement: "The refuting evidence shows the candidate is historical, scoped differently, or unsupported."},
		},
		Priority:          priority,
		RiskIfUnresolved:  "Contradictory architectural evidence remains unresolved and must not be converted into canonical truth.",
		ArchitectRequired: true,
		Status:            architecture.QuestionStatusAwaitingArchitect,
		CreatedAt:         strings.TrimSpace(createdAt),
	}
	question.ID = architecture.StableOpenQuestionID(question)
	return question
}

func candidateQuestion(
	createdAt string,
	sourceDigest string,
	templateID string,
	candidate investigator.CandidateEnvelope,
	claim architecture.Claim,
) architecture.OpenQuestion {
	proposition := claimProposition(claim)
	question := architecture.OpenQuestion{
		QuestionText:               candidateQuestionText(candidate.OutputKind, proposition),
		Scope:                      claim.Scope,
		BlocksClosureDimension:     questionDimension(candidate.OutputKind, false),
		BlocksClaims:               []string{claim.ID},
		BlocksNodes:                []string{"architecture_claim:" + claim.ID},
		QuestionTemplateID:         templateID,
		QuestionTemplateVersion:    "v1",
		QuestionSourceKind:         architecture.SourceInvestigationCandidate,
		SourceArtifactDigestSHA256: sourceDigest,
		SourceReferenceIDs:         []string{candidate.CandidateID, claim.ID},
		AcceptedAnswerTypes:        candidateAcceptedAnswerTypes(candidate.OutputKind),
		ReasonsOpen: []string{
			"investigation candidate remains non-authoritative",
			"candidate kind: " + string(candidate.OutputKind),
		},
		KnownFactIDs:            candidate.ObservationRefIDs,
		KnownEvidence:           evidenceReferences(candidate),
		SupportingEvidence:      supportingEvidenceReferences(candidate),
		RefutingEvidence:        refutingEvidenceReferences(candidate),
		FalsificationConditions: candidate.FalsificationConditions,
		SuggestedAnswerOwner:    candidateAnswerOwner(candidate.OutputKind),
		MissingEvidence:         append([]string(nil), candidate.MissingEvidenceRequestIDs...),
		Priority:                investigationQuestionPriority(candidate.OutputKind, false),
		RiskIfUnresolved:        "The architectural candidate cannot enter governed truth until its intent, scope, and evidence are reviewed.",
		ArchitectRequired:       true,
		Status:                  architecture.QuestionStatusAwaitingArchitect,
		CreatedAt:               strings.TrimSpace(createdAt),
	}
	question.ID = architecture.StableOpenQuestionID(question)
	return question
}

func candidateQuestionTemplate(kind investigator.CandidateKind) (string, bool) {
	switch kind {
	case investigator.KindBoundary:
		return TemplateStructuralWhy, true
	case investigator.KindContract:
		return TemplateMissingContractCandidate, true
	case investigator.KindOwner:
		return TemplateOwnerCandidate, true
	case investigator.KindFailureMode:
		return TemplateFailureModeCandidate, true
	case investigator.KindGovernanceDebt:
		return TemplateGovernanceDebt, true
	default:
		return "", false
	}
}

func candidateQuestionText(kind investigator.CandidateKind, proposition string) string {
	switch kind {
	case investigator.KindBoundary:
		return "The repository structurally exhibits this boundary candidate: " + proposition + ". Is this the intended current boundary, a historical artifact, or an accidental crossing, and which evidence would falsify that interpretation?"
	case investigator.KindContract:
		return "The investigation found a contract candidate: " + proposition + ". What exact stability, ownership, and compatibility semantics are intended, and which evidence would falsify them?"
	case investigator.KindOwner:
		return "The investigation found a possible single-writer owner: " + proposition + ". Is this component intentionally authoritative, which mutation paths are allowed, and which evidence would falsify that ownership?"
	case investigator.KindFailureMode:
		return "The investigation found a failure-mode candidate: " + proposition + ". Must the architecture prevent, detect, or recover from it, and which observation would falsify the candidate?"
	case investigator.KindGovernanceDebt:
		return "The investigation found a governance-debt candidate: " + proposition + ". Which governed rule or evidence is missing, and what would falsify the need for that rule?"
	default:
		return "The investigation found an architectural candidate: " + proposition + ". Is it intended current architecture, and which evidence would falsify it?"
	}
}

func candidateAcceptedAnswerTypes(kind investigator.CandidateKind) []string {
	accepted := []string{
		architecture.AnswerTypeIntentStatement,
		architecture.AnswerTypeGovernedDecisionCandidate,
		architecture.AnswerTypeHistoricalContext,
		architecture.AnswerTypeScopeClarification,
		architecture.AnswerTypeEvidencePointer,
		architecture.AnswerTypeUnknownAcknowledgement,
		architecture.AnswerTypeQuestionReframing,
	}
	if kind == investigator.KindGovernanceDebt {
		accepted = append(accepted, architecture.AnswerTypeExceptionAuthorization)
	}
	return accepted
}

func candidateAnswerOwner(kind investigator.CandidateKind) string {
	switch kind {
	case investigator.KindBoundary:
		return "boundary owner or architect"
	case investigator.KindContract:
		return "contract owner or architect"
	case investigator.KindOwner:
		return "architect responsible for authority"
	case investigator.KindFailureMode:
		return "runtime owner or architect"
	case investigator.KindGovernanceDebt:
		return "governance owner or architect"
	default:
		return "architect"
	}
}

func evidenceRequestAnswerOwner(request investigator.EvidenceRequest) string {
	switch request.ReasonCode {
	case investigator.ReasonOwnerAuthorityUnresolved, investigator.ReasonBoundaryScopeUnresolved, investigator.ReasonHistoricalRationaleUnresolved:
		return "architect"
	default:
		return "evidence provider for " + string(request.Category)
	}
}

func evidenceRequestQuestionText(request investigator.EvidenceRequest, claim architecture.Claim) string {
	proposition := claimProposition(claim)
	switch request.ReasonCode {
	case investigator.ReasonOwnerAuthorityUnresolved:
		return "Which component is intentionally authorized to own this candidate, and which evidence records that authority: " + proposition + "?"
	case investigator.ReasonBoundaryScopeUnresolved:
		return "What exact boundary scope is intended for this candidate, and which evidence establishes it: " + proposition + "?"
	case investigator.ReasonHistoricalRationaleUnresolved:
		return "Which historical decision or constraint explains this candidate: " + proposition + "?"
	case investigator.ReasonRuntimeConfirmationRequired:
		return "Which runtime observation would establish or refute this candidate: " + proposition + "?"
	case investigator.ReasonCounterexampleExecutionRequired:
		return "Which bounded counterexample execution would establish or refute this candidate: " + proposition + "?"
	default:
		return "Which " + string(request.Category) + " evidence at " + string(request.RequiredProofStrength) + " would establish or refute this candidate: " + proposition + "?"
	}
}

func questionDimension(kind investigator.CandidateKind, contested bool) string {
	if contested {
		return architecture.ClosureContradiction
	}
	switch kind {
	case investigator.KindOwner:
		return architecture.ClosureAuthority
	case investigator.KindContract:
		return architecture.ClosureContract
	case investigator.KindBoundary:
		return architecture.ClosureStructural
	case investigator.KindInvariant, investigator.KindFailureMode:
		return architecture.ClosureBehavioral
	default:
		return architecture.ClosureEvidence
	}
}

func investigationQuestionPriority(kind investigator.CandidateKind, contested bool) string {
	if contested {
		return architecture.QuestionPriorityHigh
	}
	switch kind {
	case investigator.KindOwner, investigator.KindContract:
		return architecture.QuestionPriorityHigh
	case investigator.KindBoundary, investigator.KindInvariant, investigator.KindFailureMode:
		return architecture.QuestionPriorityMedium
	default:
		return architecture.QuestionPriorityLow
	}
}

func evidenceReferences(candidate investigator.CandidateEnvelope) []string {
	refs := supportingEvidenceReferences(candidate)
	refs = append(refs, refutingEvidenceReferences(candidate)...)
	return cleanStrings(refs)
}

func supportingEvidenceReferences(candidate investigator.CandidateEnvelope) []string {
	return classQualifiedEvidenceReferences(candidate.SupportingEvidenceRefIDs)
}

func refutingEvidenceReferences(candidate investigator.CandidateEnvelope) []string {
	return classQualifiedEvidenceReferences(candidate.RefutingEvidenceRefIDs)
}

func classQualifiedEvidenceReferences(ids []string) []string {
	ids = cleanStrings(ids)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, "evidence:"+id)
	}
	return out
}

func appendInvestigationQuestion(dialogue *architecture.DialogueDocument, question architecture.OpenQuestion) (string, error) {
	if existing := findQuestionByID(dialogue.OpenQuestions, question.ID); existing != nil {
		if !questionsEquivalent(*existing, question) {
			return "", fmt.Errorf("investigation question id collision for %s", question.ID)
		}
		return InvestigationDispositionExistingCovers, nil
	}
	dialogue.OpenQuestions = append(dialogue.OpenQuestions, question)
	return InvestigationDispositionGenerated, nil
}

func investigationQuestionItem(sourceKind architecture.QuestionSourceKind, sourceID, candidateID, claimID, disposition, templateID, questionID, reason, detail string) InvestigationQuestionItem {
	return InvestigationQuestionItem{
		SourceKind:  sourceKind,
		SourceID:    sourceID,
		CandidateID: candidateID,
		ClaimID:     claimID,
		Disposition: disposition,
		TemplateID:  templateID,
		QuestionID:  questionID,
		ReasonCode:  reason,
		Detail:      detail,
	}
}

func appendInvestigationDisposition(report *InvestigationQuestionReport, item InvestigationQuestionItem) {
	switch item.Disposition {
	case InvestigationDispositionGenerated:
		report.Generated = append(report.Generated, item)
	case InvestigationDispositionExistingCovers:
		report.ExistingCoverage = append(report.ExistingCoverage, item)
	default:
		report.Skipped = append(report.Skipped, item)
	}
}

func normalizeInvestigationQuestionReport(report InvestigationQuestionReport) InvestigationQuestionReport {
	report.SchemaVersion = InvestigationQuestionSchemaVersion
	report.GeneratedBy = InvestigationQuestionGeneratedBy
	sortInvestigationQuestionItems(report.Generated)
	sortInvestigationQuestionItems(report.ExistingCoverage)
	sortInvestigationQuestionItems(report.Skipped)
	return report
}

func sortInvestigationQuestionItems(items []InvestigationQuestionItem) {
	sort.SliceStable(items, func(i, j int) bool {
		a := string(items[i].SourceKind) + "\x00" + items[i].SourceID + "\x00" + items[i].QuestionID
		b := string(items[j].SourceKind) + "\x00" + items[j].SourceID + "\x00" + items[j].QuestionID
		return a < b
	})
}
