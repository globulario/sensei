// SPDX-License-Identifier: Apache-2.0

package questionpromotion

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/identity"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
	"github.com/globulario/sensei/golang/architecture/repograph"
	"github.com/globulario/sensei/golang/architecture/resultpipeline"
	"github.com/globulario/sensei/golang/architecture/resultrecording"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
	"github.com/globulario/sensei/golang/propose"
	"github.com/globulario/sensei/internal/resulttestkit"
)

const testDomain = "github.com/globulario/sensei"

var (
	seedEpoch = time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	enrollNow = time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
)

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func copyGovernedPolicy(t *testing.T, repo string) {
	t.Helper()
	src := filepath.Join(moduleRoot(t), "docs", "awareness")
	dst := filepath.Join(repo, "docs", "awareness")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"actor_roles.yaml", "mutation_paths.yaml", "observation_paths.yaml",
		"delegation_policies.yaml", "authority_grants.yaml", "authority_domains.yaml",
	} {
		data, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dst, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

type promotable struct {
	Repo              string
	TaskDir           string
	IdentityRoot      string
	DispositionDigest string
	Proposal          propose.Request
}

// proposedInvariant is the governed record a promotion realizes from the answer.
func proposedInvariant() propose.Request {
	return propose.Request{
		Kind: "invariant", ID: "invariant.promoted.reload_validates",
		Title: "Reload validates before serving", Description: "promoted from an accepted architect answer",
		SourceFiles: []string{"golang/server/reload.go"}, RelatedFailures: []string{"failure.x"},
		Domain: testDomain,
	}
}

// seedPromotable produces a repo with a recorded answered+reusable_candidate
// disposition ready to promote, plus an enrolled promotion identity and the
// governed policy carrying the promotion grant.
func seedPromotable(t *testing.T) promotable {
	return seedDispositionOnly(t, qd.DispositionAnswered, qd.ReusabilityReusableCandidate)
}

func seedDispositionOnly(t *testing.T, disp qd.Disposition, reuse qd.Reusability) promotable {
	t.Helper()
	r, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{
		Direction:   "evolve",
		Epoch:       seedEpoch,
		ResultFiles: map[string]string{"src/model.go": "package src\n\n// evolve\nfunc Publish() {}\n"},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	copyGovernedPolicy(t, r.Repo)
	if _, err := identity.Enroll(identity.EnrollOptions{Root: identity.Root(r.Repo), Now: enrollNow}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	// Record the result_transition_recorded event via the layer beneath
	// tasksession.AdvanceResultTransition, so this test never imports tasksession
	// (which now imports questionpromotion — a test-only cycle otherwise).
	head, herr := admission.TaskLedgerHead(r.TaskDir)
	if herr != nil {
		t.Fatalf("head: %v", herr)
	}
	c, perr := resultpipeline.PrepareTransition(context.Background(), resultpipeline.PrepareTransitionRequest{
		Build: resultpipeline.BuildRequest{RepositoryRoot: r.Repo, TaskDirectory: r.TaskDir,
			ResultMode: resulttransition.ResultModeRevision, ResultRevision: r.ResultRev, RepositoryDomain: resulttestkit.Domain},
		ExpectedLedgerHeadDigestSHA256: head, RecordedAt: "2026-07-16T00:00:00Z",
	})
	if perr != nil {
		t.Fatalf("prepare transition: %v", perr)
	}
	if _, rerr := resultrecording.RecordTransition(context.Background(), resultrecording.RecordRequest{TaskDirectory: r.TaskDir, Candidate: c}); rerr != nil {
		t.Fatalf("record transition: %v", rerr)
	}
	questions, err := qd.OpenQuestionsForLatestTransition(r.TaskDir)
	if err != nil || len(questions) == 0 {
		t.Fatalf("no questions: %v", err)
	}
	// Record an answered + reusable_candidate disposition.
	cand, err := qd.Prepare(qd.PrepareRequest{
		TaskDirectory: r.TaskDir, RepositoryRoot: r.Repo, IdentityRoot: identity.Root(r.Repo),
		QuestionID: questions[0].QuestionID, Disposition: disp, Reusability: reuse,
		Rationale: "the intended basis is X", AnswerID: "answer.1", AnswerBytes: []byte("the intended basis is X"),
	})
	if err != nil {
		t.Fatalf("disposition prepare: %v", err)
	}
	if _, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: r.TaskDir, Candidate: cand}); err != nil {
		t.Fatalf("disposition record: %v", err)
	}
	return promotable{
		Repo: r.Repo, TaskDir: r.TaskDir, IdentityRoot: identity.Root(r.Repo),
		DispositionDigest: cand.Receipt.ReceiptDigestSHA256, Proposal: proposedInvariant(),
	}
}

func (p promotable) request() PromoteRequest {
	return PromoteRequest{
		RepositoryRoot: p.Repo, TaskDirectory: p.TaskDir, RepositoryDomain: testDomain, IdentityRoot: p.IdentityRoot,
		QuestionDispositionReceiptDigestSHA256: p.DispositionDigest, Proposal: p.Proposal,
	}
}

// TestPromoteHappyPathReachesOneCommit proves the full transaction reaches exactly
// one authoritative commit with a valid receipt and queryable provenance.
func TestPromoteHappyPathReachesOneCommit(t *testing.T) {
	p := seedPromotable(t)
	res, err := Promote(context.Background(), p.request())
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if res.Outcome != OutcomeCommitted {
		t.Fatalf("outcome = %s (%s), want committed", res.Outcome, res.Detail)
	}
	if res.Receipt == nil || res.ReceiptDigestSHA256 == "" || res.CommittedCausalIdentitySHA256 == "" {
		t.Fatal("committed result must carry a receipt + identities")
	}
	if err := Validate(*res.Receipt); err != nil {
		t.Fatalf("committed receipt invalid: %v", err)
	}
	// The governed record now exists in source.
	inv := filepath.Join(p.Repo, "docs", "awareness", "invariants.yaml")
	data, _ := os.ReadFile(inv)
	if !bytes.Contains(data, []byte("invariant.promoted.reload_validates")) {
		t.Fatal("promoted governed record not in source")
	}
	// The persisted repository graph verifies independently.
	if _, err := repograph.VerifyPersisted(context.Background(), p.Repo); err != nil {
		t.Fatalf("persisted graph reverify: %v", err)
	}
}
