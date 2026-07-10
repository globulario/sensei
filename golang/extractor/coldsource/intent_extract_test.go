// SPDX-License-Identifier: Apache-2.0

package coldsource

import (
	"context"
	"testing"
)

func extractRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// docs: a rule-bearing line + a non-rule line
	writeFile(t, dir, "docs/architecture/ownership.md",
		"# Ownership\nDesiredService is owned by the repository and must be read via its API.\nThis paragraph is just prose with no rule.\n")
	// code comment with rule language + a plain comment
	writeFile(t, dir, "golang/repo/desired.go",
		"package repo\n// GetDesiredService is the sole source of truth for desired state.\n// just a helper\nfunc GetDesiredService() {}\n")
	// schema comment
	writeFile(t, dir, "proto/repo.proto",
		"// DesiredService must not be read from storage directly.\nmessage DesiredService {}\n")
	// test
	writeFile(t, dir, "golang/repo/desired_test.go",
		"package repo\n// the owner RPC must be used\nfunc TestOwnerRPC(t *testing.T) { GetDesiredService() }\n")
	return dir
}

func TestGatherIntentExcerpts(t *testing.T) {
	dir := extractRepo(t)
	ex := GatherIntentExcerpts(dir, []string{"docs", "comments", "schemas", "tests"}, nil, 0)
	byKind := map[string]int{}
	for _, e := range ex {
		byKind[e.Kind]++
		if e.Citation == "" || e.Text == "" {
			t.Errorf("excerpt missing citation/text: %+v", e)
		}
	}
	for _, k := range []string{"docs", "comments", "schemas", "tests"} {
		if byKind[k] == 0 {
			t.Errorf("expected at least one %s excerpt, got %d", k, byKind[k])
		}
	}
	// the non-rule prose line must NOT be gathered
	for _, e := range ex {
		if e.Text == "This paragraph is just prose with no rule." {
			t.Errorf("non-rule line should not be gathered")
		}
	}
}

func TestIntentCage_RejectsFabricated(t *testing.T) {
	allowed := map[string]string{"file:docs/x.md:2": "real"}
	good := intentDraft{Claim: "x", SourceCitations: []string{"file:docs/x.md:2"}}
	if v := validateIntentDraft(good, allowed); len(v) != 0 {
		t.Errorf("valid draft rejected: %v", v)
	}
	bad := intentDraft{Claim: "x", SourceCitations: []string{"file:fabricated.md:99"}}
	if v := validateIntentDraft(bad, allowed); len(v) == 0 {
		t.Errorf("fabricated source citation must be rejected")
	}
	none := intentDraft{Claim: "x"}
	if v := validateIntentDraft(none, allowed); len(v) == 0 {
		t.Errorf("missing source citation must be rejected")
	}
}

func TestEchoIntentDrafter_CitesOnlyProvided(t *testing.T) {
	ex := []IntentExcerpt{
		{Kind: "docs", Citation: "file:docs/a.md:1", Text: "must own X"},
		{Kind: "comments", Citation: "file:b.go:2", Text: "never do Y"},
	}
	allowed, _ := excerptIndex(ex)
	drafts, _ := EchoIntentDrafter{Max: 10}.DraftIntents(context.Background(), ex)
	if len(drafts) != 2 {
		t.Fatalf("want 2 drafts, got %d", len(drafts))
	}
	for _, d := range drafts {
		if v := validateIntentDraft(d, allowed); len(v) != 0 {
			t.Errorf("echo draft failed the cage (must only cite provided): %v", v)
		}
	}
}

func TestMaterialize_RoutesByKind(t *testing.T) {
	kind := map[string]string{
		"file:docs/a.md:1": "docs",
		"file:b.go:2":      "comments",
		"file:c_test.go:3": "tests",
		"commit:deadbeef":  "commits",
		"pr:42:7":          "prs",
	}
	d := intentDraft{
		Claim:           "x",
		SourceCitations: []string{"file:docs/a.md:1", "file:b.go:2", "file:c_test.go:3", "commit:deadbeef", "pr:42:7"},
		CodeAnchors:     []string{"file:impl.go", "file:impl_test.go"},
	}
	c := materialize(d, kind)
	// docs + pr → Sources.Docs ; comments → Sources.Comments
	if len(c.Sources.Docs) != 2 || len(c.Sources.Comments) != 1 {
		t.Errorf("sources routing wrong: docs=%v comments=%v", c.Sources.Docs, c.Sources.Comments)
	}
	// tests source + test anchor → Evidence.Tests ; commit → Evidence.Commits ; non-test anchor → Evidence.Code
	if len(c.Evidence.Tests) != 2 || len(c.Evidence.Commits) != 1 || len(c.Evidence.Code) != 1 {
		t.Errorf("evidence routing wrong: tests=%v commits=%v code=%v", c.Evidence.Tests, c.Evidence.Commits, c.Evidence.Code)
	}
	if c.Status != "candidate" || !c.ExtractedByLLM {
		t.Errorf("materialized candidate must be status=candidate, extracted")
	}
}

func TestCitationToPath(t *testing.T) {
	cases := map[string]string{
		"file:docs/a.md:42": "docs/a.md",
		"file:pkg/x.go":     "pkg/x.go",
		"docs/b.md:3":       "docs/b.md",
		"commit:abc":        "commit:abc", // not a file:, no trailing line → unchanged
	}
	for in, want := range cases {
		if got := citationToPath(in); got != want {
			t.Errorf("citationToPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// End to end: gather → echo draft → cage → materialize → ground produces an
// output class with no fabricated citation surviving.
func TestExtract_EndToEnd_Echo(t *testing.T) {
	dir := extractRepo(t)
	ex := GatherIntentExcerpts(dir, []string{"docs", "comments", "schemas", "tests"}, nil, 0)
	cands, rejected, err := DraftAndCageIntents(context.Background(), EchoIntentDrafter{Max: 20}, ex, 20)
	if err != nil {
		t.Fatalf("echo draft: %v", err)
	}
	if rejected != 0 {
		t.Errorf("echo never fabricates; cage rejected %d", rejected)
	}
	if len(cands) == 0 {
		t.Fatal("expected candidates")
	}
	git := fakeGitTouch{}
	sawGrounded := false
	for _, c := range cands {
		g := GroundIntent(c, dir, git)
		if g.GroundingTier >= TierLandedBehavior {
			sawGrounded = true
		}
	}
	if !sawGrounded {
		t.Error("expected at least one candidate to ground at >= landed_behavior (real code/proto/test anchors exist)")
	}
}
