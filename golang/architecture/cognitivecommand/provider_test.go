// SPDX-License-Identifier: AGPL-3.0-only

package cognitivecommand

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/interpretationclosure"
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

func TestCognitiveProviderPlansFromGroundedInterpretationThroughO2AndO1(t *testing.T) {
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
	planJSON, _ := json.Marshal(planProposal)
	agent := &scriptedStructuredAgent{outputs: [][]byte{planJSON}}
	provider, err := New(Config{
		Agent:               agent,
		ProviderID:          "cognitive.test",
		ProviderKind:        "deterministic-test",
		ModelIdentifier:     "fixture-v1",
		ObservedAt:          "2026-08-02T00:00:00Z",
		SupportedOperations: []providerport.Operation{providerport.OperationPlanning},
	})
	if err != nil {
		t.Fatal(err)
	}

	session := cognitiveSession(t)
	state, err := synthesis.NewSessionState(session)
	if err != nil {
		t.Fatal(err)
	}
	interpretation := groundedInterpretation(t, session)
	state, _, err = synthesis.Transition(state, testCertifiedInterpretationCommand(t, state, interpretation))
	if err != nil {
		t.Fatal(err)
	}
	planningRequest := planningRequest(t, state, interpretation)
	result, _, receipt, err := providerport.Run(context.Background(), provider, planningRequest, sequenceClock())
	if err != nil {
		t.Fatal(err)
	}
	if receipt.TerminalOutcome != providerport.OutcomeCompleted || result.PlanningPayload == nil {
		t.Fatalf("plan result=%#v receipt=%#v", result, receipt)
	}
	plan := result.PlanningPayload
	if plan.InterpretationDigestSHA256 != interpretation.InterpretationDigestSHA256 || plan.PlanGeneration != state.ExpectedPlanGeneration {
		t.Fatal("command changed Go-owned plan binding")
	}
	if plan.ProviderObservation.ProviderID != "cognitive.test" || plan.ProviderObservation.ModelIdentifier != "fixture-v1" {
		t.Fatal("plan provider observation did not come from capability snapshot")
	}
	planCommand, err := providerport.MapToCommand(state, planningRequest, result, "2026-08-02T00:00:04Z")
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

	schema, err := PlanProposalSchemaBytes()
	if err != nil {
		t.Fatal(err)
	}
	if len(agent.prompts) != 1 || !strings.Contains(string(agent.prompts[0]), string(schema)) {
		t.Fatal("planning prompt did not carry the exact embedded proposal schema")
	}
	for _, forbidden := range []string{"repository_root", "candidate-buffer", "/tmp/repository"} {
		if strings.Contains(string(agent.prompts[0]), forbidden) {
			t.Fatalf("cognitive prompt leaked %q", forbidden)
		}
	}
}

func TestCognitiveProviderRefusesInterpretationCapability(t *testing.T) {
	_, err := New(Config{
		Agent:               &scriptedStructuredAgent{},
		ProviderID:          "cognitive.test",
		ObservedAt:          "2026-08-02T00:00:00Z",
		SupportedOperations: []providerport.Operation{providerport.OperationInterpretation},
	})
	if err == nil || !strings.Contains(err.Error(), "planning-only") {
		t.Fatalf("err=%v", err)
	}
}

func TestCognitiveProviderRejectsIdentityOverrideAsTypedInvalidOutput(t *testing.T) {
	payload := `{
		"schema_version":"sensei.cognitivecommand.planproposal.v1",
		"steps":[],
		"assumptions":[],
		"risks":[],
		"stop_conditions":[],
		"plan_generation":999
	}`
	result := executePlanPayload(t, payload)
	if result.TerminalOutcome != providerport.OutcomeInvalidOutput || !strings.Contains(result.Detail, "additionalProperties") || !strings.Contains(result.Detail, "plan_generation") {
		t.Fatalf("result=%#v", result)
	}
}

func TestCognitiveProviderRejectsDuplicateKeysAsTypedInvalidOutput(t *testing.T) {
	payload := `{
		"schema_version":"sensei.cognitivecommand.planproposal.v1",
		"steps":[],
		"assumptions":[],
		"risks":[],
		"risks":["last value must not win"],
		"stop_conditions":[]
	}`
	result := executePlanPayload(t, payload)
	if result.TerminalOutcome != providerport.OutcomeInvalidOutput || !strings.Contains(result.Detail, "duplicate object key") || !strings.Contains(result.Detail, "risks") {
		t.Fatalf("result=%#v", result)
	}
}

