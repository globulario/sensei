// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"testing"
)

func intentRepo(t *testing.T) (string, fakeGitTouch) {
	t.Helper()
	dir := t.TempDir()
	// landed implementation code naming a symbol the claim references
	writeFile(t, dir, "golang/repository/desired_state.go", "package repository\nfunc GetDesiredService() {}\n")
	// a test encoding it (executable truth) — references the claimed symbol
	writeFile(t, dir, "golang/repository/desired_state_test.go", "package repository\nfunc TestDesiredReadViaRPC(t *testing.T) { GetDesiredService() }\n")
	// a proto/schema constraint (executable truth)
	writeFile(t, dir, "proto/repository.proto", "// DesiredService is owned here\nmessage DesiredService {}\n")
	// a stated ADR / design doc
	writeFile(t, dir, "docs/architecture/four-layer.md", "# Four-layer truth\nDesiredService owned by repository.\n")
	git := fakeGitTouch{}
	return dir, git
}

// strong_intent: stated (ADR) + grounded (test/code naming the symbol) agree.
func TestIntent_StrongIntent(t *testing.T) {
	dir, git := intentRepo(t)
	c := IntentCandidate{
		IntentID: "repository.desired_owned",
		Claim:    "DesiredService is read via GetDesiredService, not direct storage.",
		Sources:  Sources{Docs: []string{"docs/architecture/four-layer.md"}},
		Evidence: Evidence{
			Code:  []string{"golang/repository/desired_state.go:2"},
			Tests: []string{"golang/repository/desired_state_test.go:2"},
		},
		RelatedInvariants: []string{"four_layer.truth_read_via_owner_rpc"},
	}
	g := GroundIntent(c, dir, git)
	if g.OutputClass != StrongIntent {
		t.Fatalf("want strong_intent, got %s", g.OutputClass)
	}
	if g.GroundingTier != TierExecutableTruth {
		t.Errorf("want executable_truth (test anchor), got %s", g.GroundingTier)
	}
	if g.StatedTier != TierMaintainerIntent {
		t.Errorf("want maintainer_intent (architecture doc), got %s", g.StatedTier)
	}
	// ≥0.80 AND maps to existing invariant → auto-map (advisory).
	if g.Route != RouteAutoMap || g.DecidedBy != "auto" {
		t.Errorf("strong + existing mapping + high certainty → auto_map/auto, got %s/%s", g.Route, g.DecidedBy)
	}
}

// The 80% rule about NEW intent: high certainty but NO existing mapping must
// still route to a human (creating new intent is never auto).
func TestIntent_NewIntent_StaysHuman(t *testing.T) {
	dir, git := intentRepo(t)
	c := IntentCandidate{
		IntentID: "repository.desired_owned_novel",
		Claim:    "DesiredService is read via GetDesiredService.",
		Sources:  Sources{Docs: []string{"docs/architecture/four-layer.md"}},
		Evidence: Evidence{Tests: []string{"golang/repository/desired_state_test.go:2"}},
		// no RelatedInvariants / RelatedMetaPrinciples → novel
	}
	g := GroundIntent(c, dir, git)
	if g.OutputClass != StrongIntent {
		t.Fatalf("precondition: want strong_intent, got %s", g.OutputClass)
	}
	if g.Certainty < 0.80 {
		t.Fatalf("precondition: want high certainty, got %.2f", g.Certainty)
	}
	if g.Route != RouteHuman || g.DecidedBy != "human" {
		t.Errorf("NEW intent (no existing mapping) must stay human even at ≥0.80, got %s/%s", g.Route, g.DecidedBy)
	}
}

// hidden_intent: code/test encode it, but no doc states it.
func TestIntent_HiddenIntent(t *testing.T) {
	dir, git := intentRepo(t)
	c := IntentCandidate{
		IntentID: "repository.desired_hidden",
		Claim:    "GetDesiredService is the owner entrypoint.",
		Evidence: Evidence{Code: []string{"golang/repository/desired_state.go:2"}},
		// no Sources
	}
	g := GroundIntent(c, dir, git)
	if g.OutputClass != HiddenIntent {
		t.Fatalf("want hidden_intent, got %s", g.OutputClass)
	}
}

