// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

func buildInterpretationRequest(state synthesis.SessionState, policy ProviderPolicy) (providerport.Request, error) {
	if state.Phase != synthesis.PhaseCreated {
		return providerport.Request{}, fmt.Errorf("synthesisdriver: interpretation request requires phase %q, got %q", synthesis.PhaseCreated, state.Phase)
	}
	if err := validateProviderPolicy(policy); err != nil {
		return providerport.Request{}, err
	}
	session, err := detached(state.Session)
	if err != nil {
		return providerport.Request{}, err
	}
	request := providerport.NormalizeRequest(providerport.Request{
		SchemaVersion:                  providerport.RequestSchemaVersion,
		RequestID:                      "o7.interpretation." + state.Session.SessionDigestSHA256[:16],
		Operation:                      providerport.OperationInterpretation,
		SessionDigestSHA256:            state.Session.SessionDigestSHA256,
		RepositoryDomain:               state.Session.RepositoryDomain,
		BaseRevision:                   state.Session.BaseRevision,
		ParentArtifactDigestSHA256:     state.Session.SessionDigestSHA256,
		ExpectedPlanGeneration:         nil,
		ExpectedAttemptNumber:          nil,
		DeadlineAt:                     policy.DeadlineAt,
		MaxObservationCount:            policy.MaxObservationCount,
		MaxObservationBytes:            policy.MaxObservationBytes,
		InterpretationPayload:          &session,
		PlanningPayload:                nil,
		GenerationPayload:              nil,
		EvaluationObservationPayload:   nil,
	})
	return finalizeRequest(request)
}

func buildPlanningRequest(state synthesis.SessionState, interpretation synthesis.Interpretation, policy ProviderPolicy) (providerport.Request, error) {
	if state.Phase != synthesis.PhasePlanning {
		return providerport.Request{}, fmt.Errorf("synthesisdriver: planning request requires phase %q, got %q", synthesis.PhasePlanning, state.Phase)
	}
	if err := validateProviderPolicy(policy); err != nil {
		return providerport.Request{}, err
	}
	computed, err := synthesis.InterpretationDigest(interpretation)
	if err != nil {
		return providerport.Request{}, err
	}
	if interpretation.InterpretationDigestSHA256 != computed || computed != state.InterpretationDigestSHA256 {
		return providerport.Request{}, fmt.Errorf("synthesisdriver: planning interpretation does not match current O1 state")
	}
	copyValue, err := detached(interpretation)
	if err != nil {
		return providerport.Request{}, err
	}
	expectedGeneration := state.ExpectedPlanGeneration
	request := providerport.NormalizeRequest(providerport.Request{
		SchemaVersion:                  providerport.RequestSchemaVersion,
		RequestID:                      fmt.Sprintf("o7.planning.%s.%d", state.Session.SessionDigestSHA256[:12], expectedGeneration),
		Operation:                      providerport.OperationPlanning,
		SessionDigestSHA256:            state.Session.SessionDigestSHA256,
		RepositoryDomain:               state.Session.RepositoryDomain,
		BaseRevision:                   state.Session.BaseRevision,
		ParentArtifactDigestSHA256:     interpretation.InterpretationDigestSHA256,
		ExpectedPlanGeneration:         &expectedGeneration,
		ExpectedAttemptNumber:          nil,
		DeadlineAt:                     policy.DeadlineAt,
		MaxObservationCount:            policy.MaxObservationCount,
		MaxObservationBytes:            policy.MaxObservationBytes,
		InterpretationPayload:          nil,
		PlanningPayload:                &copyValue,
		GenerationPayload:              nil,
		EvaluationObservationPayload:   nil,
	})
	return finalizeRequest(request)
}

func finalizeRequest(request providerport.Request) (providerport.Request, error) {
	digest, err := providerport.RequestDigest(request)
	if err != nil {
		return providerport.Request{}, fmt.Errorf("synthesisdriver: compute O2 request digest: %w", err)
	}
	request.RequestDigestSHA256 = digest
	data, err := json.Marshal(request)
	if err != nil {
		return providerport.Request{}, err
	}
	if err := providerport.ValidateRequestSchema(data); err != nil {
		return providerport.Request{}, fmt.Errorf("synthesisdriver: constructed O2 request failed schema validation: %w", err)
	}
	return request, nil
}

func validateProviderPolicy(policy ProviderPolicy) error {
	if _, err := time.Parse(time.RFC3339, policy.DeadlineAt); err != nil {
		return fmt.Errorf("synthesisdriver: provider deadline_at must be RFC3339: %w", err)
	}
	if policy.MaxObservationCount < 0 || policy.MaxObservationBytes < 0 {
		return fmt.Errorf("synthesisdriver: provider observation limits must be non-negative")
	}
	return nil
}

func detached[T any](value T) (T, error) {
	var out T
	data, err := json.Marshal(value)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	return out, nil
}
