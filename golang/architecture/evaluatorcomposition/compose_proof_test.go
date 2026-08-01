// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

func validProofDischarge(t *testing.T, status closureprotocol.ReceiptStatus) (closureprotocol.ProofDischarge, []byte) {
	t.Helper()
	discharge := closureprotocol.ProofDischarge{
		ObligationID: "proof-obligation.checkpoint5.001",
		Status:       status,
		SlotResults: []closureprotocol.ProofSlotResult{
			{
				SlotID:     "slot.required-test",
				Status:     closureprotocol.DimensionPass,
				ReceiptIDs: []string{"receipt.checkpoint5.001"},
			},
		},
	}
	digest, err := closureprotocol.ProofDischargeDigest(discharge)
	if err != nil {
		t.Fatal(err)
	}
	discharge.DischargeDigestSHA256 = digest
	if err := closureprotocol.ValidateProofDischarge(discharge); err != nil {
		t.Fatalf("test ProofDischarge invalid: %v", err)
	}
	data, err := json.Marshal(discharge)
	if err != nil {
		t.Fatal(err)
	}
	return discharge, data
}

func proofAwareCheckpoint5Fixture(t *testing.T, proofDigest string) (runnercomposition.VerifiedGenerationHandoff, Result, EvaluationPolicy) {
	t.Helper()
	repoRoot, baseRevision := runTestGitRepo(t)
	session, identity := runTestSessionAndIdentity(t, "github.com/example/checkpoint5-proof", baseRevision)
	session.ProofObligationDigests = []string{proofDigest}
	session = synthesis.NormalizeSession(session)
	sessionDigest, err := synthesis.SessionDigest(session)
	if err != nil {
		t.Fatal(err)
	}
	session.SessionDigestSHA256 = sessionDigest

	plan := runTestPlan(t)
	sessionState := runTestSessionState(t, session, plan)
	inputDigest, proposedChangeDigest := precomputeExpectedDigests(t, repoRoot, baseRevision, "proof.txt", "proof-aware candidate\n")
	attempt := runTestAttempt(t, plan, inputDigest, proposedChangeDigest, synthesis.ProviderStatusCompleted)
	factory := &runTestProviderFactory{buildResult: func(workspace runnercomposition.CandidateWorkspace, request providerport.Request) providerport.Result {
		if err := workspace.WriteCandidate("proof.txt", []byte("proof-aware candidate\n")); err != nil {
			panic(err)
		}
		return runTestGenerationResult(t, request.RequestDigestSHA256, attempt)
	}}
	storeRoot := t.TempDir()
	store, err := runnercomposition.NewFSCandidateArtifactStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := runnercomposition.Run(context.Background(), sessionState, identity, repoRoot, plan, factory, store, runTestPolicy(), runFixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if handoff.RunnerReceipt.Disposition != runnercomposition.DispositionVerified {
		t.Fatalf("proof fixture O3 disposition = %q", handoff.RunnerReceipt.Disposition)
	}
	policy := fixturePolicyForHandoff(t, sessionState, handoff)
	policy.Evaluators = []EvaluatorSpec{{EvaluatorID: "proof.evaluator", Required: true}}
	policy.RequiredCheckIDs = []string{"proof-check"}
	policy.FailureClassRecommendations = []FailureClassRecommendation{
		{FailureClass: FailureClassRequiredCheckUnsatisfied, Recommendation: synthesis.RecommendArchitectReview},
	}
	policy = finishCheckpoint5Policy(t, policy)
	checkpoint, err := Run(context.Background(), sessionState, handoff, policy, store, runFixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Candidate == nil || checkpoint.SessionState.Phase != synthesis.PhaseEvaluating {
		t.Fatalf("proof fixture did not reach PhaseEvaluating: %+v", checkpoint)
	}
	return handoff, checkpoint, policy
}

func proofExecution(t *testing.T, checkpoint Result, policy EvaluationPolicy, reference EvidenceReference) EvaluatorExecution {
	t.Helper()
	execution := checkpoint5Execution(t, checkpoint, policy, "proof.evaluator", EvaluatorOutcomeCompleted,
		[]synthesis.CheckObservation{
			{
				CheckID:            "proof-check",
				Status:             synthesis.CheckPassed,
				Detail:             "closure proof discharged",
				EvidenceReferences: []string{reference.Reference},
			},
		}, nil, nil, true)
	execution.Result.EvidenceReferences = []EvidenceReference{reference}
	execution.Result = NormalizeEvaluatorResult(execution.Result)
	digest, err := EvaluatorResultDigest(execution.Result)
	if err != nil {
		t.Fatal(err)
	}
	execution.Result.ResultDigestSHA256 = digest
	if err := ValidateEvaluatorResult(execution.Result); err != nil {
		t.Fatalf("proof execution result invalid: %v", err)
	}
	return execution
}

func TestComposeEvaluationValidatesExactClosureProofDischargeBytes(t *testing.T) {
	discharge, dischargeBytes := validProofDischarge(t, closureprotocol.ReceiptValid)
	_, checkpoint, policy := proofAwareCheckpoint5Fixture(t, discharge.DischargeDigestSHA256)
	sink := NewMemoryEvidenceSink()
	reference, err := sink.Put(context.Background(), dischargeBytes)
	if err != nil {
		t.Fatal(err)
	}
	execution := proofExecution(t, checkpoint, policy, reference)

	composition := ComposeEvaluation(context.Background(), checkpoint.SessionState, *checkpoint.Candidate, policy,
		[]EvaluatorExecution{execution}, sink)
	if composition.Disposition != DispositionEvaluated || composition.Evaluation == nil {
		t.Fatalf("valid proof composition = %+v", composition)
	}
	if composition.Evaluation.Recommendation != synthesis.RecommendAcceptCandidate {
		t.Fatalf("valid proof recommendation = %q", composition.Evaluation.Recommendation)
	}
}

func TestComposeEvaluationRejectsMissingInvalidOrUncitedProofDischarge(t *testing.T) {
	discharge, dischargeBytes := validProofDischarge(t, closureprotocol.ReceiptValid)
	_, checkpoint, policy := proofAwareCheckpoint5Fixture(t, discharge.DischargeDigestSHA256)

	t.Run("missing", func(t *testing.T) {
		execution := checkpoint5Execution(t, checkpoint, policy, "proof.evaluator", EvaluatorOutcomeCompleted,
			[]synthesis.CheckObservation{checkpoint5Check("proof-check", synthesis.CheckPassed)}, nil, nil, true)
		composition := ComposeEvaluation(context.Background(), checkpoint.SessionState, *checkpoint.Candidate, policy,
			[]EvaluatorExecution{execution}, NewMemoryEvidenceSink())
		if composition.Disposition != DispositionCompositionFailure || !strings.Contains(composition.FailureDetail, "absent") {
			t.Fatalf("missing proof composition = %+v", composition)
		}
	})

	t.Run("uncited", func(t *testing.T) {
		sink := NewMemoryEvidenceSink()
		reference, err := sink.Put(context.Background(), dischargeBytes)
		if err != nil {
			t.Fatal(err)
		}
		execution := checkpoint5Execution(t, checkpoint, policy, "proof.evaluator", EvaluatorOutcomeCompleted,
			[]synthesis.CheckObservation{checkpoint5Check("proof-check", synthesis.CheckPassed)}, nil, nil, true)
		execution.Result.EvidenceReferences = []EvidenceReference{reference}
		execution.Result = NormalizeEvaluatorResult(execution.Result)
		digest, err := EvaluatorResultDigest(execution.Result)
		if err != nil {
			t.Fatal(err)
		}
		execution.Result.ResultDigestSHA256 = digest
		composition := ComposeEvaluation(context.Background(), checkpoint.SessionState, *checkpoint.Candidate, policy,
			[]EvaluatorExecution{execution}, sink)
		if composition.Disposition != DispositionCompositionFailure || !strings.Contains(composition.FailureDetail, "check-cited") {
			t.Fatalf("uncited proof composition = %+v", composition)
		}
	})

	t.Run("invalid status", func(t *testing.T) {
		invalid, invalidBytes := validProofDischarge(t, closureprotocol.ReceiptInvalid)
		_, invalidCheckpoint, invalidPolicy := proofAwareCheckpoint5Fixture(t, invalid.DischargeDigestSHA256)
		sink := NewMemoryEvidenceSink()
		reference, err := sink.Put(context.Background(), invalidBytes)
		if err != nil {
			t.Fatal(err)
		}
		execution := proofExecution(t, invalidCheckpoint, invalidPolicy, reference)
		composition := ComposeEvaluation(context.Background(), invalidCheckpoint.SessionState, *invalidCheckpoint.Candidate, invalidPolicy,
			[]EvaluatorExecution{execution}, sink)
		if composition.Disposition != DispositionCompositionFailure || !strings.Contains(composition.FailureDetail, "not valid") {
			t.Fatalf("invalid proof status composition = %+v", composition)
		}
	})
}
