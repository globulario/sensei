// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

func checkpoint4Fixture(t *testing.T) (runnercomposition.CandidateArtifact, synthesis.SessionState, EvaluationPolicy, string) {
	t.Helper()

	repoRoot, baseRevision := runTestGitRepo(t)
	session, identity := runTestSessionAndIdentity(t, "github.com/example/checkpoint4", baseRevision)
	plan := runTestPlan(t)
	sessionState := runTestSessionState(t, session, plan)

	inputDigest, proposedChangeDigest := precomputeExpectedDigests(t, repoRoot, baseRevision, "new.txt", "sealed candidate content\n")
	attempt := runTestAttempt(t, plan, inputDigest, proposedChangeDigest, synthesis.ProviderStatusCompleted)
	factory := &runTestProviderFactory{buildResult: func(workspace runnercomposition.CandidateWorkspace, request providerport.Request) providerport.Result {
		if err := workspace.WriteCandidate("new.txt", []byte("sealed candidate content\n")); err != nil {
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
		t.Fatalf("fixture did not produce a verified handoff: %q", handoff.RunnerReceipt.Disposition)
	}
	policy := fixturePolicyForHandoff(t, sessionState, handoff)
	checkpoint3, err := Run(context.Background(), sessionState, handoff, policy, store, runFixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint3.Candidate == nil || checkpoint3.SessionState.Phase != synthesis.PhaseEvaluating {
		t.Fatalf("fixture did not reach PhaseEvaluating with a candidate: %+v", checkpoint3)
	}
	return *checkpoint3.Candidate, checkpoint3.SessionState, policy, repoRoot
}

func TestCandidateMaterializerCreatesFreshIsolatedPlainSurfacesAndRevokesBeforeRemoval(t *testing.T) {
	artifact, _, _, repoRoot := checkpoint4Fixture(t)
	materializer, err := NewCandidateMaterializer(artifact.RepositoryDomain, repoRoot)
	if err != nil {
		t.Fatal(err)
	}

	first, err := materializer.Materialize(context.Background(), artifact, "mechanical.go-test", SurfaceModePlain)
	if err != nil {
		t.Fatal(err)
	}
	second, err := materializer.Materialize(context.Background(), artifact, "mechanical.go-test", SurfaceModePlain)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	firstRoot, err := first.RootPath()
	if err != nil {
		t.Fatal(err)
	}
	secondRoot, err := second.RootPath()
	if err != nil {
		t.Fatal(err)
	}
	if firstRoot == secondRoot {
		t.Fatal("fresh evaluator materializations reused one physical directory")
	}
	if first.Ref() != second.Ref() {
		t.Fatalf("same candidate/evaluator/mode should have one logical ref, got %q and %q", first.Ref(), second.Ref())
	}

	want := "sealed candidate content\n"
	for _, root := range []string{firstRoot, secondRoot} {
		got, err := os.ReadFile(filepath.Join(root, "new.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("materialized candidate content = %q, want %q", got, want)
		}
	}
	if err := os.WriteFile(filepath.Join(firstRoot, "new.txt"), []byte("evaluator-local mutation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.ReadFile(filepath.Join(secondRoot, "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(secondBytes) != want {
		t.Fatal("one evaluator surface mutation leaked into another surface")
	}

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := first.RootPath(); !errors.Is(err, ErrEvaluatorSurfaceClosed) {
		t.Fatalf("RootPath after Close error = %v, want ErrEvaluatorSurfaceClosed", err)
	}
	if _, err := os.Stat(firstRoot); !os.IsNotExist(err) {
		t.Fatalf("surface backing directory still exists after Close: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("second Close must be idempotent: %v", err)
	}
}

func TestCandidateMaterializerGitDiffSurfaceUsesPinnedBaseNotDirtyLiveCheckout(t *testing.T) {
	artifact, _, _, repoRoot := checkpoint4Fixture(t)
	if err := os.WriteFile(filepath.Join(repoRoot, "a.txt"), []byte("dirty live checkout\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "ambient-only.txt"), []byte("must never enter evaluator truth\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	materializer, err := NewCandidateMaterializer(artifact.RepositoryDomain, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	surface, err := materializer.Materialize(context.Background(), artifact, "sensei-gate", SurfaceModeGitDiff)
	if err != nil {
		t.Fatal(err)
	}
	defer surface.Close()
	root, err := surface.RootPath()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("git", "-C", root, "diff", "--name-only", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Fields(string(out))
	if len(changed) != 1 || changed[0] != "new.txt" {
		t.Fatalf("sealed diff changed files = %v, want [new.txt]", changed)
	}
	base := exec.Command("git", "-C", root, "show", "HEAD:a.txt")
	baseOut, err := base.Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(baseOut) != "original content\n" {
		t.Fatalf("disposable Git base read dirty checkout bytes: %q", baseOut)
	}
	if _, err := os.Stat(filepath.Join(root, "ambient-only.txt")); !os.IsNotExist(err) {
		t.Fatalf("ambient live-checkout file entered the evaluator surface: %v", err)
	}
}

func TestCandidateMaterializerRejectsWrongDomainAndTamperedLineage(t *testing.T) {
	artifact, _, _, repoRoot := checkpoint4Fixture(t)
	wrongDomain, err := NewCandidateMaterializer("github.com/example/other", repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrongDomain.Materialize(context.Background(), artifact, "mechanical", SurfaceModePlain); err == nil {
		t.Fatal("materializer accepted a candidate from another repository domain")
	}

	materializer, err := NewCandidateMaterializer(artifact.RepositoryDomain, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	wrongInput := artifact
	wrongInput.InputCandidateDigestSHA256 = strings.Repeat("1", 64)
	wrongInput = finishSurfaceArtifact(t, wrongInput)
	if _, err := materializer.Materialize(context.Background(), wrongInput, "mechanical", SurfaceModePlain); err == nil || !strings.Contains(err.Error(), "input_candidate_digest_sha256") {
		t.Fatalf("wrong input lineage rejection = %v", err)
	}

	wrongChange := artifact
	wrongChange.ProposedChangeDigestSHA256 = strings.Repeat("2", 64)
	wrongChange = finishSurfaceArtifact(t, wrongChange)
	if _, err := materializer.Materialize(context.Background(), wrongChange, "mechanical", SurfaceModePlain); err == nil || !strings.Contains(err.Error(), "proposed change digest") {
		t.Fatalf("wrong change lineage rejection = %v", err)
	}
}

func TestCandidateMaterializerRejectsEscapingSymlinkBeforeWritingSurface(t *testing.T) {
	artifact, _, _, repoRoot := checkpoint4Fixture(t)
	target := "../../outside"
	sum := sha256.Sum256([]byte(target))
	artifact.Manifest = append(artifact.Manifest, runnercomposition.CandidateManifestEntry{
		Path:                "links/escape",
		Mode:                runnercomposition.ModeSymlink,
		Content:             []byte{},
		SymlinkTarget:       target,
		ContentDigestSHA256: hex.EncodeToString(sum[:]),
	})
	finalDigest, err := runnercomposition.ManifestDigest(artifact.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	artifact.FinalCandidateContentDigestSHA256 = finalDigest
	artifact = finishSurfaceArtifact(t, artifact)

	materializer, err := NewCandidateMaterializer(artifact.RepositoryDomain, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := materializer.Materialize(context.Background(), artifact, "mechanical", SurfaceModePlain); err == nil || !strings.Contains(err.Error(), "outside the evaluator surface") {
		t.Fatalf("escaping symlink rejection = %v", err)
	}
}

func finishSurfaceArtifact(t *testing.T, artifact runnercomposition.CandidateArtifact) runnercomposition.CandidateArtifact {
	t.Helper()
	artifact = runnercomposition.NormalizeCandidateArtifact(artifact)
	digest, err := runnercomposition.CandidateArtifactDigest(artifact)
	if err != nil {
		t.Fatal(err)
	}
	artifact.CandidateArtifactDigestSHA256 = digest
	if err := runnercomposition.ValidateCandidateArtifact(artifact); err != nil {
		t.Fatalf("test fixture artifact is invalid: %v", err)
	}
	return artifact
}
