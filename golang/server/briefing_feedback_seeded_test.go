// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/briefingfeedback"
	"github.com/globulario/sensei/golang/architecture/identity"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
	qp "github.com/globulario/sensei/golang/architecture/questionpromotion"
	"github.com/globulario/sensei/golang/architecture/tasksession"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/propose"
	"github.com/globulario/sensei/internal/resulttestkit"
)

const feedbackTestDomain = "github.com/globulario/sensei"

// seedServerPromotion produces a repository with one committed governed promotion scoped to
// scopeFiles, mirroring the tasksession seed so the server test drives the REAL owner.
func seedServerPromotion(t *testing.T, scopeFiles []string) string {
	return seedServerPromotionWithAnswer(t, scopeFiles, "basis X")
}

// seedServerPromotionWithAnswer is seedServerPromotion with an explicit answer/rationale text
// (used by the privacy proof to plant a sentinel that must never reach a feedback surface).
func seedServerPromotionWithAnswer(t *testing.T, scopeFiles []string, answer string) string {
	t.Helper()
	epoch := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	enroll := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	r, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{
		Direction: "evolve", Epoch: epoch,
		ResultFiles: map[string]string{"src/model.go": "package src\n\n// evolve\nfunc Publish() {}\n"},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, file, _, _ := runtime.Caller(0)
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	src := filepath.Join(moduleRoot, "docs", "awareness")
	dst := filepath.Join(r.Repo, "docs", "awareness")
	os.MkdirAll(dst, 0o755)
	for _, name := range []string{"actor_roles.yaml", "mutation_paths.yaml", "observation_paths.yaml", "delegation_policies.yaml", "authority_grants.yaml", "authority_domains.yaml"} {
		data, rerr := os.ReadFile(filepath.Join(src, name))
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		os.WriteFile(filepath.Join(dst, name), data, 0o644)
	}
	if _, err := identity.Enroll(identity.EnrollOptions{Root: identity.Root(r.Repo), Now: enroll}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	adv, err := tasksession.AdvanceResultTransition(context.Background(), tasksession.AdvanceResultRequest{
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
		Rationale: answer, AnswerID: "answer.1", AnswerBytes: []byte(answer),
		EffectiveScopeDomain: feedbackTestDomain, EffectiveScopeFiles: scopeFiles,
	})
	if err != nil {
		t.Fatalf("disposition prepare: %v", err)
	}
	if _, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: r.TaskDir, Candidate: cand}); err != nil {
		t.Fatalf("disposition record: %v", err)
	}
	res, err := qp.Promote(context.Background(), qp.PromoteRequest{
		RepositoryRoot: r.Repo, TaskDirectory: r.TaskDir, RepositoryDomain: feedbackTestDomain, IdentityRoot: identity.Root(r.Repo),
		QuestionDispositionReceiptDigestSHA256: cand.Receipt.ReceiptDigestSHA256,
		Proposal: propose.Request{Kind: "invariant", ID: "invariant.promoted.x", Title: "Reload validates", Description: "x",
			SourceFiles: scopeFiles, RelatedFailures: []string{"failure.x"}, Domain: feedbackTestDomain},
		EffectiveScopeDomain: feedbackTestDomain, EffectiveScopeFiles: scopeFiles,
	})
	if err != nil || res.Outcome != qp.OutcomeCommitted {
		t.Fatalf("promote: %v (%s)", err, res.Outcome)
	}
	return r.Repo
}

