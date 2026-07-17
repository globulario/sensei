// SPDX-License-Identifier: Apache-2.0

package resultpipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

var e0 = time.Unix(0, 0).UTC()

const e2eDomain = "github.com/globulario/sensei"

func e2eGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func e2eWrite(t *testing.T, repo, rel, content string) {
	t.Helper()
	p := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const e2eInvariants = `invariants:
  - id: test.publish_mutates_state
    title: Publish mutates package identity
    severity: critical
    status: active
    protects:
      files:
        - src/model.go
    required_tests:
      - src/model_test.go:TestPublish
`

func e2eActor() closureprotocol.ActorBinding {
	return closureprotocol.ActorBinding{PrincipalID: "actor.test", ActorKind: closureprotocol.ActorAgent, Roles: []string{"role.repository_repair_agent"}, Issuer: "sensei.local"}
}

func e2eResolution(actorDigest, baseDigest string) closureprotocol.AuthorityResolution {
	r := closureprotocol.AuthorityResolution{
		ActorBindingDigestSHA256:         actorDigest,
		BaseBindingDigestSHA256:          baseDigest,
		ClosureAssessmentDigestSHA256:    "closure0",
		OperationSetDigestSHA256:         "ops0",
		AuthorityPolicyGraphDigestSHA256: "policygraph0",
		PolicyID:                         "admission.strict.v2",
		EvaluatedAt:                      "2026-07-16T12:00:00Z",
		Status:                           closureprotocol.ReceiptValid,
		OperationResults: []closureprotocol.AuthorityResolutionOperation{
			{OperationID: "op.1", Status: closureprotocol.ReceiptValid, SelectedMechanism: closureprotocol.MechanismRepositoryEdit},
		},
	}
	d, err := closureprotocol.AuthorityResolutionDigest(r)
	if err != nil {
		panic(err)
	}
	r.AuthorityResolutionDigestSHA256 = d
	return r
}

// e2eSeed builds a real admitted, scope-verified task ledger over a governed
// repository whose base graph digest is the true recomputed digest, mutating the
// worktree before observation so the observed change matches. It returns the repo
// root and task directory.
func e2eSeed(t *testing.T) (repo, taskDir string) {
	return e2eSeedVariant(t, "package src\n\nfunc Publish() {}\n\nfunc Revoke() {}\n")
}

func e2eSeedVariant(t *testing.T, resultSrc string) (repo, taskDir string) {
	t.Helper()
	repo = t.TempDir()
	e2eGit(t, repo, "init", "-q")
	e2eWrite(t, repo, "docs/awareness/invariants.yaml", e2eInvariants)
	e2eWrite(t, repo, "src/model.go", "package src\n\nfunc Publish() {}\n")
	e2eGit(t, repo, "add", "-A")
	e2eGit(t, repo, "commit", "-q", "-m", "base")
	baseRev := e2eGit(t, repo, "rev-parse", "HEAD")

	baseTree, err := binding.ResolveTreeIdentity(context.Background(), repo, baseRev)
	if err != nil {
		t.Fatal(err)
	}
	// The true base graph digest, recomputed exactly as the pipeline will.
	cg, err := compileGovernedGraph(context.Background(), repo, e2eDomain, nil)
	if err != nil {
		t.Fatalf("compile base graph: %v", err)
	}
	baseGraphDigest := cg.artifact.GraphSemanticDigestSHA256

	base := closureprotocol.BaseBinding{
		Repository: closureprotocol.RepositorySnapshot{Domain: e2eDomain, Revision: baseRev, RevisionStatus: "resolved", TreeDigestSHA256: baseTree.DigestSHA256},
		Graph:      closureprotocol.GraphSnapshot{SchemaVersion: "awareness-ontology/0.2", DigestSHA256: baseGraphDigest, DigestStatus: "resolved"},
		Task:       closureprotocol.TaskBinding{ID: "task.test", SessionID: "session.test"},
		Policies: closureprotocol.PolicyBinding{
			Admission: "admission.strict.v2", Certification: "certification.architectural_closure.v1",
			Completion: "completion.architectural_closure.v1", Revocation: "revocation.architectural_closure.v1",
			Ledger: "ledger.task.v1", Canonicalization: "canon.v1",
		},
	}

	// Mutate the worktree BEFORE observation so the observed change captures it.
	e2eWrite(t, repo, "src/model.go", resultSrc)

	taskDir = t.TempDir()
	validator := func(et closureprotocol.LedgerEventType, mt string, data []byte) error {
		return ledger.ValidateTaskEventPayload(et, data)
	}
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(validator))
	head := func() string {
		h, err := admission.TaskLedgerHead(taskDir)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}

	// task_prepared carries the base binding and the authoritative closure request.
	closureReq := closure.Request{
		SchemaVersion: "1", TaskID: base.Task.ID,
		Binding: binding.ToClaimDocumentBinding(base),
		Scope: closure.Scope{
			Domain: e2eDomain, TaskClass: "repository_repair", RiskClass: closure.RiskArchitectureSensitive,
			AccessMode: closure.AccessWrite, DirectionRequirement: closure.DirectionNotApplicable, Files: []string{"src/model.go"},
		},
	}
	closureBytes, err := closure.MarshalCanonicalRequestYAML(closureReq)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.StoreArtifactBytes(closureBytes, "application/yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(), ledger.AppendRequest{
		TaskID: base.Task.ID, SessionID: base.Task.SessionID, ExpectedHeadDigestSHA256: "",
		EventType: closureprotocol.LedgerEventTaskPrepared,
		Payload: ledger.TaskEventPayload{
			SchemaVersion: ledger.EventPayloadSchemaVersion, EventType: closureprotocol.LedgerEventTaskPrepared,
			TaskID: base.Task.ID, SessionID: base.Task.SessionID, BaseBinding: &base,
			Artifacts: map[string]closureprotocol.LedgerPayloadRef{"closure_request": ref},
		},
		PayloadMediaType: "application/yaml", ProducerID: "test", ProducedAt: e0,
	}); err != nil {
		t.Fatalf("task_prepared: %v", err)
	}

	actor := e2eActor()
	actorDigest := closureprotocol.MustSemanticDigest(actor)
	baseDigest, err := binding.SemanticDigestBase(base)
	if err != nil {
		t.Fatal(err)
	}
	resolution := e2eResolution(actorDigest, baseDigest)
	authorityDigest := resolution.AuthorityResolutionDigestSHA256
	if _, err := admission.RecordAuthorityResolved(store, head(), base.Task, resolution, actor, closureprotocol.ChangePlan{PlanID: "plan.test"}, base, nil, e0); err != nil {
		t.Fatalf("authority: %v", err)
	}

	decision := closureprotocol.AdmissionDecision{
		DecisionID: "decision.test", RequestDigestSHA256: "req0", PolicyID: "admission.strict.v2",
		OperationVerdicts: []closureprotocol.OperationAdmissionVerdict{{OperationID: "op.1", Verdict: "admitted"}},
		CapabilityID:      "cap.test", CompletionPolicyID: "completion.architectural_closure.v1",
	}
	decisionDigest := closureprotocol.MustSemanticDigest(decision)
	if _, err := admission.RecordAdmissionDecided(store, head(), decision, base.Task, e0); err != nil {
		t.Fatalf("decision: %v", err)
	}

	consumption := closureprotocol.CapabilityConsumption{
		CapabilityID: "cap.test", Task: base.Task, ConsumerActor: actor,
		ConsumedOperationIDs: []string{"op.1"}, ConsumedAt: "2026-07-16T12:00:00Z",
		DecisionDigestSHA256: decisionDigest, OneUseStatus: closureprotocol.ReceiptValid,
	}
	if _, err := admission.RecordAdmissionConsumed(store, head(), consumption, e0); err != nil {
		t.Fatalf("consumption: %v", err)
	}

	observed, err := resulttransition.ObserveChange(repo, baseRev, "", actorDigest, authorityDigest)
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if _, err := admission.RecordChangeObserved(store, head(), base.Task, observed, e0); err != nil {
		t.Fatalf("observed: %v", err)
	}
	observedDigest, err := admission.ObservedChangeSetDigest(observed)
	if err != nil {
		t.Fatal(err)
	}

	scope := admission.ScopeVerification{
		CapabilityID: "cap.test", DecisionDigestSHA256: decisionDigest,
		ActorBindingDigestSHA256: actorDigest, AuthorityResolutionDigestSHA256: authorityDigest,
		BaseTreeDigestSHA256: observed.BaseTreeDigestSHA256, ResultTreeDigestSHA256: observed.ResultTreeDigestSHA256,
		ObservedChangeSetDigestSHA256: observedDigest, VerifiedOperationIDs: []string{"op.1"},
		Status: closureprotocol.ReceiptValid, VerifiedAt: "2026-07-16T12:00:00Z",
	}
	sd, err := admission.ScopeVerificationDigest(scope)
	if err != nil {
		t.Fatal(err)
	}
	scope.ScopeVerificationDigestSHA256 = sd
	if _, err := admission.RecordScopeVerified(store, head(), base.Task, scope, e0); err != nil {
		t.Fatalf("scope: %v", err)
	}
	return repo, taskDir
}

