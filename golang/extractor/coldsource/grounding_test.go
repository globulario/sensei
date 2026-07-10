// SPDX-License-Identifier: Apache-2.0

package coldsource

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness-graph/golang/extractor"
)

// fakeGitTouch is a GitVerifier that also reports commit→path touches, so the
// commit-relatedness check is exercised without a real repo.
type fakeGitTouch struct {
	known   map[string]bool
	touches map[string]map[string]bool // sha -> path -> touched
}

func (f fakeGitTouch) CommitExists(sha string) bool { return f.known[sha] }
func (f fakeGitTouch) CommitTouchesPath(sha, path string) bool {
	return f.touches[sha] != nil && f.touches[sha][path]
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	abs := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ground(reason string, cites ...string) *extractor.PromotionProposal {
	return &extractor.PromotionProposal{Reason: reason, SourcePaths: cites}
}

// claimedSymbols must pull code-like identifiers from prose and ignore English
// words and all-caps acronyms.
func TestClaimedSymbols(t *testing.T) {
	got := claimedSymbols("makeMounts must accumulate cleanupActions; WhenDeleted stays optional. " +
		"The dropHTTPProbeProtocol path and PodStatusPatchCall matter. Not over HTTPS or via RBAC.")
	want := map[string]bool{
		"makeMounts": true, "cleanupActions": true, "WhenDeleted": true,
		"dropHTTPProbeProtocol": true, "PodStatusPatchCall": true,
	}
	for _, s := range got {
		if !want[s] {
			t.Errorf("unexpected symbol extracted: %q (all-caps/English should be excluded)", s)
		}
		delete(want, s)
	}
	for s := range want {
		t.Errorf("expected symbol not extracted: %q", s)
	}
}

// landed_commit: a non-test source file whose claimed symbol is present.
func TestGround_LandedCommit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pkg/kubelet/kubelet_pods.go", "package kubelet\nfunc makeMounts() {}\n")
	git := fakeGitTouch{}
	g := GroundCandidate(ground("makeMounts must register cleanupActions", "file:pkg/kubelet/kubelet_pods.go:2"), dir, git)
	if g.Overall != TierLandedCommit {
		t.Fatalf("want landed_commit, got %s", g.Overall)
	}
	if g.SymbolMismatch {
		t.Errorf("symbol present, should not be a mismatch")
	}
}

// test_encoded (gold): a test file whose claimed symbol is present.
func TestGround_TestEncoded(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pkg/kubelet/kubelet_pods_test.go", "package kubelet\n// trackingSubpath forces partial-failure cleanup\nfunc trackingSubpath() {}\n")
	g := GroundCandidate(ground("trackingSubpath asserts cleanup on partial failure", "file:pkg/kubelet/kubelet_pods_test.go:3"), dir, fakeGitTouch{})
	if g.Overall != TierTestEncoded {
		t.Fatalf("want test_encoded, got %s", g.Overall)
	}
}

// The k8s #1/#5 catch: cited file resolves and the line is in range, but the
// claimed symbol is ABSENT — must demote to unresolved, not pass as today's cage.
func TestGround_SymbolAbsent_Demotes(t *testing.T) {
	dir := t.TempDir()
	// validation.go exists, line 2 in range — but it is unrelated code; the
	// claimed dropHTTPProbeProtocol is nowhere in it.
	writeFile(t, dir, "pkg/apis/core/validation/validation.go", "package validation\nfunc ValidatePortNumOrName() {}\n")
	g := GroundCandidate(ground(
		"validation must not gate on dropHTTPProbeProtocol feature state",
		"file:pkg/apis/core/validation/validation.go:2"), dir, fakeGitTouch{})
	if g.Overall != TierUnresolved {
		t.Fatalf("symbol-absent citation must be unresolved, got %s", g.Overall)
	}
	if !g.SymbolMismatch {
		t.Errorf("expected SymbolMismatch flag")
	}
}

