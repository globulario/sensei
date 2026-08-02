// SPDX-License-Identifier: AGPL-3.0-only

package cognitivecommand

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

type scriptedStructuredAgent struct {
	outputs [][]byte
	prompts [][]byte
}

func (a *scriptedStructuredAgent) Complete(_ context.Context, prompt []byte, _ providerport.Observer) ([]byte, error) {
	a.prompts = append(a.prompts, append([]byte{}, prompt...))
	if len(a.outputs) == 0 {
		return nil, &invalidScriptError{}
	}
	output := append([]byte{}, a.outputs[0]...)
	a.outputs = a.outputs[1:]
	return output, nil
}

type invalidScriptError struct{}

func (*invalidScriptError) Error() string { return "script exhausted" }

func TestCognitiveProviderAdvancesInterpretationAndPlanningThroughO2AndO1(t *testing.T) {
	interpretationProposal := InterpretationProposal{
		SchemaVersion:             InterpretationProposalSchemaVersion,
		ApplicableIntent:          []string{"intent.test"},
		BindingInvariants:         []string{"invariant.test"},
		RelevantContracts:        []string{"contract.test"},
		AuthorityBoundaries:      []string{"provider-output-is-not-authority"},
		KnownFailureModes:        []string{},
		ForbiddenFixes:           []string{"ambient-repository-read"},
		RequiredProofObligations: []string{"proof.test"},
		Assumptions:              []string{},
		UnresolvedQuestions:      []string{"closure content was not resolved"},
		Limitations:              []synthesis.Limitation{},
	}
	planProposal := PlanProposal{
		SchemaVersion: PlanProposalSchemaVersion,
		Steps: []synthesis.PlanStep{{
			StepID:           "step-1",
			Description:      "write the bounded candidate",
			IntendedFiles:    []string{"new.txt"},
			IntendedSymbols:  []string{},
			ExpectedEvidence: []string{"new.txt content"},
		}},
		Assumptions:    []string{},
		Risks:          []string{"candidate may fail evaluation"},
		StopConditions: []string{"stop on governed refusal"},
	}
	interpretationJSON, _ := json.Marshal(interpretationProposal)
	planJSON, _ := json.Marshal(planProposal)
	agent := &scriptedStructuredAgent{outputs: [][]byte{interpretationJSON, planJSON}}
	provider, err := New(Config{
		Agent:               agent,
		ProviderID:          "cognitive.test",
		ProviderKind:        "deterministic-test",
		ModelIdentifier:     "fixture-v1",
		ObservedAt:          "2026-08-02T00:00:00Z",
		SupportedOperations: []providerport.Operation{providerport.OperationInterpretation, providerport.OperationPlanning},
	})
	if err != nil {
		t.Fatal(err)
	}

	session := cognitiveSession(t)
	state, err := synthesis.NewSessionState(session)
	if err != nil {
		t.Fatal(err)
	}
	interpretationRequest := interpretationRequest(t, session)
	clock := sequenceClock()
	result, _, receipt, err := providerport.Run(context.Background(), provider, interpretationRequest, clock)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.TerminalOutcome != providerport.OutcomeCompleted || result.InterpretationPayload == nil {
		t.Fatalf("interpretation result=%#v receipt=%#v", result, receipt)
	}
	interpretation := result.InterpretationPayload
	if interpretation.Objective != session.Objective || interpretation.SessionDigestSHA256 != session.SessionDigestSHA256 {
		t.Fatal("command changed Go-owned interpretation identity")
	}
	if len(interpretation.SourceReferences) != 0 {
		t.Fatal("session-only command invented source references")
	}
	if len(interpretation.Limitations) == 0 || interpretation.Limitations[len(interpretation.Limitations)-1].Reason != sessionOnlyContextReason {
		t.Fatal("mandatory session-only limitation was not recorded")
	}
	command, err := providerport.MapToCommand(state, interpretationRequest, result, "2026-08-02T00:00:02Z")
	if err != nil {
		t.Fatal(err)
	}
	state, _, err = synthesis.Transition(state, command)
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != synthesis.PhasePlanning {
		t.Fatalf("phase=%q", state.Phase)
	}

	planningRequest := planningRequest(t, state, *interpretation)
	planResult, _, planReceipt, err := providerport.Run(context.Background(), provider, planningRequest, clock)
	if err != nil {
		t.Fatal(err)
	}
	if planReceipt.TerminalOutcome != providerport.OutcomeCompleted || planResult.PlanningPayload == nil {
		t.Fatalf("plan result=%#v receipt=%#v", planResult, planReceipt)
	}
	plan := planResult.PlanningPayload
	if plan.InterpretationDigestSHA256 != interpretation.InterpretationDigestSHA256 || plan.PlanGeneration != state.ExpectedPlanGeneration {
		t.Fatal("command changed Go-owned plan binding")
	}
	if plan.ProviderObservation.ProviderID != "cognitive.test" || plan.ProviderObservation.ModelIdentifier != "fixture-v1" {
		t.Fatal("plan provider observation did not come from capability snapshot")
	}
	planCommand, err := providerport.MapToCommand(state, planningRequest, planResult, "2026-08-02T00:00:04Z")
	if err != nil {
		t.Fatal(err)
	}
	state, _, err = synthesis.Transition(state, planCommand)
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != synthesis.PhasePlanned || state.LatestPlanDigestSHA256 != plan.PlanDigestSHA256 {
		t.Fatalf("phase=%q latest_plan=%q", state.Phase, state.LatestPlanDigestSHA256)
	}
	if len(agent.prompts) != 2 || strings.Contains(string(agent.prompts[0]), "repository_root") || strings.Contains(string(agent.prompts[1]), "candidate-buffer") {
		t.Fatal("cognitive prompts leaked filesystem authority")
	}
}

