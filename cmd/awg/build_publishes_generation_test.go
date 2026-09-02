// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"strings"
	"testing"
)

// EVERY PATH THAT LOADS THE STORE PUBLISHES THE GENERATION THAT DESCRIBES IT.
//
// There are two: the domain-scoped --repo update and the cold-start --all
// replace. Only --repo published a proof set, so --all rewrote every triple in
// the store and left ~/.sensei/graph/<store> naming the previous publication.
// expectedGraphMarker prefers that store-scoped record over the marker file, so
// briefing and preflight compared the live store against a generation it was
// not built to and refused.
//
// The refusal was correct in FORM and false in FACT, which is the worst kind:
// indistinguishable from a genuinely stale store, so the honest reaction --
// rebuild -- was the one that did not help. Measured 2026-09-02 on a new store
// whose port had last been used three days earlier.
//
// A source-level check rather than an integration one, deliberately. Driving
// the real path needs a live Oxigraph, and a test that skips when one is absent
// is not a check -- it would have been absent in CI for exactly the same reason
// the defect survived.
func TestBothStoreLoadPathsPublishTheGeneration(t *testing.T) {
	b, err := os.ReadFile("cmd_build.go")
	if err != nil {
		t.Fatalf("read cmd_build.go: %v", err)
	}
	src := string(b)

	// REFUSE TO PASS VACUOUSLY. If the anchors are renamed the scan finds
	// nothing and would report success exactly like full coverage.
	const allAnchor = "--all replaces the ENTIRE store"
	const repoAnchor = "Publish the complete proof set for EVERY registered domain"
	if !strings.Contains(src, allAnchor) {
		t.Fatalf("anchor %q not found: this check no longer locates the cold-start path", allAnchor)
	}
	if !strings.Contains(src, repoAnchor) {
		t.Fatalf("anchor %q not found: this check no longer locates the scoped path", repoAnchor)
	}

	// The cold-start path runs from its warning to the end of that function.
	allPath := src[strings.Index(src, allAnchor):]
	if end := strings.Index(allPath, "\n// buildClosureReport"); end > 0 {
		allPath = allPath[:end]
	}
	if !strings.Contains(allPath, "publishProofSet(") {
		t.Error("the cold-start (--all) path loads the store and never publishes a " +
			"generation for it. The store's content is replaced and the record that " +
			"identifies it is not, so every freshness-gated surface compares the live " +
			"store against the publication it replaced.")
	}

	if got := strings.Count(src, "publishProofSet("); got < 2 {
		t.Errorf("publishProofSet is called %d time(s); both store-load paths must "+
			"publish, or one of them leaves the store's identity describing content "+
			"that is gone", got)
	}
}
