// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/synthesis"
	"github.com/globulario/sensei/golang/architecture/workspacecontract"
)

// --- run.go fixture builders: a real git repo, a valid Session/Identity/
// SessionState/Plan chain, and a controllable fake Provider/
// GenerationProviderFactory. ---

func fixedNow() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

func runTestRepo(t *testing.T) (repoRoot, baseRevision string) {
	t.Helper()
	repoRoot = initTestRepo(t, func(root string) {
		if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("original content\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, root, "add", "-A")
		runGit(t, root, "commit", "-q", "-m", "init")
	})
	return repoRoot, runGit(t, repoRoot, "rev-parse", "HEAD")
}

// runTestIdentity builds a minimal, real workspacecontract.Identity bound to
// repositoryDomain/baseRevision -- the canonical workspace owner Run cross-
// checks a session's WorkspaceIdentityDigestSHA256 reference against.
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

// runTestSessionAndIdentity builds a matched (Session, Identity) pair:
// identity is built first, then session's WorkspaceIdentityDigestSHA256 is
// set to identity's own real digest -- exactly the binding Run verifies.
func runTestSessionAndIdentity(t *testing.T, repositoryDomain, baseRevision string) (synthesis.Session, workspacecontract.Identity) {
	t.Helper()
	identity := runTestIdentity(t, repositoryDomain, baseRevision)
	identityDigest, err := workspacecontract.IdentityDigest(identity)
	if err != nil {
		t.Fatal(err)
	}
	s := synthesis.Session{
		SchemaVersion:                 synthesis.SessionSchemaVersion,
		SessionID:                     "session.run-fixture.001",
		GeneratedBy:                   synthesis.GeneratedBy,
		RepositoryDomain:              repositoryDomain,
		BaseRevision:                  baseRevision,
		WorkspaceIdentityDigestSHA256: identityDigest,
		GraphAuthorityDigestSHA256:    zeroDigest,
		TaskSessionDigestSHA256:       zeroDigest,
		ClosureDigestSHA256:           zeroDigest,
		Objective:                     "run.go fixture session",
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

func runTestPlan(t *testing.T, interpretationDigest string) synthesis.Plan {
	t.Helper()
	p := synthesis.Plan{
		SchemaVersion:              synthesis.PlanSchemaVersion,
		PlanID:                     "plan.run-fixture.001",
		InterpretationDigestSHA256: interpretationDigest,
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

// runTestSessionState builds a SessionState in PhaseAttempting whose
// LatestPlanDigestSHA256/PlanGeneration match plan exactly -- the "current
// accepted plan" authority Run verifies plan against -- with the next
// attempt reserved.
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

// fakeProviderFactory hands the workspace to a workspace-aware execute
// closure at construction time (Provider.Execute's own signature has no
// workspace parameter -- a real provider captures it via NewProvider,
// exactly like this fake does).
type fakeProviderFactory struct {
	newProviderErr error
	describeErr    error
	executeErr     error
	buildResult    func(workspace CandidateWorkspace, request providerport.Request) providerport.Result
}

type workspaceBoundProvider struct {
	workspace   CandidateWorkspace
	describeErr error
	executeErr  error
	buildResult func(workspace CandidateWorkspace, request providerport.Request) providerport.Result
}

func (p *workspaceBoundProvider) Describe(ctx context.Context) (providerport.Capabilities, error) {
	if p.describeErr != nil {
		return providerport.Capabilities{}, p.describeErr
	}
	c := providerport.Capabilities{
		SchemaVersion: providerport.CapabilitiesSchemaVersion,
		ProviderObservation: synthesis.ProviderObservation{
			ProviderID: "provider.fake", ProviderKind: "test-double", ObservedAt: "2026-01-01T00:00:00Z",
		},
		SupportedOperations: []providerport.Operation{providerport.OperationGeneration},
	}
	digest, err := providerport.CapabilitiesDigest(c)
	if err != nil {
		return providerport.Capabilities{}, err
	}
	c.CapabilitiesDigestSHA256 = digest
	return providerport.NormalizeCapabilities(c), nil
}

func (p *workspaceBoundProvider) Execute(ctx context.Context, request providerport.Request, obs providerport.Observer) (providerport.Result, error) {
	if p.executeErr != nil {
		return providerport.Result{}, p.executeErr
	}
	return p.buildResult(p.workspace, request), nil
}

func (f *fakeProviderFactory) NewProvider(workspace CandidateWorkspace) (providerport.Provider, error) {
	if f.newProviderErr != nil {
		return nil, f.newProviderErr
	}
	return &workspaceBoundProvider{workspace: workspace, describeErr: f.describeErr, executeErr: f.executeErr, buildResult: f.buildResult}, nil
}

func runTestPolicy() RequestPolicy {
	return RequestPolicy{DeadlineAt: "2099-01-01T00:00:00Z", MaxObservationCount: 100, MaxObservationBytes: 1 << 20}
}

// precomputeExpectedDigests independently derives the exact
// InputCandidateDigestSHA256/ProposedChangeDigestSHA256/manifest a
// successful run over repoRoot@baseRevision, with editFile written into the
// buffer, must produce -- using the SAME real, deterministic functions
// run.go itself calls, on a throwaway snapshot/buffer pair, never through
// Run itself. Since these functions are pure and content-addressed, a real
// run applying the identical edit is guaranteed to compute identical
// digests.
func precomputeExpectedDigests(t *testing.T, repoRoot, baseRevision, editPath, editContent string) (inputDigest, proposedChangeDigest, finalDigest string, finalManifest []CandidateManifestEntry) {
	t.Helper()
	snapshotDir, snapshotManifest, inputDigest, snapshotCleanup, err := ExtractSnapshot(context.Background(), repoRoot, baseRevision)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshotCleanup()
	bufferDir, _, _, bufferCleanup, err := InitializeCandidateBuffer(snapshotDir, snapshotManifest)
	if err != nil {
		t.Fatal(err)
	}
	defer bufferCleanup()
	if err := os.WriteFile(filepath.Join(bufferDir, editPath), []byte(editContent), 0o644); err != nil {
		t.Fatal(err)
	}
	proposedChangeDigest, err = GitChangeDigest(context.Background(), snapshotDir, bufferDir)
	if err != nil {
		t.Fatal(err)
	}
	finalManifest, err = BuildManifest(bufferDir)
	if err != nil {
		t.Fatal(err)
	}
	finalDigest, err = ManifestDigest(finalManifest)
	if err != nil {
		t.Fatal(err)
	}
	return inputDigest, proposedChangeDigest, finalDigest, finalManifest
}

func runTestAttempt(t *testing.T, plan synthesis.Plan, inputDigest, proposedChangeDigest string) synthesis.Attempt {
	t.Helper()
	a := synthesis.Attempt{
		SchemaVersion:              synthesis.AttemptSchemaVersion,
		AttemptID:                  "attempt.run-fixture.001",
		AttemptNumber:              1,
		PlanGeneration:             plan.PlanGeneration,
		PlanDigestSHA256:           plan.PlanDigestSHA256,
		InputCandidateDigestSHA256: inputDigest,
		ProviderObservation:        synthesis.ProviderObservation{ProviderID: "provider.fake", ProviderKind: "test-double", ObservedAt: "2026-01-01T00:00:00Z"},
		ProposedChangeDigestSHA256: proposedChangeDigest,
		TerminalProviderStatus:     synthesis.ProviderStatusCompleted,
		ProducedAt:                 "2026-01-01T00:00:00Z",
	}
	digest, err := synthesis.AttemptDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	a.AttemptDigestSHA256 = digest
	return synthesis.NormalizeAttempt(a)
}

func generationResult(t *testing.T, requestDigest string, attempt synthesis.Attempt) providerport.Result {
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

// unavailableResult builds a providerport.Result with TerminalOutcome
// unavailable, referencing requestDigest, for tests that just need O2 to
// stop early without a real generation payload.
func unavailableResult(t *testing.T, requestDigest string) providerport.Result {
	t.Helper()
	r := providerport.Result{
		SchemaVersion: providerport.ResultSchemaVersion, RequestDigestSHA256: requestDigest,
		Operation: providerport.OperationGeneration, TerminalOutcome: providerport.OutcomeUnavailable, Detail: "stop early",
	}
	digest, err := providerport.ResultDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.ResultDigestSHA256 = digest
	return providerport.NormalizeResult(r)
}

// --- tests ---

func TestRunProducesVerifiedDispositionOnHappyPath(t *testing.T) {
	repoRoot, baseRevision := runTestRepo(t)
	session, identity := runTestSessionAndIdentity(t, "github.com/example/repo", baseRevision)
	plan := runTestPlan(t, zeroDigest)
	sessionState := runTestSessionState(t, session, plan)

	wantInput, wantProposedChange, wantFinal, wantManifest := precomputeExpectedDigests(t, repoRoot, baseRevision, "new.txt", "added by provider\n")
	attempt := runTestAttempt(t, plan, wantInput, wantProposedChange)

	factory := &fakeProviderFactory{buildResult: func(workspace CandidateWorkspace, request providerport.Request) providerport.Result {
		if err := workspace.WriteCandidate("new.txt", []byte("added by provider\n")); err != nil {
			panic(err) // t.Fatal is unsafe here: Provider.Execute runs in a goroutine.
		}
		return generationResult(t, request.RequestDigestSHA256, attempt)
	}}

	storeRoot := t.TempDir()
	store, err := NewFSCandidateArtifactStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := Run(context.Background(), sessionState, identity, repoRoot, plan, factory, store, runTestPolicy(), fixedNow)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if receipt.Disposition != DispositionVerified {
		t.Fatalf("Disposition = %q, want %q (failure_detail: %q)", receipt.Disposition, DispositionVerified, receipt.FailureDetail)
	}
	if err := ValidateRunnerReceipt(receipt); err != nil {
		t.Errorf("Run produced an invalid RunnerReceipt: %v", err)
	}
	if receipt.InputCandidateDigestSHA256 == nil || *receipt.InputCandidateDigestSHA256 != wantInput {
		t.Errorf("InputCandidateDigestSHA256 = %v, want %q", receipt.InputCandidateDigestSHA256, wantInput)
	}
	if receipt.ProposedChangeDigestSHA256 == nil || *receipt.ProposedChangeDigestSHA256 != wantProposedChange {
		t.Errorf("ProposedChangeDigestSHA256 = %v, want %q", receipt.ProposedChangeDigestSHA256, wantProposedChange)
	}
	if receipt.FinalCandidateContentDigestSHA256 == nil || *receipt.FinalCandidateContentDigestSHA256 != wantFinal {
		t.Errorf("FinalCandidateContentDigestSHA256 = %v, want %q", receipt.FinalCandidateContentDigestSHA256, wantFinal)
	}
	if receipt.CleanupSucceeded == nil || !*receipt.CleanupSucceeded {
		t.Errorf("CleanupSucceeded = %v, want true", receipt.CleanupSucceeded)
	}
	if receipt.CandidateArtifactDigestSHA256 == nil {
		t.Fatal("CandidateArtifactDigestSHA256 is nil on a verified receipt")
	}

	// The sealed artifact must be independently retrievable and carry the
	// exact final manifest.
	artifact, err := store.Get(context.Background(), *receipt.CandidateArtifactDigestSHA256)
	if err != nil {
		t.Fatalf("sealed artifact not retrievable: %v", err)
	}
	gotDigest, err := ManifestDigest(artifact.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest, err := ManifestDigest(wantManifest)
	if err != nil {
		t.Fatal(err)
	}
	if gotDigest != wantDigest {
		t.Error("sealed artifact's manifest does not match the independently precomputed final manifest")
	}

	// Buffer non-leakage: the run must never touch the real repository
	// checkout.
	status := runGit(t, repoRoot, "status", "--porcelain")
	if status != "" {
		t.Errorf("real repository checkout was modified by Run: %q", status)
	}
}

func TestRunRejectsWrongPhaseBeforeAnySnapshot(t *testing.T) {
	repoRoot, baseRevision := runTestRepo(t)
	session, identity := runTestSessionAndIdentity(t, "github.com/example/repo", baseRevision)
	plan := runTestPlan(t, zeroDigest)
	sessionState := runTestSessionState(t, session, plan)
	sessionState.Phase = synthesis.PhasePlanning // not PhaseAttempting

	factory := &fakeProviderFactory{buildResult: func(CandidateWorkspace, providerport.Request) providerport.Result {
		panic("provider must never be constructed/invoked for a session in the wrong phase") // t.Fatal is unsafe in a goroutine.
	}}
	storeRoot := t.TempDir()
	store, err := NewFSCandidateArtifactStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Run(context.Background(), sessionState, identity, repoRoot, plan, factory, store, runTestPolicy(), fixedNow)
	if err == nil {
		t.Fatal("expected an error for a session not in PhaseAttempting")
	}

	entries, err := os.ReadDir(storeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a snapshot/artifact appeared despite the phase rejection: %v", entries)
	}
}

// TestRunRejectsIdentityNotMatchingSessionDigest proves the structural
// binding blocker's identity half: an Identity whose own recomputed digest
// does not equal Session.WorkspaceIdentityDigestSHA256 is rejected before
// any snapshot, even though its Binding fields otherwise look plausible.
func TestRunRejectsIdentityNotMatchingSessionDigest(t *testing.T) {
	repoRoot, baseRevision := runTestRepo(t)
	session, _ := runTestSessionAndIdentity(t, "github.com/example/repo", baseRevision)
	plan := runTestPlan(t, zeroDigest)
	sessionState := runTestSessionState(t, session, plan)

	// A DIFFERENT, unrelated identity -- never the one session's digest
	// reference actually points to.
	wrongIdentity := runTestIdentity(t, "github.com/example/unrelated-repo", baseRevision)

	factory := &fakeProviderFactory{buildResult: func(CandidateWorkspace, providerport.Request) providerport.Result {
		panic("provider must never be invoked when identity fails verification") // t.Fatal is unsafe in a goroutine.
	}}
	storeRoot := t.TempDir()
	store, err := NewFSCandidateArtifactStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Run(context.Background(), sessionState, wrongIdentity, repoRoot, plan, factory, store, runTestPolicy(), fixedNow)
	if err == nil {
		t.Fatal("expected an error for an identity not matching session.WorkspaceIdentityDigestSHA256")
	}
}

// TestRunRejectsPlanNotMatchingSessionState proves the structural binding
// blocker's plan half: a Plan that is internally valid but does not match
// sessionState.LatestPlanDigestSHA256 is rejected before any snapshot.
func TestRunRejectsPlanNotMatchingSessionState(t *testing.T) {
	repoRoot, baseRevision := runTestRepo(t)
	session, identity := runTestSessionAndIdentity(t, "github.com/example/repo", baseRevision)
	acceptedPlan := runTestPlan(t, zeroDigest)
	sessionState := runTestSessionState(t, session, acceptedPlan)

	// A DIFFERENT, unrelated (but internally valid) plan -- never the
	// session's currently accepted one.
	unrelatedPlan := runTestPlan(t, sha256Hex([]byte("a different interpretation entirely")))

	factory := &fakeProviderFactory{buildResult: func(CandidateWorkspace, providerport.Request) providerport.Result {
		panic("provider must never be invoked when plan authority fails verification") // t.Fatal is unsafe in a goroutine.
	}}
	storeRoot := t.TempDir()
	store, err := NewFSCandidateArtifactStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Run(context.Background(), sessionState, identity, repoRoot, unrelatedPlan, factory, store, runTestPolicy(), fixedNow)
	if err == nil {
		t.Fatal("expected an error for a plan not matching sessionState.LatestPlanDigestSHA256")
	}
}

func TestRunProducesSnapshotFailureForInvalidBaseRevision(t *testing.T) {
	repoRoot, _ := runTestRepo(t)
	nonexistentRevision := "0000000000000000000000000000000000000000" // well-formed hex, does not exist
	session, identity := runTestSessionAndIdentity(t, "github.com/example/repo", nonexistentRevision)
	plan := runTestPlan(t, zeroDigest)
	sessionState := runTestSessionState(t, session, plan)

	factory := &fakeProviderFactory{buildResult: func(CandidateWorkspace, providerport.Request) providerport.Result {
		panic("provider must never be invoked when the snapshot itself fails") // t.Fatal is unsafe in a goroutine.
	}}
	storeRoot := t.TempDir()
	store, err := NewFSCandidateArtifactStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := Run(context.Background(), sessionState, identity, repoRoot, plan, factory, store, runTestPolicy(), fixedNow)
	if err != nil {
		t.Fatalf("Run returned an error rather than a snapshot-failure receipt: %v", err)
	}
	if receipt.Disposition != DispositionSnapshotFailure {
		t.Fatalf("Disposition = %q, want %q", receipt.Disposition, DispositionSnapshotFailure)
	}
	if err := ValidateRunnerReceipt(receipt); err != nil {
		t.Errorf("invalid RunnerReceipt: %v", err)
	}
	if receipt.CleanupSucceeded != nil {
		t.Errorf("CleanupSucceeded = %v, want nil for snapshot-failure", receipt.CleanupSucceeded)
	}
	if receipt.InputCandidateDigestSHA256 != nil {
		t.Error("InputCandidateDigestSHA256 must be nil for snapshot-failure")
	}
}

func TestRunProducesProviderConstructionFailure(t *testing.T) {
	repoRoot, baseRevision := runTestRepo(t)
	session, identity := runTestSessionAndIdentity(t, "github.com/example/repo", baseRevision)
	plan := runTestPlan(t, zeroDigest)
	sessionState := runTestSessionState(t, session, plan)

	factory := &fakeProviderFactory{newProviderErr: errors.New("simulated factory failure")}
	storeRoot := t.TempDir()
	store, err := NewFSCandidateArtifactStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := Run(context.Background(), sessionState, identity, repoRoot, plan, factory, store, runTestPolicy(), fixedNow)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if receipt.Disposition != DispositionProviderConstructionFailure {
		t.Fatalf("Disposition = %q, want %q", receipt.Disposition, DispositionProviderConstructionFailure)
	}
	if err := ValidateRunnerReceipt(receipt); err != nil {
		t.Errorf("invalid RunnerReceipt: %v", err)
	}
	if receipt.CleanupSucceeded == nil || !*receipt.CleanupSucceeded {
		t.Errorf("CleanupSucceeded = %v, want true (snapshot AND workspace close must have succeeded)", receipt.CleanupSucceeded)
	}
	if receipt.InputCandidateDigestSHA256 == nil {
		t.Error("InputCandidateDigestSHA256 must be present for provider-construction-failure")
	}
}

// TestRunProducesO2NonCompletedWhenExecuteReturnsAnError proves
// providerport.Run's own documented convention: a Go error FROM
// Provider.Execute is local/infrastructure failure that Run itself maps to
// a data-carrying OutcomeUnavailable Result, never propagated as Run's own
// error return -- so this lands on DispositionO2NonCompleted, not
// DispositionO2RunError.
func TestRunProducesO2NonCompletedWhenExecuteReturnsAnError(t *testing.T) {
	repoRoot, baseRevision := runTestRepo(t)
	session, identity := runTestSessionAndIdentity(t, "github.com/example/repo", baseRevision)
	plan := runTestPlan(t, zeroDigest)
	sessionState := runTestSessionState(t, session, plan)

	factory := &fakeProviderFactory{executeErr: errors.New("simulated provider crash")}
	storeRoot := t.TempDir()
	store, err := NewFSCandidateArtifactStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := Run(context.Background(), sessionState, identity, repoRoot, plan, factory, store, runTestPolicy(), fixedNow)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if receipt.Disposition != DispositionO2NonCompleted {
		t.Fatalf("Disposition = %q, want %q", receipt.Disposition, DispositionO2NonCompleted)
	}
	if err := ValidateRunnerReceipt(receipt); err != nil {
		t.Errorf("invalid RunnerReceipt: %v", err)
	}
	if receipt.CleanupSucceeded == nil || !*receipt.CleanupSucceeded {
		t.Errorf("CleanupSucceeded = %v, want true (workspace close must be attempted even on this path)", receipt.CleanupSucceeded)
	}
}

// TestRunProducesO2RunError forces providerport.Run ITSELF to fail (a
// Describe error, which describeBounded propagates as Run's own error --
// distinct from an Execute error, which Run absorbs into OutcomeUnavailable
// data instead, per the test above) -- the one genuine reachable path to
// DispositionO2RunError: "no valid Result/Receipt was constructed at all."
func TestRunProducesO2RunError(t *testing.T) {
	repoRoot, baseRevision := runTestRepo(t)
	session, identity := runTestSessionAndIdentity(t, "github.com/example/repo", baseRevision)
	plan := runTestPlan(t, zeroDigest)
	sessionState := runTestSessionState(t, session, plan)

	factory := &fakeProviderFactory{describeErr: errors.New("simulated describe failure")}
	storeRoot := t.TempDir()
	store, err := NewFSCandidateArtifactStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := Run(context.Background(), sessionState, identity, repoRoot, plan, factory, store, runTestPolicy(), fixedNow)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if receipt.Disposition != DispositionO2RunError {
		t.Fatalf("Disposition = %q, want %q (detail: %q)", receipt.Disposition, DispositionO2RunError, receipt.FailureDetail)
	}
	if err := ValidateRunnerReceipt(receipt); err != nil {
		t.Errorf("invalid RunnerReceipt: %v", err)
	}
}

func TestRunProducesO2NonCompleted(t *testing.T) {
	repoRoot, baseRevision := runTestRepo(t)
	session, identity := runTestSessionAndIdentity(t, "github.com/example/repo", baseRevision)
	plan := runTestPlan(t, zeroDigest)
	sessionState := runTestSessionState(t, session, plan)

	factory := &fakeProviderFactory{buildResult: func(workspace CandidateWorkspace, request providerport.Request) providerport.Result {
		return unavailableResult(t, request.RequestDigestSHA256)
	}}
	storeRoot := t.TempDir()
	store, err := NewFSCandidateArtifactStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := Run(context.Background(), sessionState, identity, repoRoot, plan, factory, store, runTestPolicy(), fixedNow)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if receipt.Disposition != DispositionO2NonCompleted {
		t.Fatalf("Disposition = %q, want %q", receipt.Disposition, DispositionO2NonCompleted)
	}
	if err := ValidateRunnerReceipt(receipt); err != nil {
		t.Errorf("invalid RunnerReceipt: %v", err)
	}
	if receipt.ResultDigestSHA256 == nil || receipt.O2ReceiptDigestSHA256 == nil {
		t.Error("Result/O2Receipt digests must be present for o2-non-completed")
	}
}

// TestRunProducesDigestMismatchWithoutRepairingTheResult proves declared-
// vs-computed comparison, not repair: a provider that declares a wrong
// ProposedChangeDigestSHA256 is rejected byte-for-byte -- the RunnerReceipt
// records the disposition, but Result itself is never altered anywhere.
func TestRunProducesDigestMismatchWithoutRepairingTheResult(t *testing.T) {
	repoRoot, baseRevision := runTestRepo(t)
	session, identity := runTestSessionAndIdentity(t, "github.com/example/repo", baseRevision)
	plan := runTestPlan(t, zeroDigest)
	sessionState := runTestSessionState(t, session, plan)

	wantInput, _, _, _ := precomputeExpectedDigests(t, repoRoot, baseRevision, "new.txt", "added by provider\n")
	// Declares a WRONG proposed-change digest -- never what O3 will
	// actually compute for the real edit made below.
	attempt := runTestAttempt(t, plan, wantInput, sha256Hex([]byte("not the real change")))

	var capturedResult providerport.Result
	factory := &fakeProviderFactory{buildResult: func(workspace CandidateWorkspace, request providerport.Request) providerport.Result {
		if err := workspace.WriteCandidate("new.txt", []byte("added by provider\n")); err != nil {
			panic(err) // t.Fatal is unsafe here: Provider.Execute runs in a goroutine.
		}
		capturedResult = generationResult(t, request.RequestDigestSHA256, attempt)
		return capturedResult
	}}
	storeRoot := t.TempDir()
	store, err := NewFSCandidateArtifactStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := Run(context.Background(), sessionState, identity, repoRoot, plan, factory, store, runTestPolicy(), fixedNow)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if receipt.Disposition != DispositionDigestMismatch {
		t.Fatalf("Disposition = %q, want %q (detail: %q)", receipt.Disposition, DispositionDigestMismatch, receipt.FailureDetail)
	}
	if err := ValidateRunnerReceipt(receipt); err != nil {
		t.Errorf("invalid RunnerReceipt: %v", err)
	}
	// Per the design doc, "the candidate is still sealed, as evidence of
	// what the mismatch actually was" -- digest-mismatch's field-presence
	// requires CandidateArtifact present, same as verified.
	if receipt.CandidateArtifactDigestSHA256 == nil {
		t.Fatal("digest-mismatch must still seal a CandidateArtifact, as evidence of the mismatch")
	}
	if receipt.ProposedChangeDigestSHA256 == nil {
		t.Error("ProposedChangeDigestSHA256 (O3's own computed value) must still be present on digest-mismatch")
	}

	// The sealed artifact must carry O3's OWN computed digest, never the
	// provider's wrong declared one.
	sealed, err := store.Get(context.Background(), *receipt.CandidateArtifactDigestSHA256)
	if err != nil {
		t.Fatalf("sealed mismatch-evidence artifact not retrievable: %v", err)
	}
	if sealed.ProposedChangeDigestSHA256 != *receipt.ProposedChangeDigestSHA256 {
		t.Error("sealed artifact's proposed_change_digest_sha256 does not match O3's own computed value")
	}
	if sealed.ProposedChangeDigestSHA256 == attempt.ProposedChangeDigestSHA256 {
		t.Error("sealed artifact carries the provider's WRONG declared digest instead of O3's own computed one")
	}

	// Result itself was never repaired: the attempt's declared (wrong)
	// digest is untouched.
	if capturedResult.GenerationPayload.ProposedChangeDigestSHA256 != attempt.ProposedChangeDigestSHA256 {
		t.Error("the provider's declared Result was mutated -- O3 must never repair a divergent Result")
	}
}

// TestRunProducesSealFailure forces CandidateArtifactStore.Put to fail
// (read-only store root) after every earlier stage succeeded and matched.
func TestRunProducesSealFailure(t *testing.T) {
	repoRoot, baseRevision := runTestRepo(t)
	session, identity := runTestSessionAndIdentity(t, "github.com/example/repo", baseRevision)
	plan := runTestPlan(t, zeroDigest)
	sessionState := runTestSessionState(t, session, plan)

	wantInput, wantProposedChange, _, _ := precomputeExpectedDigests(t, repoRoot, baseRevision, "new.txt", "added by provider\n")
	attempt := runTestAttempt(t, plan, wantInput, wantProposedChange)
	factory := &fakeProviderFactory{buildResult: func(workspace CandidateWorkspace, request providerport.Request) providerport.Result {
		if err := workspace.WriteCandidate("new.txt", []byte("added by provider\n")); err != nil {
			panic(err) // t.Fatal is unsafe here: Provider.Execute runs in a goroutine.
		}
		return generationResult(t, request.RequestDigestSHA256, attempt)
	}}

	storeRoot := t.TempDir()
	store, err := NewFSCandidateArtifactStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(storeRoot, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(storeRoot, 0o755) })

	receipt, err := Run(context.Background(), sessionState, identity, repoRoot, plan, factory, store, runTestPolicy(), fixedNow)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if receipt.Disposition != DispositionSealFailure {
		t.Fatalf("Disposition = %q, want %q (detail: %q)", receipt.Disposition, DispositionSealFailure, receipt.FailureDetail)
	}
	if err := ValidateRunnerReceipt(receipt); err != nil {
		t.Errorf("invalid RunnerReceipt: %v", err)
	}
	if receipt.FinalCandidateContentDigestSHA256 == nil {
		t.Error("FinalCandidateContentDigestSHA256 must be present for seal-failure")
	}
	if receipt.CandidateArtifactDigestSHA256 != nil {
		t.Error("CandidateArtifactDigestSHA256 must be nil for seal-failure")
	}
}

// TestRunGivesEachAttemptAFreshProvider proves hard law 5: two Run calls
// against the same factory each receive a distinct Provider instance.
func TestRunGivesEachAttemptAFreshProvider(t *testing.T) {
	repoRoot, baseRevision := runTestRepo(t)
	session, identity := runTestSessionAndIdentity(t, "github.com/example/repo", baseRevision)
	plan := runTestPlan(t, zeroDigest)
	sessionState := runTestSessionState(t, session, plan)

	var seen []CandidateWorkspace
	factory := &fakeProviderFactory{buildResult: func(workspace CandidateWorkspace, request providerport.Request) providerport.Result {
		seen = append(seen, workspace)
		return unavailableResult(t, request.RequestDigestSHA256)
	}}
	storeRoot := t.TempDir()
	store, err := NewFSCandidateArtifactStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Run(context.Background(), sessionState, identity, repoRoot, plan, factory, store, runTestPolicy(), fixedNow); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), sessionState, identity, repoRoot, plan, factory, store, runTestPolicy(), fixedNow); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("expected 2 provider invocations, got %d", len(seen))
	}
	if seen[0] == seen[1] {
		t.Error("both attempts observed the identical CandidateWorkspace instance -- providers were not given fresh, independent workspaces")
	}
}

// TestRunStableInputCandidateDigestAcrossAttempts proves hard law 7: two
// attempts in the same plan generation produce the same
// InputCandidateDigestSHA256.
func TestRunStableInputCandidateDigestAcrossAttempts(t *testing.T) {
	repoRoot, baseRevision := runTestRepo(t)
	session, identity := runTestSessionAndIdentity(t, "github.com/example/repo", baseRevision)
	plan := runTestPlan(t, zeroDigest)
	sessionState := runTestSessionState(t, session, plan)

	stopEarly := func(workspace CandidateWorkspace, request providerport.Request) providerport.Result {
		return unavailableResult(t, request.RequestDigestSHA256)
	}
	storeRoot := t.TempDir()
	store, err := NewFSCandidateArtifactStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}

	r1, err := Run(context.Background(), sessionState, identity, repoRoot, plan, &fakeProviderFactory{buildResult: stopEarly}, store, runTestPolicy(), fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	sessionState.ExpectedAttemptNumber = 2
	r2, err := Run(context.Background(), sessionState, identity, repoRoot, plan, &fakeProviderFactory{buildResult: stopEarly}, store, runTestPolicy(), fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if r1.InputCandidateDigestSHA256 == nil || r2.InputCandidateDigestSHA256 == nil || *r1.InputCandidateDigestSHA256 != *r2.InputCandidateDigestSHA256 {
		t.Errorf("InputCandidateDigestSHA256 diverged across attempts in the same plan generation: %v vs %v", r1.InputCandidateDigestSHA256, r2.InputCandidateDigestSHA256)
	}
}

// --- injection-based, deterministic proofs for the three dispositions the
// public API alone cannot cleanly isolate (each requires forcing an
// internal step to fail independently of an EARLIER step that uses the
// same underlying primitive also failing). Overrides the package-level
// indirection vars run.go calls through, always restored via t.Cleanup. ---

func TestRunProducesWorkspaceInitFailureWhenBufferInitFails(t *testing.T) {
	repoRoot, baseRevision := runTestRepo(t)
	session, identity := runTestSessionAndIdentity(t, "github.com/example/repo", baseRevision)
	plan := runTestPlan(t, zeroDigest)
	sessionState := runTestSessionState(t, session, plan)

	orig := initializeCandidateBufferFn
	initializeCandidateBufferFn = func(snapshotDir string, snapshotManifest []CandidateManifestEntry) (string, []CandidateManifestEntry, string, func() error, error) {
		return "", nil, "", nil, errors.New("simulated candidate buffer initialization failure")
	}
	t.Cleanup(func() { initializeCandidateBufferFn = orig })

	factory := &fakeProviderFactory{buildResult: func(CandidateWorkspace, providerport.Request) providerport.Result {
		panic("provider must never be invoked when buffer initialization fails") // t.Fatal is unsafe in a goroutine.
	}}
	storeRoot := t.TempDir()
	store, err := NewFSCandidateArtifactStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := Run(context.Background(), sessionState, identity, repoRoot, plan, factory, store, runTestPolicy(), fixedNow)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if receipt.Disposition != DispositionWorkspaceInitFailure {
		t.Fatalf("Disposition = %q, want %q (detail: %q)", receipt.Disposition, DispositionWorkspaceInitFailure, receipt.FailureDetail)
	}
	if err := ValidateRunnerReceipt(receipt); err != nil {
		t.Errorf("invalid RunnerReceipt: %v", err)
	}
	// The real snapshot WAS created (extractSnapshotFn was not overridden)
	// and must still have been cleaned up despite the later failure.
	if receipt.CleanupSucceeded == nil || !*receipt.CleanupSucceeded {
		t.Errorf("CleanupSucceeded = %v, want true (the real snapshot must still have been cleaned up)", receipt.CleanupSucceeded)
	}
}

// closeFailingWorkspace wraps a real CandidateWorkspace, delegating every
// method except Close, which deterministically fails -- letting a fake
// provider read/write through a fully functional workspace while Run's own
// freeze step is guaranteed to fail.
type closeFailingWorkspace struct {
	CandidateWorkspace
	closeErr error
}

func (w *closeFailingWorkspace) Close() error {
	w.CandidateWorkspace.Close()
	return w.closeErr
}

func TestRunProducesWorkspaceFreezeFailureWhenCloseFails(t *testing.T) {
	repoRoot, baseRevision := runTestRepo(t)
	session, identity := runTestSessionAndIdentity(t, "github.com/example/repo", baseRevision)
	plan := runTestPlan(t, zeroDigest)
	sessionState := runTestSessionState(t, session, plan)

	orig := newCandidateWorkspaceFn
	newCandidateWorkspaceFn = func(snapshotRoot, bufferRoot string) (CandidateWorkspace, error) {
		real, err := orig(snapshotRoot, bufferRoot)
		if err != nil {
			return nil, err
		}
		return &closeFailingWorkspace{CandidateWorkspace: real, closeErr: errors.New("simulated workspace close failure")}, nil
	}
	t.Cleanup(func() { newCandidateWorkspaceFn = orig })

	wantInput, wantProposedChange, _, _ := precomputeExpectedDigests(t, repoRoot, baseRevision, "new.txt", "added by provider\n")
	attempt := runTestAttempt(t, plan, wantInput, wantProposedChange)
	factory := &fakeProviderFactory{buildResult: func(workspace CandidateWorkspace, request providerport.Request) providerport.Result {
		if err := workspace.WriteCandidate("new.txt", []byte("added by provider\n")); err != nil {
			panic(err)
		}
		return generationResult(t, request.RequestDigestSHA256, attempt)
	}}
	storeRoot := t.TempDir()
	store, err := NewFSCandidateArtifactStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := Run(context.Background(), sessionState, identity, repoRoot, plan, factory, store, runTestPolicy(), fixedNow)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if receipt.Disposition != DispositionWorkspaceFreezeFailure {
		t.Fatalf("Disposition = %q, want %q (detail: %q)", receipt.Disposition, DispositionWorkspaceFreezeFailure, receipt.FailureDetail)
	}
	if err := ValidateRunnerReceipt(receipt); err != nil {
		t.Errorf("invalid RunnerReceipt: %v", err)
	}
}

func TestRunProducesEvidenceComputationFailureWhenFinalManifestBuildFails(t *testing.T) {
	repoRoot, baseRevision := runTestRepo(t)
	session, identity := runTestSessionAndIdentity(t, "github.com/example/repo", baseRevision)
	plan := runTestPlan(t, zeroDigest)
	sessionState := runTestSessionState(t, session, plan)

	orig := buildFinalManifestFn
	buildFinalManifestFn = func(root string) ([]CandidateManifestEntry, error) {
		return nil, errors.New("simulated final manifest build failure")
	}
	t.Cleanup(func() { buildFinalManifestFn = orig })

	wantInput, wantProposedChange, _, _ := precomputeExpectedDigests(t, repoRoot, baseRevision, "new.txt", "added by provider\n")
	attempt := runTestAttempt(t, plan, wantInput, wantProposedChange)
	factory := &fakeProviderFactory{buildResult: func(workspace CandidateWorkspace, request providerport.Request) providerport.Result {
		if err := workspace.WriteCandidate("new.txt", []byte("added by provider\n")); err != nil {
			panic(err)
		}
		return generationResult(t, request.RequestDigestSHA256, attempt)
	}}
	storeRoot := t.TempDir()
	store, err := NewFSCandidateArtifactStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := Run(context.Background(), sessionState, identity, repoRoot, plan, factory, store, runTestPolicy(), fixedNow)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if receipt.Disposition != DispositionEvidenceComputationFailure {
		t.Fatalf("Disposition = %q, want %q (detail: %q)", receipt.Disposition, DispositionEvidenceComputationFailure, receipt.FailureDetail)
	}
	if err := ValidateRunnerReceipt(receipt); err != nil {
		t.Errorf("invalid RunnerReceipt: %v", err)
	}
	// The workspace WAS successfully closed before this failure (step 7
	// succeeds; step 8 is where buildFinalManifestFn is invoked) -- cleanup
	// must still reflect that.
	if receipt.CleanupSucceeded == nil || !*receipt.CleanupSucceeded {
		t.Errorf("CleanupSucceeded = %v, want true", receipt.CleanupSucceeded)
	}
}

// closeTrackingWorkspace wraps a real CandidateWorkspace and records whether
// Close was ever called, delegating to the real Close otherwise.
type closeTrackingWorkspace struct {
	CandidateWorkspace
	closed *bool
}

func (w *closeTrackingWorkspace) Close() error {
	*w.closed = true
	return w.CandidateWorkspace.Close()
}

// TestRunClosesWorkspaceOnEveryEarlyFailurePath proves blocker-3's fix
// directly: workspace.Close is actually INVOKED (not merely "cleanup still
// reports success," which would stay true even if Close were never called,
// since the other cleanups would still succeed on their own) on every path
// where a workspace was constructed but the run stops before reaching a
// completed O2 result -- provider-construction-failure, o2-run-error, and
// o2-non-completed.
func TestRunClosesWorkspaceOnEveryEarlyFailurePath(t *testing.T) {
	cases := []struct {
		name    string
		factory func() *fakeProviderFactory
	}{
		{"provider-construction-failure", func() *fakeProviderFactory {
			return &fakeProviderFactory{newProviderErr: errors.New("simulated factory failure")}
		}},
		{"o2-run-error", func() *fakeProviderFactory {
			return &fakeProviderFactory{describeErr: errors.New("simulated describe failure")}
		}},
		{"o2-non-completed", func() *fakeProviderFactory {
			return &fakeProviderFactory{executeErr: errors.New("simulated provider crash")}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repoRoot, baseRevision := runTestRepo(t)
			session, identity := runTestSessionAndIdentity(t, "github.com/example/repo", baseRevision)
			plan := runTestPlan(t, zeroDigest)
			sessionState := runTestSessionState(t, session, plan)

			closed := false
			orig := newCandidateWorkspaceFn
			newCandidateWorkspaceFn = func(snapshotRoot, bufferRoot string) (CandidateWorkspace, error) {
				real, err := orig(snapshotRoot, bufferRoot)
				if err != nil {
					return nil, err
				}
				return &closeTrackingWorkspace{CandidateWorkspace: real, closed: &closed}, nil
			}
			t.Cleanup(func() { newCandidateWorkspaceFn = orig })

			storeRoot := t.TempDir()
			store, err := NewFSCandidateArtifactStore(storeRoot)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := Run(context.Background(), sessionState, identity, repoRoot, plan, c.factory(), store, runTestPolicy(), fixedNow); err != nil {
				t.Fatal(err)
			}
			if !closed {
				t.Error("workspace.Close was never called on this failure path -- a retained provider handle would survive the run")
			}
		})
	}
}
