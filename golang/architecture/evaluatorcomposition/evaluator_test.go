// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

type recordingEvaluatorSurface struct {
	ref      string
	root     string
	mode     SurfaceMode
	closeErr error
	closed   int
}

func (s *recordingEvaluatorSurface) Ref() string       { return s.ref }
func (s *recordingEvaluatorSurface) Mode() SurfaceMode { return s.mode }
func (s *recordingEvaluatorSurface) RootPath() (string, error) {
	if s.closed > 0 {
		return "", ErrEvaluatorSurfaceClosed
	}
	return s.root, nil
}
func (s *recordingEvaluatorSurface) Close() error {
	s.closed++
	return s.closeErr
}

func evaluationInputForSurface(t *testing.T, surface EvaluatorSurface) EvaluationInput {
	t.Helper()
	input := fixtureEvaluationInput(t)
	input.EvaluatorSurfaceRef = surface.Ref()
	input.DeadlineAt = "2099-01-01T00:00:00Z"
	input.MaxEvidenceCount = 10
	input.MaxEvidenceBytes = 1 << 20
	input = NormalizeEvaluationInput(input)
	digest, err := EvaluationInputDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	input.EvaluationInputDigestSHA256 = digest
	if err := ValidateEvaluationInput(input); err != nil {
		t.Fatalf("test input invalid: %v", err)
	}
	return input
}

