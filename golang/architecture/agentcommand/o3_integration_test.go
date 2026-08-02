// SPDX-License-Identifier: AGPL-3.0-only

package agentcommand

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
	"github.com/globulario/sensei/golang/architecture/workspacecontract"
)

func TestAgentBridgeProducesO3VerifiedSealedCandidate(t *testing.T) {
	repoRoot, baseRevision := integrationRepo(t)
	const repositoryDomain = "github.com/example/agentcommand-o3"
	identity := integrationIdentity(t, repositoryDomain, baseRevision)
	identityDigest, err := workspacecontract.IdentityDigest(identity)
	if err != nil {
		t.Fatal(err)
	}

	zeroDigest := strings.Repeat("0", 64)
	session := synthesis.NormalizeSession(synthesis.Session{
		SchemaVersion:                 synthesis.SessionSchemaVersion,
		SessionID:                     "session.agentcommand.o3",
		GeneratedBy:                   synthesis.GeneratedBy,
		RepositoryDomain:              repositoryDomain,
		BaseRevision:                  baseRevision,
		WorkspaceIdentityDigestSHA256: identityDigest,
		GraphAuthorityDigestSHA256:    zeroDigest,
		TaskSessionDigestSHA256:       zeroDigest,
		ClosureDigestSHA256:           zeroDigest,
		ProofObligationDigests:        []string{},
		Objective:                     "prove the governed agent bridge through O3",
		RetryBudget:                   1,
		ReplanBudget:                  1,
		CreatedAt:                     "2026-08-02T00:00:00Z",
	})
	sessionDigest, err := synthesis.SessionDigest(session)
	if err != nil {
		t.Fatal(err)
	}
	session.SessionDigestSHA256 = sessionDigest

	plan := synthesis.NormalizePlan(synthesis.Plan{
		SchemaVersion:              synthesis.PlanSchemaVersion,
		PlanID:                     "plan.agentcommand.o3",
		InterpretationDigestSHA256: zeroDigest,
		PlanGeneration:             1,
		Steps: []synthesis.PlanStep{{
			StepID:           "step-1",
			Description:      "create the accepted file",
			IntendedFiles:    []string{"new.txt"},
			IntendedSymbols:  []string{},
			ExpectedEvidence: []string{"new.txt content"},
		}},
		Assumptions:    []string{},
		Risks:          []string{},
		StopConditions: []string{},
		ProviderObservation: synthesis.ProviderObservation{
			ProviderID:   "planner.fixture",
			ProviderKind: "test",
			ObservedAt:   "2026-08-02T00:00:00Z",
		},
	})
	planDigest, err := synthesis.PlanDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanDigestSHA256 = planDigest

	state, err := synthesis.NewSessionState(session)
	if err != nil {
		t.Fatal(err)
	}
	state.Phase = synthesis.PhaseAttempting
	state.PlanGeneration = plan.PlanGeneration
	state.LatestPlanDigestSHA256 = plan.PlanDigestSHA256
	state.ExpectedPlanGeneration = plan.PlanGeneration
	state.ExpectedAttemptNumber = 1

	agent := &fixedAgent{plan: finalizedPlan(t, []MutationOperation{{
		OperationID: "write-new",
		Kind:        MutationWrite,
		Path:        "new.txt",
		Content:     []byte("generated through governed bridge\n"),
	}})}
	factory, err := NewFactory(Config{
		Agent:            agent,
		ProviderID:       "agentcommand.integration",
		ProviderKind:     "test-agent-command",
		ModelIdentifier:  "fixture-v1",
		ObservedAt:       "2026-08-02T00:00:00Z",
		ProducedAt:       "2026-08-02T00:01:00Z",
		MaxSnapshotBytes: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := runnercomposition.NewFSCandidateArtifactStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixedNow := func() time.Time { return time.Date(2026, 8, 2, 0, 2, 0, 0, time.UTC) }
	handoff, err := runnercomposition.Run(
		context.Background(),
		state,
		identity,
		repoRoot,
		plan,
		factory,
		store,
		runnercomposition.RequestPolicy{
			DeadlineAt:          "2099-01-01T00:00:00Z",
			MaxObservationCount: 32,
			MaxObservationBytes: 1 << 20,
		},
		fixedNow,
	)
	if err != nil {
		t.Fatal(err)
	}
	if handoff.RunnerReceipt.Disposition != runnercomposition.DispositionVerified {
		t.Fatalf("disposition = %q, detail = %q", handoff.RunnerReceipt.Disposition, handoff.RunnerReceipt.FailureDetail)
	}
	if handoff.RunnerReceipt.CandidateArtifactDigestSHA256 == nil {
		t.Fatal("verified O3 receipt has no candidate artifact")
	}
	artifact, err := store.Get(context.Background(), *handoff.RunnerReceipt.CandidateArtifactDigestSHA256)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, entry := range artifact.Manifest {
		if entry.Path == "new.txt" {
			found = true
			if string(entry.Content) != "generated through governed bridge\n" {
				t.Fatalf("new.txt content = %q", entry.Content)
			}
		}
	}
	if !found {
		t.Fatal("sealed candidate omitted new.txt")
	}
	if handoff.Result.GenerationPayload == nil ||
		handoff.Result.GenerationPayload.ProposedChangeDigestSHA256 != artifact.ProposedChangeDigestSHA256 ||
		handoff.Result.GenerationPayload.InputCandidateDigestSHA256 != artifact.InputCandidateDigestSHA256 {
		t.Fatal("O3 post-close evidence did not match the bridge's immutable Attempt")
	}
}

func integrationRepo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	gitRun(t, root, "init", "-q")
	gitRun(t, root, "config", "user.email", "sensei@example.invalid")
	gitRun(t, root, "config", "user.name", "Sensei Test")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "base")
	return root, gitRun(t, root, "rev-parse", "HEAD")
}

func gitRun(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	command.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func integrationIdentity(t *testing.T, repositoryDomain, revisionValue string) workspacecontract.Identity {
	t.Helper()
	revision := revisionValue
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