func executePlanPayload(t *testing.T, payload string) providerport.Result {
	t.Helper()
	agent := &scriptedStructuredAgent{outputs: [][]byte{[]byte(payload)}}
	provider, err := New(Config{
		Agent: agent, ProviderID: "cognitive.test", ObservedAt: "2026-08-02T00:00:00Z",
		SupportedOperations: []providerport.Operation{providerport.OperationPlanning},
	})
	if err != nil {
		t.Fatal(err)
	}
	session := cognitiveSession(t)
	state, err := synthesis.NewSessionState(session)
	if err != nil {
		t.Fatal(err)
	}
	interpretation := groundedInterpretation(t, session)
	state, _, err = synthesis.Transition(state, testCertifiedInterpretationCommand(t, state, interpretation))
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.Execute(context.Background(), planningRequest(t, state, interpretation), nil)
	if err != nil {
		t.Fatal(err)
	}
	return result
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

func groundedInterpretation(t *testing.T, session synthesis.Session) synthesis.Interpretation {
	t.Helper()
	interpretation := synthesis.NormalizeInterpretation(synthesis.Interpretation{
		SchemaVersion:            synthesis.InterpretationSchemaVersion,
		InterpretationID:         "interpretation.grounded.test",
		SessionDigestSHA256:      session.SessionDigestSHA256,
		GeneratedBy:              synthesis.GeneratedBy,
		Objective:                session.Objective,
		ApplicableIntent:         []string{"intent.test"},
		BindingInvariants:        []string{"invariant.test"},
		RelevantContracts:        []string{"contract.test"},
		AuthorityBoundaries:      []string{"provider-output-is-not-authority"},
		KnownFailureModes:        []string{},
		ForbiddenFixes:           []string{"ambient-repository-read"},
		RequiredProofObligations: []string{"proof.test"},
		Assumptions:              []string{},
		UnresolvedQuestions:      []string{},
		SourceReferences: []synthesis.SourceReference{{
			Reference:          "awareness:intent.test",
			SourceDigestSHA256: strings.Repeat("e", 64),
		}},
		Limitations: []synthesis.Limitation{},
	})
	digest, err := synthesis.InterpretationDigest(interpretation)
	if err != nil {
		t.Fatal(err)
	}
	interpretation.InterpretationDigestSHA256 = digest
	return interpretation
}

func testCertifiedInterpretationCommand(t *testing.T, state synthesis.SessionState, interp synthesis.Interpretation) synthesis.RecordInterpretationCommand {
	t.Helper()
	truth := make([]interpretationclosure.TruthFinding, 0, len(interp.BindingInvariants))
	for _, claimID := range interp.BindingInvariants {
		truth = append(truth, interpretationclosure.UnknownTruth(
			claimID,
			"test",
			"fixture",
			"cognitivecommand fixture exercises neutral contradiction-gate coverage",
			"test-fixture:cognitivecommand-truth-challenge",
		))
	}
	proofs := make([]interpretationclosure.ProofObservation, 0, len(interp.RequiredProofObligations))
	for _, obligationID := range interp.RequiredProofObligations {
		proofs = append(proofs, interpretationclosure.ProofObservation{
			ObligationID:         obligationID,
			RequiredForAuthority: true,
			Status:               interpretationclosure.ProofSatisfied,
			EvidenceReferences:   []string{"test-fixture:cognitivecommand-proof"},
		})
	}
	receipt, err := interpretationclosure.Certify(interpretationclosure.Input{
		InterpretationDigestSHA256: interp.InterpretationDigestSHA256,
		RepositoryRevision:         state.Session.BaseRevision,
		GraphAuthorityDigestSHA256: state.Session.GraphAuthorityDigestSHA256,
		ClosureDigestSHA256:        state.Session.ClosureDigestSHA256,
		TruthFindings:              truth,
		Completeness: interpretationclosure.CompletenessAssessment{
			Status:             interpretationclosure.CompletenessComplete,
			EvidenceReferences: []string{"test-fixture:cognitivecommand-scope"},
		},
		Realization: interpretationclosure.RealizationAssessment{
			Status:             interpretationclosure.RealizationUnknown,
			EvidenceReferences: []string{"test-fixture:cognitivecommand-no-realization-claim"},
		},
		ProofObservations: proofs,
	})
	if err != nil {
		t.Fatalf("interpretationclosure.Certify: %v", err)
	}
	command, err := synthesis.NewRecordInterpretationCommand(state, interp, receipt)
	if err != nil {
		t.Fatalf("synthesis.NewRecordInterpretationCommand: %v", err)
	}
	return command
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