func evaluatorDescriptorForExecution(t *testing.T, evaluatorID string) EvaluatorDescriptor {
	t.Helper()
	descriptor := fixtureEvaluatorDescriptor(t)
	descriptor.EvaluatorID = evaluatorID
	descriptor = NormalizeEvaluatorDescriptor(descriptor)
	digest, err := EvaluatorDescriptorDigest(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	descriptor.DescriptorDigestSHA256 = digest
	if err := ValidateEvaluatorDescriptor(descriptor); err != nil {
		t.Fatalf("test descriptor invalid: %v", err)
	}
	return descriptor
}

func evaluatorResultForExecution(t *testing.T, descriptor EvaluatorDescriptor, input EvaluationInput) EvaluatorResult {
	t.Helper()
	result := fixtureEvaluatorResult(t)
	result.EvaluatorID = descriptor.EvaluatorID
	result.EvaluatorDescriptorDigestSHA256 = descriptor.DescriptorDigestSHA256
	result.EvaluationInputDigestSHA256 = input.EvaluationInputDigestSHA256
	result.CleanupSucceeded = nil
	result = NormalizeEvaluatorResult(result)
	digest, err := EvaluatorResultDigest(result)
	if err != nil {
		t.Fatal(err)
	}
	result.ResultDigestSHA256 = digest
	if err := ValidateEvaluatorResult(result); err != nil {
		t.Fatalf("test result invalid: %v", err)
	}
	return result
}

func TestExecuteEvaluatorOwnsCleanupTruthAndRedigestsResult(t *testing.T) {
	surface := &recordingEvaluatorSurface{ref: "surface://test/success/plain", root: t.TempDir(), mode: SurfaceModePlain}
	input := evaluationInputForSurface(t, surface)
	descriptor := evaluatorDescriptorForExecution(t, "evaluator.success")
	result := evaluatorResultForExecution(t, descriptor, input)
	evaluator := EvaluatorFunc{
		DescribeFunc: func(context.Context) (EvaluatorDescriptor, error) { return descriptor, nil },
		EvaluateFunc: func(context.Context, EvaluationInput) (EvaluatorResult, error) { return result, nil },
	}

	execution, err := ExecuteEvaluator(context.Background(), evaluator, input, surface)
	if err != nil {
		t.Fatal(err)
	}
	if surface.closed != 1 {
		t.Fatalf("surface Close calls = %d, want 1", surface.closed)
	}
	if execution.Result.CleanupSucceeded == nil || !*execution.Result.CleanupSucceeded {
		t.Fatal("final result did not record successful O4-owned cleanup")
	}
	if execution.Result.ResultDigestSHA256 == result.ResultDigestSHA256 {
		t.Fatal("cleanup truth changed the result but its digest was not changed")
	}
	if err := ValidateEvaluatorResult(execution.Result); err != nil {
		t.Fatalf("finalized result invalid: %v", err)
	}
}

func TestExecuteEvaluatorRecordsCleanupFailureWithoutRewritingChecks(t *testing.T) {
	cleanupErr := errors.New("simulated cleanup failure")
	surface := &recordingEvaluatorSurface{ref: "surface://test/cleanup-failure/plain", root: t.TempDir(), mode: SurfaceModePlain, closeErr: cleanupErr}
	input := evaluationInputForSurface(t, surface)
	descriptor := evaluatorDescriptorForExecution(t, "evaluator.cleanup-failure")
	result := evaluatorResultForExecution(t, descriptor, input)
	originalChecks := append([]synthesis.CheckObservation(nil), result.Checks...)
	evaluator := EvaluatorFunc{
		DescribeFunc: func(context.Context) (EvaluatorDescriptor, error) { return descriptor, nil },
		EvaluateFunc: func(context.Context, EvaluationInput) (EvaluatorResult, error) { return result, nil },
	}

	execution, err := ExecuteEvaluator(context.Background(), evaluator, input, surface)
	if err != nil {
		t.Fatal(err)
	}
	if execution.Result.CleanupSucceeded == nil || *execution.Result.CleanupSucceeded {
		t.Fatal("cleanup failure was not recorded")
	}
	if len(execution.Result.Checks) != len(originalChecks) || execution.Result.Checks[0].Status != originalChecks[0].Status {
		t.Fatal("cleanup failure rewrote evaluator check truth")
	}
	found := false
	for _, limitation := range execution.Result.Limitations {
		if strings.Contains(limitation.Reason, cleanupErr.Error()) {
			found = true
		}
	}
	if !found {
		t.Fatal("cleanup failure limitation missing")
	}
}

func TestExecuteEvaluatorClosesSurfaceOnEvaluatorErrorAndBindingRefusal(t *testing.T) {
	t.Run("evaluator error", func(t *testing.T) {
		surface := &recordingEvaluatorSurface{ref: "surface://test/eval-error/plain", root: t.TempDir(), mode: SurfaceModePlain}
		input := evaluationInputForSurface(t, surface)
		descriptor := evaluatorDescriptorForExecution(t, "evaluator.error")
		evaluator := EvaluatorFunc{
			DescribeFunc: func(context.Context) (EvaluatorDescriptor, error) { return descriptor, nil },
			EvaluateFunc: func(context.Context, EvaluationInput) (EvaluatorResult, error) {
				return EvaluatorResult{}, errors.New("evaluation failed")
			},
		}
		if _, err := ExecuteEvaluator(context.Background(), evaluator, input, surface); err == nil {
			t.Fatal("evaluator error was accepted")
		}
		if surface.closed != 1 {
			t.Fatalf("surface Close calls = %d, want 1", surface.closed)
		}
	})

	t.Run("descriptor result mismatch", func(t *testing.T) {
		cleanupErr := errors.New("binding cleanup failed")
		surface := &recordingEvaluatorSurface{ref: "surface://test/binding-error/plain", root: t.TempDir(), mode: SurfaceModePlain, closeErr: cleanupErr}
		input := evaluationInputForSurface(t, surface)
		descriptor := evaluatorDescriptorForExecution(t, "evaluator.binding")
		result := evaluatorResultForExecution(t, descriptor, input)
		result.EvaluatorID = "another-evaluator"
		result = NormalizeEvaluatorResult(result)
		digest, err := EvaluatorResultDigest(result)
		if err != nil {
			t.Fatal(err)
		}
		result.ResultDigestSHA256 = digest
		evaluator := EvaluatorFunc{
			DescribeFunc: func(context.Context) (EvaluatorDescriptor, error) { return descriptor, nil },
			EvaluateFunc: func(context.Context, EvaluationInput) (EvaluatorResult, error) { return result, nil },
		}
		if _, err := ExecuteEvaluator(context.Background(), evaluator, input, surface); err == nil || !strings.Contains(err.Error(), "evaluator_id") || !strings.Contains(err.Error(), cleanupErr.Error()) {
			t.Fatalf("binding refusal did not preserve cleanup failure: %v", err)
		}
		if surface.closed != 1 {
			t.Fatalf("surface Close calls = %d, want 1", surface.closed)
		}
	})
}
