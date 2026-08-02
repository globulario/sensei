// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/agentcommand"
	"github.com/globulario/sensei/golang/architecture/cognitivecommand"
	"github.com/globulario/sensei/golang/architecture/evaluatorcomposition"
	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
	"github.com/globulario/sensei/golang/architecture/workspacecontract"
)

type cognitiveSequenceAgent struct {
	outputs [][]byte
	prompts [][]byte
}

func (a *cognitiveSequenceAgent) Complete(_ context.Context, prompt []byte, _ providerport.Observer) ([]byte, error) {
	a.prompts = append(a.prompts, append([]byte{}, prompt...))
	if len(a.outputs) == 0 {
		return nil, fmt.Errorf("cognitive sequence exhausted")
	}
	output := append([]byte{}, a.outputs[0]...)
	a.outputs = a.outputs[1:]
	return output, nil
}

type groundedInterpretationProvider struct{}

func (groundedInterpretationProvider) Describe(ctx context.Context) (providerport.Capabilities, error) {
	if err := ctx.Err(); err != nil {
		return providerport.Capabilities{}, err
	}
	capabilities := providerport.Capabilities{
		SchemaVersion: providerport.CapabilitiesSchemaVersion,
		ProviderObservation: synthesis.ProviderObservation{
			ProviderID:      "o8.grounded-interpretation",
			ProviderKind:    "deterministic-grounded-test",
			ModelIdentifier: "fixture-v1",
			ObservedAt:      "2026-08-02T00:00:00Z",
		},
		SupportedOperations: []providerport.Operation{providerport.OperationInterpretation},
	}
	digest, err := providerport.CapabilitiesDigest(capabilities)
	if err != nil {
		return providerport.Capabilities{}, err
	}
	capabilities.CapabilitiesDigestSHA256 = digest
	return capabilities, nil
}

func (groundedInterpretationProvider) Execute(ctx context.Context, request providerport.Request, _ providerport.Observer) (providerport.Result, error) {
	if err := ctx.Err(); err != nil {
		return providerport.Result{}, err
	}
	if request.Operation != providerport.OperationInterpretation || request.InterpretationPayload == nil {
		return providerport.Result{}, fmt.Errorf("grounded interpretation provider received %q", request.Operation)
	}
	session := request.InterpretationPayload
	interpretation := synthesis.NormalizeInterpretation(synthesis.Interpretation{
		SchemaVersion:            synthesis.InterpretationSchemaVersion,
		InterpretationID:         "interpretation.o8.grounded",
		SessionDigestSHA256:      request.SessionDigestSHA256,
		GeneratedBy:              synthesis.GeneratedBy,
		Objective:                session.Objective,
		ApplicableIntent:         []string{"intent.o8.integration"},
		BindingInvariants:        []string{"invariant.o8.grounded_interpretation_required"},
		RelevantContracts:        []string{"contract.o8.planning_only"},
		AuthorityBoundaries:      []string{"command-output-is-not-authority"},
		KnownFailureModes:        []string{"session-only-interpretation-hallucination"},
		ForbiddenFixes:           []string{"ambient-repository-discovery"},
		RequiredProofObligations: []string{"proof.o8.grounded-source-reference"},
		Assumptions:              []string{},
		UnresolvedQuestions:      []string{},
		SourceReferences: []synthesis.SourceReference{{
			Reference:          "awareness:intent.o8.integration",
			SourceDigestSHA256: strings.Repeat("e", 64),
		}},
		Limitations: []synthesis.Limitation{},
	})
	interpretationDigest, err := synthesis.InterpretationDigest(interpretation)
	if err != nil {
		return providerport.Result{}, err
	}
	interpretation.InterpretationDigestSHA256 = interpretationDigest
	result := providerport.NormalizeResult(providerport.Result{
		SchemaVersion:         providerport.ResultSchemaVersion,
		RequestDigestSHA256:   request.RequestDigestSHA256,
		Operation:             request.Operation,
		TerminalOutcome:       providerport.OutcomeCompleted,
		InterpretationPayload: &interpretation,
		PayloadDigestSHA256:   &interpretationDigest,
	})
	resultDigest, err := providerport.ResultDigest(result)
	if err != nil {
		return providerport.Result{}, err
	}
	result.ResultDigestSHA256 = resultDigest
	return result, nil
}

