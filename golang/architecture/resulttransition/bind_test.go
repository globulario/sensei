// SPDX-License-Identifier: Apache-2.0

package resulttransition

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

var t0 = time.Unix(0, 0).UTC()

func gitRun(t *testing.T, repo string, args ...string) string {
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

func gitRev(t *testing.T, repo string) string {
	t.Helper()
	return gitRun(t, repo, "rev-parse", "HEAD")
}

func writeRepoFile(t *testing.T, repo, rel, content string) {
	t.Helper()
	p := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// initRepo creates a git repo with one committed source file and returns the
// repo root and the base revision.
func initRepo(t *testing.T) (string, string) {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	writeRepoFile(t, repo, "src/model.go", "base\n")
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "base")
	return repo, gitRev(t, repo)
}

func canonicalTree(t *testing.T, repo, rev string) string {
	t.Helper()
	id, err := binding.ResolveTreeIdentity(context.Background(), repo, rev)
	if err != nil {
		t.Fatal(err)
	}
	return id.DigestSHA256
}

func testActor() closureprotocol.ActorBinding {
	return closureprotocol.ActorBinding{PrincipalID: "actor.test", ActorKind: closureprotocol.ActorAgent, Roles: []string{"role.repository_repair_agent"}, Issuer: "sensei.local"}
}

func testBase(rev, treeDigest string) closureprotocol.BaseBinding {
	return closureprotocol.BaseBinding{
		Repository: closureprotocol.RepositorySnapshot{Domain: "github.com/globulario/sensei", Revision: rev, RevisionStatus: "resolved", TreeDigestSHA256: treeDigest},
		Graph:      closureprotocol.GraphSnapshot{SchemaVersion: "awareness-ontology/0.2", DigestSHA256: "g", DigestStatus: "resolved"},
		Task:       closureprotocol.TaskBinding{ID: "task.test", SessionID: "session.test"},
		Policies: closureprotocol.PolicyBinding{
			Admission: "admission.strict.v2", Certification: "certification.architectural_closure.v1",
			Completion: "completion.architectural_closure.v1", Revocation: "revocation.architectural_closure.v1",
			Ledger: "ledger.task.v1", Canonicalization: "canon.v1",
		},
	}
}

func testResolution(actorDigest, baseDigest string) closureprotocol.AuthorityResolution {
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

func testDecision() closureprotocol.AdmissionDecision {
	return closureprotocol.AdmissionDecision{
		DecisionID: "decision.test", RequestDigestSHA256: "req0", PolicyID: "admission.strict.v2",
		OperationVerdicts: []closureprotocol.OperationAdmissionVerdict{{OperationID: "op.1", Verdict: "admitted"}},
		CapabilityID:      "cap.test", CompletionPolicyID: "completion.architectural_closure.v1",
	}
}

// seedOpts configures a seeded task ledger through scope_verified.
type seedOpts struct {
	resultRev      string // committed result revision; empty => observe the live worktree
	mutateObserved func(*admission.ObservedChangeSet)
	mutateScope    func(*admission.ScopeVerification)
	stopBefore     closureprotocol.LedgerEventType // omit this event and everything after it
	changePlanTask closureprotocol.TaskBinding     // override the authority base task (wrong-task tests)
}

// seedChain records task_prepared -> authority_resolved -> admission_decided ->
// admission_consumed -> change_observed -> scope_verified with mutually
// consistent digests, observing the exact result named by opts. It returns the
// task ledger directory.
func seedChain(t *testing.T, repo, baseRev string, opts seedOpts) string {
	t.Helper()
	dir := t.TempDir()
	validator := func(et closureprotocol.LedgerEventType, mt string, data []byte) error {
		return ledger.ValidateTaskEventPayload(et, data)
	}
	store := ledger.NewStore(dir, ledger.WithPayloadValidator(validator))

	baseTree := canonicalTree(t, repo, baseRev)
	base := testBase(baseRev, baseTree)
	head := func() string {
		h, err := admission.TaskLedgerHead(dir)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}

	genesis, err := store.Append(context.Background(), ledger.AppendRequest{
		TaskID: base.Task.ID, SessionID: base.Task.SessionID, ExpectedHeadDigestSHA256: "",
		EventType:        closureprotocol.LedgerEventTaskPrepared,
		Payload:          ledger.TaskEventPayload{SchemaVersion: ledger.EventPayloadSchemaVersion, EventType: closureprotocol.LedgerEventTaskPrepared, TaskID: base.Task.ID, SessionID: base.Task.SessionID, BaseBinding: &base},
		PayloadMediaType: "application/yaml", ProducerID: "test", ProducedAt: t0,
	})
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	_ = genesis
	if opts.stopBefore == closureprotocol.LedgerEventAuthorityResolved {
		return dir
	}

	actor := testActor()
	actorDigest := closureprotocol.MustSemanticDigest(actor)
	baseDigest, err := binding.SemanticDigestBase(base)
	if err != nil {
		t.Fatal(err)
	}
	resolution := testResolution(actorDigest, baseDigest)
	authorityDigest := resolution.AuthorityResolutionDigestSHA256
	authTask := base.Task
	if opts.changePlanTask.ID != "" {
		authTask = opts.changePlanTask
	}
	authBase := base
	authBase.Task = authTask
	if _, err := admission.RecordAuthorityResolved(store, head(), base.Task, resolution, actor, closureprotocol.ChangePlan{PlanID: "plan.test"}, authBase, nil, t0); err != nil {
		t.Fatalf("authority: %v", err)
	}
	if opts.stopBefore == closureprotocol.LedgerEventAdmissionDecided {
		return dir
	}

	decision := testDecision()
	decisionDigest := closureprotocol.MustSemanticDigest(decision)
	if _, err := admission.RecordAdmissionDecided(store, head(), decision, base.Task, t0); err != nil {
		t.Fatalf("decision: %v", err)
	}
	if opts.stopBefore == closureprotocol.LedgerEventAdmissionConsumed {
		return dir
	}

	consumption := closureprotocol.CapabilityConsumption{
		CapabilityID: "cap.test", Task: base.Task, ConsumerActor: actor,
		ConsumedOperationIDs: []string{"op.1"}, ConsumedAt: "2026-07-16T12:00:00Z",
		DecisionDigestSHA256: decisionDigest, OneUseStatus: closureprotocol.ReceiptValid,
	}
	if _, err := admission.RecordAdmissionConsumed(store, head(), consumption, t0); err != nil {
		t.Fatalf("consumption: %v", err)
	}
	if opts.stopBefore == closureprotocol.LedgerEventChangeObserved {
		return dir
	}

	observed, err := ObserveChange(repo, baseRev, opts.resultRev, actorDigest, authorityDigest)
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if opts.mutateObserved != nil {
		opts.mutateObserved(&observed)
	}
	if _, err := admission.RecordChangeObserved(store, head(), base.Task, observed, t0); err != nil {
		t.Fatalf("observed: %v", err)
	}
	observedDigest, err := admission.ObservedChangeSetDigest(observed)
	if err != nil {
		t.Fatal(err)
	}
	if opts.stopBefore == closureprotocol.LedgerEventScopeVerified {
		return dir
	}

	scope := admission.ScopeVerification{
		CapabilityID: "cap.test", DecisionDigestSHA256: decisionDigest,
		ActorBindingDigestSHA256: actorDigest, AuthorityResolutionDigestSHA256: authorityDigest,
		BaseTreeDigestSHA256: observed.BaseTreeDigestSHA256, ResultTreeDigestSHA256: observed.ResultTreeDigestSHA256,
		ObservedChangeSetDigestSHA256: observedDigest, VerifiedOperationIDs: []string{"op.1"},
		Status: closureprotocol.ReceiptValid, VerifiedAt: "2026-07-16T12:00:00Z",
	}
	if opts.mutateScope != nil {
		opts.mutateScope(&scope)
	}
	sd, err := admission.ScopeVerificationDigest(scope)
	if err != nil {
		t.Fatal(err)
	}
	scope.ScopeVerificationDigestSHA256 = sd
	if _, err := admission.RecordScopeVerified(store, head(), base.Task, scope, t0); err != nil {
		t.Fatalf("scope: %v", err)
	}
	return dir
}

func mustBind(t *testing.T, repo, dir string, mode ResultMode, resultRev string) BoundRepositoryResult {
	t.Helper()
	got, err := BindRepositoryResult(context.Background(), BindResultRequest{RepositoryRoot: repo, TaskDirectory: dir, Mode: mode, ResultRevision: resultRev})
	if err != nil {
		t.Fatalf("BindRepositoryResult: %v", err)
	}
	return got
}

func wantBindError(t *testing.T, repo, dir string, mode ResultMode, resultRev, contains string) {
	t.Helper()
	_, err := BindRepositoryResult(context.Background(), BindResultRequest{RepositoryRoot: repo, TaskDirectory: dir, Mode: mode, ResultRevision: resultRev})
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", contains)
	}
	if !strings.Contains(err.Error(), contains) {
		t.Fatalf("error = %q, want containing %q", err.Error(), contains)
	}
}

// mutateWorktree writes a modification (uncommitted) to the tracked source.
func mutateWorktree(t *testing.T, repo, content string) {
	t.Helper()
	writeRepoFile(t, repo, "src/model.go", content)
}

// commitResult applies a modification and commits it, returning the result rev.
func commitResult(t *testing.T, repo, content string) string {
	t.Helper()
	writeRepoFile(t, repo, "src/model.go", content)
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "result")
	return gitRev(t, repo)
}
