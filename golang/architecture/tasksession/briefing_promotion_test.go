// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/identity"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
	qp "github.com/globulario/sensei/golang/architecture/questionpromotion"
	"github.com/globulario/sensei/golang/propose"
	"github.com/globulario/sensei/internal/resulttestkit"
)

const cDomain = "github.com/globulario/sensei"

var (
	cEpoch  = time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	cEnroll = time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
)

func tsModuleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// seedCommittedPromotion produces a repo with one committed governed promotion
// whose effective scope is scopeFiles.
func seedCommittedPromotion(t *testing.T, scopeFiles []string) string {
	t.Helper()
	r, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{
		Direction: "evolve", Epoch: cEpoch,
		ResultFiles: map[string]string{"src/model.go": "package src\n\n// evolve\nfunc Publish() {}\n"},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	src := filepath.Join(tsModuleRoot(t), "docs", "awareness")
	dst := filepath.Join(r.Repo, "docs", "awareness")
	os.MkdirAll(dst, 0o755)
	for _, name := range []string{"actor_roles.yaml", "mutation_paths.yaml", "observation_paths.yaml", "delegation_policies.yaml", "authority_grants.yaml", "authority_domains.yaml"} {
		data, rerr := os.ReadFile(filepath.Join(src, name))
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		os.WriteFile(filepath.Join(dst, name), data, 0o644)
	}
	if _, err := identity.Enroll(identity.EnrollOptions{Root: identity.Root(r.Repo), Now: cEnroll}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	adv, err := AdvanceResultTransition(context.Background(), AdvanceResultRequest{
		RepositoryRoot: r.Repo, TaskDirectory: r.TaskDir, RepositoryDomain: resulttestkit.Domain, ResultRevision: r.ResultRev,
	})
	if err != nil || adv.TransitionID == "" {
		t.Fatalf("advance: %v (%s)", err, adv.Outcome)
	}
	qs, err := qd.OpenQuestionsForLatestTransition(r.TaskDir)
	if err != nil || len(qs) == 0 {
		t.Fatalf("no questions: %v", err)
	}
	cand, err := qd.Prepare(qd.PrepareRequest{
		TaskDirectory: r.TaskDir, RepositoryRoot: r.Repo, IdentityRoot: identity.Root(r.Repo),
		QuestionID: qs[0].QuestionID, Disposition: qd.DispositionAnswered, Reusability: qd.ReusabilityReusableCandidate,
		Rationale: "basis X", AnswerID: "answer.1", AnswerBytes: []byte("basis X"),
		EffectiveScopeDomain: cDomain, EffectiveScopeFiles: scopeFiles,
	})
	if err != nil {
		t.Fatalf("disposition prepare: %v", err)
	}
	if _, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: r.TaskDir, Candidate: cand}); err != nil {
		t.Fatalf("disposition record: %v", err)
	}
	res, err := qp.Promote(context.Background(), qp.PromoteRequest{
		RepositoryRoot: r.Repo, TaskDirectory: r.TaskDir, RepositoryDomain: cDomain, IdentityRoot: identity.Root(r.Repo),
		QuestionDispositionReceiptDigestSHA256: cand.Receipt.ReceiptDigestSHA256,
		Proposal: propose.Request{Kind: "invariant", ID: "invariant.promoted.x", Title: "Reload validates", Description: "x",
			SourceFiles: scopeFiles, RelatedFailures: []string{"failure.x"}, Domain: cDomain},
		EffectiveScopeDomain: cDomain, EffectiveScopeFiles: scopeFiles,
	})
	if err != nil || res.Outcome != qp.OutcomeCommitted {
		t.Fatalf("promote: %v (%s)", err, res.Outcome)
	}
	return r.Repo
}

// A committed, scope-relevant promotion appears with exact provenance.
func TestBriefingConsumesRelevantPromotion(t *testing.T) {
	file := "golang/server/reload.go"
	repo := seedCommittedPromotion(t, []string{file})
	got, findings := collectPromotedKnowledge(repo, file, map[string]bool{file: true}, cDomain)
	if len(findings) != 0 {
		t.Fatalf("unexpected integrity findings: %v", findings)
	}
	if len(got) != 1 {
		t.Fatalf("promoted records = %d, want 1", len(got))
	}
	r := got[0]
	if r.Kind != "invariant" || r.CanonicalRecordID != "invariant.promoted.x" {
		t.Fatalf("wrong record: %+v", r)
	}
	// Exact provenance back-links present.
	for name, v := range map[string]string{
		"node": r.GovernedNodeIRI, "lineage": r.PromotionLineageID, "receipt": r.ReceiptDigestSHA256,
		"question": r.QuestionID, "answer": r.AnswerID, "disposition": r.DispositionReceiptDigestSHA256,
		"task": r.TaskID, "session": r.SessionID,
	} {
		if v == "" {
			t.Errorf("provenance %s missing", name)
		}
	}
}

