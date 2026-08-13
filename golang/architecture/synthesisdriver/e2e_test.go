// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/agentcommand"
	"github.com/globulario/sensei/golang/architecture/evaluatorcomposition"
	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
	"github.com/globulario/sensei/golang/architecture/workspacecontract"
)

const testZeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"

type cognitiveProvider struct {
	operation   providerport.Operation
	unavailable bool
}

func (p *cognitiveProvider) Describe(context.Context) (providerport.Capabilities, error) {
	capabilities := providerport.Capabilities{
		SchemaVersion: providerport.CapabilitiesSchemaVersion,
		ProviderObservation: synthesis.ProviderObservation{
			ProviderID:   "o7." + string(p.operation),
			ProviderKind: "deterministic-test",
			ObservedAt:   "2026-08-02T00:00:00Z",
		},
		SupportedOperations: []providerport.Operation{p.operation},
	}
	digest, err := providerport.CapabilitiesDigest(capabilities)
	if err != nil {
		return providerport.Capabilities{}, err
	}
	capabilities.CapabilitiesDigestSHA256 = digest
	return capabilities, nil
}

func (p *cognitiveProvider) Execute(_ context.Context, request providerport.Request, _ providerport.Observer) (providerport.Result, error) {
	if p.unavailable {
		return finishProviderResult(providerport.Result{
			SchemaVersion:       providerport.ResultSchemaVersion,
			RequestDigestSHA256: request.RequestDigestSHA256,
			Operation:           request.Operation,
			TerminalOutcome:     providerport.OutcomeUnavailable,
			Detail:              "deliberately unavailable",
		})
	}
	switch request.Operation {
	case providerport.OperationInterpretation:
		interpretation := synthesis.NormalizeInterpretation(synthesis.Interpretation{
			SchemaVersion:            synthesis.InterpretationSchemaVersion,
			InterpretationID:         "interpretation.o7.test",
			SessionDigestSHA256:      request.SessionDigestSHA256,
			GeneratedBy:              synthesis.GeneratedBy,
			Objective:                request.InterpretationPayload.Objective,
			ApplicableIntent:         []string{"intent.o7.test"},
			BindingInvariants:        []string{},
			RelevantContracts:        []string{},
			AuthorityBoundaries:      []string{"providers-have-no-admission-authority"},
			KnownFailureModes:        []string{},
			ForbiddenFixes:           []string{"ambient-repository-mutation"},
			RequiredProofObligations: []string{},
			Assumptions:              []string{},
			UnresolvedQuestions:      []string{},
			SourceReferences:         []synthesis.SourceReference{},
			Limitations:              []synthesis.Limitation{},
		})
		digest, err := synthesis.InterpretationDigest(interpretation)
		if err != nil {
			return providerport.Result{}, err
		}
		interpretation.InterpretationDigestSHA256 = digest
		return finishCompletedResult(request, digest, &interpretation, nil)

	case providerport.OperationPlanning:
		plan := synthesis.NormalizePlan(synthesis.Plan{
			SchemaVersion:              synthesis.PlanSchemaVersion,
			PlanID:                     "plan.o7.test",
			InterpretationDigestSHA256: request.PlanningPayload.InterpretationDigestSHA256,
			PlanGeneration:             *request.ExpectedPlanGeneration,
			Steps: []synthesis.PlanStep{{
				StepID:           "step-write-new",
				Description:      "write the governed candidate file",
				IntendedFiles:    []string{"new.txt"},
				IntendedSymbols:  []string{},
				ExpectedEvidence: []string{"candidate-content"},
			}},
			Assumptions:    []string{},
			Risks:          []string{},
			StopConditions: []string{},
			ProviderObservation: synthesis.ProviderObservation{
				ProviderID:   "o7.planner",
				ProviderKind: "deterministic-test",
				ObservedAt:   "2026-08-02T00:00:00Z",
			},
		})
		digest, err := synthesis.PlanDigest(plan)
		if err != nil {
			return providerport.Result{}, err
		}
		plan.PlanDigestSHA256 = digest
		return finishCompletedResult(request, digest, nil, &plan)
	default:
		return providerport.Result{}, nil
	}
}

