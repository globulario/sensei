// SPDX-License-Identifier: AGPL-3.0-only

package providerport

import (
	"encoding/json"
	"fmt"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// MapToCommand is the O2-to-O1 pure mapping layer: given the O2 Request and
// its completed Result Run produced, it produces the exact synthesis.Command
// golang/architecture/synthesis.Transition must be called with next -- the
// candidate O1 artifact travels embedded inside that command (e.g.
// RecordPlanCommand.Plan), as an independent deep copy, never an alias of
// request/result's own memory. MapToCommand never calls Transition itself,
// never mutates state, and never reads a clock -- completedAt is
// caller-supplied, mirroring RecordEvaluationCommand's own convention, and
// is used only for the evaluation-observation operation.
//
// Run's validation is not durable across this boundary: Request and Result
// carry pointers and slices, so a caller holding them after Run returns
// could still mutate an embedded candidate, and Go's *T dereference alone
// only shallow-copies -- slice-backed fields would keep aliasing the
// original's backing array. MapToCommand therefore does not trust that
// request/result are still what Run validated. It re-validates from
// scratch: Request's own schema and declared-vs-computed digest, Result's
// own schema and declared-vs-computed digest, and the completed payload's
// declared-vs-computed digest (both the embedded artifact's own field and
// Result.PayloadDigestSHA256) -- exactly the checks Run itself already
// made, repeated here because nothing guarantees the values it validated
// are the same bytes MapToCommand now sees. Only once all of that holds
// does it deep-copy the candidate artifact (via a JSON round-trip, so no
// nested slice/map survives as a shared reference) and construct the
// returned command from that independent copy.
//
// It also independently re-verifies that request and result bind to the
// CURRENT session state, not merely that they were internally
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

	if err := validateDocument(ValidateRequestSchema, request); err != nil {
		return nil, fmt.Errorf("providerport: request failed schema validation: %w", err)
	}
	reqDigest, err := RequestDigest(request)
	if err != nil {
		return nil, fmt.Errorf("providerport: compute request digest: %w", err)
	}
	if reqDigest != request.RequestDigestSHA256 {
		return nil, fmt.Errorf("providerport: request declares digest %q but its actual computed digest is %q -- mutated since validation", request.RequestDigestSHA256, reqDigest)
	}
	if err := validateDocument(ValidateResultSchema, result); err != nil {
		return nil, fmt.Errorf("providerport: result failed schema validation: %w", err)
	}
	resDigest, err := ResultDigest(result)
	if err != nil {
		return nil, fmt.Errorf("providerport: compute result digest: %w", err)
	}
	if resDigest != result.ResultDigestSHA256 {
		return nil, fmt.Errorf("providerport: result declares digest %q but its actual computed digest is %q -- mutated since validation", result.ResultDigestSHA256, resDigest)
	}
	payloadDeclared, payloadComputed, err := payloadDigests(result)
	if err != nil {
		return nil, fmt.Errorf("providerport: compute payload digest: %w", err)
	}
	if payloadDeclared != payloadComputed {
		return nil, fmt.Errorf("providerport: embedded payload declares digest %q but its actual computed digest is %q -- mutated since validation", payloadDeclared, payloadComputed)
	}
	if result.PayloadDigestSHA256 == nil || *result.PayloadDigestSHA256 != payloadComputed {
		return nil, fmt.Errorf("providerport: result payload_digest_sha256 does not match the embedded payload's actual computed digest -- mutated since validation")
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
		detached, err := deepCopy(*result.InterpretationPayload)
		if err != nil {
			return nil, fmt.Errorf("providerport: deep-copy candidate interpretation: %w", err)
		}
		return synthesis.RecordInterpretationCommand{Interpretation: detached}, nil

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
		detached, err := deepCopy(*result.PlanningPayload)
		if err != nil {
			return nil, fmt.Errorf("providerport: deep-copy candidate plan: %w", err)
		}
		return synthesis.RecordPlanCommand{Plan: detached}, nil

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
		detached, err := deepCopy(*result.GenerationPayload)
		if err != nil {
			return nil, fmt.Errorf("providerport: deep-copy candidate attempt: %w", err)
		}
		return synthesis.RecordAttemptCommand{Attempt: detached}, nil

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
		detached, err := deepCopy(*result.EvaluationObservationPayload)
		if err != nil {
			return nil, fmt.Errorf("providerport: deep-copy candidate evaluation: %w", err)
		}
		return synthesis.RecordEvaluationCommand{Evaluation: detached, CompletedAt: completedAt}, nil

	default:
		return nil, fmt.Errorf("providerport: unknown operation %q", request.Operation)
	}
}

// deepCopy returns v with every nested slice, map, and pointer fully
// independent of v's own backing memory, via a JSON marshal/unmarshal
// round-trip -- the same canonicalization every O1/O2 document already
// round-trips through for schema validation and digesting, so this adds no
// new serialization behavior to keep in sync as fields are added. A plain
// `x := *ptr` dereference only shallow-copies: slice-backed fields (e.g.
// Plan.Steps, Interpretation.SourceReferences) would still alias the
// original's backing array, so a caller mutating the source Result after
// MapToCommand returns could silently alter the command already handed to
// Transition. deepCopy is what makes the returned command detached.
func deepCopy[T any](v T) (T, error) {
	var out T
	data, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	return out, nil
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
