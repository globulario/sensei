// SPDX-License-Identifier: AGPL-3.0-only

package questiondisposition_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/identity"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
	"github.com/globulario/sensei/golang/architecture/tasksession"
	"github.com/globulario/sensei/internal/resulttestkit"
)

// seedEpoch stamps the seeded ledger. It post-dates grant.sensei.question_disposition's
// valid_from (2026-07-15) so the anchored disposition resolves; enrollNow precedes it.
var (
	seedEpoch = time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	enrollNow = time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
)

// governedPolicyFiles are the six authority sources authority.LoadPolicyIndex reads.
var governedPolicyFiles = []string{
	"actor_roles.yaml", "mutation_paths.yaml", "observation_paths.yaml",
	"delegation_policies.yaml", "authority_grants.yaml", "authority_domains.yaml",
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// copyGovernedPolicy copies the real docs/awareness authority sources into repo,
// so the dedicated question-disposition domain/grant/mutation_path resolve. When
// dropGrants are named, those grant ids are stripped (adversarial no-grant tests).
func copyGovernedPolicy(t *testing.T, repo string, dropGrants ...string) {
	t.Helper()
	src := filepath.Join(moduleRoot(t), "docs", "awareness")
	dst := filepath.Join(repo, "docs", "awareness")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range governedPolicyFiles {
		data, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if name == "authority_grants.yaml" {
			for _, g := range dropGrants {
				data = stripGrant(t, data, g)
			}
		}
		if err := os.WriteFile(filepath.Join(dst, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

type disposableEnv struct {
	Repo         string
	TaskDir      string
	IdentityRoot string
	QuestionID   string
	ScopeDomain  string
	Head         string
}

// seedDisposable seeds a full task ledger through result_transition_recorded that
// carries at least one architect question, copies the governed policy, and
// enrolls a locally-trusted answering identity.
func seedDisposable(t *testing.T, dropGrants ...string) disposableEnv {
	t.Helper()
	r, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{
		Direction:   "evolve",
		Epoch:       seedEpoch,
		ResultFiles: map[string]string{"src/model.go": "package src\n\n// evolve\nfunc Publish() {}\n"},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	copyGovernedPolicy(t, r.Repo, dropGrants...)
	if _, err := identity.Enroll(identity.EnrollOptions{Root: identity.Root(r.Repo), Now: enrollNow}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	adv, err := tasksession.AdvanceResultTransition(context.Background(), tasksession.AdvanceResultRequest{
		RepositoryRoot: r.Repo, TaskDirectory: r.TaskDir,
		RepositoryDomain: resulttestkit.Domain, ResultRevision: r.ResultRev,
	})
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if adv.TransitionID == "" {
		t.Fatalf("advance recorded no transition (outcome %s)", adv.Outcome)
	}
	questions, err := qd.OpenQuestionsForLatestTransition(r.TaskDir)
	if err != nil {
		t.Fatalf("list questions: %v", err)
	}
	if len(questions) == 0 {
		t.Fatal("seeded transition carries no architect questions")
	}
	head, err := ledgerHead(r.TaskDir)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	return disposableEnv{
		Repo: r.Repo, TaskDir: r.TaskDir, IdentityRoot: identity.Root(r.Repo),
		QuestionID: questions[0].QuestionID, ScopeDomain: questions[0].ScopeDomain, Head: head,
	}
}

func answeredReusable(env disposableEnv) qd.PrepareRequest {
	return qd.PrepareRequest{
		TaskDirectory: env.TaskDir, RepositoryRoot: env.Repo, IdentityRoot: env.IdentityRoot,
		QuestionID: env.QuestionID, Disposition: qd.DispositionAnswered, Reusability: qd.ReusabilityReusableCandidate,
		Rationale: "the direction basis is X", AnswerID: "answer.1", AnswerBytes: []byte("the intended basis is X"),
	}
}