func TestO7CompletesWithGroundedInterpretationAndO8Planning(t *testing.T) {
	planJSON, err := json.Marshal(cognitivecommand.PlanProposal{
		SchemaVersion: cognitivecommand.PlanProposalSchemaVersion,
		Steps: []synthesis.PlanStep{{
			StepID:           "step-new-file",
			Description:      "write the governed candidate file",
			IntendedFiles:    []string{"new.txt"},
			IntendedSymbols:  []string{},
			ExpectedEvidence: []string{"candidate-content"},
		}},
		Assumptions:    []string{},
		Risks:          []string{},
		StopConditions: []string{"stop on governed refusal"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cognitiveAgent := &cognitiveSequenceAgent{outputs: [][]byte{planJSON}}
	planningProvider, err := cognitivecommand.New(cognitivecommand.Config{
		Agent:               cognitiveAgent,
		ProviderID:          "o8.cognitive.planning",
		ProviderKind:        "deterministic-structured-agent",
		ModelIdentifier:     "fixture-v1",
		ObservedAt:          "2026-08-02T00:00:00Z",
		SupportedOperations: []providerport.Operation{providerport.OperationPlanning},
	})
	if err != nil {
		t.Fatal(err)
	}

	repoRoot, revision := createO7Repository(t)
	const repositoryDomain = "github.com/example/o8"
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
		ProviderID:       "o8.generator",
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
	result, err := Run(context.Background(), state, Config{
		WorkspaceIdentity:      identity,
		RepositoryRoot:         repoRoot,
		CandidateStore:         store,
		InterpretationProvider: groundedInterpretationProvider{},
		PlanningProvider:       planningProvider,
		GenerationFactory:      generationFactory,
		EvaluationEngine: &O4Engine{
			Store:         store,
			PolicyFactory: passingPolicyFactory(t),
			Materializer:  materializer,
			Evaluators: []EvaluatorBinding{{
				EvaluatorID: "o7.candidate-content",
				SurfaceMode: evaluatorcomposition.SurfaceModePlain,
				New:         passingEvaluatorFactory(t),
			}},
			Now: clock,
		},
		InterpretationPolicy: ProviderPolicy{DeadlineAt: "2099-01-01T00:00:00Z", MaxObservationCount: 8, MaxObservationBytes: 4096},
		PlanningPolicy:       ProviderPolicy{DeadlineAt: "2099-01-01T00:00:00Z", MaxObservationCount: 8, MaxObservationBytes: 4096},
		GenerationPolicy:     runnercomposition.RequestPolicy{DeadlineAt: "2099-01-01T00:00:00Z", MaxObservationCount: 8, MaxObservationBytes: 4096},
		MaxSteps:             10,
		Now:                  clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.Disposition != DispositionCandidateReady || result.SessionState.Phase != synthesis.PhaseSucceeded {
		t.Fatalf("disposition=%q phase=%q detail=%q", result.Receipt.Disposition, result.SessionState.Phase, result.Receipt.Detail)
	}
	if len(cognitiveAgent.prompts) != 1 {
		t.Fatalf("cognitive prompt count=%d", len(cognitiveAgent.prompts))
	}
	for _, forbidden := range []string{"repository_root", "candidate-buffer", repoRoot} {
		if strings.Contains(string(cognitiveAgent.prompts[0]), forbidden) {
			t.Fatalf("cognitive prompt leaked %q", forbidden)
		}
	}
	if result.Interpretation == nil || len(result.Interpretation.SourceReferences) == 0 {
		t.Fatal("O8 planning did not consume a grounded interpretation")
	}
}
