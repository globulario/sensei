// SPDX-License-Identifier: Apache-2.0

package coldsource

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness-graph/golang/extractor"
)

// ── triangulation: >=2 distinct source types eligible; single-source held ────

func TestTriangulate_RequiresTwoDistinctSourceTypes(t *testing.T) {
	signals := []ColdSignal{
		// theme A: a revert + a PR review on the same component → eligible
		{SourceType: SourceRevertCommit, ThemeKey: "repo.upstream", CommitSHA: "abc123", MatchedText: "Revert foo"},
		{SourceType: SourcePRReview, ThemeKey: "repo.upstream", FilePath: "repo/upstream/x.go", Line: 5, PRID: "7", CommentID: "9", MatchedText: "must not absorb 404"},
		// theme B: two reverts (same source type) → held back
		{SourceType: SourceRevertCommit, ThemeKey: "repo.alone", CommitSHA: "d1", MatchedText: "Revert a"},
		{SourceType: SourceRevertCommit, ThemeKey: "repo.alone", CommitSHA: "d2", MatchedText: "Revert b"},
	}
	eligible, held := Triangulate(signals)
	if len(eligible) != 1 || eligible[0].ThemeKey != "repo.upstream" {
		t.Fatalf("expected 1 eligible theme repo.upstream, got %+v", eligible)
	}
	if len(held) != 1 || held[0].ThemeKey != "repo.alone" {
		t.Fatalf("expected single-source theme held back, got %+v", held)
	}
}

// ── revert/regression matcher ────────────────────────────────────────────────

func TestExtractReverts_MatchesRevertAndRegression(t *testing.T) {
	commits := []CommitRecord{
		{SHA: "s1", Subject: `Revert "infra: scylla probe loopback"`, Files: []string{"golang/node_agent/x.go"}},
		{SHA: "s2", Subject: "fix: address INC-2026-0016 regression", Files: []string{"golang/verifier/v.go"}},
		{SHA: "s3", Subject: "feat: add a normal feature", Files: []string{"golang/x.go"}},
	}
	got := ExtractReverts(commits)
	if len(got) != 2 {
		t.Fatalf("expected 2 signals (revert + regression), got %d: %+v", len(got), got)
	}
	if got[0].ProposedClass != extractor.CandidateForbiddenFix {
		t.Errorf("revert should hint ForbiddenFix, got %s", got[0].ProposedClass)
	}
	if got[1].ProposedClass != extractor.CandidateFailureMode {
		t.Errorf("regression should hint FailureMode, got %s", got[1].ProposedClass)
	}
}

// ── PR review classifier (negatives win first) ───────────────────────────────

func TestClassifyComment_NegativeWinsFirst(t *testing.T) {
	cases := map[string]string{
		"You must not store tokens on disk":      extractor.CandidateForbiddenFix,
		"this always breaks the leader election": extractor.CandidateFailureMode,
		"the build_id must be server-generated":  extractor.CandidateInvariant,
	}
	for body, want := range cases {
		ok, class := classifyComment(body)
		if !ok || class != want {
			t.Errorf("classifyComment(%q) = (%v,%s), want (true,%s)", body, ok, class, want)
		}
	}
	if ok, _ := classifyComment("nice work, lgtm"); ok {
		t.Errorf("non-rule comment should not match")
	}
}

// ── draft contract: citations must come from the bundle ──────────────────────

func TestValidateDraft_RejectsFabricatedAndNonCandidate(t *testing.T) {
	b := Bundle{
		ThemeKey:    "t",
		SourceTypes: []SourceType{SourcePRReview, SourceRevertCommit},
		Signals: []ColdSignal{
			{SourceType: SourcePRReview, FilePath: "a.go", Line: 1},
			{SourceType: SourceRevertCommit, CommitSHA: "abc"},
		},
	}
	// fabricated citation
	bad := &extractor.PromotionProposal{Status: "candidate", CandidateClass: "X", SourcePaths: []string{"file:not-in-bundle.go"}}
	if v := ValidateDraft(bad, b); len(v) == 0 {
		t.Errorf("expected violation for fabricated citation")
	}
	// non-candidate status
	active := &extractor.PromotionProposal{Status: "active", CandidateClass: "X", SourcePaths: []string{"file:a.go:1"}}
	if v := ValidateDraft(active, b); len(v) == 0 {
		t.Errorf("expected violation for non-candidate status")
	}
	// valid
	good := &extractor.PromotionProposal{Status: "candidate", CandidateClass: "X", SourcePaths: []string{"file:a.go:1", "commit:abc"}}
	if v := ValidateDraft(good, b); len(v) != 0 {
		t.Errorf("expected no violations, got %v", v)
	}
}

func TestEchoDrafter_ProducesContractValidCandidate(t *testing.T) {
	b := Bundle{
		ThemeKey:    "repo.upstream",
		SourceTypes: []SourceType{SourcePRReview, SourceRevertCommit},
		Signals: []ColdSignal{
			{SourceType: SourcePRReview, FilePath: "repo/upstream/x.go", Line: 5, PRID: "7", CommentID: "9", ProposedClass: extractor.CandidateForbiddenFix, MatchedText: "must not absorb 404 into unavailable"},
			{SourceType: SourceRevertCommit, CommitSHA: "abc123", ProposedClass: extractor.CandidateForbiddenFix, MatchedText: "Revert wrong classification"},
		},
	}
	p, err := EchoDrafter{}.Draft(context.Background(), b)
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	if v := ValidateDraft(p, b); len(v) != 0 {
		t.Fatalf("echo draft violated contract: %v", v)
	}
	if p.Status != "candidate" {
		t.Errorf("status must be candidate, got %s", p.Status)
	}
	if p.CandidateClass != extractor.CandidateForbiddenFix {
		t.Errorf("dominant class should be ForbiddenFix, got %s", p.CandidateClass)
	}
}

// ── shallow guard ────────────────────────────────────────────────────────────

func TestIsShallow_DenylistAndThinEvidence(t *testing.T) {
	denylisted := Bundle{Signals: []ColdSignal{{MatchedText: "this is long enough to be meaningful"}}}
	p := &extractor.PromotionProposal{Theme: "vendor.foo.bar"}
	if shallow, _ := IsShallow(p, denylisted); !shallow {
		t.Errorf("vendored theme should be shallow")
	}
	thin := Bundle{Signals: []ColdSignal{{MatchedText: "x"}}}
	p2 := &extractor.PromotionProposal{Theme: "repo.real"}
	if shallow, _ := IsShallow(p2, thin); !shallow {
		t.Errorf("thin evidence should be shallow")
	}
}

// ── citation check: file existence + line range + commit + pr preservation ───

type fakeGit struct{ known map[string]bool }

func (f fakeGit) CommitExists(sha string) bool { return f.known[sha] }

func TestCheckCitations_Resolution(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.go"), []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git := fakeGit{known: map[string]bool{"abc123": true}}

	ok, res := CheckCitations([]string{"file:real.go:2", "commit:abc123", "pr:7:9"}, dir, git)
	if !ok {
		t.Fatalf("expected all resolvable, got %+v", res)
	}

	okBad, _ := CheckCitations([]string{"file:missing.go"}, dir, git)
	if okBad {
		t.Errorf("missing file should fail")
	}
	okLine, _ := CheckCitations([]string{"file:real.go:99"}, dir, git)
	if okLine {
		t.Errorf("out-of-range line should fail")
	}
	okSha, _ := CheckCitations([]string{"commit:deadbeef"}, dir, git)
	if okSha {
		t.Errorf("unknown commit should fail")
	}
}
