// SPDX-License-Identifier: Apache-2.0

package coldsource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/awareness-graph/golang/extractor"
)

// #7 — a corpus of ONLY revert signals (one source type) yields zero eligible
// bundles, no matter how many themes. Reverts alone never auto-draft.
func TestTriangulate_RevertsOnly_NoEligible(t *testing.T) {
	var signals []ColdSignal
	for _, theme := range []string{"a", "b", "c", "a", "b"} { // repeats included
		signals = append(signals, ColdSignal{
			SourceType: SourceRevertCommit, ThemeKey: "repo." + theme,
			CommitSHA: "sha-" + theme, MatchedText: "Revert something on " + theme,
		})
	}
	eligible, held := Triangulate(signals)
	if len(eligible) != 0 {
		t.Fatalf("reverts-only must yield 0 eligible bundles, got %d: %+v", len(eligible), eligible)
	}
	if len(held) == 0 {
		t.Fatalf("reverts-only should still produce held-back bundles")
	}
	for _, b := range held {
		if len(b.SourceTypes) != 1 || b.SourceTypes[0] != SourceRevertCommit {
			t.Errorf("held bundle %q should be single-source revert, got %v", b.ThemeKey, b.SourceTypes)
		}
	}
}

// #1/#2 — the cage: EmitCandidate refuses any output path that is not under a
// candidates/ tree, and forces status:candidate + promoted_from:cold_source.
func TestEmitCandidate_CageGuard(t *testing.T) {
	p := &extractor.PromotionProposal{
		CandidateID: "candidate.repo.x", CandidateClass: extractor.CandidateForbiddenFix,
		Status: "candidate", Theme: "repo.x", SourcePaths: []string{"commit:abc"},
	}

	// Reject: not under candidates/.
	bad := filepath.Join(t.TempDir(), "docs", "awareness", "invariants")
	if _, err := EmitCandidate(bad, p); err == nil {
		t.Fatalf("expected refusal to write outside candidates/ tree")
	}

	// Accept: under candidates/.
	good := filepath.Join(t.TempDir(), "docs", "awareness", "candidates")
	path, err := EmitCandidate(good, p)
	if err != nil {
		t.Fatalf("emit under candidates/: %v", err)
	}
	if !strings.Contains(filepath.ToSlash(path), "/candidates/") {
		t.Errorf("written path must be under candidates/: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "status: candidate") {
		t.Errorf("emitted YAML must carry status: candidate; got:\n%s", body)
	}
	if !strings.Contains(body, "promoted_from: cold_source") {
		t.Errorf("emitted YAML must record promoted_from: cold_source")
	}
}

// EmitCandidate must force status:candidate even if a (mis)drafted proposal
// arrives with an active status — the on-disk node can never be active.
func TestEmitCandidate_ForcesCandidateStatus(t *testing.T) {
	p := &extractor.PromotionProposal{
		CandidateID: "candidate.repo.y", CandidateClass: extractor.CandidateInvariant,
		Status: "active", Theme: "repo.y", SourcePaths: []string{"file:x.go:1"},
	}
	dir := filepath.Join(t.TempDir(), "candidates")
	path, err := EmitCandidate(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "status: active") {
		t.Fatalf("emit must never write status: active")
	}
	if !strings.Contains(string(data), "status: candidate") {
		t.Fatalf("emit must force status: candidate")
	}
}

// #4 — a fabricated provenance id (a pr citation not present in the bundle) is
// rejected by the contract check even though pr ids are not network-verifiable.
func TestValidateDraft_RejectsFabricatedPRId(t *testing.T) {
	b := Bundle{
		ThemeKey:    "t",
		SourceTypes: []SourceType{SourcePRReview, SourceRevertCommit},
		Signals: []ColdSignal{
			{SourceType: SourcePRReview, PRID: "7", CommentID: "9"},
			{SourceType: SourceRevertCommit, CommitSHA: "abc"},
		},
	}
	p := &extractor.PromotionProposal{
		Status: "candidate", CandidateClass: "X",
		SourcePaths: []string{"pr:999:000"}, // fabricated — not in bundle
	}
	if v := ValidateDraft(p, b); len(v) == 0 {
		t.Fatalf("expected rejection of fabricated pr id not present in bundle")
	}
}
