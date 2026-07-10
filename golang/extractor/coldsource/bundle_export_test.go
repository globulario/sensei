// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/globulario/awareness-graph/golang/extractor"
)

// BundleID is deterministic and sensitive to theme + evidence.
func TestBundleID_DeterministicAndSensitive(t *testing.T) {
	b := llmBundle("repo.x")
	if b.BundleID() != b.BundleID() {
		t.Fatal("BundleID must be deterministic")
	}
	if llmBundle("repo.y").BundleID() == b.BundleID() {
		t.Error("theme change must change the id")
	}
	b3 := llmBundle("repo.x")
	b3.Signals[0].MatchedText = "an entirely different rule statement"
	if b3.BundleID() == b.BundleID() {
		t.Error("evidence (matched text) change must change the id")
	}
}

// Export envelope carries the bundle, the schema, and the prompt contract.
func TestExportEnvelope_Shape(t *testing.T) {
	env := NewExportEnvelope([]Bundle{llmBundle("repo.x")})
	if len(env.Bundles) != 1 {
		t.Fatalf("want 1 bundle, got %d", len(env.Bundles))
	}
	be := env.Bundles[0]
	if be.BundleID == "" || be.Theme != "repo.x" || len(be.AllowedCitations) == 0 {
		t.Fatalf("bad export: %+v", be)
	}
	if len(env.CandidateSchema) == 0 || strings.TrimSpace(env.PromptContract) == "" {
		t.Error("export must include candidate_schema and prompt_contract")
	}
}

// Reconstructing a bundle from its export reproduces the id and allowed-citation
// set — so validate-draft's oracle validates against an equivalent bundle.
func TestBundleFromExport_RoundTrip(t *testing.T) {
	b := llmBundle("repo.x")
	rb := BundleFromExport(ExportBundle(b))
	if rb.BundleID() != b.BundleID() {
		t.Fatalf("reconstructed id %s != original %s", rb.BundleID(), b.BundleID())
	}
	a := strings.Join(sortedKeys(b.AllowedCitations()), "|")
	c := strings.Join(sortedKeys(rb.AllowedCitations()), "|")
	if a != c {
		t.Errorf("allowed citations differ:\n  orig: %s\n  recon: %s", a, c)
	}
}

// A matched draft maps to a candidate (status forced) and passes the cage.
func TestStdinDrafter_MatchValidatesAndForcesCandidate(t *testing.T) {
	b := llmBundle("repo.x")
	cits := sortedKeys(b.AllowedCitations())
	d := SubmittedDraft{
		BundleID: b.BundleID(), CandidateClass: extractor.CandidateForbiddenFix,
		Reason: "a sufficiently long, load-bearing reason", Confidence: "high",
		SourcePaths: []string{cits[0]},
	}
	p, err := NewStdinDrafter([]SubmittedDraft{d}).Draft(context.Background(), b)
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	if p.Status != "candidate" {
		t.Errorf("status must be forced to candidate, got %q", p.Status)
	}
	if v := ValidateDraft(p, b); len(v) != 0 {
		t.Fatalf("matched draft should pass the cage: %v", v)
	}
}

// A draft bound to a different bundle_id (stale/missing) is rejected.
func TestStdinDrafter_StaleOrMissingBundleRejected(t *testing.T) {
	b := llmBundle("repo.x")
	d := SubmittedDraft{BundleID: "deadbeefdeadbeef", CandidateClass: extractor.CandidateInvariant, SourcePaths: []string{"commit:abc123"}}
	_, err := NewStdinDrafter([]SubmittedDraft{d}).Draft(context.Background(), b)
	var de DraftError
	if !errors.As(err, &de) || de.Kind != "no_draft_supplied" {
		t.Fatalf("expected no_draft_supplied for stale/missing bundle, got %v", err)
	}
}

// Fabricated citation in a matched draft is still rejected by the cage.
func TestStdinDrafter_FabricatedCitationRejectedByCage(t *testing.T) {
	b := llmBundle("repo.x")
	d := SubmittedDraft{
		BundleID: b.BundleID(), CandidateClass: extractor.CandidateInvariant,
		Reason: "x", SourcePaths: []string{"file:not-in-bundle.go:1"},
	}
	p, err := NewStdinDrafter([]SubmittedDraft{d}).Draft(context.Background(), b)
	if err != nil {
		t.Fatalf("draft (map) should succeed: %v", err)
	}
	if v := ValidateDraft(p, b); len(v) == 0 {
		t.Fatalf("fabricated citation must be rejected by ValidateDraft")
	}
}

// Unknown class is rejected at map time.
func TestStdinDrafter_BadClassRejected(t *testing.T) {
	b := llmBundle("repo.x")
	d := SubmittedDraft{BundleID: b.BundleID(), CandidateClass: "WhateverCandidate", SourcePaths: []string{"commit:abc123"}}
	_, err := NewStdinDrafter([]SubmittedDraft{d}).Draft(context.Background(), b)
	var de DraftError
	if !errors.As(err, &de) || de.Kind != "bad_class" {
		t.Fatalf("expected bad_class, got %v", err)
	}
}

func TestParseSubmittedDrafts_ArraySingleMalformed(t *testing.T) {
	arr, err := ParseSubmittedDrafts(strings.NewReader(`[{"bundle_id":"a","candidate_class":"InvariantCandidate"}]`))
	if err != nil || len(arr) != 1 || arr[0].BundleID != "a" {
		t.Fatalf("array parse: %+v err=%v", arr, err)
	}
	one, err := ParseSubmittedDrafts(strings.NewReader(`{"bundle_id":"b","candidate_class":"InvariantCandidate"}`))
	if err != nil || len(one) != 1 || one[0].BundleID != "b" {
		t.Fatalf("single parse: %+v err=%v", one, err)
	}
	if _, err := ParseSubmittedDrafts(strings.NewReader("not json at all")); err == nil {
		t.Error("malformed input must error")
	}
}
