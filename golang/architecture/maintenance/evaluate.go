// SPDX-License-Identifier: Apache-2.0

package maintenance

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture"
)

func Evaluate(ctx Context) (Result, error) {
	current, err := architecture.NormalizeClaimDocument(ctx.Current)
	if err != nil {
		return Result{}, err
	}
	ctx.Current = current
	if ctx.Previous != nil {
		prev, err := architecture.NormalizeClaimDocument(*ctx.Previous)
		if err != nil {
			return Result{}, err
		}
		ctx.Previous = &prev
		if err := rejectSemanticDivergence(prev, current); err != nil {
			return Result{}, err
		}
	}
	if ctx.Evidence != nil {
		ev := normalizeEvidenceStateDocument(*ctx.Evidence)
		if err := ValidateEvidenceStateDocument(ev, &ctx.ObservedBinding); err != nil {
			return Result{}, err
		}
		ctx.Evidence = &ev
	}
	if ctx.Dialogue != nil {
		d, err := architecture.NormalizeDialogueDocument(*ctx.Dialogue)
		if err != nil {
			return Result{}, err
		}
		ctx.Dialogue = &d
	}
	if ctx.EvaluatedAt != "" {
		if _, err := time.Parse(time.RFC3339, ctx.EvaluatedAt); err != nil {
			return Result{}, errors.New("evaluated_at must be RFC3339")
		}
	}
	order, err := dependencyOrder(current.Claims)
	if err != nil {
		return Result{}, err
	}
	receiptByID := map[string]architecture.ClaimFactReceipt{}
	for _, r := range current.FactReceipts {
		receiptByID[r.Fact.ID] = r
	}
	prevByID := map[string]architecture.Claim{}
	if ctx.Previous != nil {
		for _, c := range ctx.Previous.Claims {
			prevByID[c.ID] = c
		}
	}
	claimByID := map[string]architecture.Claim{}
	for _, c := range current.Claims {
		claimByID[c.ID] = c
	}
	statusByID := map[string]string{}
	evalsByID := map[string]ClaimEvaluation{}
	maintained := make([]architecture.Claim, 0, len(current.Claims))
	for _, id := range order {
		claim := claimByID[id]
		eval, updated := evaluateClaim(ctx, claim, prevByID[claim.ID], receiptByID, statusByID)
		statusByID[claim.ID] = updated.EpistemicStatus
		evalsByID[claim.ID] = eval
		maintained = append(maintained, updated)
	}
	report := Report{
		SchemaVersion:    SchemaVersion,
		GeneratedBy:      GeneratedBy,
		EvaluatedAt:      ctx.EvaluatedAt,
		CurrentBinding:   current.Binding,
		ObservedBinding:  ctx.ObservedBinding,
		ClaimEvaluations: sortedEvaluations(evalsByID),
	}
	if ctx.Previous != nil {
		b := ctx.Previous.Binding
		report.PreviousBinding = &b
		report.RetiredClaims = retiredClaims(*ctx.Previous, current)
	}
	doc := current
	doc.GeneratedBy = GeneratedBy
	doc.Claims = maintained
	doc, err = architecture.NormalizeClaimDocument(doc)
	if err != nil {
		return Result{}, err
	}
	report = normalizeReport(report)
	return Result{Document: doc, Report: report}, nil
}