func finishCompletedResult(request providerport.Request, payloadDigest string, interpretation *synthesis.Interpretation, plan *synthesis.Plan) (providerport.Result, error) {
	result := providerport.NormalizeResult(providerport.Result{
		SchemaVersion:         providerport.ResultSchemaVersion,
		RequestDigestSHA256:   request.RequestDigestSHA256,
		Operation:             request.Operation,
		TerminalOutcome:       providerport.OutcomeCompleted,
		PayloadDigestSHA256:   &payloadDigest,
		InterpretationPayload: interpretation,
		PlanningPayload:       plan,
	})
	return finishProviderResult(result)
}

func finishProviderResult(result providerport.Result) (providerport.Result, error) {
	digest, err := providerport.ResultDigest(result)
	if err != nil {
		return providerport.Result{}, err
	}
	result.ResultDigestSHA256 = digest
	return result, nil
}

type mutationAgent struct{}

func (mutationAgent) Generate(context.Context, agentcommand.GenerationPrompt, providerport.Observer) (agentcommand.MutationPlan, error) {
	plan := agentcommand.NormalizeMutationPlan(agentcommand.MutationPlan{
		SchemaVersion: agentcommand.MutationPlanSchemaVersion,
		Summary:       "write one governed file",
		Operations: []agentcommand.MutationOperation{{
			OperationID: "write-new",
			Kind:        agentcommand.MutationWrite,
			Path:        "new.txt",
			Content:     []byte("generated by O7\n"),
		}},
	})
	digest, err := agentcommand.MutationPlanDigest(plan)
	if err != nil {
		return agentcommand.MutationPlan{}, err
	}
	plan.MutationPlanDigestSHA256 = digest
	return plan, nil
}

