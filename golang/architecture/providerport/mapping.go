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
// self-consistent when Run produced them: session identity (digest,
// repository domain, base revision), the exact parent artifact this
// operation extends, and the plan generation/attempt number state
// currently expects. A request/result pair that was valid when produced
// but has since gone stale (state advanced, replanned, or resumed under
// it) is rejected here, before it ever reaches Transition -- Transition
// would likely also reject a stale reference on its own terms, but this
// boundary is intentionally the first, cheaper, and more specific place
// that check happens.
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
		if request.ParentArtifactDigestSHA256 != state.Session.SessionDigestSHA256 {
			return nil, fmt.Errorf("providerport: interpretation request references parent digest %q, expected the session digest %q -- wrong parent", request.ParentArtifactDigestSHA256, state.Session.SessionDigestSHA256)
		}
		if result.InterpretationPayload == nil {
			return nil, fmt.Errorf("providerport: completed interpretation result carries no interpretation_payload")
		}
		return synthesis.RecordInterpretationCommand{Interpretation: *result.InterpretationPayload}, nil

	case OperationPlanning:
		if request.ParentArtifactDigestSHA256 != state.InterpretationDigestSHA256 {
			return nil, fmt.Errorf("providerport: planning request references parent digest %q, expected the session's interpretation digest %q -- wrong parent", request.ParentArtifactDigestSHA256, state.InterpretationDigestSHA256)
		}
		if request.ExpectedPlanGeneration == nil || *request.ExpectedPlanGeneration != state.ExpectedPlanGeneration {
			return nil, fmt.Errorf("providerport: planning request expects plan generation %s, session currently expects %d -- wrong generation", formatIntPtr(request.ExpectedPlanGeneration), state.ExpectedPlanGeneration)
		}
		if result.PlanningPayload == nil {
			return nil, fmt.Errorf("providerport: completed planning result carries no planning_payload")
		}
		return synthesis.RecordPlanCommand{Plan: *result.PlanningPayload}, nil

	case OperationGeneration:
		if request.ParentArtifactDigestSHA256 != state.LatestPlanDigestSHA256 {
			return nil, fmt.Errorf("providerport: generation request references parent digest %q, expected the current plan digest %q -- wrong parent", request.ParentArtifactDigestSHA256, state.LatestPlanDigestSHA256)
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
		return synthesis.RecordAttemptCommand{Attempt: *result.GenerationPayload}, nil

	case OperationEvaluationObservation:
		if request.ParentArtifactDigestSHA256 != state.LatestAttemptDigestSHA256 {
			return nil, fmt.Errorf("providerport: evaluation-observation request references parent digest %q, expected the current attempt digest %q -- wrong parent", request.ParentArtifactDigestSHA256, state.LatestAttemptDigestSHA256)
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
		return synthesis.RecordEvaluationCommand{Evaluation: *result.EvaluationObservationPayload, CompletedAt: completedAt}, nil

	default:
		return nil, fmt.Errorf("providerport: unknown operation %q", request.Operation)
	}
}

func formatIntPtr(p *int) string {
	if p == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%d", *p)
}
