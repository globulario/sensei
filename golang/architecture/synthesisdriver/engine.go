// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/evaluatorcomposition"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// EvaluationPolicyFactory binds one immutable O4 policy to the exact current
// session, attempt, and sealed candidate carried by handoff.
type EvaluationPolicyFactory interface {
	Build(ctx context.Context, state synthesis.SessionState, handoff runnercomposition.VerifiedGenerationHandoff) (evaluatorcomposition.EvaluationPolicy, error)
}

// EvaluationPolicyFactoryFunc is the function adapter for
// EvaluationPolicyFactory.
type EvaluationPolicyFactoryFunc func(context.Context, synthesis.SessionState, runnercomposition.VerifiedGenerationHandoff) (evaluatorcomposition.EvaluationPolicy, error)

func (f EvaluationPolicyFactoryFunc) Build(ctx context.Context, state synthesis.SessionState, handoff runnercomposition.VerifiedGenerationHandoff) (evaluatorcomposition.EvaluationPolicy, error) {
	if f == nil {
		return evaluatorcomposition.EvaluationPolicy{}, errors.New("synthesisdriver: nil evaluation policy factory")
	}
	return f(ctx, state, handoff)
}

// EvaluatorFactory constructs one evaluator bound to one fresh O4 surface.
type EvaluatorFactory func(surface evaluatorcomposition.EvaluatorSurface) (evaluatorcomposition.Evaluator, error)

// EvaluatorBinding gives O7 only the capability needed to satisfy one explicit
// evaluator ID already selected by the caller-owned O4 policy.
type EvaluatorBinding struct {
	EvaluatorID string
	SurfaceMode evaluatorcomposition.SurfaceMode
	New         EvaluatorFactory
}

// O4Engine is the concrete O7 EvaluationEngine. It delegates every semantic
// decision, materialization, evaluator input, cleanup, composition, and O1
// transition to evaluatorcomposition.
type O4Engine struct {
	Store         runnercomposition.CandidateArtifactStore
	PolicyFactory EvaluationPolicyFactory
	Materializer  *evaluatorcomposition.CandidateMaterializer
	Evaluators    []EvaluatorBinding
	Resolver      evaluatorcomposition.EvidenceResolver
	Now           func() time.Time
}