func TestRunHappyPathThroughRealOwners(t *testing.T) {
	repoRoot, revision := createO7Repository(t)
	const repositoryDomain = "github.com/example/o7"
	identity := createO7Identity(repositoryDomain, revision)
	identityDigest, err := workspacecontract.IdentityDigest(identity)
	if err != nil {
		t.Fatal(err)
	}
	session := createO7Session(t, repositoryDomain, revision, identityDigest, 1, 1)
	state, err := synthesis.NewSessionState(session)
	if err != nil {
		t.Fatal(err)
	}
	store, err := runnercomposition.NewFSCandidateArtifactStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	generationFactory, err := agentcommand.NewFactory(agentcommand.Config{
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
		PolicyFactory: passingPolicyFactory(t),
		Materializer:  materializer,
		Evaluators: []EvaluatorBinding{{
			EvaluatorID: "o7.candidate-content",
			SurfaceMode: evaluatorcomposition.SurfaceModePlain,
			New:         passingEvaluatorFactory(t),
		}},
		Now: clock,
	}
	result, err := Run(context.Background(), state, Config{
		WorkspaceIdentity:       identity,
		RepositoryRoot:          repoRoot,
		CandidateStore:          store,
		InterpretationProvider:  &cognitiveProvider{operation: providerport.OperationInterpretation},
		InterpretationAuthority: testGoverningInterpretationAuthority(),
		PlanningProvider:        &cognitiveProvider{operation: providerport.OperationPlanning},
		GenerationFactory:       generationFactory,
		EvaluationEngine:        engine,
		InterpretationPolicy: ProviderPolicy{
			DeadlineAt: "2099-01-01T00:00:00Z", MaxObservationCount: 8, MaxObservationBytes: 4096,
		},
		PlanningPolicy: ProviderPolicy{
			DeadlineAt: "2099-01-01T00:00:00Z", MaxObservationCount: 8, MaxObservationBytes: 4096,
		},
		GenerationPolicy: runnercomposition.RequestPolicy{
			DeadlineAt: "2099-01-01T00:00:00Z", MaxObservationCount: 8, MaxObservationBytes: 4096,
		},
		MaxSteps: 10,
		Now:      clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.Disposition != DispositionCandidateReady || result.SessionState.Phase != synthesis.PhaseSucceeded {
		t.Fatalf("result disposition=%q phase=%q detail=%q", result.Receipt.Disposition, result.SessionState.Phase, result.Receipt.Detail)
	}
	if result.Candidate == nil || result.SessionState.Receipt == nil {
		t.Fatal("successful O7 result lacks candidate or O1 receipt")
	}
	if len(result.Trace.ProviderExecutions) != 2 || len(result.Trace.GenerationHandoffs) != 1 || len(result.Trace.EvaluationResults) != 1 {
		t.Fatalf("trace counts: O2=%d O3=%d O4=%d", len(result.Trace.ProviderExecutions), len(result.Trace.GenerationHandoffs), len(result.Trace.EvaluationResults))
	}
	artifact, err := store.Get(context.Background(), result.Candidate.CandidateArtifactDigestSHA256)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, entry := range artifact.Manifest {
		if entry.Path == "new.txt" {
			found = true
			if string(entry.Content) != "generated by O7\n" {
				t.Fatalf("new.txt content = %q", entry.Content)
			}
		}
	}
	if !found {
		t.Fatal("sealed O7 candidate omitted new.txt")
	}
	if err := ValidateRunReceipt(result.Receipt); err != nil {
		t.Fatal(err)
	}
}

func passingPolicyFactory(t *testing.T) EvaluationPolicyFactory {
	t.Helper()
	return EvaluationPolicyFactoryFunc(func(_ context.Context, state synthesis.SessionState, handoff runnercomposition.VerifiedGenerationHandoff) (evaluatorcomposition.EvaluationPolicy, error) {
		candidateDigest := *handoff.RunnerReceipt.CandidateArtifactDigestSHA256
		policy := evaluatorcomposition.EvaluationPolicy{
			SchemaVersion:                 evaluatorcomposition.EvaluationPolicySchemaVersion,
			PolicyID:                      "policy.o7.pass",
			SessionDigestSHA256:           state.Session.SessionDigestSHA256,
			AttemptDigestSHA256:           handoff.Result.GenerationPayload.AttemptDigestSHA256,
			CandidateArtifactDigestSHA256: candidateDigest,
			Evaluators: []evaluatorcomposition.EvaluatorSpec{{
				EvaluatorID: "o7.candidate-content", Required: true,
			}},
			DeadlineAt:                  "2099-01-01T00:00:00Z",
			MaxEvidenceCount:            8,
			MaxEvidenceBytes:            4096,
			RequiredCheckIDs:            []string{"candidate-content"},
			FailureClassRecommendations: []evaluatorcomposition.FailureClassRecommendation{},
		}
		digest, err := evaluatorcomposition.EvaluationPolicyDigest(policy)
		if err != nil {
			return evaluatorcomposition.EvaluationPolicy{}, err
		}
		policy.PolicyDigestSHA256 = digest
		return policy, nil
	})
}

func passingEvaluatorFactory(t *testing.T) EvaluatorFactory {
	t.Helper()
	return func(surface evaluatorcomposition.EvaluatorSurface) (evaluatorcomposition.Evaluator, error) {
		root, err := surface.RootPath()
		if err != nil {
			return nil, err
		}
		descriptor := evaluatorcomposition.EvaluatorDescriptor{
			SchemaVersion:        evaluatorcomposition.EvaluatorDescriptorSchemaVersion,
			EvaluatorID:          "o7.candidate-content",
			EvaluatorKind:        "mechanical",
			EvaluatorVersion:     "v1",
			SupportedCheckIDs:    []string{"candidate-content"},
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
			DescribeFunc: func(context.Context) (evaluatorcomposition.EvaluatorDescriptor, error) {
				return descriptor, nil
			},
			EvaluateFunc: func(_ context.Context, input evaluatorcomposition.EvaluationInput) (evaluatorcomposition.EvaluatorResult, error) {
				content, err := os.ReadFile(filepath.Join(root, "new.txt"))
				if err != nil {
					return evaluatorcomposition.EvaluatorResult{}, err
				}
				status := synthesis.CheckPassed
				failures := []string{}
				if string(content) != "generated by O7\n" {
					status = synthesis.CheckFailed
					failures = []string{string(evaluatorcomposition.FailureClassMechanicalCheckFailure)}
				}
				result := evaluatorcomposition.NormalizeEvaluatorResult(evaluatorcomposition.EvaluatorResult{
					SchemaVersion:                   evaluatorcomposition.EvaluatorResultSchemaVersion,
					EvaluatorID:                     descriptor.EvaluatorID,
					EvaluatorDescriptorDigestSHA256: descriptor.DescriptorDigestSHA256,
					EvaluationInputDigestSHA256:     input.EvaluationInputDigestSHA256,
					TerminalOutcome:                 evaluatorcomposition.EvaluatorOutcomeCompleted,
					Checks: []synthesis.CheckObservation{{
						CheckID: "candidate-content", Status: status, EvidenceReferences: []string{},
					}},
					EvidenceReferences:       []evaluatorcomposition.EvidenceReference{},
					ClassifiedFailureReasons: failures,
					Limitations:              []synthesis.Limitation{},
					CleanupSucceeded:         nil,
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

func createO7Repository(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "sensei@example.invalid")
	runGit(t, root, "config", "user.name", "Sensei Test")
	if err := os.WriteFile(filepath.Join(root, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "base")
	return root, runGit(t, root, "rev-parse", "HEAD")
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func createO7Identity(repositoryDomain, revision string) workspacecontract.Identity {
	return workspacecontract.NormalizeIdentity(workspacecontract.Identity{
		SchemaVersion:          workspacecontract.IdentitySchemaVersion,
		GeneratedBy:            workspacecontract.GeneratedBy,
		CompositionState:       workspacecontract.CompositionComplete,
		RepositoryDomainSource: workspacecontract.RepositoryDomainConfigured,
		Binding: workspacecontract.Binding{
			RepositoryDomain: repositoryDomain,
			Revision:         &revision,
			RevisionStatus:   workspacecontract.RevisionResolved,
		},
		CoverageState: "not_requested",
		TaskIdentity: workspacecontract.TaskIdentity{
			State: workspacecontract.TaskIdentityNotRequested,
		},
	})
}

func createO7Session(t *testing.T, repositoryDomain, revision, identityDigest string, retryBudget, replanBudget int) synthesis.Session {
	t.Helper()
	session := synthesis.NormalizeSession(synthesis.Session{
		SchemaVersion:                 synthesis.SessionSchemaVersion,
		SessionID:                     "session.o7.test",
		GeneratedBy:                   synthesis.GeneratedBy,
		RepositoryDomain:              repositoryDomain,
		BaseRevision:                  revision,
		WorkspaceIdentityDigestSHA256: identityDigest,
		GraphAuthorityDigestSHA256:    testZeroDigest,
		TaskSessionDigestSHA256:       testZeroDigest,
		ClosureDigestSHA256:           testZeroDigest,
		ProofObligationDigests:        []string{},
		Objective:                     "prove O7 through real owners",
		RetryBudget:                   retryBudget,
		ReplanBudget:                  replanBudget,
		CreatedAt:                     "2026-08-02T00:00:00Z",
	})
	digest, err := synthesis.SessionDigest(session)
	if err != nil {
		t.Fatal(err)
	}
	session.SessionDigestSHA256 = digest
	return session
}

func fixedClock() func() time.Time {
	current := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	return func() time.Time {
		current = current.Add(time.Second)
		return current
	}
}