// stale_intent: a doc states it, but the cited code does NOT contain the claimed
// symbol (docs vs code disagree).
func TestIntent_StaleIntent_SymbolAbsent(t *testing.T) {
	dir, git := intentRepo(t)
	c := IntentCandidate{
		IntentID: "repository.desired_stale",
		Claim:    "Reads go through GetLegacyDesiredViaStorage, the old owner.",
		Sources:  Sources{Docs: []string{"docs/architecture/four-layer.md"}},
		Evidence: Evidence{Code: []string{"golang/repository/desired_state.go:2"}}, // symbol absent here
	}
	g := GroundIntent(c, dir, git)
	if g.OutputClass != StaleIntent {
		t.Fatalf("want stale_intent (doc names a symbol the code lacks), got %s", g.OutputClass)
	}
	if !g.SymbolMismatch {
		t.Errorf("expected SymbolMismatch")
	}
	if g.Route != RouteHuman {
		t.Errorf("findings always route human, got %s", g.Route)
	}
}

// ambiguous_owner: two distinct owners declared for one truth.
func TestIntent_AmbiguousOwner(t *testing.T) {
	dir, git := intentRepo(t)
	c := IntentCandidate{
		IntentID: "node.stable_ip_owner",
		Claim:    "The node's stable IP is owned by ...",
		Owners:   []string{"PrimaryIP", "StableIP"},
		Evidence: Evidence{Code: []string{"golang/repository/desired_state.go:2"}},
	}
	g := GroundIntent(c, dir, git)
	if g.OutputClass != AmbiguousOwner {
		t.Fatalf("want ambiguous_owner, got %s", g.OutputClass)
	}
	if g.Route != RouteHuman {
		t.Errorf("ambiguous_owner must route human, got %s", g.Route)
	}
}

// missing_invariant: scars imply it, no doc, no anchor.
func TestIntent_MissingInvariant(t *testing.T) {
	dir, git := intentRepo(t)
	c := IntentCandidate{
		IntentID:   "diagnostics.output_bounded",
		Claim:      "Diagnostic output must be bounded.",
		ScarsImply: true,
		// no Sources, no Evidence
	}
	g := GroundIntent(c, dir, git)
	if g.OutputClass != MissingInvariant {
		t.Fatalf("want missing_invariant, got %s", g.OutputClass)
	}
}

// ungrounded_claim: stated only, no anchor proves it.
func TestIntent_UngroundedClaim(t *testing.T) {
	dir, git := intentRepo(t)
	c := IntentCandidate{
		IntentID: "some.unproven_intent",
		Claim:    "This SomeBehavior must hold.",
		Sources:  Sources{Docs: []string{"README.md"}},
		// no Evidence
	}
	g := GroundIntent(c, dir, git)
	if g.OutputClass != UngroundedClaim {
		t.Fatalf("want ungrounded_claim, got %s", g.OutputClass)
	}
	if g.StatedTier != TierDocsOnly {
		t.Errorf("README → docs_only, got %s", g.StatedTier)
	}
}

// proto/schema anchor is executable truth (Tier 1).
func TestIntent_ProtoIsExecutableTruth(t *testing.T) {
	dir, git := intentRepo(t)
	c := IntentCandidate{
		IntentID: "repository.proto_contract",
		Claim:    "DesiredService is a contract.",
		Sources:  Sources{Docs: []string{"docs/architecture/four-layer.md"}},
		Evidence: Evidence{Code: []string{"proto/repository.proto:2"}},
	}
	g := GroundIntent(c, dir, git)
	if g.GroundingTier != TierExecutableTruth {
		t.Fatalf("a resolving .proto constraint must tier as executable_truth, got %s", g.GroundingTier)
	}
}

// source tiering: ADR/architecture → maintainer_intent; README → docs_only.
func TestClassifySourceTier(t *testing.T) {
	cases := map[string]TrustTier{
		"docs/adr/0007-ownership.md":       TierMaintainerIntent,
		"docs/architecture/four-layer.md":  TierMaintainerIntent,
		"docs/design/rfc-routing.md":       TierMaintainerIntent,
		"README.md":                        TierDocsOnly,
		"docs/tutorial/getting-started.md": TierDocsOnly,
		"pkg/foo/naming-convention":        TierWeakHint,
	}
	for path, want := range cases {
		if got := classifySourceTier(path); got != want {
			t.Errorf("classifySourceTier(%q) = %s, want %s", path, got, want)
		}
	}
}