func (e *O4Engine) Evaluate(ctx context.Context, state synthesis.SessionState, handoff runnercomposition.VerifiedGenerationHandoff) (evaluatorcomposition.Result, error) {
	if e == nil {
		return evaluatorcomposition.Result{}, errors.New("synthesisdriver: nil O4 engine")
	}
	if e.Store == nil || e.PolicyFactory == nil || e.Materializer == nil || e.Now == nil {
		return evaluatorcomposition.Result{}, errors.New("synthesisdriver: incomplete O4 engine capability set")
	}
	bindings, err := canonicalEvaluatorBindings(e.Evaluators)
	if err != nil {
		return evaluatorcomposition.Result{}, err
	}
	policy, err := e.PolicyFactory.Build(ctx, state, handoff)
	if err != nil {
		return evaluatorcomposition.Result{}, fmt.Errorf("synthesisdriver: build O4 policy: %w", err)
	}
	if err := evaluatorcomposition.ValidateEvaluationPolicy(policy); err != nil {
		return evaluatorcomposition.Result{}, fmt.Errorf("synthesisdriver: O4 policy: %w", err)
	}

	checkpoint, err := evaluatorcomposition.Run(ctx, state, handoff, policy, e.Store, e.Now)
	if err != nil {
		return evaluatorcomposition.Result{}, fmt.Errorf("synthesisdriver: O4 accept generation handoff: %w", err)
	}
	if checkpoint.Receipt != nil || checkpoint.Candidate == nil {
		return checkpoint, nil
	}

	bindingByID := make(map[string]EvaluatorBinding, len(bindings))
	for _, binding := range bindings {
		bindingByID[binding.EvaluatorID] = binding
	}
	policySpecByID := make(map[string]evaluatorcomposition.EvaluatorSpec, len(policy.Evaluators))
	for _, spec := range policy.Evaluators {
		policySpecByID[spec.EvaluatorID] = spec
	}

	executions := make([]evaluatorcomposition.EvaluatorExecution, 0, len(policy.Evaluators))
	for _, spec := range policy.Evaluators {
		binding, ok := bindingByID[spec.EvaluatorID]
		if !ok {
			// CompleteEvaluation owns required-versus-optional missing evaluator
			// semantics. O7 does not invent a replacement evaluator.
			continue
		}
		surface, materializeErr := e.Materializer.Materialize(ctx, *checkpoint.Candidate, spec.EvaluatorID, binding.SurfaceMode)
		if materializeErr != nil {
			if spec.Required {
				return evaluatorcomposition.TerminateEvaluationUnavailable(
					checkpoint,
					handoff,
					policy,
					evaluatorcomposition.DispositionMaterializationFailure,
					fmt.Sprintf("required evaluator %q materialization failed: %v", spec.EvaluatorID, materializeErr),
					executions,
					e.Now,
				)
			}
			continue
		}
		input, inputErr := evaluatorcomposition.BuildEvaluationInput(checkpoint.SessionState, *checkpoint.Candidate, policy, surface)
		if inputErr != nil {
			_ = surface.Close()
			return evaluatorcomposition.Result{}, fmt.Errorf("synthesisdriver: build O4 input for %q: %w", spec.EvaluatorID, inputErr)
		}
		evaluator, evaluatorErr := binding.New(surface)
		if evaluatorErr != nil {
			_ = surface.Close()
			if spec.Required {
				return evaluatorcomposition.TerminateEvaluationUnavailable(
					checkpoint,
					handoff,
					policy,
					evaluatorcomposition.DispositionRequiredEvaluatorUnavailable,
					fmt.Sprintf("required evaluator %q construction failed: %v", spec.EvaluatorID, evaluatorErr),
					executions,
					e.Now,
				)
			}
			continue
		}
		execution, executionErr := evaluatorcomposition.ExecuteEvaluator(ctx, evaluator, input, surface)
		if executionErr != nil {
			if spec.Required {
				return evaluatorcomposition.TerminateEvaluationUnavailable(
					checkpoint,
					handoff,
					policy,
					evaluatorcomposition.DispositionRequiredEvaluatorUnavailable,
					fmt.Sprintf("required evaluator %q execution failed: %v", spec.EvaluatorID, executionErr),
					executions,
					e.Now,
				)
			}
			continue
		}
		executions = append(executions, execution)
	}

	return evaluatorcomposition.CompleteEvaluation(ctx, checkpoint, handoff, policy, executions, e.Resolver, e.Now)
}

func canonicalEvaluatorBindings(bindings []EvaluatorBinding) ([]EvaluatorBinding, error) {
	out := append([]EvaluatorBinding{}, bindings...)
	seen := make(map[string]struct{}, len(out))
	for i := range out {
		out[i].EvaluatorID = strings.TrimSpace(out[i].EvaluatorID)
		if out[i].EvaluatorID == "" || out[i].New == nil {
			return nil, fmt.Errorf("synthesisdriver: evaluator binding %d is incomplete", i)
		}
		if out[i].SurfaceMode != evaluatorcomposition.SurfaceModePlain && out[i].SurfaceMode != evaluatorcomposition.SurfaceModeGitDiff {
			return nil, fmt.Errorf("synthesisdriver: evaluator %q has unsupported surface mode %q", out[i].EvaluatorID, out[i].SurfaceMode)
		}
		if _, duplicate := seen[out[i].EvaluatorID]; duplicate {
			return nil, fmt.Errorf("synthesisdriver: duplicate evaluator binding %q", out[i].EvaluatorID)
		}
		seen[out[i].EvaluatorID] = struct{}{}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EvaluatorID < out[j].EvaluatorID })
	return out, nil
}