func evaluateClaim(ctx Context, claim architecture.Claim, previous architecture.Claim, receiptByID map[string]architecture.ClaimFactReceipt, statusByID map[string]string) (ClaimEvaluation, architecture.Claim) {
	reasons := []Reason{}
	lanes := ProofLanes{
		Binding:            mergeLane(VerifyRepositoryRevision(ctx.RepositoryRoot, ctx.Current.Binding), VerifyGraphDigest(ctx.Current.Binding, ctx.ObservedBinding)),
		PremiseFacts:       LaneState{State: LaneAbsent},
		Dependencies:       LaneState{State: LaneAbsent},
		SupportingEvidence: LaneState{State: LaneAbsent},
		RefutingEvidence:   LaneState{State: LaneAbsent},
		Conflict:           LaneState{State: LaneAbsent},
		Supersession:       LaneState{State: LaneAbsent},
	}
	reasons = append(reasons, lanes.Binding.Reasons...)
	premiseCurrent := len(claim.PremiseFacts) > 0
	premiseStale := false
	premiseUnknown := false
	for _, id := range claim.PremiseFacts {
		r, ok := receiptByID[id]
		if !ok {
			premiseUnknown = true
			reasons = append(reasons, Reason{Code: "premise.provenance_unavailable", Detail: id + " missing from fact receipts"})
			continue
		}
		lane := VerifySourceReceipt(ctx.RepositoryRoot, r)
		reasons = append(reasons, lane.Reasons...)
		switch lane.State {
		case LaneStale:
			premiseStale = true
		case LaneUnknown:
			premiseUnknown = true
		}
	}
	lanes.PremiseFacts = stateFromBooleans(len(claim.PremiseFacts) > 0, premiseCurrent && !premiseStale && !premiseUnknown, premiseStale, premiseUnknown, "premise.current")
	lanes.PremiseFacts.Reasons = filterReasons(reasons, "premise.")

	depCurrent := len(claim.DependsOnClaims) > 0
	depStale := false
	depUnknown := false
	for _, dep := range claim.DependsOnClaims {
		switch statusByID[dep] {
		case architecture.StatusSupported:
			reasons = append(reasons, Reason{Code: "dependency.supported", Detail: dep + " is supported"})
		case architecture.StatusStale:
			depStale = true
			reasons = append(reasons, Reason{Code: "dependency.stale", Detail: dep + " is stale"})
		case architecture.StatusUnknown:
			depUnknown = true
			reasons = append(reasons, Reason{Code: "dependency.unknown", Detail: dep + " is unknown"})
		case architecture.StatusContested:
			depUnknown = true
			reasons = append(reasons, Reason{Code: "dependency.contested", Detail: dep + " is contested"})
		case architecture.StatusRefuted:
			depUnknown = true
			reasons = append(reasons, Reason{Code: "dependency.refuted", Detail: dep + " is refuted"})
		case architecture.StatusSuperseded:
			depUnknown = true
			reasons = append(reasons, Reason{Code: "dependency.superseded", Detail: dep + " is superseded"})
		default:
			depUnknown = true
			reasons = append(reasons, Reason{Code: "dependency.unknown", Detail: dep + " has not been evaluated"})
		}
	}
	lanes.Dependencies = stateFromBooleans(len(claim.DependsOnClaims) > 0, depCurrent && !depStale && !depUnknown, depStale, depUnknown, "dependency.supported")
	lanes.Dependencies.Reasons = filterReasons(reasons, "dependency.")

	supportActive, supportStale, supportUnknown, supportReasons := evidenceLane(ctx, claim.SupportingEvidence, "evidence.support")
	refuteActive, refuteStale, refuteUnknown, refuteReasons := evidenceLane(ctx, claim.RefutingEvidence, "evidence.refutation")
	reasons = append(reasons, supportReasons...)
	reasons = append(reasons, refuteReasons...)
	lanes.SupportingEvidence = evidenceLaneState(len(claim.SupportingEvidence) > 0, supportActive, supportStale, supportUnknown, supportReasons)
	lanes.RefutingEvidence = evidenceLaneState(len(claim.RefutingEvidence) > 0, refuteActive, refuteStale, refuteUnknown, refuteReasons)
	if sameEvidenceInBoth(claim.SupportingEvidence, claim.RefutingEvidence) {
		refuteUnknown = true
		reasons = append(reasons, Reason{Code: "evidence.refutation.unknown", Detail: "same evidence is both supporting and refuting"})
	}

	if len(claim.ConflictsWith) > 0 {
		lanes.Conflict = LaneState{State: LaneActive, Reasons: []Reason{{Code: "conflict.explicit", Detail: strings.Join(claim.ConflictsWith, ", ")}}}
		reasons = append(reasons, lanes.Conflict.Reasons...)
	}
	if claim.SupersededBy != "" {
		lanes.Supersession = LaneState{State: LaneActive, Reasons: []Reason{{Code: "supersession.explicit", Detail: claim.SupersededBy}}}
		reasons = append(reasons, lanes.Supersession.Reasons...)
	}

	currentSupport := (len(claim.PremiseFacts) > 0 && !premiseStale && !premiseUnknown) || supportActive || (len(claim.DependsOnClaims) > 0 && !depStale && !depUnknown)
	status := architecture.StatusUnknown
	freshness := "unknown"
	switch {
	case claim.SupersededBy != "":
		status, freshness = architecture.StatusSuperseded, "historical"
	case lanes.Binding.State == LaneStale || premiseStale || depStale:
		status, freshness = architecture.StatusStale, "stale"
	case lanes.Binding.State == LaneUnknown || premiseUnknown || depUnknown || refuteUnknown:
		status, freshness = architecture.StatusUnknown, "unknown"
	case refuteActive && currentSupport:
		status, freshness = architecture.StatusContested, "current"
	case refuteActive && !currentSupport:
		status, freshness = architecture.StatusRefuted, "current"
	case len(claim.ConflictsWith) > 0 && currentSupport:
		status, freshness = architecture.StatusContested, "current"
	case len(claim.ConflictsWith) > 0 && !currentSupport:
		status, freshness = architecture.StatusUnknown, "unknown"
	case currentSupport:
		status, freshness = architecture.StatusSupported, "current"
	case previous.ID != "" && (previous.EpistemicStatus == architecture.StatusSupported || previous.EpistemicStatus == architecture.StatusContested || previous.EpistemicStatus == architecture.StatusRefuted):
		status, freshness = architecture.StatusStale, "stale"
	case supportStale || refuteStale:
		status, freshness = architecture.StatusStale, "stale"
	default:
		status, freshness = architecture.StatusUnknown, "unknown"
	}

	updated := claim
	updated.EpistemicStatus = status
	updated.Freshness = freshness
	if ctx.EvaluatedAt != "" {
		updated.LastValidatedAt = ctx.EvaluatedAt
	}
	updated.Unknowns = dedupeStrings(append(updated.Unknowns, reasonDetailsForUnknowns(status, reasons)...))

	eval := ClaimEvaluation{
		ClaimID:         claim.ID,
		PreviousStatus:  previous.EpistemicStatus,
		InputStatus:     claim.EpistemicStatus,
		EvaluatedStatus: status,
		Disposition:     disposition(previous, claim, updated, ctx),
		ProofLanes:      normalizeProofLanes(lanes),
		Reasons:         dedupeReasons(reasons),
		OpenQuestions:   dialogueSummaries(ctx.Dialogue, claim.ID),
	}
	return eval, updated
}

