// SPDX-License-Identifier: AGPL-3.0-only

package benchmark

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// senseiRepoFixture builds a throwaway checkout shaped like a Sensei repo: a
// git repository with an authored awareness corpus and a build transaction
// stamp. It is deliberately not a copy of the real repository — the point is to
// control every input so drift can be introduced one identity at a time.
func senseiRepoFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")

	writeFixture(t, filepath.Join(root, "docs", "awareness", "invariants.yaml"), "invariants: []\n")
	// candidates/ is written but must NOT contribute to the authored-corpus
	// identity: candidate knowledge is not active authority.
	writeFixture(t, filepath.Join(root, "docs", "awareness", "candidates", "proposal.yaml"), "candidate: one\n")
	writeFixture(t, filepath.Join(root, "golang", "server", "embeddata", "awareness.transaction.tsv"),
		"format\tv1\nseed\tdigest_sha256\taaaa1111\nseed\ttriple_count\t100\nrepo\tawareness-graph\tcafe0001\nrepo\tservices\tmissing\n")

	run("add", "-A")
	run("commit", "-m", "fixture")
	return root
}

func writeFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureAuthorityStateBindsGoverningIdentity(t *testing.T) {
	repo := senseiRepoFixture(t)
	got := CaptureAuthorityState(repo)

	if got.CaptureState != AuthorityCaptureBound {
		t.Fatalf("capture state = %q (%s), want %q", got.CaptureState, got.CaptureReason, AuthorityCaptureBound)
	}
	if got.SenseiRevision == "" {
		t.Error("sensei revision not bound")
	}
	if got.SeedDigestSHA256 != "aaaa1111" {
		t.Errorf("seed digest = %q, want the stamp's value", got.SeedDigestSHA256)
	}
	if got.SeedTripleCount != "100" {
		t.Errorf("seed triple count = %q, want 100", got.SeedTripleCount)
	}
	if got.GraphBuildCommit != "cafe0001" {
		t.Errorf("graph build commit = %q, want cafe0001", got.GraphBuildCommit)
	}
	if got.AuthoredCorpusDigestSHA256 == "" {
		t.Error("authored corpus digest not bound")
	}
	if got.TransactionStampSHA256 == "" {
		t.Error("transaction stamp digest not bound")
	}
	if got.SenseiTreeDirty {
		t.Error("clean fixture reported dirty")
	}
}

// TestCaptureAuthorityStateUnavailableIsTyped locks in that a capture which
// cannot complete reports a typed reason instead of an empty state. A silent
// empty here would let "we never bound the authority" be read downstream as
// "the authority matched".
func TestCaptureAuthorityStateUnavailableIsTyped(t *testing.T) {
	for _, tc := range []struct {
		name   string
		repo   string
		reason string
	}{
		{"unset", "", AuthorityCaptureReasonRepoUnset},
		{"not a git checkout", t.TempDir(), AuthorityCaptureReasonRepoNotGit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := CaptureAuthorityState(tc.repo)
			if got.CaptureState != AuthorityCaptureUnavailable {
				t.Errorf("capture state = %q, want %q", got.CaptureState, AuthorityCaptureUnavailable)
			}
			if got.CaptureReason != tc.reason {
				t.Errorf("capture reason = %q, want %q", got.CaptureReason, tc.reason)
			}
		})
	}
}

// TestAuthoredCorpusDigestIgnoresCandidates proves candidate churn does not
// register as an authority change, because candidates are not active authority.
func TestAuthoredCorpusDigestIgnoresCandidates(t *testing.T) {
	repo := senseiRepoFixture(t)
	before := CaptureAuthorityState(repo).AuthoredCorpusDigestSHA256

	writeFixture(t, filepath.Join(repo, "docs", "awareness", "candidates", "another.yaml"), "candidate: two\n")
	if after := CaptureAuthorityState(repo).AuthoredCorpusDigestSHA256; after != before {
		t.Errorf("candidate churn changed the authored-corpus identity: %q -> %q", before, after)
	}

	writeFixture(t, filepath.Join(repo, "docs", "awareness", "invariants.yaml"), "invariants: [changed]\n")
	if after := CaptureAuthorityState(repo).AuthoredCorpusDigestSHA256; after == before {
		t.Error("an authored-source change did not move the authored-corpus identity")
	}
}

