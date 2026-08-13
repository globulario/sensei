// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/agentcommand"
	"github.com/globulario/sensei/golang/architecture/evaluatorcomposition"
	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
	"github.com/globulario/sensei/golang/architecture/workspacecontract"
)

type failPredicate func(evaluatorcomposition.EvaluationInput) bool

func TestRunConsumesExactlyOneRetry(t *testing.T) {
	state, config := lifecycleHarness(
		t,
		1,
		0,
		string(evaluatorcomposition.FailureClassMechanicalCheckFailure),
		synthesis.RecommendRetryGeneration,
		func(input evaluatorcomposition.EvaluationInput) bool { return input.AttemptNumber == 1 },
	)
	result, err := Run(context.Background(), state, config)
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.Disposition != DispositionCandidateReady || result.SessionState.ConsumedRetryBudget() != 1 {
		t.Fatalf("disposition=%q consumed_retry=%d phase=%q", result.Receipt.Disposition, result.SessionState.ConsumedRetryBudget(), result.SessionState.Phase)
	}
	if len(result.Trace.GenerationHandoffs) != 2 || len(result.Trace.EvaluationResults) != 2 || result.SessionState.AttemptNumber != 2 {
		t.Fatalf("attempt trace O3=%d O4=%d state_attempt=%d", len(result.Trace.GenerationHandoffs), len(result.Trace.EvaluationResults), result.SessionState.AttemptNumber)
	}
	if result.SessionState.PlanGeneration != 1 {
		t.Fatalf("retry changed plan generation to %d", result.SessionState.PlanGeneration)
	}
}

func TestRunConsumesExactlyOneReplan(t *testing.T) {
	state, config := lifecycleHarness(
		t,
		0,
		1,
		string(evaluatorcomposition.FailureClassAuditPlanLevel),
		synthesis.RecommendReplan,
		func(input evaluatorcomposition.EvaluationInput) bool { return input.PlanGeneration == 1 },
	)
	result, err := Run(context.Background(), state, config)
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.Disposition != DispositionCandidateReady || result.SessionState.ConsumedReplanBudget() != 1 {
		t.Fatalf("disposition=%q consumed_replan=%d phase=%q", result.Receipt.Disposition, result.SessionState.ConsumedReplanBudget(), result.SessionState.Phase)
	}
	if result.SessionState.PlanGeneration != 2 || result.SessionState.AttemptNumber != 2 {
		t.Fatalf("generation=%d attempt=%d", result.SessionState.PlanGeneration, result.SessionState.AttemptNumber)
	}
	if len(result.Trace.ProviderExecutions) != 3 {
		t.Fatalf("provider executions=%d, want interpretation plus two plans", len(result.Trace.ProviderExecutions))
	}
}

func TestRunProviderStopPreservesO1Phase(t *testing.T) {
	state, config := lifecycleHarness(t, 0, 0, "", "", nil)
	config.InterpretationProvider = &cognitiveProvider{operation: providerport.OperationInterpretation, unavailable: true}
	result, err := Run(context.Background(), state, config)
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.Disposition != DispositionProviderStopped || result.SessionState.Phase != synthesis.PhaseCreated || result.SessionState.Receipt != nil {
		t.Fatalf("disposition=%q phase=%q receipt=%#v", result.Receipt.Disposition, result.SessionState.Phase, result.SessionState.Receipt)
	}
	if len(result.Trace.ProviderExecutions) != 1 || len(result.Trace.Events) != 0 {
		t.Fatalf("provider executions=%d events=%d", len(result.Trace.ProviderExecutions), len(result.Trace.Events))
	}
}

func TestRunStepLimitCannotEnlargeSessionBudgets(t *testing.T) {
	state, config := lifecycleHarness(t, 2, 2, "", "", nil)
	config.MaxSteps = 1
	result, err := Run(context.Background(), state, config)
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.Disposition != DispositionStepLimitReached || result.SessionState.Phase != synthesis.PhasePlanning {
		t.Fatalf("disposition=%q phase=%q", result.Receipt.Disposition, result.SessionState.Phase)
	}
	if result.SessionState.RemainingRetryBudget != 2 || result.SessionState.RemainingReplanBudget != 2 || result.SessionState.Receipt != nil {
		t.Fatal("step limit mutated O1 budgets or invented a terminal receipt")
	}
}

