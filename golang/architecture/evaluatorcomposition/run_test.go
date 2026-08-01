// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
	"github.com/globulario/sensei/golang/architecture/workspacecontract"
)

// --- fixture builders: a real git repo, a valid Session/Identity/
// SessionState/Plan chain, and a controllable fake Provider -- mirroring
// golang/architecture/runnercomposition/run_test.go's own fixtures, since
// producing a genuine VerifiedGenerationHandoff requires actually running
// O3's real, exported Run. ---

func runFixedNow() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("git %v: %v\n%s", args, err, ee.Stderr)
		}
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func runTestGitRepo(t *testing.T) (repoRoot, baseRevision string) {
	t.Helper()
	repoRoot = t.TempDir()
	runGit(t, repoRoot, "init", "-q")
	if err := os.WriteFile(filepath.Join(repoRoot, "a.txt"), []byte("original content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoRoot, "add", "-A")
	runGit(t, repoRoot, "commit", "-q", "-m", "init")
	return repoRoot, runGit(t, repoRoot, "rev-parse", "HEAD")
}

func runTestIdentity(t *testing.T, repositoryDomain, baseRevision string) workspacecontract.Identity {
	t.Helper()
	revision := baseRevision
	id := workspacecontract.Identity{
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
		TaskIdentity:  workspacecontract.TaskIdentity{State: workspacecontract.TaskIdentityNotRequested},
	}
	return workspacecontract.NormalizeIdentity(id)
}

func runTestSessionAndIdentity(t *testing.T, repositoryDomain, baseRevision string) (synthesis.Session, workspacecontract.Identity) {
	t.Helper()
	identity := runTestIdentity(t, repositoryDomain, baseRevision)
	identityDigest, err := workspacecontract.IdentityDigest(identity)
	if err != nil {
		t.Fatal(err)
	}
	s := synthesis.Session{
		SchemaVersion:                 synthesis.SessionSchemaVersion,
		SessionID:                     "session.o4-run-fixture.001",
		GeneratedBy:                   synthesis.GeneratedBy,
		RepositoryDomain:              repositoryDomain,
		BaseRevision:                  baseRevision,
		WorkspaceIdentityDigestSHA256: identityDigest,
		GraphAuthorityDigestSHA256:    zeroDigest,
		TaskSessionDigestSHA256:       zeroDigest,
		ClosureDigestSHA256:           zeroDigest,
		Objective:                     "o4 run.go fixture session",
		RetryBudget:                   3,
		ReplanBudget:                  1,
		CreatedAt:                     "2026-01-01T00:00:00Z",
	}
	digest, err := synthesis.SessionDigest(s)
	if err != nil {
		t.Fatal(err)
	}
	s.SessionDigestSHA256 = digest
	return synthesis.NormalizeSession(s), identity
}

func runTestPlan(t *testing.T) synthesis.Plan {
	t.Helper()
	p := synthesis.Plan{
		SchemaVersion:              synthesis.PlanSchemaVersion,
		PlanID:                     "plan.o4-run-fixture.001",
		InterpretationDigestSHA256: zeroDigest,
		PlanGeneration:             1,
		Steps: []synthesis.PlanStep{
			{StepID: "step.1", Description: "add a file", IntendedFiles: []string{"new.txt"}},
		},
		ProviderObservation: synthesis.ProviderObservation{ProviderID: "provider.fixture", ProviderKind: "test-double", ObservedAt: "2026-01-01T00:00:00Z"},
	}
	digest, err := synthesis.PlanDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	p.PlanDigestSHA256 = digest
	return synthesis.NormalizePlan(p)
}

func runTestSessionState(t *testing.T, session synthesis.Session, plan synthesis.Plan) synthesis.SessionState {
	t.Helper()
	state, err := synthesis.NewSessionState(session)
	if err != nil {
		t.Fatal(err)
	}
	state.Phase = synthesis.PhaseAttempting
	state.PlanGeneration = plan.PlanGeneration
	state.LatestPlanDigestSHA256 = plan.PlanDigestSHA256
	state.ExpectedPlanGeneration = plan.PlanGeneration
	state.ExpectedAttemptNumber = 1
	return state
}

// runTestProviderFactory hands the workspace to a workspace-aware execute
// closure, exactly mirroring runnercomposition's own test fixture.
type runTestProviderFactory struct {
	buildResult func(workspace runnercomposition.CandidateWorkspace, request providerport.Request) providerport.Result
}

type runTestWorkspaceBoundProvider struct {
	workspace   runnercomposition.CandidateWorkspace
	buildResult func(workspace runnercomposition.CandidateWorkspace, request providerport.Request) providerport.Result
}

func (p *runTestWorkspaceBoundProvider) Describe(ctx context.Context) (providerport.Capabilities, error) {
	c := providerport.Capabilities{
		SchemaVersion:       providerport.CapabilitiesSchemaVersion,
		ProviderObservation: synthesis.ProviderObservation{ProviderID: "provider.o4-fake", ProviderKind: "test-double", ObservedAt: "2026-01-01T00:00:00Z"},
		SupportedOperations: []providerport.Operation{providerport.OperationGeneration},
	}
	digest, err := providerport.CapabilitiesDigest(c)
	if err != nil {
		return providerport.Capabilities{}, err
	}
	c.CapabilitiesDigestSHA256 = digest
	return providerport.NormalizeCapabilities(c), nil
}

func (p *runTestWorkspaceBoundProvider) Execute(ctx context.Context, request providerport.Request, obs providerport.Observer) (providerport.Result, error) {
	return p.buildResult(p.workspace, request), nil
}

func (f *runTestProviderFactory) NewProvider(workspace runnercomposition.CandidateWorkspace) (providerport.Provider, error) {
	return &runTestWorkspaceBoundProvider{workspace: workspace, buildResult: f.buildResult}, nil
}

// runTestPolicy is O3's own RequestPolicy fixture, reused verbatim.
func runTestPolicy() runnercomposition.RequestPolicy {
	return runnercomposition.RequestPolicy{DeadlineAt: "2099-01-01T00:00:00Z", MaxObservationCount: 100, MaxObservationBytes: 1 << 20}
}

// precomputeExpectedDigests independently derives the exact
// InputCandidateDigestSHA256/ProposedChangeDigestSHA256 a successful O3 run
// over repoRoot@baseRevision, with editContent written to editPath in the
// buffer, must produce -- using the SAME real, deterministic functions O3's
// Run itself calls, on a throwaway snapshot/buffer pair, never through Run
// itself.
func precomputeExpectedDigests(t *testing.T, repoRoot, baseRevision, editPath, editContent string) (inputDigest, proposedChangeDigest string) {
	t.Helper()
	snapshotDir, snapshotManifest, inputDigest, snapshotCleanup, err := runnercomposition.ExtractSnapshot(context.Background(), repoRoot, baseRevision)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshotCleanup()
	bufferDir, _, _, bufferCleanup, err := runnercomposition.InitializeCandidateBuffer(snapshotDir, snapshotManifest)
	if err != nil {
		t.Fatal(err)
	}
	defer bufferCleanup()
	if err := os.WriteFile(filepath.Join(bufferDir, editPath), []byte(editContent), 0o644); err != nil {
		t.Fatal(err)
	}
	proposedChangeDigest, err = runnercomposition.GitChangeDigest(context.Background(), snapshotDir, bufferDir)
	if err != nil {
		t.Fatal(err)
	}
	return inputDigest, proposedChangeDigest
}

// runTestAttempt builds a schema-valid, digest-consistent synthesis.Attempt
// referencing plan and the precomputed digests, with the given terminal
// provider status.
func runTestAttempt(t *testing.T, plan synthesis.Plan, inputDigest, proposedChangeDigest string, status synthesis.TerminalProviderStatus) synthesis.Attempt {
	t.Helper()
	a := synthesis.Attempt{
		SchemaVersion:              synthesis.AttemptSchemaVersion,
		AttemptID:                  "attempt.o4-run-fixture.001",
		AttemptNumber:              1,
		PlanGeneration:             plan.PlanGeneration,
		PlanDigestSHA256:           plan.PlanDigestSHA256,
		InputCandidateDigestSHA256: inputDigest,
		ProviderObservation:        synthesis.ProviderObservation{ProviderID: "provider.o4-fake", ProviderKind: "test-double", ObservedAt: "2026-01-01T00:00:00Z"},
		ProposedChangeDigestSHA256: proposedChangeDigest,
		TerminalProviderStatus:     status,
		ProducedAt:                 "2026-01-01T00:00:00Z",
	}
	digest, err := synthesis.AttemptDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	a.AttemptDigestSHA256 = digest
	return synthesis.NormalizeAttempt(a)
}

func runTestGenerationResult(t *testing.T, requestDigest string, attempt synthesis.Attempt) providerport.Result {
	t.Helper()
	payloadDigest := attempt.AttemptDigestSHA256
	r := providerport.Result{
		SchemaVersion:       providerport.ResultSchemaVersion,
		RequestDigestSHA256: requestDigest,
		Operation:           providerport.OperationGeneration,
		TerminalOutcome:     providerport.OutcomeCompleted,
		Detail:              "generation complete",
		GenerationPayload:   &attempt,
		PayloadDigestSHA256: &payloadDigest,
	}
	digest, err := providerport.ResultDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.ResultDigestSHA256 = digest
	return providerport.NormalizeResult(r)
}

// verifiedHandoffFixture runs O3's real, exported Run end to end and
// returns the resulting VerifiedGenerationHandoff (which must be
// DispositionVerified), the pre-transition SessionState it was produced
// against, and the store it was sealed into -- everything O4's Run needs.
// status lets a caller build the invalid_output short-circuit fixture.
func verifiedHandoffFixture(t *testing.T, status synthesis.TerminalProviderStatus) (runnercomposition.VerifiedGenerationHandoff, synthesis.SessionState, runnercomposition.CandidateArtifactStore) {
	return verifiedHandoffFixtureForDomain(t, status, "github.com/example/repo")
}

// verifiedHandoffFixtureForDomain is verifiedHandoffFixture parameterized
// by repository domain, so two calls can build genuinely distinct sessions
// (different SessionDigestSHA256/WorkspaceIdentityDigestSHA256) for
// cross-binding-mismatch tests -- two calls with the identical hardcoded
// fixture domain would otherwise produce byte-identical sessions.
func verifiedHandoffFixtureForDomain(t *testing.T, status synthesis.TerminalProviderStatus, repositoryDomain string) (runnercomposition.VerifiedGenerationHandoff, synthesis.SessionState, runnercomposition.CandidateArtifactStore) {
	t.Helper()
	repoRoot, baseRevision := runTestGitRepo(t)
	session, identity := runTestSessionAndIdentity(t, repositoryDomain, baseRevision)
	plan := runTestPlan(t)
	sessionState := runTestSessionState(t, session, plan)

	inputDigest, proposedChangeDigest := precomputeExpectedDigests(t, repoRoot, baseRevision, "new.txt", "added by provider\n")
	attempt := runTestAttempt(t, plan, inputDigest, proposedChangeDigest, status)

	factory := &runTestProviderFactory{buildResult: func(workspace runnercomposition.CandidateWorkspace, request providerport.Request) providerport.Result {
		if err := workspace.WriteCandidate("new.txt", []byte("added by provider\n")); err != nil {
			panic(err) // t.Fatal is unsafe here: Execute runs in a goroutine.
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
		t.Fatalf("O3 Run returned an error building the fixture: %v", err)
	}
	if handoff.RunnerReceipt.Disposition != runnercomposition.DispositionVerified {
		t.Fatalf("O3 fixture Run did not produce a verified disposition: %q (detail: %q)", handoff.RunnerReceipt.Disposition, handoff.RunnerReceipt.FailureDetail)
	}
	return handoff, sessionState, store
}

// fixturePolicyForHandoff builds a valid, digest-consistent EvaluationPolicy
// correctly bound to sessionState/handoff's exact attempt and candidate.
func fixturePolicyForHandoff(t *testing.T, sessionState synthesis.SessionState, handoff runnercomposition.VerifiedGenerationHandoff) EvaluationPolicy {
	t.Helper()
	if handoff.Result.GenerationPayload == nil {
		t.Fatal("fixture handoff has no generation payload")
	}
	attemptDigest, err := synthesis.AttemptDigest(*handoff.Result.GenerationPayload)
	if err != nil {
		t.Fatal(err)
	}
	p := EvaluationPolicy{
		SchemaVersion:                 EvaluationPolicySchemaVersion,
		PolicyID:                      "policy.o4-run-fixture.001",
		SessionDigestSHA256:           sessionState.Session.SessionDigestSHA256,
		AttemptDigestSHA256:           attemptDigest,
		CandidateArtifactDigestSHA256: *handoff.RunnerReceipt.CandidateArtifactDigestSHA256,
		Evaluators:                    []EvaluatorSpec{{EvaluatorID: "mechanical.go-test", Required: true}},
		DeadlineAt:                    "2099-01-01T00:00:00Z",
		MaxEvidenceCount:              100,
		MaxEvidenceBytes:              1 << 20,
		RequiredCheckIDs:              []string{"go-test"},
		FailureClassRecommendations: []FailureClassRecommendation{
			{FailureClass: string(FailureClassMechanicalCheckFailure), Recommendation: synthesis.RecommendRetryGeneration},
		},
	}
	digest, err := EvaluationPolicyDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	p.PolicyDigestSHA256 = digest
	return p
}

// --- tests ---

func TestRunLoadsAndCrossBindsCandidateOnHappyPath(t *testing.T) {
	handoff, sessionState, store := verifiedHandoffFixture(t, synthesis.ProviderStatusCompleted)
	policy := fixturePolicyForHandoff(t, sessionState, handoff)

	result, err := Run(context.Background(), sessionState, handoff, policy, store, runFixedNow)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if result.Receipt != nil {
		t.Fatalf("Receipt is non-nil on the happy path: disposition %q, detail %q", result.Receipt.Disposition, result.Receipt.FailureDetail)
	}
	if result.Candidate == nil {
		t.Fatal("Candidate is nil on the happy path")
	}
	if result.SessionState.Phase != synthesis.PhaseEvaluating {
		t.Errorf("SessionState.Phase = %q, want %q", result.SessionState.Phase, synthesis.PhaseEvaluating)
	}
	if *handoff.RunnerReceipt.CandidateArtifactDigestSHA256 != result.Candidate.CandidateArtifactDigestSHA256 {
		t.Error("returned Candidate's digest does not match the O3 receipt's reference")
	}
	if len(result.Events) == 0 {
		t.Error("expected at least one event from the first transition")
	}
}

func TestRunProducesInvalidOutputTerminatedAfterExactlyOneTransition(t *testing.T) {
	handoff, sessionState, store := verifiedHandoffFixture(t, synthesis.ProviderStatusInvalidOutput)
	policy := fixturePolicyForHandoff(t, sessionState, handoff)

	result, err := Run(context.Background(), sessionState, handoff, policy, store, runFixedNow)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if result.Candidate != nil {
		t.Fatal("Candidate must be nil for invalid-output-terminated -- no candidate load must be attempted")
	}
	if result.Receipt == nil {
		t.Fatal("Receipt is nil")
	}
	if result.Receipt.Disposition != DispositionInvalidOutputTerminated {
		t.Fatalf("Disposition = %q, want %q", result.Receipt.Disposition, DispositionInvalidOutputTerminated)
	}
	if err := ValidateEvaluationReceipt(*result.Receipt); err != nil {
		t.Errorf("invalid EvaluationReceipt: %v", err)
	}
	if result.Receipt.O1TerminalReceiptDigestSHA256 == nil {
		t.Error("O1TerminalReceiptDigestSHA256 must be non-nil for invalid-output-terminated")
	}
	if result.SessionState.Phase != synthesis.PhaseFailed {
		t.Errorf("SessionState.Phase = %q, want %q", result.SessionState.Phase, synthesis.PhaseFailed)
	}
	if result.SessionState.Receipt == nil {
		t.Fatal("O1 SessionState.Receipt is nil after termination")
	}
	if result.SessionState.Receipt.ReceiptDigestSHA256 != *result.Receipt.O1TerminalReceiptDigestSHA256 {
		t.Error("Receipt.O1TerminalReceiptDigestSHA256 does not match the actual O1 terminal receipt digest")
	}
}

func TestRunProducesCandidateLoadFailureWithGovernedSecondTransition(t *testing.T) {
	handoff, sessionState, store := verifiedHandoffFixture(t, synthesis.ProviderStatusCompleted)
	policy := fixturePolicyForHandoff(t, sessionState, handoff)

	// Force a candidate-load failure by pointing the receipt at a digest the
	// store never sealed -- a stand-in for "the exact sealed artifact could
	// not be loaded."
	tamperedHandoff := handoff
	missingDigest := "1111111111111111111111111111111111111111111111111111111111111111"
	tamperedReceipt := handoff.RunnerReceipt
	tamperedReceipt.CandidateArtifactDigestSHA256 = &missingDigest
	// Recompute so the tampered receipt is itself internally digest-valid --
	// the load failure must come from the store, not from handoff validation
	// rejecting a self-inconsistent receipt first.
	tamperedReceipt = runnercomposition.NormalizeRunnerReceipt(tamperedReceipt)
	digest, err := runnercomposition.RunnerReceiptDigest(tamperedReceipt)
	if err != nil {
		t.Fatal(err)
	}
	tamperedReceipt.RunnerReceiptDigestSHA256 = digest
	tamperedHandoff.RunnerReceipt = tamperedReceipt

	policy.CandidateArtifactDigestSHA256 = missingDigest
	policy, err = finishPolicyFixture(t, policy)
	if err != nil {
		t.Fatal(err)
	}

	result, err := Run(context.Background(), sessionState, tamperedHandoff, policy, store, runFixedNow)
	if err != nil {
		t.Fatalf("Run returned an error rather than a candidate-load-failure receipt: %v", err)
	}
	if result.Candidate != nil {
		t.Fatal("Candidate must be nil on candidate-load-failure")
	}
	if result.Receipt == nil {
		t.Fatal("Receipt is nil")
	}
	if result.Receipt.Disposition != DispositionCandidateLoadFailure {
		t.Fatalf("Disposition = %q, want %q (detail: %q)", result.Receipt.Disposition, DispositionCandidateLoadFailure, result.Receipt.FailureDetail)
	}
	if err := ValidateEvaluationReceipt(*result.Receipt); err != nil {
		t.Errorf("invalid EvaluationReceipt: %v", err)
	}
	if result.Receipt.O1TerminalReceiptDigestSHA256 == nil {
		t.Fatal("O1TerminalReceiptDigestSHA256 must be non-nil for candidate-load-failure")
	}
	// Hard law 21: SessionState must NOT be left parked in PhaseEvaluating --
	// EvaluatorUnavailableCommand always terminates.
	if result.SessionState.Phase != synthesis.PhaseFailed {
		t.Errorf("SessionState.Phase = %q, want %q -- SessionState must not be left parked in PhaseEvaluating", result.SessionState.Phase, synthesis.PhaseFailed)
	}
	if result.SessionState.Receipt == nil || result.SessionState.Receipt.TerminalReason != synthesis.ReasonEvaluatorUnavailable {
		t.Error("expected O1 to terminate with ReasonEvaluatorUnavailable")
	}
}

func TestRunProducesCandidateLoadFailureOnCrossBindingMismatch(t *testing.T) {
	// Build a SECOND, unrelated session/attempt/candidate sealed into its
	// own store, copy that sealed artifact into the FIRST session's store,
	// then point the first handoff's receipt at it -- a self-consistent,
	// genuinely loadable artifact that simply does not belong to this
	// session.
	handoffA, sessionStateA, storeA := verifiedHandoffFixture(t, synthesis.ProviderStatusCompleted)
	handoffB, _, storeB := verifiedHandoffFixtureForDomain(t, synthesis.ProviderStatusCompleted, "github.com/example/unrelated-repo")

	digestB := *handoffB.RunnerReceipt.CandidateArtifactDigestSHA256
	artifactB, err := storeB.Get(context.Background(), digestB)
	if err != nil {
		t.Fatal(err)
	}
	if err := storeA.Put(context.Background(), artifactB); err != nil {
		t.Fatal(err)
	}

	tamperedReceipt := handoffA.RunnerReceipt
	tamperedReceipt.CandidateArtifactDigestSHA256 = &digestB
	tamperedReceipt = runnercomposition.NormalizeRunnerReceipt(tamperedReceipt)
	digest, err := runnercomposition.RunnerReceiptDigest(tamperedReceipt)
	if err != nil {
		t.Fatal(err)
	}
	tamperedReceipt.RunnerReceiptDigestSHA256 = digest
	tamperedHandoff := handoffA
	tamperedHandoff.RunnerReceipt = tamperedReceipt

	policy := fixturePolicyForHandoff(t, sessionStateA, handoffA)
	policy.CandidateArtifactDigestSHA256 = digestB
	policy, err = finishPolicyFixture(t, policy)
	if err != nil {
		t.Fatal(err)
	}

	result, err := Run(context.Background(), sessionStateA, tamperedHandoff, policy, storeA, runFixedNow)
	if err != nil {
		t.Fatalf("Run returned an error rather than a candidate-load-failure receipt: %v", err)
	}
	if result.Receipt == nil || result.Receipt.Disposition != DispositionCandidateLoadFailure {
		t.Fatalf("expected DispositionCandidateLoadFailure from a cross-binding mismatch, got %+v", result.Receipt)
	}
	if !strings.Contains(result.Receipt.FailureDetail, "cross-binding") {
		t.Errorf("expected a cross-binding failure detail, got %q", result.Receipt.FailureDetail)
	}
}

// finishPolicyFixture recomputes and sets PolicyDigestSHA256.
func finishPolicyFixture(t *testing.T, p EvaluationPolicy) (EvaluationPolicy, error) {
	t.Helper()
	digest, err := EvaluationPolicyDigest(p)
	if err != nil {
		return EvaluationPolicy{}, err
	}
	p.PolicyDigestSHA256 = digest
	return p, nil
}

// --- generation-handoff seam precondition failures: all Go errors, no
// EvaluationReceipt, no O1 Transition call at all. ---

func TestRunRejectsNonVerifiedDisposition(t *testing.T) {
	handoff, sessionState, store := verifiedHandoffFixture(t, synthesis.ProviderStatusCompleted)
	policy := fixturePolicyForHandoff(t, sessionState, handoff)

	tampered := handoff
	// digest-mismatch shares DispositionVerified's exact field-presence shape
	// (Result/O2Receipt/InputCandidate/ProposedChange/FinalCandidateContent/
	// CandidateArtifact all present) but requires a non-empty FailureDetail
	// -- set that too, so this receipt is fully self-consistent per
	// ValidateRunnerReceipt and the ONLY thing evaluatorcomposition.Run can
	// reject it for is the disposition itself.
	tampered.RunnerReceipt.Disposition = runnercomposition.DispositionDigestMismatch
	tampered.RunnerReceipt.FailureDetail = "simulated digest mismatch for this negative control"
	tampered.RunnerReceipt = runnercomposition.NormalizeRunnerReceipt(tampered.RunnerReceipt)
	digest, err := runnercomposition.RunnerReceiptDigest(tampered.RunnerReceipt)
	if err != nil {
		t.Fatal(err)
	}
	tampered.RunnerReceipt.RunnerReceiptDigestSHA256 = digest
	if err := runnercomposition.ValidateRunnerReceipt(tampered.RunnerReceipt); err != nil {
		t.Fatalf("test fixture bug: tampered RunnerReceipt is not otherwise valid: %v", err)
	}

	if _, err := Run(context.Background(), sessionState, tampered, policy, store, runFixedNow); err == nil {
		t.Fatal("expected an error for a non-verified RunnerReceipt disposition")
	}
}

func TestRunRejectsRunnerReceiptReferencingAnUnrelatedResult(t *testing.T) {
	handoffA, sessionStateA, storeA := verifiedHandoffFixture(t, synthesis.ProviderStatusCompleted)
	handoffB, _, _ := verifiedHandoffFixtureForDomain(t, synthesis.ProviderStatusCompleted, "github.com/example/unrelated-repo-2")
	policy := fixturePolicyForHandoff(t, sessionStateA, handoffA)

	// A self-consistent Result (its own digest is internally correct) but
	// not the one the RunnerReceipt actually references.
	tampered := handoffA
	tampered.Result = handoffB.Result

	if _, err := Run(context.Background(), sessionStateA, tampered, policy, storeA, runFixedNow); err == nil {
		t.Fatal("expected an error when handoff.Result does not match the RunnerReceipt's own result_digest_sha256 reference")
	}
}

func TestRunRejectsTamperedO2Receipt(t *testing.T) {
	handoff, sessionState, store := verifiedHandoffFixture(t, synthesis.ProviderStatusCompleted)
	policy := fixturePolicyForHandoff(t, sessionState, handoff)

	tampered := handoff
	tampered.O2Receipt.ReceiptDigestSHA256 = "1111111111111111111111111111111111111111111111111111111111111111"

	if _, err := Run(context.Background(), sessionState, tampered, policy, store, runFixedNow); err == nil {
		t.Fatal("expected an error for a tampered O2Receipt")
	}
}

func TestRunRejectsInvalidPolicy(t *testing.T) {
	handoff, sessionState, store := verifiedHandoffFixture(t, synthesis.ProviderStatusCompleted)
	policy := fixturePolicyForHandoff(t, sessionState, handoff)
	policy.PolicyDigestSHA256 = "1111111111111111111111111111111111111111111111111111111111111111"

	if _, err := Run(context.Background(), sessionState, handoff, policy, store, runFixedNow); err == nil {
		t.Fatal("expected an error for an invalid policy")
	}
}

func TestRunRejectsPolicyBoundToAnUnrelatedSession(t *testing.T) {
	handoffA, sessionStateA, storeA := verifiedHandoffFixture(t, synthesis.ProviderStatusCompleted)
	handoffB, sessionStateB, _ := verifiedHandoffFixtureForDomain(t, synthesis.ProviderStatusCompleted, "github.com/example/unrelated-repo-3")

	// A policy correctly self-digested and correctly bound to session B --
	// but supplied for session A's Run call.
	policyForB := fixturePolicyForHandoff(t, sessionStateB, handoffB)

	if _, err := Run(context.Background(), sessionStateA, handoffA, policyForB, storeA, runFixedNow); err == nil {
		t.Fatal("expected an error for a policy bound to an unrelated session")
	}
}

func TestRunRejectsPolicyBoundToAnUnrelatedCandidateArtifact(t *testing.T) {
	handoff, sessionState, store := verifiedHandoffFixture(t, synthesis.ProviderStatusCompleted)
	policy := fixturePolicyForHandoff(t, sessionState, handoff)
	policy.CandidateArtifactDigestSHA256 = "1111111111111111111111111111111111111111111111111111111111111111"
	policy, err := finishPolicyFixture(t, policy)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Run(context.Background(), sessionState, handoff, policy, store, runFixedNow); err == nil {
		t.Fatal("expected an error for a policy bound to an unrelated candidate artifact digest")
	}
}

func TestRunRejectsWrongSessionPhase(t *testing.T) {
	handoff, sessionState, store := verifiedHandoffFixture(t, synthesis.ProviderStatusCompleted)
	policy := fixturePolicyForHandoff(t, sessionState, handoff)

	wrongPhase := sessionState
	wrongPhase.Phase = synthesis.PhaseEvaluating // not PhaseAttempting

	if _, err := Run(context.Background(), wrongPhase, handoff, policy, store, runFixedNow); err == nil {
		t.Fatal("expected an error for a session not in PhaseAttempting (MapToCommand must reject)")
	}
}
