// SPDX-License-Identifier: AGPL-3.0-only

package main

// Phase 9.5 Checkpoint 5 — guarded architect-answer mutation handlers.
//
// Handler law (mirrors the read handlers, plus mutation discipline):
//   validate request SHAPE (InvalidArgument on malformed) → delegate to the
//   write provider (which resolves + verifies authority server-side) → map the
//   owner result losslessly. Handlers assign NO semantics. A DOMAIN refusal
//   (unconfigured, mismatch, unauthorized, ineligible, stale, contested) is a
//   SUCCESSFUL RPC carrying a typed refusal with mutation_applied=false and the
//   UNCHANGED ledger identity — it is NEVER a transport error and NEVER looks
//   like a mutation that succeeded. Transport errors are reserved for malformed
//   requests (InvalidArgument) only.

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// PrepareArchitectAnswerDisposition builds the pure candidate (writes nothing).
func (s *server) PrepareArchitectAnswerDisposition(_ context.Context, req *awarenesspb.PrepareArchitectAnswerDispositionRequest) (*awarenesspb.PrepareArchitectAnswerDispositionResponse, error) {
	in, err := dispositionInputFromProto(req.GetInput())
	if err != nil {
		return nil, err
	}
	cand, _, b, ref := s.prepareDisposition(in)
	if ref != nil {
		return &awarenesspb.PrepareArchitectAnswerDispositionResponse{Refusal: refusalToProto(ref, b)}, nil
	}
	return &awarenesspb.PrepareArchitectAnswerDispositionResponse{
		Candidate: &awarenesspb.ArchitectureDispositionCandidate{
			QuestionId:                     b.QuestionID,
			ReceiptDigestSha256:            cand.Receipt.ReceiptDigestSHA256,
			ReceiptByteDigestSha256:        cand.ReceiptByteDigestSHA256,
			ExpectedLedgerHeadDigestSha256: cand.ExpectedLedgerHeadDigestSHA256,
			AnchorEntryDigestSha256:        cand.AnchorEntryDigestSHA256,
		},
	}, nil
}

// RecordArchitectAnswerDisposition commits exactly one disposition.
func (s *server) RecordArchitectAnswerDisposition(_ context.Context, req *awarenesspb.RecordArchitectAnswerDispositionRequest) (*awarenesspb.RecordArchitectAnswerDispositionResponse, error) {
	in, err := dispositionInputFromProto(req.GetInput())
	if err != nil {
		return nil, err
	}
	res, prevHead, b, ref := s.recordDisposition(in, req.GetExpectedLedgerHeadDigestSha256())
	if ref != nil {
		return &awarenesspb.RecordArchitectAnswerDispositionResponse{Refusal: refusalToProto(ref, b)}, nil
	}
	return &awarenesspb.RecordArchitectAnswerDispositionResponse{Receipt: dispositionReceiptToProto(res, prevHead, b)}, nil
}

// ── proto → owner input ──────────────────────────────────────────────────────

func dispositionInputFromProto(pin *awarenesspb.ArchitectureDispositionInput) (dispositionInput, error) {
	if pin == nil {
		return dispositionInput{}, status.Error(codes.InvalidArgument, "input is required")
	}
	if err := validateControlRepositoryIdentity(pin.GetRepositoryIdentity()); err != nil {
		return dispositionInput{}, err
	}
	if pin.GetQuestionId() == "" {
		return dispositionInput{}, status.Error(codes.InvalidArgument, "question_id is required")
	}
	disp, err := dispositionFromProto(pin.GetDisposition())
	if err != nil {
		return dispositionInput{}, err
	}
	reuse, err := reusabilityFromProto(pin.GetReusability())
	if err != nil {
		return dispositionInput{}, err
	}
	return dispositionInput{
		repositoryIdentity: pin.GetRepositoryIdentity(), domain: pin.GetDomain(),
		taskID: pin.GetTaskId(), sessionID: pin.GetSessionId(), questionID: pin.GetQuestionId(),
		actor: pin.GetActorIdentity(), disposition: disp, reusability: reuse,
		rationale: pin.GetRationale(), answerID: pin.GetAnswerId(), answerBytes: pin.GetAnswerBytes(),
		scopeDomain: pin.GetEffectiveScopeDomain(), scopeFiles: pin.GetEffectiveScopeFiles(), evidence: pin.GetEvidenceRefs(),
	}, nil
}

