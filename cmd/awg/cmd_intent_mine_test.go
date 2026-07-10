// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/extractor/coldsource"
)

// TestApplyIntentGroundings_RoundTrip proves --apply's contract end to end:
// a strong_intent ≥0.80 is written as an importable intent file that the real
// importer turns into aw:Intent triples; a sub-bar/finding candidate is parked
// under candidates/ and the importer skips it (no triples).
func TestApplyIntentGroundings_RoundTrip(t *testing.T) {
	repo := t.TempDir()

	cands := []coldsource.IntentCandidate{
		{
			IntentID: "build-shell-once",
			Claim:    "The DOM shell must be built exactly once and never rebuilt on refresh.",
			Category: "ui-truth",
			Evidence: coldsource.Evidence{Code: []string{"file:apps/web/src/pages/cluster_nodes.ts"}},
		},
		{
			IntentID: "maybe-poll-faster",
			Claim:    "Polling might be faster somewhere.",
			Category: "performance",
		},
	}
	groundings := []coldsource.IntentGrounding{
		{IntentID: "build-shell-once", OutputClass: coldsource.StrongIntent, Certainty: 0.91},
		{IntentID: "maybe-poll-faster", OutputClass: coldsource.StaleIntent, Certainty: 0.40},
	}

	landed, parked, skipped, err := applyIntentGroundings(repo, cands, groundings)
	if err != nil {
		t.Fatalf("applyIntentGroundings: %v", err)
	}
	if landed != 1 || parked != 1 || skipped != 0 {
		t.Fatalf("landed=%d parked=%d skipped=%d, want 1/1/0", landed, parked, skipped)
	}

	awDir := filepath.Join(repo, "docs", "awareness")

	// The strong intent is an importable single-entity file (id + level).
	intentFile := filepath.Join(awDir, "intent_build_shell_once.yaml")
	raw, err := os.ReadFile(intentFile)
	if err != nil {
		t.Fatalf("expected applied intent file: %v", err)
	}
	for _, want := range []string{"id: intent.build_shell_once", "level: constraint", "intent:", "status: active"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("applied intent file missing %q:\n%s", want, raw)
		}
	}

	// The sub-bar candidate is parked under candidates/.
	parkedRaw, err := os.ReadFile(filepath.Join(awDir, "candidates", "intents.yaml"))
	if err != nil {
		t.Fatalf("expected parked candidate file: %v", err)
	}
	if !strings.Contains(string(parkedRaw), "intent.maybe_poll_faster") || !strings.Contains(string(parkedRaw), "status: candidate") {
		t.Errorf("parked candidate file wrong:\n%s", parkedRaw)
	}

	// Round-trip: the real importer turns the applied file into aw:Intent triples,
	// and SKIPS the parked candidate (no triples for it).
	var buf bytes.Buffer
	e, _, err := extractor.ImportAwarenessDir(awDir, &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessDir: %v", err)
	}
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "intent/intent.build_shell_once") || !strings.Contains(out, "#Intent>") {
		t.Errorf("applied intent did not import as aw:Intent triples:\n%s", out)
	}
	if strings.Contains(out, "maybe_poll_faster") {
		t.Errorf("parked candidate leaked into the graph (candidates/ must be skipped)")
	}
}

// TestApplyIntentGroundings_Idempotent: a second apply of the same passing
// intent does not rewrite it (skipped), so re-runs are safe.
func TestApplyIntentGroundings_Idempotent(t *testing.T) {
	repo := t.TempDir()
	cands := []coldsource.IntentCandidate{{IntentID: "x-rule", Claim: "X must hold."}}
	gs := []coldsource.IntentGrounding{{IntentID: "x-rule", OutputClass: coldsource.StrongIntent, Certainty: 0.85}}

	if l, _, _, err := applyIntentGroundings(repo, cands, gs); err != nil || l != 1 {
		t.Fatalf("first apply: landed=%d err=%v, want 1/nil", l, err)
	}
	l, _, skipped, err := applyIntentGroundings(repo, cands, gs)
	if err != nil || l != 0 || skipped != 1 {
		t.Fatalf("second apply: landed=%d skipped=%d err=%v, want 0/1/nil", l, skipped, err)
	}
}

// TestCanonicalIntentID normalizes mined ids to the canonical dotted form.
func TestCanonicalIntentID(t *testing.T) {
	for in, want := range map[string]string{
		"build-shell-once":        "intent.build_shell_once",
		"intent.build-shell-once": "intent.build_shell_once",
		"Preserve Cached Data":    "intent.preserve_cached_data",
		"session_storage_auth":    "intent.session_storage_auth",
	} {
		if got := canonicalIntentID(in); got != want {
			t.Errorf("canonicalIntentID(%q) = %q, want %q", in, got, want)
		}
	}
}