// File-scoped, matching repository: the verified promotion appears as a structured record with
// exact provenance, its governed identity is a referenced id, prose matches the structured set,
// and no raw answer text / verification error / absolute root leaks.
func TestBriefingFeedback_VerifiedPromotionEndToEnd(t *testing.T) {
	file := "golang/server/reload.go"
	repo := seedServerPromotion(t, []string{file})
	s := testFeedbackServer(&briefingRepositoryContext{Root: repo, Domain: feedbackTestDomain})

	p, err := s.briefingFeedback(context.Background(), feedbackBriefingScope{effectiveDomain: feedbackTestDomain, file: file, rawFile: file, rawDomain: feedbackTestDomain})
	if err != nil {
		t.Fatal(err)
	}
	if p.Availability != briefingfeedback.FeedbackAvailable || len(p.Records) != 1 {
		t.Fatalf("want feedback_available with 1 record, got %q recs=%d", p.Availability, len(p.Records))
	}
	rec := p.Records[0]
	if rec.CanonicalRecordID != "invariant.promoted.x" || rec.GovernedKind != "invariant" {
		t.Fatalf("record identity wrong: %+v", rec)
	}
	for name, v := range map[string]string{
		"lineage": rec.PromotionLineageID, "receipt": rec.PromotionReceiptDigestSHA256,
		"question": rec.QuestionID, "answer": rec.AnswerID, "disposition": rec.DispositionReceiptDigestSHA256,
	} {
		if v == "" {
			t.Errorf("provenance %s missing", name)
		}
	}

	// Referenced ids carry the governed identity, NOT lineage/question/answer identities.
	refs := feedbackReferencedIDs(p)
	if len(refs) != 1 || refs[0] != "invariant:invariant.promoted.x" {
		t.Fatalf("referenced ids = %v, want [invariant:invariant.promoted.x]", refs)
	}
	for _, r := range refs {
		if strings.Contains(r, rec.PromotionLineageID) || strings.Contains(r, rec.QuestionID) || strings.Contains(r, rec.AnswerID) {
			t.Fatalf("provenance identity leaked into referenced ids: %q", r)
		}
	}

	// Wire mapping preserves the digest and leaks no absolute root.
	wire, err := briefingFeedbackToProto(p)
	if err != nil {
		t.Fatal(err)
	}
	if wire.GetDigestSha256() != p.DigestSHA256 {
		t.Fatalf("wire digest not preserved")
	}

	// Prose parity: every governed record in prose exists in the structured set; no raw answer
	// text or absolute repo root appears.
	prose := briefingFeedbackProse(p)
	if !strings.Contains(prose, "invariant:invariant.promoted.x") {
		t.Fatalf("prose missing the governed record: %q", prose)
	}
	if strings.Contains(prose, "basis X") {
		t.Fatalf("prose leaked raw answer text")
	}
	if strings.Contains(prose, repo) {
		t.Fatalf("prose leaked the absolute repository root")
	}
	// Combined status: an EMPTY base with available feedback lifts to OK.
	if got := combineBriefingStatus(awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY, p.Availability); got != awarenesspb.BriefingStatus_BRIEFING_STATUS_OK {
		t.Fatalf("available feedback must lift EMPTY base to OK, got %s", got)
	}
}

// Out-of-scope verified promotion yields empty feedback; a foreign-domain request yields
// unavailable without invoking the owner against the configured root.
func TestBriefingFeedback_OutOfScopeIsEmpty(t *testing.T) {
	repo := seedServerPromotion(t, []string{"golang/server/reload.go"})
	s := testFeedbackServer(&briefingRepositoryContext{Root: repo, Domain: feedbackTestDomain})
	p, err := s.briefingFeedback(context.Background(), feedbackBriefingScope{effectiveDomain: feedbackTestDomain, file: "cmd/other/main.go", rawFile: "cmd/other/main.go", rawDomain: feedbackTestDomain})
	if err != nil {
		t.Fatal(err)
	}
	if p.Availability != briefingfeedback.FeedbackEmpty || len(p.Records) != 0 {
		t.Fatalf("out-of-scope must be feedback_empty, got %q recs=%d", p.Availability, len(p.Records))
	}
	// An empty projection renders no feedback prose section.
	if briefingFeedbackProse(p) != "" {
		t.Fatalf("empty feedback must render no prose section")
	}
}

// Backslash and slash spellings of the same file produce one canonical slash identity and the
// same feedback result — the graph and feedback legs never reason about different spellings.
func TestBriefingFeedback_SlashIdentityParity(t *testing.T) {
	file := "golang/server/reload.go"
	repo := seedServerPromotion(t, []string{file})
	s := testFeedbackServer(&briefingRepositoryContext{Root: repo, Domain: feedbackTestDomain})

	// The RPC computes scope.file via filepath.ToSlash; both spellings collapse to the same.
	slash, err := s.briefingFeedback(context.Background(), feedbackBriefingScope{
		effectiveDomain: feedbackTestDomain, file: "golang/server/reload.go",
		rawFile: "golang/server/reload.go", rawDomain: feedbackTestDomain,
	})
	if err != nil {
		t.Fatal(err)
	}
	back, err := s.briefingFeedback(context.Background(), feedbackBriefingScope{
		effectiveDomain: feedbackTestDomain, file: filepath.ToSlash(`golang\server\reload.go`),
		rawFile: `golang\server\reload.go`, rawDomain: feedbackTestDomain,
	})
	if err != nil {
		t.Fatal(err)
	}
	if slash.Availability != briefingfeedback.FeedbackAvailable || back.Availability != briefingfeedback.FeedbackAvailable {
		t.Fatalf("both spellings must admit: %q / %q", slash.Availability, back.Availability)
	}
	if slash.DigestSHA256 != back.DigestSHA256 {
		t.Fatalf("backslash and slash spellings produced different feedback digests")
	}
	if len(slash.RequestedFiles) != 1 || slash.RequestedFiles[0] != file {
		t.Fatalf("requested file identity is not slash-canonical: %v", slash.RequestedFiles)
	}
}