func dispositionFromProto(d awarenesspb.ArchitectureDisposition) (qd.Disposition, error) {
	switch d {
	case awarenesspb.ArchitectureDisposition_ARCHITECTURE_DISPOSITION_ANSWERED:
		return qd.DispositionAnswered, nil
	case awarenesspb.ArchitectureDisposition_ARCHITECTURE_DISPOSITION_DISMISSED:
		return qd.DispositionDismissed, nil
	case awarenesspb.ArchitectureDisposition_ARCHITECTURE_DISPOSITION_DEFERRED:
		return qd.DispositionDeferred, nil
	case awarenesspb.ArchitectureDisposition_ARCHITECTURE_DISPOSITION_TASK_LOCAL:
		return qd.DispositionTaskLocal, nil
	default:
		return "", status.Error(codes.InvalidArgument, "disposition is required and must be a valid value")
	}
}

func reusabilityFromProto(r awarenesspb.ArchitectureReusability) (qd.Reusability, error) {
	switch r {
	case awarenesspb.ArchitectureReusability_ARCHITECTURE_REUSABILITY_NONE:
		return qd.ReusabilityNone, nil
	case awarenesspb.ArchitectureReusability_ARCHITECTURE_REUSABILITY_REUSABLE_CANDIDATE:
		return qd.ReusabilityReusableCandidate, nil
	case awarenesspb.ArchitectureReusability_ARCHITECTURE_REUSABILITY_TASK_LOCAL:
		return qd.ReusabilityTaskLocal, nil
	default:
		return "", status.Error(codes.InvalidArgument, "reusability is required and must be a valid value")
	}
}

// ── owner result → proto ─────────────────────────────────────────────────────

func refusalToProto(ref *mutationRefusal, b mutationBindings) *awarenesspb.ArchitectureMutationRefusal {
	head := ref.LedgerHead
	if head == "" {
		head = b.LedgerHead
	}
	return &awarenesspb.ArchitectureMutationRefusal{
		ReasonCode: ref.Code, Detail: ref.Detail, Owner: ref.Owner, MutationApplied: false,
		Audit: &awarenesspb.ArchitectureMutationAudit{
			OperationKind: b.OperationKind, ActorIdentity: b.Actor, Domain: b.Domain,
			TaskId: b.TaskID, SessionId: b.SessionID, QuestionId: b.QuestionID,
			// A refusal leaves the ledger identity UNCHANGED: previous == resulting.
			PreviousLedgerHeadSha256: head, ResultingLedgerHeadSha256: head,
			OwnerOutcome: ref.Code, ReplayStatus: "none", MutationApplied: false,
		},
	}
}

func dispositionReceiptToProto(res *qd.RecordResult, prevHead string, b mutationBindings) *awarenesspb.ArchitectureDispositionReceipt {
	outcome, replay, applied := dispositionOutcomeToProto(res.Outcome)
	return &awarenesspb.ArchitectureDispositionReceipt{
		Outcome:                  outcome,
		QuestionId:               res.QuestionID,
		ReceiptDigestSha256:      res.ReceiptDigestSHA256,
		EntryDigestSha256:        res.EntryDigestSHA256,
		PreviousLedgerHeadSha256: res.PreviousLedgerHeadSHA256,
		CurrentLedgerHeadSha256:  res.CurrentLedgerHeadSHA256,
		LedgerSequence:           int64(res.LedgerSequence),
		ContestedPriorDigests:    res.ContestedPriorDigests,
		ProjectionState:          res.ProjectionState,
		Audit: &awarenesspb.ArchitectureMutationAudit{
			OperationIdentity: res.ReceiptDigestSHA256, OperationKind: b.OperationKind,
			ActorIdentity: b.Actor, Domain: b.Domain, TaskId: b.TaskID, SessionId: b.SessionID,
			QuestionId: res.QuestionID, PreviousLedgerHeadSha256: res.PreviousLedgerHeadSHA256,
			ResultingLedgerHeadSha256: res.CurrentLedgerHeadSHA256, OwnerOutcome: string(res.Outcome),
			ReplayStatus: replay, MutationApplied: applied,
		},
	}
}

func dispositionOutcomeToProto(o qd.RecordOutcome) (awarenesspb.ArchitectureDispositionOutcome, string, bool) {
	switch o {
	case qd.OutcomeRecorded:
		return awarenesspb.ArchitectureDispositionOutcome_ARCHITECTURE_DISPOSITION_OUTCOME_RECORDED, "applied", true
	case qd.OutcomeReplayed:
		return awarenesspb.ArchitectureDispositionOutcome_ARCHITECTURE_DISPOSITION_OUTCOME_REPLAYED, "replay", false
	case qd.OutcomeReconciled:
		return awarenesspb.ArchitectureDispositionOutcome_ARCHITECTURE_DISPOSITION_OUTCOME_RECONCILED, "reconciled", false
	case qd.OutcomeContested:
		return awarenesspb.ArchitectureDispositionOutcome_ARCHITECTURE_DISPOSITION_OUTCOME_CONTESTED, "contested", true
	default:
		return awarenesspb.ArchitectureDispositionOutcome_ARCHITECTURE_DISPOSITION_OUTCOME_UNSPECIFIED, "none", false
	}
}