func evidenceLane(ctx Context, refs []string, prefix string) (active, stale, unknown bool, reasons []Reason) {
	if len(refs) == 0 {
		return false, false, false, nil
	}
	if ctx.Evidence == nil {
		return false, false, true, []Reason{{Code: "evidence.snapshot_missing", Detail: "evidence-state snapshot not supplied"}}
	}
	byID := ctx.Evidence.ByID()
	for _, ref := range refs {
		ev, ok := byID[strings.TrimPrefix(ref, "evidence:")]
		if !ok {
			r := evidenceReason(prefix, ref, nil)
			reasons = append(reasons, r)
			unknown = true
			continue
		}
		r := evidenceReason(prefix, ref, &ev)
		reasons = append(reasons, r)
		if EvidenceIsActive(ev, ctx.Evidence.Binding, ctx.ObservedBinding) {
			active = true
			continue
		}
		if r.Code == prefix+".stale" {
			stale = true
		} else if r.Code == prefix+".unknown" {
			unknown = true
		}
	}
	return active, stale, unknown, reasons
}

func dependencyOrder(claims []architecture.Claim) ([]string, error) {
	byID := map[string]architecture.Claim{}
	for _, c := range claims {
		byID[c.ID] = c
	}
	state := map[string]int{}
	var out []string
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 1 {
			return fmt.Errorf("dependency.cycle: %s", id)
		}
		if state[id] == 2 {
			return nil
		}
		state[id] = 1
		deps := append([]string{}, byID[id].DependsOnClaims...)
		sort.Strings(deps)
		for _, dep := range deps {
			if _, ok := byID[dep]; ok {
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		state[id] = 2
		out = append(out, id)
		return nil
	}
	ids := make([]string, 0, len(claims))
	for _, c := range claims {
		ids = append(ids, c.ID)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func rejectSemanticDivergence(prev, cur architecture.ClaimDocument) error {
	prevByID := map[string]architecture.Claim{}
	for _, c := range prev.Claims {
		prevByID[c.ID] = c
	}
	for _, c := range cur.Claims {
		p, ok := prevByID[c.ID]
		if !ok {
			continue
		}
		if semanticKey(p) != semanticKey(c) {
			return fmt.Errorf("claim.identity_diverged: %s", c.ID)
		}
	}
	return nil
}

func semanticKey(c architecture.Claim) string {
	repo := c.Scope.Repository
	if repo == "" {
		repo = c.Scope.Repo
	}
	return strings.Join([]string{
		repo,
		c.Statement.Subject,
		c.Statement.Predicate,
		c.Statement.Object,
		c.ArchitecturalPlane,
		c.InferenceRule,
		strings.Join(c.PremiseFacts, ","),
		strings.Join(c.DependsOnClaims, ","),
	}, "\x00")
}

func retiredClaims(prev, cur architecture.ClaimDocument) []RetiredClaim {
	current := map[string]bool{}
	for _, c := range cur.Claims {
		current[c.ID] = true
	}
	var out []RetiredClaim
	for _, c := range prev.Claims {
		if current[c.ID] {
			continue
		}
		out = append(out, RetiredClaim{
			ClaimID:         c.ID,
			PreviousStatus:  c.EpistemicStatus,
			EvaluatedStatus: architecture.StatusStale,
			Disposition:     DispositionRetired,
			Reasons:         []Reason{{Code: "claim.retired", Detail: "claim is absent from the current document"}},
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ClaimID < out[j].ClaimID })
	return out
}

func disposition(previous, input, evaluated architecture.Claim, ctx Context) string {
	if previous.ID == "" {
		return DispositionIntroduced
	}
	if previous.EpistemicStatus != evaluated.EpistemicStatus {
		return DispositionChanged
	}
	if evaluated.EpistemicStatus == architecture.StatusSupported && proofBindingChanged(previous, input, ctx) {
		return DispositionRevalidated
	}
	return DispositionRetained
}

func proofBindingChanged(previous, input architecture.Claim, ctx Context) bool {
	return previous.Freshness != input.Freshness || ctx.Current.Binding.Revision != "" || ctx.Current.Binding.GraphDigestSHA256 != ""
}

func dialogueSummaries(doc *architecture.DialogueDocument, claimID string) []QuestionSummary {
	if doc == nil {
		return nil
	}
	answerByID := map[string]architecture.ArchitectAnswer{}
	for _, a := range doc.Answers {
		answerByID[a.ID] = a
	}
	var out []QuestionSummary
	for _, q := range doc.OpenQuestions {
		if !contains(q.BlocksClaims, claimID) {
			continue
		}
		s := QuestionSummary{QuestionID: q.ID, Status: q.Status}
		for _, aid := range q.ResolvedByAnswers {
			a, ok := answerByID[aid]
			if !ok {
				continue
			}
			s.AcceptedAnswerIDs = append(s.AcceptedAnswerIDs, aid)
			s.AnswerClasses = append(s.AnswerClasses, a.Classifications...)
			s.AnswerGovernance = append(s.AnswerGovernance, a.GovernanceStatus)
			if q.Status == architecture.QuestionStatusAcceptedUnknown {
				s.NonProbativeReasons = append(s.NonProbativeReasons, Reason{Code: "dialogue.accepted_unknown", Detail: q.ID + " accepted unknown"})
			}
			if a.GovernanceStatus == architecture.AnswerGovernanceAcceptedForQuestion {
				s.NonProbativeReasons = append(s.NonProbativeReasons, Reason{Code: "dialogue.resolved_non_probative", Detail: aid + " resolves only the question artifact"})
				s.NonProbativeReasons = append(s.NonProbativeReasons, Reason{Code: "dialogue.answer_not_evidence", Detail: aid + " is not evidence"})
			}
		}
		switch q.Status {
		case architecture.QuestionStatusOpen:
			s.NonProbativeReasons = append(s.NonProbativeReasons, Reason{Code: "dialogue.question_open", Detail: q.ID + " is open"})
		case architecture.QuestionStatusAwaitingArchitect:
			s.NonProbativeReasons = append(s.NonProbativeReasons, Reason{Code: "dialogue.awaiting_architect", Detail: q.ID + " awaits architect"})
		case architecture.QuestionStatusAwaitingEvidence:
			s.NonProbativeReasons = append(s.NonProbativeReasons, Reason{Code: "dialogue.awaiting_evidence", Detail: q.ID + " awaits evidence"})
		case architecture.QuestionStatusAnswered:
			s.NonProbativeReasons = append(s.NonProbativeReasons, Reason{Code: "dialogue.answered", Detail: q.ID + " is answered but non-probative"})
		}
		s.AcceptedAnswerIDs = dedupeStrings(s.AcceptedAnswerIDs)
		s.AnswerClasses = dedupeStrings(s.AnswerClasses)
		s.AnswerGovernance = dedupeStrings(s.AnswerGovernance)
		s.NonProbativeReasons = dedupeReasons(s.NonProbativeReasons)
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].QuestionID < out[j].QuestionID })
	return out
}