// TestVerifyAuthorityStateDetectsEachDrift is the reproducibility guarantee:
// every identity that can make a replay incomparable must be detected by its
// own typed code. Without the authority binding on FreezeReceipt there is
// nothing to compare and every one of these cases silently reads as a match.
func TestVerifyAuthorityStateDetectsEachDrift(t *testing.T) {
	base := AuthorityState{
		CaptureState:               AuthorityCaptureBound,
		SenseiRevision:             "rev1",
		SeedDigestSHA256:           "seed1",
		SeedTripleCount:            "100",
		AuthoredCorpusDigestSHA256: "corpus1",
		GraphBuildCommit:           "build1",
		PairedRepoCommit:           "paired1",
		TransactionStampSHA256:     "stamp1",
	}
	if got := VerifyAuthorityState(&base, base); got.Verdict != AuthorityReplayMatch || !got.Comparable {
		t.Fatalf("identical authority: verdict=%s comparable=%v, want %s/true", got.Verdict, got.Comparable, AuthorityReplayMatch)
	}

	for _, tc := range []struct {
		name   string
		mutate func(*AuthorityState)
		code   string
	}{
		{"sensei revision", func(s *AuthorityState) { s.SenseiRevision = "rev2" }, AuthorityDriftSenseiRevision},
		{"seed digest", func(s *AuthorityState) { s.SeedDigestSHA256 = "seed2" }, AuthorityDriftSeedDigest},
		{"seed triple count", func(s *AuthorityState) { s.SeedTripleCount = "101" }, AuthorityDriftSeedTripleCount},
		{"authored corpus", func(s *AuthorityState) { s.AuthoredCorpusDigestSHA256 = "corpus2" }, AuthorityDriftAuthoredCorpus},
		{"graph build commit", func(s *AuthorityState) { s.GraphBuildCommit = "build2" }, AuthorityDriftGraphBuildCommit},
		{"paired repo commit", func(s *AuthorityState) { s.PairedRepoCommit = "paired2" }, AuthorityDriftPairedRepoCommit},
		{"transaction stamp", func(s *AuthorityState) { s.TransactionStampSHA256 = "stamp2" }, AuthorityDriftStampDigest},
		{"dirty at replay", func(s *AuthorityState) { s.SenseiTreeDirty = true }, AuthorityDriftDirtyAtReplay},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observed := base
			tc.mutate(&observed)
			got := VerifyAuthorityState(&base, observed)
			if got.Verdict != AuthorityReplayDrifted {
				t.Fatalf("verdict = %s, want %s", got.Verdict, AuthorityReplayDrifted)
			}
			if got.Comparable {
				t.Error("drifted authority reported as comparable")
			}
			if !hasReasonCode(got.Reasons, tc.code) {
				t.Errorf("reasons %v do not name %s", codes(got.Reasons), tc.code)
			}
		})
	}
}

// TestVerifyAuthorityStateDirtyFreezeIsNotComparable: a dirty tree at freeze
// time means the recorded revision does not identify the code that ran, so
// matching revisions prove nothing.
func TestVerifyAuthorityStateDirtyFreezeIsNotComparable(t *testing.T) {
	frozen := AuthorityState{CaptureState: AuthorityCaptureBound, SenseiRevision: "rev1", SenseiTreeDirty: true}
	observed := AuthorityState{CaptureState: AuthorityCaptureBound, SenseiRevision: "rev1"}
	got := VerifyAuthorityState(&frozen, observed)
	if got.Verdict != AuthorityReplayDrifted || got.Comparable {
		t.Fatalf("verdict=%s comparable=%v, want %s/false", got.Verdict, got.Comparable, AuthorityReplayDrifted)
	}
	if !hasReasonCode(got.Reasons, AuthorityDriftDirtyAtFreeze) {
		t.Errorf("reasons %v do not name %s", codes(got.Reasons), AuthorityDriftDirtyAtFreeze)
	}
}

