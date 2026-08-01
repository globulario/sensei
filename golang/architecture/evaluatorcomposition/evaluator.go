// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// Evaluator is the single provider-neutral checkpoint-4 evaluator port.
// Evaluator kind remains data in EvaluatorDescriptor, never a parallel
// interface hierarchy. Implementations observe one exact EvaluationInput and
// return one independently attributable EvaluatorResult. They never return an
// O1 Recommendation or call synthesis.Transition.
type Evaluator interface {
	Describe(ctx context.Context) (EvaluatorDescriptor, error)
	Evaluate(ctx context.Context, input EvaluationInput) (EvaluatorResult, error)
}

// EvaluatorFunc is a deterministic test double and lightweight adapter. It is
// intentionally explicit rather than ambient: both functions are constructor
// data, so a test can prove exactly which descriptor/result was returned.
type EvaluatorFunc struct {
	DescribeFunc func(context.Context) (EvaluatorDescriptor, error)
	EvaluateFunc func(context.Context, EvaluationInput) (EvaluatorResult, error)
}

func (e EvaluatorFunc) Describe(ctx context.Context) (EvaluatorDescriptor, error) {
	if e.DescribeFunc == nil {
		return EvaluatorDescriptor{}, fmt.Errorf("EvaluatorFunc.Describe: nil DescribeFunc")
	}
	return e.DescribeFunc(ctx)
}

func (e EvaluatorFunc) Evaluate(ctx context.Context, input EvaluationInput) (EvaluatorResult, error) {
	if e.EvaluateFunc == nil {
		return EvaluatorResult{}, fmt.Errorf("EvaluatorFunc.Evaluate: nil EvaluateFunc")
	}
	return e.EvaluateFunc(ctx, input)
}

// BuildEvaluationInput constructs and validates the exact input document for
// one already-materialized evaluator surface. It is valid only after
// checkpoint 3 recorded the attempt and entered PhaseEvaluating.
func BuildEvaluationInput(state synthesis.SessionState, artifact runnercomposition.CandidateArtifact, policy EvaluationPolicy, surface EvaluatorSurface) (EvaluationInput, error) {
	if state.Phase != synthesis.PhaseEvaluating {
		return EvaluationInput{}, fmt.Errorf("BuildEvaluationInput: session phase %q is not %q", state.Phase, synthesis.PhaseEvaluating)
	}
	if surface == nil {
		return EvaluationInput{}, fmt.Errorf("BuildEvaluationInput: nil evaluator surface")
	}
	if err := runnercomposition.ValidateCandidateArtifact(artifact); err != nil {
		return EvaluationInput{}, fmt.Errorf("BuildEvaluationInput: invalid candidate artifact: %w", err)
	}
	if err := ValidateEvaluationPolicy(policy); err != nil {
		return EvaluationInput{}, fmt.Errorf("BuildEvaluationInput: invalid evaluation policy: %w", err)
	}
	if policy.SessionDigestSHA256 != state.Session.SessionDigestSHA256 || artifact.SessionDigestSHA256 != state.Session.SessionDigestSHA256 {
		return EvaluationInput{}, fmt.Errorf("BuildEvaluationInput: session identity mismatch among state, policy, and candidate")
	}
	if policy.AttemptDigestSHA256 != state.LatestAttemptDigestSHA256 {
		return EvaluationInput{}, fmt.Errorf("BuildEvaluationInput: policy attempt digest %q does not match state latest attempt %q", policy.AttemptDigestSHA256, state.LatestAttemptDigestSHA256)
	}
	if policy.CandidateArtifactDigestSHA256 != artifact.CandidateArtifactDigestSHA256 {
		return EvaluationInput{}, fmt.Errorf("BuildEvaluationInput: policy candidate digest %q does not match artifact %q", policy.CandidateArtifactDigestSHA256, artifact.CandidateArtifactDigestSHA256)
	}
	if artifact.RepositoryDomain != state.Session.RepositoryDomain || artifact.BaseRevision != state.Session.BaseRevision {
		return EvaluationInput{}, fmt.Errorf("BuildEvaluationInput: candidate repository/base identity does not match session")
	}
	if artifact.PlanGeneration != state.PlanGeneration || artifact.AttemptNumber != state.AttemptNumber {
		return EvaluationInput{}, fmt.Errorf("BuildEvaluationInput: candidate plan generation/attempt does not match recorded state")
	}
	ref := strings.TrimSpace(surface.Ref())
	if ref == "" {
		return EvaluationInput{}, fmt.Errorf("BuildEvaluationInput: evaluator surface has an empty reference")
	}

	input := EvaluationInput{
		SchemaVersion:                  EvaluationInputSchemaVersion,
		SessionDigestSHA256:            state.Session.SessionDigestSHA256,
		AttemptDigestSHA256:            state.LatestAttemptDigestSHA256,
		CandidateArtifactDigestSHA256:  artifact.CandidateArtifactDigestSHA256,
		RepositoryDomain:               state.Session.RepositoryDomain,
		BaseRevision:                   state.Session.BaseRevision,
		PlanGeneration:                 state.PlanGeneration,
		AttemptNumber:                  state.AttemptNumber,
		EvaluatorSurfaceRef:            ref,
		DeadlineAt:                     policy.DeadlineAt,
		MaxEvidenceCount:               policy.MaxEvidenceCount,
		MaxEvidenceBytes:               policy.MaxEvidenceBytes,
		RequiredProofObligationDigests: append([]string(nil), state.Session.ProofObligationDigests...),
	}
	input = NormalizeEvaluationInput(input)
	digest, err := EvaluationInputDigest(input)
	if err != nil {
		return EvaluationInput{}, fmt.Errorf("BuildEvaluationInput: compute digest: %w", err)
	}
	input.EvaluationInputDigestSHA256 = digest
	if err := ValidateEvaluationInput(input); err != nil {
		return EvaluationInput{}, fmt.Errorf("BuildEvaluationInput: constructed invalid input: %w", err)
	}
	return input, nil
}