// TestRunFinalizesTerminalStateReachedOnTheLastAllowedStep is the direct
// regression test for a live review finding: config.EvaluationEngine.Evaluate
// can transition state all the way to PhaseSucceeded/PhaseFailed within a
// single PhaseAttempting iteration (transitionRecordEvaluation resolves
// PhaseEvaluating -> {Succeeded | Retry | Replan | Failed} in one call).
// Before this fix, the driver loop only checked for that terminal phase at
// the TOP of the NEXT iteration -- so a transition landing on the last
// allowed step (step == config.MaxSteps) meant the loop exited with no next
// iteration at all, falling through to the step-limit finishResult with a
// terminal receipt already stamped on state, which ValidateRunReceipt's own
// DispositionStepLimitReached case rejects outright ("nonterminal stop
// cannot invent an O1 terminal receipt") -- turning a genuine success into
// a hard Go error, silently orphaning a sealed candidate with no lineage
// ever persisted.
//
// Determines the exact natural step count for an ordinary first-attempt
// success (no retries/replans configured), then re-runs with MaxSteps
// pinned to precisely that count -- the exact boundary the finding
// describes -- and asserts it still succeeds cleanly.
func TestRunFinalizesTerminalStateReachedOnTheLastAllowedStep(t *testing.T) {
	state, config := lifecycleHarness(t, 0, 0, "", "", nil)
	config.MaxSteps = 20
	baseline, err := Run(context.Background(), state, config)
	if err != nil {
		t.Fatalf("baseline run: %v", err)
	}
	if baseline.Receipt.Disposition != DispositionCandidateReady {
		t.Fatalf("baseline disposition = %q, want candidate-ready", baseline.Receipt.Disposition)
	}

	state2, config2 := lifecycleHarness(t, 0, 0, "", "", nil)
	config2.MaxSteps = baseline.Receipt.StepCount
	result, err := Run(context.Background(), state2, config2)
	if err != nil {
		t.Fatalf("Run at the exact step-limit boundary returned an error instead of a valid result: %v", err)
	}
	if result.Receipt.Disposition != DispositionCandidateReady {
		t.Fatalf("disposition = %q, want candidate-ready (MaxSteps=%d exactly matches the natural completion step)", result.Receipt.Disposition, config2.MaxSteps)
	}
	if result.SessionState.Phase != synthesis.PhaseSucceeded || result.SessionState.Receipt == nil {
		t.Fatalf("phase=%q receipt=%v, want succeeded with a real O1 receipt", result.SessionState.Phase, result.SessionState.Receipt)
	}
	if result.Receipt.StepCount != config2.MaxSteps {
		t.Fatalf("StepCount = %d, want %d (the step that actually produced the terminal transition, not one later)", result.Receipt.StepCount, config2.MaxSteps)
	}
}

// TestRunFinalizesTerminalFailureReachedOnTheLastAllowedStep covers the
// same boundary for the PhaseFailed side (RecommendAbort), not just
// PhaseSucceeded.
func TestRunFinalizesTerminalFailureReachedOnTheLastAllowedStep(t *testing.T) {
	state, config := lifecycleHarness(
		t, 0, 0,
		string(evaluatorcomposition.FailureClassMechanicalCheckFailure),
		synthesis.RecommendAbort,
		func(input evaluatorcomposition.EvaluationInput) bool { return input.AttemptNumber == 1 },
	)
	config.MaxSteps = 20
	baseline, err := Run(context.Background(), state, config)
	if err != nil {
		t.Fatalf("baseline run: %v", err)
	}
	if baseline.Receipt.Disposition != DispositionTerminalFailure {
		t.Fatalf("baseline disposition = %q, want terminal-failure", baseline.Receipt.Disposition)
	}

	state2, config2 := lifecycleHarness(
		t, 0, 0,
		string(evaluatorcomposition.FailureClassMechanicalCheckFailure),
		synthesis.RecommendAbort,
		func(input evaluatorcomposition.EvaluationInput) bool { return input.AttemptNumber == 1 },
	)
	config2.MaxSteps = baseline.Receipt.StepCount
	result, err := Run(context.Background(), state2, config2)
	if err != nil {
		t.Fatalf("Run at the exact step-limit boundary returned an error instead of a valid result: %v", err)
	}
	if result.Receipt.Disposition != DispositionTerminalFailure {
		t.Fatalf("disposition = %q, want terminal-failure (MaxSteps=%d exactly matches the natural completion step)", result.Receipt.Disposition, config2.MaxSteps)
	}
	if result.SessionState.Phase != synthesis.PhaseFailed || result.SessionState.Receipt == nil {
		t.Fatalf("phase=%q receipt=%v, want failed with a real O1 receipt", result.SessionState.Phase, result.SessionState.Receipt)
	}
}