// An out-of-scope promotion is excluded.
func TestBriefingExcludesUnrelatedPromotion(t *testing.T) {
	repo := seedCommittedPromotion(t, []string{"golang/server/reload.go"})
	got, findings := collectPromotedKnowledge(repo, "cmd/other/main.go", map[string]bool{"cmd/other/main.go": true}, cDomain)
	if len(got) != 0 {
		t.Fatalf("out-of-scope promotion appeared: %+v", got)
	}
	if len(findings) != 0 {
		t.Fatalf("unexpected findings: %v", findings)
	}
}

// A broken conjunct excludes the promotion and reports a typed integrity finding.
func TestBriefingExcludesTamperedPromotionWithFinding(t *testing.T) {
	file := "golang/server/reload.go"
	repo := seedCommittedPromotion(t, []string{file})
	// Tamper the persisted repository graph.
	graphPath := filepath.Join(repo, ".sensei", "project", "graph.nt")
	data, _ := os.ReadFile(graphPath)
	os.WriteFile(graphPath, append(data, []byte("\n<x> <y> <z> .\n")...), 0o644)

	got, findings := collectPromotedKnowledge(repo, file, map[string]bool{file: true}, cDomain)
	if len(got) != 0 {
		t.Fatalf("tampered promotion entered binding context: %+v", got)
	}
	if len(findings) != 1 {
		t.Fatalf("integrity findings = %d, want 1", len(findings))
	}
}

// Deterministic replay: an unchanged world yields identical promoted-knowledge.
func TestBriefingPromotedKnowledgeDeterministic(t *testing.T) {
	file := "golang/server/reload.go"
	repo := seedCommittedPromotion(t, []string{file})
	a, _ := collectPromotedKnowledge(repo, file, map[string]bool{file: true}, cDomain)
	b, _ := collectPromotedKnowledge(repo, file, map[string]bool{file: true}, cDomain)
	if len(a) != len(b) || len(a) != 1 {
		t.Fatalf("non-deterministic count: %d vs %d", len(a), len(b))
	}
	if a[0] != b[0] {
		t.Fatal("promoted-knowledge content differs across identical calls")
	}
}

// A task-local (non-reusable) disposition never appears as promoted knowledge
// (it was never committed as a promotion — discovery finds nothing).
func TestBriefingNeverSurfacesTaskLocal(t *testing.T) {
	// A repo with a disposition but NO committed promotion.
	r, _ := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{Direction: "evolve", Epoch: cEpoch})
	got, findings := collectPromotedKnowledge(r.Repo, "golang/server/reload.go", map[string]bool{"golang/server/reload.go": true}, cDomain)
	if len(got) != 0 || len(findings) != 0 {
		t.Fatalf("surfaced non-promoted knowledge: %v / %v", got, findings)
	}
}

// An incomplete promotion (journal head short of promotion_committed) is excluded
// with an integrity finding.
func TestBriefingExcludesIncompletePromotion(t *testing.T) {
	file := "golang/server/reload.go"
	repo := seedCommittedPromotion(t, []string{file})
	// Truncate the journal: remove the promotion_committed entry (highest NNNNNN.json).
	promoBase := filepath.Join(repo, ".sensei", "project", "promotions")
	dirs, _ := os.ReadDir(promoBase)
	jdir := filepath.Join(promoBase, dirs[0].Name(), "journal")
	entries, _ := os.ReadDir(jdir)
	last := entries[len(entries)-1].Name()
	os.Remove(filepath.Join(jdir, last))

	got, findings := collectPromotedKnowledge(repo, file, map[string]bool{file: true}, cDomain)
	if len(got) != 0 {
		t.Fatalf("incomplete promotion entered binding context: %+v", got)
	}
	if len(findings) != 1 {
		t.Fatalf("integrity findings = %d, want 1", len(findings))
	}
}

// Consumption is read-only: no governed source, graph, journal, or task-ledger
// mutation.
func TestBriefingConsumptionHasNoSideEffects(t *testing.T) {
	file := "golang/server/reload.go"
	repo := seedCommittedPromotion(t, []string{file})
	before := snapshotTree(t, repo)
	if _, _ = collectPromotedKnowledge(repo, file, map[string]bool{file: true}, cDomain); true {
	}
	after := snapshotTree(t, repo)
	if before != after {
		t.Fatal("promoted-knowledge consumption mutated the repository")
	}
}

func snapshotTree(t *testing.T, root string) string {
	t.Helper()
	var b []byte
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		b = append(b, []byte(p)...)
		b = append(b, byte(info.Size()))
		return nil
	})
	return string(b)
}
