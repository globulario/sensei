// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"context"
	"encoding/json"
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
	output := append([]byte{}, a.outputs[0]...)
	a.outputs = a.outputs[1:]
	return output, nil
}

func TestO7CompletesWithO8CognitiveProviders(t *testing.T) {
	interpretationJSON, err := json.Marshal(cognitivecommand.InterpretationProposal{
		SchemaVersion:            cognitivecommand.InterpretationProposalSchemaVersion,
		ApplicableIntent:         []string{"intent.o8.integration"},
		BindingInvariants:        []string{},
		RelevantContracts:        []string{},
		AuthorityBoundaries:      []string{"command-output-is-not-authority"},
		KnownFailureModes:        []string{},
		ForbiddenFixes:           []string{"ambient-repository-discovery"},
		RequiredProofObligations: []string{},
		Assumptions:              []string{},
		UnresolvedQuestions:      []string{"closure content is represented only by digest"},
		Limitations:              []synthesis.Limitation{},
	})
	if err != nil {
		t.Fatal(err)
	}
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
	cognitiveAgent := &cognitiveSequenceAgent{outputs: [][]byte{interpretationJSON, planJSON}}
	cognitiveProvider, err := cognitivecommand.New(cognitivecommand.Config{
		Agent:               cognitiveAgent,
		ProviderID:          "o8.cognitive.integration",
		ProviderKind:        "deterministic-structured-agent",
		ModelIdentifier:     "fixture-v1",
		ObservedAt:          "2026-08-02T00:00:00Z",
		SupportedOperations: []providerport.Operation{providerport.OperationInterpretation, providerport.OperationPlanning},
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
		InterpretationProvider: cognitiveProvider,
		PlanningProvider:       cognitiveProvider,
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
	if len(cognitiveAgent.prompts) != 2 {
		t.Fatalf("cognitive prompt count=%d", len(cognitiveAgent.prompts))
	}
	for _, prompt := range cognitiveAgent.prompts {
		text := string(prompt)
		for _, forbidden := range []string{"repository_root", "candidate-buffer", repoRoot} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("cognitive prompt leaked %q", forbidden)
			}
		}
	}
	if result.Interpretation == nil || len(result.Interpretation.Limitations) == 0 {
		t.Fatal("O8 interpretation omitted the mandatory context limitation")
	}
}