type invalidGenerationAgent struct{}

func (invalidGenerationAgent) Generate(context.Context, agentcommand.GenerationPrompt, providerport.Observer) (agentcommand.MutationPlan, error) {
	return agentcommand.MutationPlan{}, &agentcommand.InvalidOutputError{Detail: "deliberately invalid generation output"}
}

func TestRunO3NonVerifiedStopsBeforeO4(t *testing.T) {
	state, config := lifecycleHarness(t, 0, 0, "", "", nil)
	factory, err := agentcommand.NewFactory(agentcommand.Config{
		Agent:            invalidGenerationAgent{},
		ProviderID:       "o7.invalid-generator",
		ProviderKind:     "deterministic-test",
		ObservedAt:       "2026-08-02T00:00:00Z",
		ProducedAt:       "2026-08-02T00:01:00Z",
		MaxSnapshotBytes: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	config.GenerationFactory = factory
	result, err := Run(context.Background(), state, config)
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.Disposition != DispositionRunnerStopped || result.SessionState.Phase != synthesis.PhaseAttempting {
		t.Fatalf("disposition=%q phase=%q", result.Receipt.Disposition, result.SessionState.Phase)
	}
	if len(result.Trace.GenerationHandoffs) != 1 || len(result.Trace.EvaluationResults) != 0 {
		t.Fatalf("O3=%d O4=%d", len(result.Trace.GenerationHandoffs), len(result.Trace.EvaluationResults))
	}
}

func TestRunRequiredEvaluatorUnavailableUsesO4TerminalPath(t *testing.T) {
	state, config := lifecycleHarness(t, 0, 0, "", "", nil)
	engine := config.EvaluationEngine.(*O4Engine)
	engine.Evaluators = nil
	result, err := Run(context.Background(), state, config)
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.Disposition != DispositionTerminalFailure || result.SessionState.Phase != synthesis.PhaseFailed {
		t.Fatalf("disposition=%q phase=%q", result.Receipt.Disposition, result.SessionState.Phase)
	}
	if len(result.Trace.EvaluationResults) != 1 || result.Trace.EvaluationResults[0].Receipt == nil ||
		result.Trace.EvaluationResults[0].Receipt.Disposition != evaluatorcomposition.DispositionRequiredEvaluatorUnavailable {
		t.Fatalf("evaluation result = %#v", result.Trace.EvaluationResults)
	}
}

func TestRunReceiptDigestExcludesObservationTimestamps(t *testing.T) {
	receipt := RunReceipt{
		SchemaVersion:                  RunReceiptSchemaVersion,
		ReceiptID:                      "o7.receipt.test",
		GeneratedBy:                    GeneratedBy,
		SessionDigestSHA256:            strings.Repeat("a", 64),
		FinalPhase:                     string(synthesis.PhasePlanning),
		Disposition:                    DispositionStepLimitReached,
		StepCount:                      1,
		O2ReceiptDigestsSHA256:         []string{strings.Repeat("b", 64)},
		RunnerReceiptDigestsSHA256:     []string{},
		EvaluationReceiptDigestsSHA256: []string{},
		Detail:                         "bounded stop",
		StartedAt:                      "2026-08-02T00:00:00Z",
		CompletedAt:                    "2026-08-02T00:00:01Z",
	}
	first, err := RunReceiptDigest(receipt)
	if err != nil {
		t.Fatal(err)
	}
	receipt.StartedAt = "2030-01-01T00:00:00Z"
	receipt.CompletedAt = "2030-01-01T00:00:01Z"
	second, err := RunReceiptDigest(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("observation timestamps changed receipt identity: %s != %s", first, second)
	}
}

func lifecycleHarness(
	t *testing.T,
	retryBudget int,
	replanBudget int,
	failureClass string,
	recommendation synthesis.Recommendation,
	shouldFail failPredicate,
) (synthesis.SessionState, Config) {
	t.Helper()
	repoRoot, revision := createO7Repository(t)
	const repositoryDomain = "github.com/example/o7-lifecycle"
	identity := createO7Identity(repositoryDomain, revision)
	identityDigest, err := workspacecontract.IdentityDigest(identity)
	if err != nil {
		t.Fatal(err)
	}
	session := createO7Session(t, repositoryDomain, revision, identityDigest, retryBudget, replanBudget)
	state, err := synthesis.NewSessionState(session)
	if err != nil {
		t.Fatal(err)
	}
	store, err := runnercomposition.NewFSCandidateArtifactStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	factory, err := agentcommand.NewFactory(agentcommand.Config{
		Agent:            mutationAgent{},
		ProviderID:       "o7.generator",
		ProviderKind:     "deterministic-test",
		ObservedAt:       "2026-08-02T00:00:00Z",
		ProducedAt:       "2026-08-02T00:01:00Z",
		MaxSnapshotBytes: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	materializer, err := evaluatorcomposition.NewCandidateMaterializer(repositoryDomain, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	clock := fixedClock()
	engine := &O4Engine{
		Store:         store,
		PolicyFactory: lifecyclePolicyFactory(failureClass, recommendation),
		Materializer:  materializer,
		Evaluators: []EvaluatorBinding{{
			EvaluatorID: "o7.lifecycle",
			SurfaceMode: evaluatorcomposition.SurfaceModePlain,
			New:         lifecycleEvaluatorFactory(shouldFail, failureClass),
		}},
		Now: clock,
	}
	return state, Config{
		WorkspaceIdentity:        identity,
		RepositoryRoot:           repoRoot,
		CandidateStore:           store,
		InterpretationProvider:   &cognitiveProvider{operation: providerport.OperationInterpretation},
		InterpretationAuthority:  testGoverningInterpretationAuthority(),
		PlanningProvider:         &cognitiveProvider{operation: providerport.OperationPlanning},
		GenerationFactory:        factory,
		EvaluationEngine:         engine,
		InterpretationPolicy:     ProviderPolicy{DeadlineAt: "2099-01-01T00:00:00Z", MaxObservationCount: 8, MaxObservationBytes: 4096},
		PlanningPolicy:           ProviderPolicy{DeadlineAt: "2099-01-01T00:00:00Z", MaxObservationCount: 8, MaxObservationBytes: 4096},
		GenerationPolicy:         runnercomposition.RequestPolicy{DeadlineAt: "2099-01-01T00:00:00Z", MaxObservationCount: 8, MaxObservationBytes: 4096},
		MaxSteps:                 20,
		Now:                      clock,
	}
}

func lifecyclePolicyFactory(failureClass string, recommendation synthesis.Recommendation) EvaluationPolicyFactory {
	return EvaluationPolicyFactoryFunc(func(_ context.Context, state synthesis.SessionState, handoff runnercomposition.VerifiedGenerationHandoff) (evaluatorcomposition.EvaluationPolicy, error) {
		mappings := []evaluatorcomposition.FailureClassRecommendation{}
		if failureClass != "" {
			mappings = append(mappings,
				evaluatorcomposition.FailureClassRecommendation{
					FailureClass: failureClass, Recommendation: recommendation,
				},
				evaluatorcomposition.FailureClassRecommendation{
					FailureClass:   evaluatorcomposition.FailureClassRequiredCheckUnsatisfied,
					Recommendation: recommendation,
				},
			)
		}
		policy := evaluatorcomposition.EvaluationPolicy{
			SchemaVersion:                 evaluatorcomposition.EvaluationPolicySchemaVersion,
			PolicyID:                      "policy.o7.lifecycle",
			SessionDigestSHA256:           state.Session.SessionDigestSHA256,
			AttemptDigestSHA256:           handoff.Result.GenerationPayload.AttemptDigestSHA256,
			CandidateArtifactDigestSHA256: *handoff.RunnerReceipt.CandidateArtifactDigestSHA256,
			Evaluators:                    []evaluatorcomposition.EvaluatorSpec{{EvaluatorID: "o7.lifecycle", Required: true}},
			DeadlineAt:                    "2099-01-01T00:00:00Z",
			MaxEvidenceCount:              8,
			MaxEvidenceBytes:              4096,
			RequiredCheckIDs:              []string{"lifecycle-check"},
			FailureClassRecommendations:   mappings,
		}
		digest, err := evaluatorcomposition.EvaluationPolicyDigest(policy)
		if err != nil {
			return evaluatorcomposition.EvaluationPolicy{}, err
		}
		policy.PolicyDigestSHA256 = digest
		return policy, nil
	})
}

func lifecycleEvaluatorFactory(shouldFail failPredicate, failureClass string) EvaluatorFactory {
	return func(surface evaluatorcomposition.EvaluatorSurface) (evaluatorcomposition.Evaluator, error) {
		root, err := surface.RootPath()
		if err != nil {
			return nil, err
		}
		descriptor := evaluatorcomposition.EvaluatorDescriptor{
			SchemaVersion:        evaluatorcomposition.EvaluatorDescriptorSchemaVersion,
			EvaluatorID:          "o7.lifecycle",
			EvaluatorKind:        "mechanical",
			EvaluatorVersion:     "v1",
			SupportedCheckIDs:    []string{"lifecycle-check"},
			Deterministic:        true,
			RequiredCapabilities: []string{},
			Limitations:          []synthesis.Limitation{},
		}
		descriptorDigest, err := evaluatorcomposition.EvaluatorDescriptorDigest(descriptor)
		if err != nil {
			return nil, err
		}
		descriptor.DescriptorDigestSHA256 = descriptorDigest
		return evaluatorcomposition.EvaluatorFunc{
			DescribeFunc: func(context.Context) (evaluatorcomposition.EvaluatorDescriptor, error) { return descriptor, nil },
			EvaluateFunc: func(_ context.Context, input evaluatorcomposition.EvaluationInput) (evaluatorcomposition.EvaluatorResult, error) {
				if _, err := os.Stat(filepath.Join(root, "new.txt")); err != nil {
					return evaluatorcomposition.EvaluatorResult{}, err
				}
				status := synthesis.CheckPassed
				failures := []string{}
				if shouldFail != nil && shouldFail(input) {
					status = synthesis.CheckFailed
					failures = []string{failureClass}
				}
				result := evaluatorcomposition.NormalizeEvaluatorResult(evaluatorcomposition.EvaluatorResult{
					SchemaVersion:                   evaluatorcomposition.EvaluatorResultSchemaVersion,
					EvaluatorID:                     descriptor.EvaluatorID,
					EvaluatorDescriptorDigestSHA256: descriptor.DescriptorDigestSHA256,
					EvaluationInputDigestSHA256:     input.EvaluationInputDigestSHA256,
					TerminalOutcome:                 evaluatorcomposition.EvaluatorOutcomeCompleted,
					Checks:                          []synthesis.CheckObservation{{CheckID: "lifecycle-check", Status: status, EvidenceReferences: []string{}}},
					EvidenceReferences:              []evaluatorcomposition.EvidenceReference{},
					ClassifiedFailureReasons:        failures,
					Limitations:                     []synthesis.Limitation{},
					CleanupSucceeded:                nil,
				})
				digest, err := evaluatorcomposition.EvaluatorResultDigest(result)
				if err != nil {
					return evaluatorcomposition.EvaluatorResult{}, err
				}
				result.ResultDigestSHA256 = digest
				return result, nil
			},
		}, nil
	}
}
