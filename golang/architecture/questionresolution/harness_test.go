// SPDX-License-Identifier: AGPL-3.0-only

package questionresolution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/identity"
	"github.com/globulario/sensei/golang/architecture/ledger"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
	"github.com/globulario/sensei/golang/architecture/questionpromotion"
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

// world is a seeded repository with a recorded result transition, an enrolled
// certification actor, and the governed policy carrying the isolated triple.
type world struct {
	Repo         string
	TaskDir      string
	IdentityRoot string
	Questions    []qd.OpenQuestionRef
}

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

func seedWorld(t *testing.T) world { return seedWorldDir(t, "evolve") }

func seedWorldDir(t *testing.T, direction string) world {
	t.Helper()
	r, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{
		Direction:   direction,
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
	if err != nil {
		t.Fatalf("questions: %v", err)
	}
	return world{Repo: r.Repo, TaskDir: r.TaskDir, IdentityRoot: identity.Root(r.Repo), Questions: questions}
}

// promote turns an answered+reusable_candidate disposition into a committed
// governed promotion, returning the promotion lineage id.
func (w world) promote(t *testing.T, dispositionDigest string, p propose.Request) string {
	t.Helper()
	res, err := questionpromotion.Promote(context.Background(), questionpromotion.PromoteRequest{
		RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir, RepositoryDomain: testDomain, IdentityRoot: w.IdentityRoot,
		QuestionDispositionReceiptDigestSHA256: dispositionDigest, Proposal: p,
	})
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if res.Outcome != questionpromotion.OutcomeCommitted {
		t.Fatalf("promote outcome = %s (%s)", res.Outcome, res.Detail)
	}
	return res.PromotionLineageID
}

// proposedInvariant is a governed record a promotion may realize.
func proposedInvariant() propose.Request {
	return propose.Request{
		Kind: "invariant", ID: "invariant.promoted.reload_validates",
		Title: "Reload validates before serving", Description: "promoted from an accepted architect answer",
		SourceFiles: []string{"golang/server/reload.go"}, RelatedFailures: []string{"failure.x"},
		Domain: testDomain,
	}
}

// dispose records a disposition for one question, omitting an answer for
// non-answered dispositions (dismissed/deferred carry none).
func (w world) dispose2(t *testing.T, questionID string, disp qd.Disposition, reuse qd.Reusability, salt string) string {
	t.Helper()
	req := qd.PrepareRequest{
		TaskDirectory: w.TaskDir, RepositoryRoot: w.Repo, IdentityRoot: w.IdentityRoot,
		QuestionID: questionID, Disposition: disp, Reusability: reuse,
		Rationale: "the intended basis is " + salt,
	}
	if disp == qd.DispositionAnswered {
		req.AnswerID = "answer." + shortID(questionID) + salt
		req.AnswerBytes = []byte("the intended basis is " + salt)
	}
	cand, err := qd.Prepare(req)
	if err != nil {
		t.Fatalf("dispose prepare %s: %v", questionID, err)
	}
	if _, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: w.TaskDir, Candidate: cand}); err != nil {
		t.Fatalf("dispose record %s: %v", questionID, err)
	}
	return cand.Receipt.ReceiptDigestSHA256
}

func (w world) certify(t *testing.T) CertifyResult {
	t.Helper()
	res, err := Certify(context.Background(), CertifyRequest{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir, IdentityRoot: w.IdentityRoot})
	if err != nil {
		t.Fatalf("certify: %v", err)
	}
	return res
}

func (w world) summarize(t *testing.T) Summary {
	t.Helper()
	s, err := Summarize(context.Background(), SummaryRequest{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	return s
}

// writeUnrelatedBrokenPromotion fabricates a promotion candidate in the repository
// promotion index whose claimed disposition digest belongs to no current-task
// question. It has no journal/graph/governed record, so it would fail verification —
// but it must be routed as unrelated and excluded before verification, so it can
// never block this task's certificate.
func writeUnrelatedBrokenPromotion(t *testing.T, repo, dispositionDigest string) {
	t.Helper()
	dir := filepath.Join(repo, ".sensei", "project", "promotions", "unrelated-lineage")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"question_disposition_receipt_digest_sha256":"` + dispositionDigest + `"}`
	if err := os.WriteFile(filepath.Join(dir, "receipt.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func tamperGraph(t *testing.T, repo string) {
	t.Helper()
	p := filepath.Join(repo, ".sensei", "project", "graph.nt")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	if err := os.WriteFile(p, append(data, []byte("\n<x> <y> <z> .\n")...), 0o644); err != nil {
		t.Fatalf("tamper graph: %v", err)
	}
}

// treeDigest hashes every file under root (path + bytes), skipping the transient
// governed-mutation lock, so a clean read leaves it unchanged.
func treeDigest(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.Contains(p, ".governed-mutation.lock") || strings.HasSuffix(p, ".tmp") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		data, _ := os.ReadFile(p)
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
}

func certFileCount(t *testing.T, repo string) int {
	t.Helper()
	dir := filepath.Join(repo, filepath.FromSlash(CertificationsRelDir))
	n := 0
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, "certificate.json") {
			n++
		}
		return nil
	})
	return n
}

func hasLedgerEvent(t *testing.T, taskDir, eventType string) bool {
	t.Helper()
	chain, err := ledger.NewStore(taskDir).VerifyChain()
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}
	for _, e := range chain.Entries {
		if string(e.Entry.EventType) == eventType {
			return true
		}
	}
	return false
}

func ledgerEntryCount(t *testing.T, taskDir string) int {
	t.Helper()
	rep, err := ledger.NewStore(taskDir).Verify()
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return rep.EntryCount
}