// TestVerifyAuthorityStateDistinguishesNotBoundFromUnverifiable keeps the three
// non-match worlds apart. Collapsing them would hide whether we are asserting
// the authority changed, that we cannot tell, or that it was never bound.
func TestVerifyAuthorityStateDistinguishesNotBoundFromUnverifiable(t *testing.T) {
	bound := AuthorityState{CaptureState: AuthorityCaptureBound, SenseiRevision: "rev1"}

	if got := VerifyAuthorityState(nil, bound); got.Verdict != AuthorityReplayNotBound || got.Comparable {
		t.Errorf("nil frozen: verdict=%s comparable=%v, want %s/false", got.Verdict, got.Comparable, AuthorityReplayNotBound)
	}

	unbound := AuthorityState{CaptureState: AuthorityCaptureUnavailable, CaptureReason: AuthorityCaptureReasonStampAbsent}
	got := VerifyAuthorityState(&unbound, bound)
	if got.Verdict != AuthorityReplayUnverifiable || got.Comparable {
		t.Errorf("unbound freeze: verdict=%s comparable=%v, want %s/false", got.Verdict, got.Comparable, AuthorityReplayUnverifiable)
	}
	if !hasReasonCode(got.Reasons, AuthorityCaptureReasonStampAbsent) {
		t.Errorf("reasons %v do not carry the typed capture reason", codes(got.Reasons))
	}

	if got := VerifyAuthorityState(&bound, unbound); got.Verdict != AuthorityReplayUnverifiable || got.Comparable {
		t.Errorf("unbound replay: verdict=%s comparable=%v, want %s/false", got.Verdict, got.Comparable, AuthorityReplayUnverifiable)
	}
}

// TestVerifyAuthorityStateNeverSubstitutesFrozenForObserved guards the rule the
// seedmeta authority path already enforces: the expected value must never stand
// in as the observed one. A zero observed state must be unverifiable, never a
// match.
func TestVerifyAuthorityStateNeverSubstitutesFrozenForObserved(t *testing.T) {
	frozen := AuthorityState{CaptureState: AuthorityCaptureBound, SenseiRevision: "rev1", SeedDigestSHA256: "seed1"}
	got := VerifyAuthorityState(&frozen, AuthorityState{})
	if got.Verdict == AuthorityReplayMatch || got.Comparable {
		t.Fatalf("empty observation reported as %s (comparable=%v)", got.Verdict, got.Comparable)
	}
}

func hasReasonCode(reasons []Reason, code string) bool {
	for _, r := range reasons {
		if r.Code == code {
			return true
		}
	}
	return false
}

func codes(reasons []Reason) []string {
	out := make([]string, 0, len(reasons))
	for _, r := range reasons {
		out = append(out, r.Code)
	}
	return out
}

// TestFreezeDoesNotDirtyTheAuthorityWithItsOwnWorkspace: a frozen workspace may
// legitimately live inside the Sensei checkout. The dirty-tree observation runs
// `git status --porcelain`, so if the workspace is created before the authority
// is captured, the freeze's own untracked scratch files make a clean checkout
// look dirty — and every later replay is then rejected as incomparable for a
// change nobody made.
func TestFreezeDoesNotDirtyTheAuthorityWithItsOwnWorkspace(t *testing.T) {
	sensei := senseiRepoFixture(t)
	if got := CaptureAuthorityState(sensei); got.SenseiTreeDirty {
		t.Fatal("fixture checkout is dirty before the freeze; test cannot prove anything")
	}

	repo, base := localRepo(t)
	taskPath, oraclePath := writeManifests(t, base, "")
	// The workspace lives INSIDE the Sensei checkout, which is the case that
	// exposes the ordering.
	workspace := filepath.Join(sensei, "eval-workspace")

	receipt, _, err := Freeze(FreezeOptions{
		TaskPath: taskPath, SourceRepo: repo, OraclePath: oraclePath,
		OutputDir: workspace, SenseiRepo: sensei,
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.AuthorityState == nil {
		t.Fatal("freeze recorded no authority state")
	}
	if receipt.AuthorityState.CaptureState != AuthorityCaptureBound {
		t.Fatalf("capture state = %q (%s), want %q", receipt.AuthorityState.CaptureState, receipt.AuthorityState.CaptureReason, AuthorityCaptureBound)
	}
	if receipt.AuthorityState.SenseiTreeDirty {
		t.Error("the freeze's own workspace made the governing authority look dirty; " +
			"capture the authority before creating the workspace")
	}
}