func TestCognitiveProviderRejectsIdentityOverrideAsTypedInvalidOutput(t *testing.T) {
	payload := `{
		"schema_version":"sensei.cognitivecommand.interpretationproposal.v1",
		"applicable_intent":[],
		"binding_invariants":[],
		"relevant_contracts":[],
		"authority_boundaries":[],
		"known_failure_modes":[],
		"forbidden_fixes":[],
		"required_proof_obligations":[],
		"assumptions":[],
		"unresolved_questions":[],
		"limitations":[],
		"objective":"provider override"
	}`
	agent := &scriptedStructuredAgent{outputs: [][]byte{[]byte(payload)}}
	provider, err := New(Config{
		Agent: agent, ProviderID: "cognitive.test", ObservedAt: "2026-08-02T00:00:00Z",
		SupportedOperations: []providerport.Operation{providerport.OperationInterpretation},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.Execute(context.Background(), interpretationRequest(t, cognitiveSession(t)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.TerminalOutcome != providerport.OutcomeInvalidOutput || !strings.Contains(result.Detail, "unknown field") {
		t.Fatalf("result=%#v", result)
	}
}

func cognitiveSession(t *testing.T) synthesis.Session {
	t.Helper()
	session := synthesis.NormalizeSession(synthesis.Session{
		SchemaVersion:                 synthesis.SessionSchemaVersion,
		SessionID:                     "session.cognitive.test",
		GeneratedBy:                   synthesis.GeneratedBy,
		RepositoryDomain:              "github.com/globulario/sensei",
		BaseRevision:                  strings.Repeat("1", 40),
		WorkspaceIdentityDigestSHA256: strings.Repeat("a", 64),
		GraphAuthorityDigestSHA256:    strings.Repeat("b", 64),
		TaskSessionDigestSHA256:       strings.Repeat("c", 64),
		ClosureDigestSHA256:           strings.Repeat("d", 64),
		ProofObligationDigests:        []string{},
		Objective:                     "create new.txt without ambient repository access",
		RetryBudget:                   1,
		ReplanBudget:                  1,
		CreatedAt:                     "2026-08-02T00:00:00Z",
	})
	digest, err := synthesis.SessionDigest(session)
	if err != nil {
		t.Fatal(err)
	}
	session.SessionDigestSHA256 = digest
	return session
}

func interpretationRequest(t *testing.T, session synthesis.Session) providerport.Request {
	t.Helper()
	request := providerport.NormalizeRequest(providerport.Request{
		SchemaVersion:                  providerport.RequestSchemaVersion,
		RequestID:                      "request.cognitive.interpretation",
		Operation:                      providerport.OperationInterpretation,
		SessionDigestSHA256:            session.SessionDigestSHA256,
		RepositoryDomain:               session.RepositoryDomain,
		BaseRevision:                   session.BaseRevision,
		ParentArtifactDigestSHA256:     session.SessionDigestSHA256,
		DeadlineAt:                     "2099-01-01T00:00:00Z",
		MaxObservationCount:            8,
		MaxObservationBytes:            4096,
		InterpretationPayload:          &session,
	})
	digest, err := providerport.RequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	request.RequestDigestSHA256 = digest
	return request
}

func planningRequest(t *testing.T, state synthesis.SessionState, interpretation synthesis.Interpretation) providerport.Request {
	t.Helper()
	generation := state.ExpectedPlanGeneration
	request := providerport.NormalizeRequest(providerport.Request{
		SchemaVersion:              providerport.RequestSchemaVersion,
		RequestID:                  "request.cognitive.planning",
		Operation:                  providerport.OperationPlanning,
		SessionDigestSHA256:        state.Session.SessionDigestSHA256,
		RepositoryDomain:           state.Session.RepositoryDomain,
		BaseRevision:               state.Session.BaseRevision,
		ParentArtifactDigestSHA256: interpretation.InterpretationDigestSHA256,
		ExpectedPlanGeneration:     &generation,
		DeadlineAt:                 "2099-01-01T00:00:00Z",
		MaxObservationCount:        8,
		MaxObservationBytes:        4096,
		PlanningPayload:            &interpretation,
	})
	digest, err := providerport.RequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	request.RequestDigestSHA256 = digest
	return request
}

func sequenceClock() func() time.Time {
	current := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	return func() time.Time {
		current = current.Add(time.Second)
		return current
	}
}