func TestBuildEndToEndWorktree(t *testing.T) {
	repo, taskDir := e2eSeed(t)

	statusBefore := e2eGit(t, repo, "status", "--porcelain")
	headBefore := e2eGit(t, repo, "rev-parse", "HEAD")

	res, err := Build(context.Background(), BuildRequest{
		RepositoryRoot: repo, TaskDirectory: taskDir,
		ResultMode: resulttransition.ResultModeWorktree, RepositoryDomain: e2eDomain,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Ten stages, each exactly once, all bound to the one result.
	if len(res.StageArtifacts) != len(closureprotocol.ResultPipelineStages) {
		t.Fatalf("got %d stage artifacts, want %d", len(res.StageArtifacts), len(closureprotocol.ResultPipelineStages))
	}
	seen := map[closureprotocol.ResultPipelineStage]int{}
	for _, a := range res.StageArtifacts {
		seen[a.Stage]++
		if a.Receipt.ResultBindingDigestSHA256 != res.ResultBindingDigestSHA256 {
			t.Fatalf("stage %s not bound to the current result", a.Stage)
		}
	}
	for _, st := range closureprotocol.ResultPipelineStages {
		if seen[st] != 1 {
			t.Fatalf("stage %s appears %d times", st, seen[st])
		}
	}

	// The closure stage used the recorded task request, not a synthesized scope.
	if got := res.ClosureReport.Request.Scope.TaskClass; got != "repository_repair" {
		t.Fatalf("closure used task_class %q, want the recorded repository_repair", got)
	}
	if got := res.ClosureReport.Request.Scope.Files; len(got) != 1 || got[0] != "src/model.go" {
		t.Fatalf("closure scope files = %v, want the recorded [src/model.go]", got)
	}

	// The candidate transition validates against the frozen contract.
	if err := closureprotocol.ValidateResultTransitionReceipt(e2eCandidateReceipt(res)); err != nil {
		t.Fatalf("candidate transition is not contract-valid: %v", err)
	}

	// The repository is untouched: worktree, HEAD, and no result-root leak.
	if got := e2eGit(t, repo, "status", "--porcelain"); got != statusBefore {
		t.Fatalf("worktree status changed: %q -> %q", statusBefore, got)
	}
	if got := e2eGit(t, repo, "rev-parse", "HEAD"); got != headBefore {
		t.Fatalf("HEAD moved: %s -> %s", headBefore, got)
	}
}

// canonicalProjection is a deterministic fingerprint of a build result that
// excludes any materialization path (there should be none anyway).
func canonicalProjection(res BuildResult) string {
	var b strings.Builder
	b.WriteString(res.ResultBinding.ResultTreeDigestSHA256 + "|" + res.ResultBinding.GraphDigestSHA256 + "|" + res.ResultBindingDigestSHA256 + "\n")
	b.WriteString(res.EvaluatedAt + "|" + res.PipelinePolicyID + "\n")
	for _, a := range res.StageArtifacts {
		b.WriteString(string(a.Stage) + "|" + a.Receipt.ReceiptDigestSHA256 + "|" + sha256hex(a.Bytes) + "\n")
	}
	b.WriteString(res.ClosureReport.Verdict + "|" + res.ProofRequirements.ExtractionCompleteness + "|" + res.ProofRequirements.ProvingDisposition + "\n")
	for _, l := range res.Limitations {
		b.WriteString("lim:" + l + "\n")
	}
	return b.String()
}

// §9 repeated execution: Build over the same ledger and result is byte-identical,
// because the evaluation clock comes from the ledger, not time.Now.
func TestBuildDeterministicRepeated(t *testing.T) {
	repo, taskDir := e2eSeed(t)
	req := BuildRequest{RepositoryRoot: repo, TaskDirectory: taskDir, ResultMode: resulttransition.ResultModeWorktree, RepositoryDomain: e2eDomain}
	first, err := Build(context.Background(), req)
	if err != nil {
		t.Fatalf("Build 1: %v", err)
	}
	want := canonicalProjection(first)
	for i := 0; i < 3; i++ {
		got, err := Build(context.Background(), req)
		if err != nil {
			t.Fatalf("Build %d: %v", i+2, err)
		}
		if canonicalProjection(got) != want {
			t.Fatalf("build %d is not byte-identical to the first", i+2)
		}
	}
}

// §9 no temporary paths: no materialization root, temp index, or repository
// absolute path appears in any stage artifact or limitation.
func TestBuildNoTemporaryPaths(t *testing.T) {
	repo, taskDir := e2eSeed(t)
	res, err := Build(context.Background(), BuildRequest{RepositoryRoot: repo, TaskDirectory: taskDir, ResultMode: resulttransition.ResultModeWorktree, RepositoryDomain: e2eDomain})
	if err != nil {
		t.Fatal(err)
	}
	needles := []string{"sensei-p7-root-", "sensei-p7-idx-", repo, taskDir}
	for _, a := range res.StageArtifacts {
		for _, n := range needles {
			if strings.Contains(string(a.Bytes), n) {
				t.Fatalf("stage %s leaks a temporary/absolute path %q", a.Stage, n)
			}
		}
	}
	for _, l := range res.Limitations {
		for _, n := range needles {
			if strings.Contains(l, n) {
				t.Fatalf("limitation leaks a temporary/absolute path %q: %s", n, l)
			}
		}
	}
}

// §8 committed vs worktree: the same tree yields the same result tree and graph
// digest and the same ten stages, regardless of result mode.
func TestBuildCommittedMatchesWorktreeTree(t *testing.T) {
	repo, taskDir := e2eSeed(t)
	wt, err := Build(context.Background(), BuildRequest{RepositoryRoot: repo, TaskDirectory: taskDir, ResultMode: resulttransition.ResultModeWorktree, RepositoryDomain: e2eDomain})
	if err != nil {
		t.Fatalf("worktree build: %v", err)
	}
	// Commit the exact worktree result; the committed tree is identical.
	e2eGit(t, repo, "add", "-A")
	e2eGit(t, repo, "commit", "-q", "-m", "result")
	resultRev := e2eGit(t, repo, "rev-parse", "HEAD")
	committed, err := Build(context.Background(), BuildRequest{RepositoryRoot: repo, TaskDirectory: taskDir, ResultMode: resulttransition.ResultModeRevision, ResultRevision: resultRev, RepositoryDomain: e2eDomain})
	if err != nil {
		t.Fatalf("committed build: %v", err)
	}
	if wt.ResultBinding.ResultTreeDigestSHA256 != committed.ResultBinding.ResultTreeDigestSHA256 {
		t.Fatal("identical trees produced different result tree digests across modes")
	}
	if wt.ResultBinding.GraphDigestSHA256 != committed.ResultBinding.GraphDigestSHA256 {
		t.Fatal("identical trees produced different graph digests across modes")
	}
	if len(committed.StageArtifacts) != len(closureprotocol.ResultPipelineStages) {
		t.Fatalf("committed build produced %d stages", len(committed.StageArtifacts))
	}
	// The committed result additionally carries the result revision; the worktree
	// result does not — an honest difference, not a fabricated one.
	if committed.ResultBinding.ResultRevision != resultRev || wt.ResultBinding.ResultRevision != "" {
		t.Fatalf("result revisions wrong: committed=%q worktree=%q", committed.ResultBinding.ResultRevision, wt.ResultBinding.ResultRevision)
	}
}

// §7 relocated checkout: two independent checkouts with identical Git objects and
// task-ledger bytes at different absolute paths produce canonically identical
// output.
func TestBuildRelocatedCheckout(t *testing.T) {
	repo, taskDir := e2eSeed(t)
	req := BuildRequest{RepositoryRoot: repo, TaskDirectory: taskDir, ResultMode: resulttransition.ResultModeWorktree, RepositoryDomain: e2eDomain}
	first, err := Build(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	base := t.TempDir()
	repo2 := filepath.Join(base, "relocated-repo")
	task2 := filepath.Join(base, "relocated-task")
	if out, err := exec.Command("cp", "-a", repo, repo2).CombinedOutput(); err != nil {
		t.Fatalf("cp repo: %v\n%s", err, out)
	}
	if out, err := exec.Command("cp", "-a", taskDir, task2).CombinedOutput(); err != nil {
		t.Fatalf("cp task: %v\n%s", err, out)
	}
	relocated, err := Build(context.Background(), BuildRequest{RepositoryRoot: repo2, TaskDirectory: task2, ResultMode: resulttransition.ResultModeWorktree, RepositoryDomain: e2eDomain})
	if err != nil {
		t.Fatalf("relocated build: %v", err)
	}
	if canonicalProjection(relocated) != canonicalProjection(first) {
		t.Fatal("relocated checkout produced different canonical output")
	}
}

// §7 parallel execution: concurrent Build over the same read-only repository and
// ledger is race-clean and identical, with no shared mutable state.
func TestBuildParallel(t *testing.T) {
	repo, taskDir := e2eSeed(t)
	req := BuildRequest{RepositoryRoot: repo, TaskDirectory: taskDir, ResultMode: resulttransition.ResultModeWorktree, RepositoryDomain: e2eDomain}
	first, err := Build(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	want := canonicalProjection(first)

	const n = 6
	var wg sync.WaitGroup
	got := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := Build(context.Background(), req)
			if err != nil {
				errs[i] = err
				return
			}
			got[i] = canonicalProjection(res)
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("parallel build %d: %v", i, errs[i])
		}
		if got[i] != want {
			t.Fatalf("parallel build %d diverged", i)
		}
	}
}

// §7 same graph, different tree: two result trees whose governed sources (hence
// architecture graph) are identical but whose code differs must stay
// distinguished by tree identity — a matching graph digest never permits
// cross-tree reuse.
func TestBuildSameGraphDifferentTree(t *testing.T) {
	// e2eSeed mutates src/model.go; a second seed with a DIFFERENT src mutation
	// but identical docs/awareness yields the same graph, a different tree.
	repoA, taskA := e2eSeed(t)
	a, err := Build(context.Background(), BuildRequest{RepositoryRoot: repoA, TaskDirectory: taskA, ResultMode: resulttransition.ResultModeWorktree, RepositoryDomain: e2eDomain})
	if err != nil {
		t.Fatal(err)
	}
	repoB, taskB := e2eSeedVariant(t, "package src\n\nfunc Publish() {}\n\nfunc Delete() {}\n")
	b, err := Build(context.Background(), BuildRequest{RepositoryRoot: repoB, TaskDirectory: taskB, ResultMode: resulttransition.ResultModeWorktree, RepositoryDomain: e2eDomain})
	if err != nil {
		t.Fatal(err)
	}
	if a.ResultBinding.GraphDigestSHA256 != b.ResultBinding.GraphDigestSHA256 {
		t.Skip("governed sources diverged unexpectedly; not a valid same-graph case")
	}
	if a.ResultBinding.ResultTreeDigestSHA256 == b.ResultBinding.ResultTreeDigestSHA256 {
		t.Fatal("different code trees must have different tree digests")
	}
	if a.ResultBindingDigestSHA256 == b.ResultBindingDigestSHA256 {
		t.Fatal("same graph but different tree must NOT share a result binding digest")
	}
	// No stage receipt of A is reusable for B.
	bReceipts := map[string]bool{}
	for _, x := range b.StageArtifacts {
		bReceipts[x.Receipt.ReceiptDigestSHA256] = true
	}
	for _, x := range a.StageArtifacts {
		if bReceipts[x.Receipt.ReceiptDigestSHA256] {
			t.Fatalf("stage %s receipt is reusable across distinct result trees", x.Stage)
		}
	}
}

// §7 base/result swap: a worktree changed after scope_verified no longer matches
// the observed change and is refused.
func TestBuildRejectsPostScopeVerificationMutation(t *testing.T) {
	repo, taskDir := e2eSeed(t)
	e2eWrite(t, repo, "src/model.go", "package src\n\nfunc Publish() {}\n\nfunc Revoke() {}\n\nfunc Sneak() {}\n")
	_, err := Build(context.Background(), BuildRequest{RepositoryRoot: repo, TaskDirectory: taskDir, ResultMode: resulttransition.ResultModeWorktree, RepositoryDomain: e2eDomain})
	if err == nil || !strings.Contains(err.Error(), "observed_change_mismatch") {
		t.Fatalf("expected observed_change_mismatch, got %v", err)
	}
}

// §8 structural omission over the ACTUAL live build: removing any one of the ten
// stage receipts + derivations fails the frozen validator.
func TestBuildOmissionStructural(t *testing.T) {
	repo, taskDir := e2eSeed(t)
	res, err := Build(context.Background(), BuildRequest{RepositoryRoot: repo, TaskDirectory: taskDir, ResultMode: resulttransition.ResultModeWorktree, RepositoryDomain: e2eDomain})
	if err != nil {
		t.Fatal(err)
	}
	full := e2eCandidateReceipt(res)
	if err := closureprotocol.ValidateResultTransitionReceipt(full); err != nil {
		t.Fatalf("full transition should validate: %v", err)
	}
	for i, dropped := range res.StageArtifacts {
		c := full
		c.OperationalArtifactReceipts = dropReceipt(full.OperationalArtifactReceipts, i)
		c.Derivations = dropDerivation(full.Derivations, i)
		if err := closureprotocol.ValidateResultTransitionReceipt(c); err == nil {
			t.Fatalf("dropping stage %s still validated", dropped.Stage)
		}
	}
}

func dropReceipt(in []closureprotocol.ArtifactReceipt, i int) []closureprotocol.ArtifactReceipt {
	out := append([]closureprotocol.ArtifactReceipt{}, in[:i]...)
	return append(out, in[i+1:]...)
}

func dropDerivation(in []closureprotocol.ArtifactDerivation, i int) []closureprotocol.ArtifactDerivation {
	out := append([]closureprotocol.ArtifactDerivation{}, in[:i]...)
	return append(out, in[i+1:]...)
}

func e2eCandidateReceipt(res BuildResult) closureprotocol.ResultTransitionReceipt {
	var receipts []closureprotocol.ArtifactReceipt
	var derivations []closureprotocol.ArtifactDerivation
	for _, a := range res.StageArtifacts {
		receipts = append(receipts, a.Receipt)
		derivations = append(derivations, a.Derivation)
	}
	b := res.BoundRepositoryResult
	return closureprotocol.ResultTransitionReceipt{
		Task:                              b.Task,
		BaseBindingDigestSHA256:           b.BaseBindingDigestSHA256,
		ActorBindingDigestSHA256:          b.ActorBindingDigestSHA256,
		AuthorityResolutionDigestSHA256:   b.AuthorityResolutionDigestSHA256,
		AdmissionDecisionDigestSHA256:     b.AdmissionDecisionDigestSHA256,
		CapabilityConsumptionDigestSHA256: b.CapabilityConsumptionDigestSHA256,
		ObservedChangeSetDigestSHA256:     b.ObservedChangeSetDigestSHA256,
		ScopeVerificationDigestSHA256:     b.ScopeVerificationDigestSHA256,
		ResultBinding:                     res.ResultBinding,
		ResultBindingDigestSHA256:         res.ResultBindingDigestSHA256,
		OperationalArtifactReceipts:       receipts,
		Derivations:                       derivations,
		PipelinePolicyID:                  res.PipelinePolicyID,
		RecordedAt:                        res.EvaluatedAt,
		Status:                            "valid",
	}
}
