// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"os"
	"path/filepath"
	"testing"
)

// coldsource → intent: a scar with a real code anchor (symbol present) but no
// stated source grounds to hidden_intent (encoded, undocumented).
func TestScarToIntent_Hidden(t *testing.T) {
	dir, git := intentRepo(t)
	sc := ScarToIntent("c1", "forbidden_fix", "repo.desired",
		"GetDesiredService is the owner entrypoint",
		[]string{"file:golang/repository/desired_state.go:2"})
	if !sc.ScarsImply || len(sc.Sources.Docs) != 0 {
		t.Fatalf("scar candidate must set ScarsImply and have no stated sources: %+v", sc)
	}
	if len(sc.Evidence.Code) != 1 {
		t.Fatalf("file citation should route to Evidence.Code: %+v", sc.Evidence)
	}
	g := GroundIntent(sc, dir, git)
	if g.OutputClass != HiddenIntent {
		t.Fatalf("grounded scar with no doc → hidden_intent, got %s", g.OutputClass)
	}
}

// coldsource → intent: a scar whose only citation is a PR review (dropped) has no
// resolving evidence → missing_invariant (scars imply it; nothing encodes it).
func TestScarToIntent_Missing(t *testing.T) {
	dir, git := intentRepo(t)
	sc := ScarToIntent("c2", "failure_mode", "diag.bounded",
		"diagnostic output must be bounded", []string{"pr:9:9"})
	if len(sc.Evidence.Code)+len(sc.Evidence.Commits)+len(sc.Evidence.Tests) != 0 {
		t.Fatalf("pr-only scar must drop the review citation: %+v", sc.Evidence)
	}
	g := GroundIntent(sc, dir, git)
	if g.OutputClass != MissingInvariant {
		t.Fatalf("ungrounded scar with ScarsImply → missing_invariant, got %s", g.OutputClass)
	}
}

// intent → coldsource: divergence findings yield finder hints; clean candidates
// do not.
func TestFinderHintsFromGroundings(t *testing.T) {
	dir, git := intentRepo(t)
	stale := IntentCandidate{
		IntentID: "x.stale",
		Claim:    "Reads go through GetLegacyDesiredViaStorage",
		Sources:  Sources{Docs: []string{"docs/architecture/four-layer.md"}},
		Evidence: Evidence{Code: []string{"file:golang/repository/desired_state.go:2"}},
	}
	gStale := GroundIntent(stale, dir, git)
	if gStale.OutputClass != StaleIntent {
		t.Fatalf("precondition: want stale_intent, got %s", gStale.OutputClass)
	}
	strong := IntentCandidate{
		IntentID: "x.strong",
		Claim:    "GetDesiredService is the owner",
		Sources:  Sources{Docs: []string{"docs/architecture/four-layer.md"}},
		Evidence: Evidence{Tests: []string{"file:golang/repository/desired_state_test.go:2"}},
	}
	gStrong := GroundIntent(strong, dir, git)

	hints := FinderHintsFromGroundings([]IntentGrounding{gStale, gStrong})
	if len(hints) != 1 {
		t.Fatalf("want 1 hint (from stale only), got %d: %+v", len(hints), hints)
	}
	if hints[0].File != "golang/repository/desired_state.go" || hints[0].Class != StaleIntent {
		t.Errorf("hint should point at the divergent file: %+v", hints[0])
	}
}

// LoadColdsourceAsIntent reads a coldsource candidate YAML and lifts each.
func TestLoadColdsourceAsIntent(t *testing.T) {
	dir := t.TempDir()
	yaml := `candidates:
  - id: cs.one
    class: forbidden_fix
    theme: repo.desired
    reason: GetDesiredService owns desired state
    source_paths: ["file:golang/repository/desired_state.go:2", "commit:abc", "pr:1:2"]
`
	path := filepath.Join(dir, "cs.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cands, err := LoadColdsourceAsIntent(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 {
		t.Fatalf("want 1 lifted candidate, got %d", len(cands))
	}
	c := cands[0]
	if !c.ScarsImply {
		t.Error("lifted candidate must set ScarsImply")
	}
	if len(c.Evidence.Code) != 1 || len(c.Evidence.Commits) != 1 {
		t.Errorf("file→Code, commit→Commits, pr dropped: %+v", c.Evidence)
	}
}
