// SPDX-License-Identifier: Apache-2.0

package coldsource

import "testing"

// Non-Go test-file conventions must normalize to the same concept stem as their
// sibling, so a fix on foo and a comment on its test land on one theme. The
// caller strips the outer extension before conceptStem sees the name.
func TestConceptStem_NonGoTestNormalization(t *testing.T) {
	cases := map[string]string{
		// TS/JS: outer ext already stripped by themeFromPath -> "foo.test", etc.
		"foo.test": "foo",
		"foo.spec": "foo",
		// Python suffix + prefix conventions.
		"foo_test": "foo",
		"test_foo": "foo",
		// Go still works.
		"foo_test_go_stem": "foo_test_go_stem", // unrelated stem unchanged
	}
	for in, want := range cases {
		if got := conceptStem(in); got != want {
			t.Errorf("conceptStem(%q) = %q, want %q", in, got, want)
		}
	}
	// End-to-end through themeFromPath for the real filenames.
	paths := map[string]string{
		"src/foo.test.ts":  "src.foo",
		"src/foo.spec.ts":  "src.foo",
		"src/foo.test.tsx": "src.foo",
		"src/foo.spec.tsx": "src.foo",
		"pkg/foo_test.py":  "pkg.foo",
		"pkg/test_foo.py":  "pkg.foo",
		"pkg/foo_test.go":  "pkg.foo",
		"src/foo.ts":       "src.foo",
	}
	for p, want := range paths {
		if got := themeFromPath(p); got != want {
			t.Errorf("themeFromPath(%q) = %q, want %q", p, got, want)
		}
	}
}

func TestMatchConventional(t *testing.T) {
	yes := []string{
		"fix: handle null config",
		"fix(router): avoid double resolve",
		"fix!: breaking change to resolver",
		"perf: cache the parsed config",
		"refactor: extract the plugin container",
		"Refactor: case-insensitive prefix",
	}
	no := []string{
		"feat: add new option",
		"docs: fix broken links", // docs, not a code scar
		"chore: bump deps",
		"style: reformat",
		"revert: \"fix: bad thing\"", // revert is the stronger scar, handled elsewhere
		"random subject without prefix",
	}
	for _, s := range yes {
		if ok, _ := matchConventional(CommitRecord{Subject: s}); !ok {
			t.Errorf("matchConventional(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if ok, _ := matchConventional(CommitRecord{Subject: s}); ok {
			t.Errorf("matchConventional(%q) = true, want false", s)
		}
	}
}

// Conventional extraction skips commits that are reverts (stronger scar) and
// drops excluded surfaces (build/dep churn).
func TestExtractConventional_SkipsRevertsAndSurfaces(t *testing.T) {
	commits := []CommitRecord{
		{SHA: "a1", Subject: "fix(node): resolve config once",
			Files: []string{"packages/vite/src/node/config.ts", "pnpm-lock.yaml"}},
		{SHA: "b2", Subject: "revert: \"fix: bad\"", // revert -> skipped here
			Files: []string{"packages/vite/src/node/plugin.ts"}},
		{SHA: "c3", Subject: "fix: bump build only",
			Files: []string{"dist/foo.js", "node_modules/x/y.js"}}, // all excluded -> dropped
	}
	sigs := ExtractConventionalCommits(commits)
	if len(sigs) != 1 {
		t.Fatalf("expected exactly 1 conventional signal (the real .ts file), got %d: %+v", len(sigs), sigs)
	}
	s := sigs[0]
	if s.SourceType != SourceConventionalCommit {
		t.Errorf("source = %q, want conventional_commit", s.SourceType)
	}
	if s.FilePath != "packages/vite/src/node/config.ts" {
		t.Errorf("file = %q, want the .ts source", s.FilePath)
	}
}

// The channel model: corroboration must cross commit<->review, not stack two
// signals of the same channel.
func TestChannelTriangulation(t *testing.T) {
	mk := func(theme string, st SourceType) ColdSignal {
		return ColdSignal{SourceType: st, ThemeKey: theme, FilePath: theme + ".ts"}
	}
	cases := []struct {
		name    string
		sigs    []ColdSignal
		wantElg bool
	}{
		{"revert+review (preserved)", []ColdSignal{mk("t", SourceRevertCommit), mk("t", SourcePRReview)}, true},
		{"conventional+review (new)", []ColdSignal{mk("t", SourceConventionalCommit), mk("t", SourcePRReview)}, true},
		{"conventional+thread (new)", []ColdSignal{mk("t", SourceConventionalCommit), mk("t", SourceReviewThread)}, true},
		{"commit-only held back", []ColdSignal{mk("t", SourceRevertCommit), mk("t", SourceConventionalCommit)}, false},
		{"review-only held back", []ColdSignal{mk("t", SourcePRReview), mk("t", SourceReviewThread)}, false},
		{"single signal held back", []ColdSignal{mk("t", SourceConventionalCommit)}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			eligible, held := Triangulate(c.sigs)
			if c.wantElg && len(eligible) != 1 {
				t.Fatalf("want eligible, got eligible=%d held=%d", len(eligible), len(held))
			}
			if !c.wantElg && len(eligible) != 0 {
				t.Fatalf("want held back, got eligible=%d", len(eligible))
			}
		})
	}
}

// Review-thread signal requires BOTH density (>=minThreadComments) and rule
// language; one signal per theme, not per comment.
func TestExtractReviewThreads_DensityAndRuleLanguage(t *testing.T) {
	f := "packages/vite/src/node/config.ts"
	dense := []ReviewComment{
		{PRID: "1", CommentID: "a", Path: f, Body: "this must not mutate the shared config"},
		{PRID: "1", CommentID: "b", Path: f, Body: "agreed"},
		{PRID: "2", CommentID: "c", Path: f, Body: "still LGTM"},
	}
	got := ExtractReviewThreads(dense)
	if len(got) != 1 || got[0].SourceType != SourceReviewThread {
		t.Fatalf("dense rule thread should yield exactly 1 review_thread signal, got %+v", got)
	}
	if got[0].FilePath != f || got[0].CommentID != "a" {
		t.Errorf("thread should anchor to the first rule comment, got %+v", got[0])
	}

	// Below the density bar -> nothing.
	if g := ExtractReviewThreads(dense[:2]); len(g) != 0 {
		t.Errorf("2 comments is below the density bar; want 0, got %d", len(g))
	}
	// Dense but no rule language -> nothing (suppresses stylistic-nit threads).
	nits := []ReviewComment{
		{PRID: "1", CommentID: "a", Path: f, Body: "nit: rename this"},
		{PRID: "1", CommentID: "b", Path: f, Body: "extra blank line"},
		{PRID: "2", CommentID: "c", Path: f, Body: "typo here"},
	}
	if g := ExtractReviewThreads(nits); len(g) != 0 {
		t.Errorf("dense stylistic thread (no rule language) must not fire; got %d", len(g))
	}
}