// EvaluatorExecution is checkpoint 4's bounded output for one evaluator. It
// preserves the exact descriptor/input/result trio without composing any
// recommendation or changing O1 state.
type EvaluatorExecution struct {
	Descriptor EvaluatorDescriptor
	Input      EvaluationInput
	Result     EvaluatorResult
}

func closeEvaluatorFailure(surface EvaluatorSurface, failure error) error {
	if cleanupErr := surface.Close(); cleanupErr != nil {
		return fmt.Errorf("%v; cleanup: %w", failure, cleanupErr)
	}
	return failure
}

// ExecuteEvaluator performs one evaluator invocation and owns the disposable
// surface lifecycle. The evaluator must return a schema/digest-valid result
// with CleanupSucceeded unset; O4 revokes/removes the surface, records cleanup
// truth, re-digests the result, and validates it again. A cleanup failure is an
// attributable limitation, not permission to rewrite the evaluator's checks.
func ExecuteEvaluator(ctx context.Context, evaluator Evaluator, input EvaluationInput, surface EvaluatorSurface) (EvaluatorExecution, error) {
	if surface == nil {
		return EvaluatorExecution{}, fmt.Errorf("ExecuteEvaluator: nil surface")
	}
	if evaluator == nil {
		return EvaluatorExecution{}, closeEvaluatorFailure(surface, fmt.Errorf("ExecuteEvaluator: nil evaluator"))
	}
	if input.EvaluatorSurfaceRef != surface.Ref() {
		return EvaluatorExecution{}, closeEvaluatorFailure(surface, fmt.Errorf("ExecuteEvaluator: input surface ref %q does not match surface %q", input.EvaluatorSurfaceRef, surface.Ref()))
	}
	if err := ValidateEvaluationInput(input); err != nil {
		return EvaluatorExecution{}, closeEvaluatorFailure(surface, fmt.Errorf("ExecuteEvaluator: invalid input: %w", err))
	}

	descriptor, err := evaluator.Describe(ctx)
	if err != nil {
		cleanupErr := surface.Close()
		if cleanupErr != nil {
			return EvaluatorExecution{}, fmt.Errorf("ExecuteEvaluator: describe: %v; cleanup: %w", err, cleanupErr)
		}
		return EvaluatorExecution{}, fmt.Errorf("ExecuteEvaluator: describe: %w", err)
	}
	if err := ValidateEvaluatorDescriptor(descriptor); err != nil {
		return EvaluatorExecution{}, closeEvaluatorFailure(surface, fmt.Errorf("ExecuteEvaluator: invalid descriptor: %w", err))
	}

	result, evalErr := evaluator.Evaluate(ctx, input)
	if evalErr != nil {
		cleanupErr := surface.Close()
		if cleanupErr != nil {
			return EvaluatorExecution{}, fmt.Errorf("ExecuteEvaluator: evaluate: %v; cleanup: %w", evalErr, cleanupErr)
		}
		return EvaluatorExecution{}, fmt.Errorf("ExecuteEvaluator: evaluate: %w", evalErr)
	}
	if result.EvaluatorID != descriptor.EvaluatorID {
		return EvaluatorExecution{}, closeEvaluatorFailure(surface, fmt.Errorf("ExecuteEvaluator: result evaluator_id %q does not match descriptor %q", result.EvaluatorID, descriptor.EvaluatorID))
	}
	if result.EvaluatorDescriptorDigestSHA256 != descriptor.DescriptorDigestSHA256 {
		return EvaluatorExecution{}, closeEvaluatorFailure(surface, fmt.Errorf("ExecuteEvaluator: result descriptor digest does not match accepted descriptor"))
	}
	if result.EvaluationInputDigestSHA256 != input.EvaluationInputDigestSHA256 {
		return EvaluatorExecution{}, closeEvaluatorFailure(surface, fmt.Errorf("ExecuteEvaluator: result input digest does not match exact EvaluationInput"))
	}
	if result.CleanupSucceeded != nil {
		return EvaluatorExecution{}, closeEvaluatorFailure(surface, fmt.Errorf("ExecuteEvaluator: evaluator must leave cleanup_succeeded nil; O4 owns surface cleanup truth"))
	}
	if err := ValidateEvaluatorResult(result); err != nil {
		return EvaluatorExecution{}, closeEvaluatorFailure(surface, fmt.Errorf("ExecuteEvaluator: evaluator returned invalid result: %w", err))
	}

	cleanupErr := surface.Close()
	cleanupSucceeded := cleanupErr == nil
	result.CleanupSucceeded = &cleanupSucceeded
	if cleanupErr != nil {
		result.Limitations = append(result.Limitations, synthesis.Limitation{
			Source:   "evaluatorcomposition.surface",
			Scope:    descriptor.EvaluatorID,
			Reason:   "disposable evaluator surface cleanup failed: " + cleanupErr.Error(),
			Blocking: false,
		})
	}
	result = NormalizeEvaluatorResult(result)
	digest, err := EvaluatorResultDigest(result)
	if err != nil {
		return EvaluatorExecution{}, fmt.Errorf("ExecuteEvaluator: recompute result digest after cleanup: %w", err)
	}
	result.ResultDigestSHA256 = digest
	if err := ValidateEvaluatorResult(result); err != nil {
		return EvaluatorExecution{}, fmt.Errorf("ExecuteEvaluator: finalized result invalid: %w", err)
	}
	return EvaluatorExecution{Descriptor: descriptor, Input: input, Result: result}, nil
}