// review_suggestion: only a pr: citation (plus a symbol-absent file) → segregated.
func TestGround_ReviewOnly_Segregated(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "test/integration/scheduler_perf/scheduler_perf.go", "package benchmark\nfunc getSpecFromFile() {}\n")
	g := GroundCandidate(ground(
		"defer cleanup plus a mutex around churnCancels",
		"file:test/integration/scheduler_perf/scheduler_perf.go:2", "pr:139441:3379370553"), dir, fakeGitTouch{})
	if g.Overall != TierReviewSuggestion {
		t.Fatalf("want review_suggestion (pr-only after symbol-absent file), got %s", g.Overall)
	}
}

// unresolved: missing file and unknown commit → rejected outright.
func TestGround_Unresolved(t *testing.T) {
	dir := t.TempDir()
	g := GroundCandidate(ground("anything", "file:does/not/exist.go:1", "commit:deadbeef"), dir, fakeGitTouch{})
	if g.Overall != TierUnresolved {
		t.Fatalf("want unresolved, got %s", g.Overall)
	}
}

// commit relatedness: a real SHA that touches the cited file grounds at
// landed_commit; a real SHA touching nothing cited is only a review_suggestion.
func TestGround_CommitTouchesFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "apps/v1/types.go", "package v1\n// WhenDeleted optional\n")
	related := fakeGitTouch{
		known:   map[string]bool{"abc123": true},
		touches: map[string]map[string]bool{"abc123": {"apps/v1/types.go": true}},
	}
	// commit alone, touching the cited file → landed_commit.
	g := GroundCandidate(ground("WhenDeleted markers", "commit:abc123", "file:apps/v1/types.go:2"), dir, related)
	if g.Overall != TierLandedCommit {
		t.Fatalf("want landed_commit for related commit, got %s", g.Overall)
	}

	// Laundering shape: cited file resolves but its claimed symbol is absent
	// (→ unresolved), and a real SHA touches none of the cited files
	// (→ review_suggestion). Overall must not exceed review_suggestion.
	unrelated := fakeGitTouch{known: map[string]bool{"abc123": true}} // touches nothing
	g2 := GroundCandidate(ground("the doFrobnicate rule", "commit:abc123", "file:apps/v1/types.go:2"), dir, unrelated)
	if g2.Overall != TierReviewSuggestion {
		t.Fatalf("unrelated commit + symbol-absent file must not exceed review_suggestion, got %s", g2.Overall)
	}
}

// A draft that names no code-like symbol must still ground on its citations —
// "no symbol" must never silently demote a real landed-commit candidate.
func TestGround_NoSymbol_NoDemotion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "src/foo.go", "package foo\nvar x = 1\n")
	g := GroundCandidate(ground("a generic rule with no identifier named", "file:src/foo.go:1"), dir, fakeGitTouch{})
	if g.Overall != TierLandedCommit {
		t.Fatalf("no-symbol candidate should ground on its file citation (landed_commit), got %s", g.Overall)
	}
	if g.SymbolMismatch {
		t.Errorf("no claimed symbols → no mismatch")
	}
}

// Drift: the claimed symbol exists but far from the cited line → grounded, flagged.
func TestGround_Drift(t *testing.T) {
	dir := t.TempDir()
	lines := "package p\n"
	for i := 0; i < 60; i++ {
		lines += "// filler\n"
	}
	lines += "func makeMounts() {}\n" // ~line 62
	writeFile(t, dir, "src/m.go", lines)
	g := GroundCandidate(ground("makeMounts cleanup", "file:src/m.go:2"), dir, fakeGitTouch{})
	if g.Overall != TierLandedCommit {
		t.Fatalf("want landed_commit, got %s", g.Overall)
	}
	if !g.Drifted {
		t.Errorf("expected Drifted flag (symbol ~60 lines from cited line 2)")
	}
}
