// SPDX-License-Identifier: AGPL-3.0-only

package providerport

import (
	"fmt"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// MapToCommand is the O2-to-O1 pure mapping layer: given a validated O2
// Request and its completed Result (both already schema/digest-verified by
// Run), it produces the exact synthesis.Command
// golang/architecture/synthesis.Transition must be called with next -- the
// candidate O1 artifact travels embedded inside that command (e.g.
// RecordPlanCommand.Plan). MapToCommand never calls Transition itself,
// never mutates state, and never reads a clock -- completedAt is
// caller-supplied, mirroring RecordEvaluationCommand's own convention, and
// is used only for the evaluation-observation operation.
//
// Before mapping, it independently re-verifies that request and result
// bind to the CURRENT session state, not merely that they were internally
// self-consistent when Run produced them:
//
//   - phase: the operation must be legal for state.Phase right now
//     (interpretation only from Created, planning only from Planning,
//     generation only from Attempting, evaluation-observation only from
//     Evaluating) -- otherwise Transition would reject the mapped command
//     anyway, just later and less specifically.
//   - the parent chain: the request's embedded parent artifact's own
//     declared digest, that artifact's independently recomputed digest,
//     request.ParentArtifactDigestSHA256, and the CURRENT session's
//     corresponding parent digest must all agree. A request could
//     otherwise declare the correct current parent digest while embedding
//     a different Session/Interpretation/Plan/Attempt entirely for the
//     provider to have actually consumed.
//   - the candidate artifact's own cross-references: Result's embedded
//     candidate (the thing about to become a new O1 artifact) must itself
//     reference the CURRENT parent digest and, where applicable, the
//     CURRENT plan generation/attempt number -- not merely whatever the
//     request claimed. A stale or self-contradictory candidate is rejected
//     here rather than being silently accepted into RecordPlanCommand/
//     RecordAttemptCommand and only caught later by Transition.
//   - generation/attempt: request.ExpectedPlanGeneration/
//     ExpectedAttemptNumber must match what state currently expects.
//
// A request/result pair that was valid when produced but has since gone
// stale (state advanced, replanned, or resumed under it) is rejected here,
// before it ever reaches Transition.
func MapToCommand(state synthesis.SessionState, request Request, result Result, completedAt string) (synthesis.Command, error) {
	if result.TerminalOutcome != OutcomeCompleted {
		return nil, fmt.Errorf("providerport: cannot map a non-completed result (terminal_outcome=%s) into an O1 command", result.TerminalOutcome)
	}
	if result.RequestDigestSHA256 != request.RequestDigestSHA256 || result.Operation != request.Operation {
		return nil, fmt.Errorf("providerport: result does not reference the given request")
	}
	if request.SessionDigestSHA256 != state.Session.SessionDigestSHA256 {
		return nil, fmt.Errorf("providerport: request references session digest %q, current session is %q -- stale identity", request.SessionDigestSHA256, state.Session.SessionDigestSHA256)
	}
	if request.RepositoryDomain != state.Session.RepositoryDomain || request.BaseRevision != state.Session.BaseRevision {
		return nil, fmt.Errorf("providerport: request references %s@%s, current session is %s@%s -- stale identity", request.RepositoryDomain, request.BaseRevision, state.Session.RepositoryDomain, state.Session.BaseRevision)
	}

	switch request.Operation {
	case OperationInterpretation:
		if state.Phase != synthesis.PhaseCreated {
			return nil, fmt.Errorf("providerport: interpretation is only legal from phase %s, session is in phase %s -- wrong phase", synthesis.PhaseCreated, state.Phase)
		}
		if request.InterpretationPayload == nil {
			return nil, fmt.Errorf("providerport: interpretation request carries no interpretation_payload")
		}
		computed, digestErr := synthesis.SessionDigest(*request.InterpretationPayload)
		if err := verifyParentChain("session", request.InterpretationPayload.SessionDigestSHA256, computed, digestErr, request.ParentArtifactDigestSHA256, state.Session.SessionDigestSHA256); err != nil {
			return nil, err
		}
		if result.InterpretationPayload == nil {
			return nil, fmt.Errorf("providerport: completed interpretation result carries no interpretation_payload")
		}
		if result.InterpretationPayload.SessionDigestSHA256 != state.Session.SessionDigestSHA256 {
			return nil, fmt.Errorf("providerport: candidate interpretation references session digest %q, current session is %q -- stale candidate", result.InterpretationPayload.SessionDigestSHA256, state.Session.SessionDigestSHA256)
		}
		return synthesis.RecordInterpretationCommand{Interpretation: *result.InterpretationPayload}, nil

	case OperationPlanning:
		if state.Phase != synthesis.PhasePlanning {
			return nil, fmt.Errorf("providerport: planning is only legal from phase %s, session is in phase %s -- wrong phase", synthesis.PhasePlanning, state.Phase)
		}
		if request.PlanningPayload == nil {
			return nil, fmt.Errorf("providerport: planning request carries no planning_payload")
		}
		computed, digestErr := synthesis.InterpretationDigest(*request.PlanningPayload)
		if err := verifyParentChain("interpretation", request.PlanningPayload.InterpretationDigestSHA256, computed, digestErr, request.ParentArtifactDigestSHA256, state.InterpretationDigestSHA256); err != nil {
			return nil, err
		}
		if request.ExpectedPlanGeneration == nil || *request.ExpectedPlanGeneration != state.ExpectedPlanGeneration {
			return nil, fmt.Errorf("providerport: planning request expects plan generation %s, session currently expects %d -- wrong generation", formatIntPtr(request.ExpectedPlanGeneration), state.ExpectedPlanGeneration)
		}
		if result.PlanningPayload == nil {
			return nil, fmt.Errorf("providerport: completed planning result carries no planning_payload")
		}
		if result.PlanningPayload.InterpretationDigestSHA256 != state.InterpretationDigestSHA256 {
			return nil, fmt.Errorf("providerport: candidate plan references interpretation digest %q, current session interpretation is %q -- stale candidate", result.PlanningPayload.InterpretationDigestSHA256, state.InterpretationDigestSHA256)
		}
		if result.PlanningPayload.PlanGeneration != state.ExpectedPlanGeneration {
			return nil, fmt.Errorf("providerport: candidate plan generation %d does not match the session's expected generation %d -- stale candidate", result.PlanningPayload.PlanGeneration, state.ExpectedPlanGeneration)
		}
		return synthesis.RecordPlanCommand{Plan: *result.PlanningPayload}, nil

	case OperationGeneration:
		if state.Phase != synthesis.PhaseAttempting {
			return nil, fmt.Errorf("providerport: generation is only legal from phase %s, session is in phase %s -- wrong phase", synthesis.PhaseAttempting, state.Phase)
		}
		if request.GenerationPayload == nil {
			return nil, fmt.Errorf("providerport: generation request carries no generation_payload")
		}
		computed, digestErr := synthesis.PlanDigest(*request.GenerationPayload)
		if err := verifyParentChain("plan", request.GenerationPayload.PlanDigestSHA256, computed, digestErr, request.ParentArtifactDigestSHA256, state.LatestPlanDigestSHA256); err != nil {
			return nil, err
		}
		if request.ExpectedPlanGeneration == nil || *request.ExpectedPlanGeneration != state.PlanGeneration {
			return nil, fmt.Errorf("providerport: generation request references plan generation %s, session's current recorded generation is %d -- wrong generation", formatIntPtr(request.ExpectedPlanGeneration), state.PlanGeneration)
		}
		if request.ExpectedAttemptNumber == nil || *request.ExpectedAttemptNumber != state.ExpectedAttemptNumber {
			return nil, fmt.Errorf("providerport: generation request expects attempt number %s, session currently expects %d -- wrong attempt", formatIntPtr(request.ExpectedAttemptNumber), state.ExpectedAttemptNumber)
		}
		if result.GenerationPayload == nil {
			return nil, fmt.Errorf("providerport: completed generation result carries no generation_payload")
		}
		if result.GenerationPayload.PlanDigestSHA256 != state.LatestPlanDigestSHA256 {
			return nil, fmt.Errorf("providerport: candidate attempt references plan digest %q, current plan is %q -- stale candidate", result.GenerationPayload.PlanDigestSHA256, state.LatestPlanDigestSHA256)
		}
		if result.GenerationPayload.PlanGeneration != state.PlanGeneration {
			return nil, fmt.Errorf("providerport: candidate attempt plan generation %d does not match the session's current recorded generation %d -- stale candidate", result.GenerationPayload.PlanGeneration, state.PlanGeneration)
		}
		if result.GenerationPayload.AttemptNumber != state.ExpectedAttemptNumber {
			return nil, fmt.Errorf("providerport: candidate attempt number %d does not match the session's expected attempt number %d -- stale candidate", result.GenerationPayload.AttemptNumber, state.ExpectedAttemptNumber)
		}
		return synthesis.RecordAttemptCommand{Attempt: *result.GenerationPayload}, nil

	case OperationEvaluationObservation:
		if state.Phase != synthesis.PhaseEvaluating {
			return nil, fmt.Errorf("providerport: evaluation-observation is only legal from phase %s, session is in phase %s -- wrong phase", synthesis.PhaseEvaluating, state.Phase)
		}
		if request.EvaluationObservationPayload == nil {
			return nil, fmt.Errorf("providerport: evaluation-observation request carries no evaluation_observation_payload")
		}
		computed, digestErr := synthesis.AttemptDigest(*request.EvaluationObservationPayload)
		if err := verifyParentChain("attempt", request.EvaluationObservationPayload.AttemptDigestSHA256, computed, digestErr, request.ParentArtifactDigestSHA256, state.LatestAttemptDigestSHA256); err != nil {
			return nil, err
		}
		if request.ExpectedPlanGeneration == nil || *request.ExpectedPlanGeneration != state.PlanGeneration {
			return nil, fmt.Errorf("providerport: evaluation-observation request references plan generation %s, session's current recorded generation is %d -- wrong generation", formatIntPtr(request.ExpectedPlanGeneration), state.PlanGeneration)
		}
		if request.ExpectedAttemptNumber == nil || *request.ExpectedAttemptNumber != state.AttemptNumber {
			return nil, fmt.Errorf("providerport: evaluation-observation request references attempt number %s, session's current recorded attempt is %d -- wrong attempt", formatIntPtr(request.ExpectedAttemptNumber), state.AttemptNumber)
		}
		if result.EvaluationObservationPayload == nil {
			return nil, fmt.Errorf("providerport: completed evaluation-observation result carries no evaluation_observation_payload")
		}
		if result.EvaluationObservationPayload.AttemptDigestSHA256 != state.LatestAttemptDigestSHA256 {
			return nil, fmt.Errorf("providerport: candidate evaluation references attempt digest %q, current attempt is %q -- stale candidate", result.EvaluationObservationPayload.AttemptDigestSHA256, state.LatestAttemptDigestSHA256)
		}
		return synthesis.RecordEvaluationCommand{Evaluation: *result.EvaluationObservationPayload, CompletedAt: completedAt}, nil

	default:
		return nil, fmt.Errorf("providerport: unknown operation %q", request.Operation)
	}
}

// verifyParentChain checks the four-way equality the O2-to-O1 parent
// binding requires: an embedded parent artifact's own declared digest,
// its independently recomputed digest, the request's separate
// ParentArtifactDigestSHA256 field, and the current session state's
// corresponding parent digest must all agree. A request could otherwise
// declare the correct current parent digest while embedding a different
// artifact entirely for the provider to have actually consumed (declared
// != computed), or declare a self-consistent but stale/wrong parent
// (computed == requestParentDigest, but requestParentDigest !=
// currentParentDigest).
func verifyParentChain(kind, declared, computed string, digestErr error, requestParentDigest, currentParentDigest string) error {
	if digestErr != nil {
		return fmt.Errorf("providerport: compute embedded %s digest: %w", kind, digestErr)
	}
	if declared != computed {
		return fmt.Errorf("providerport: embedded %s declares digest %q but its actual computed digest is %q -- contradictory embedded parent", kind, declared, computed)
	}
	if requestParentDigest != computed {
		return fmt.Errorf("providerport: request parent digest %q does not match its own embedded %s's real digest %q -- contradictory embedded parent", requestParentDigest, kind, computed)
	}
	if requestParentDigest != currentParentDigest {
		return fmt.Errorf("providerport: %s request references parent digest %q, expected %q -- wrong parent", kind, requestParentDigest, currentParentDigest)
	}
	return nil
}

func formatIntPtr(p *int) string {
	if p == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%d", *p)
}
