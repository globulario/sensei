// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"sort"
	"testing"
)

// themeFromPath keys at file-concept granularity (dir + concept stem), so a
// broad component directory no longer collapses unrelated files into one bucket.
func TestThemeFromPath_FileConceptGranularity(t *testing.T) {
	cases := map[string]string{
		"":                                     "",
		"listeners.go":                         "listeners",
		"go.mod":                               "go",
		"go.sum":                               "go", // go.mod + go.sum share one concept
		"modules/caddyhttp/server.go":          "modules.caddyhttp.server",
		"modules/caddyhttp/matchers.go":        "modules.caddyhttp.matchers",
		"modules/caddyhttp/encode/encode.go":   "modules.caddyhttp.encode.encode",
		"caddyconfig/httpcaddyfile/options.go": "caddyconfig.httpcaddyfile.options",
	}
	for in, want := range cases {
		if got := themeFromPath(in); got != want {
			t.Errorf("themeFromPath(%q) = %q, want %q", in, got, want)
		}
	}

	// The whole point: two unrelated files in the SAME broad directory must NOT
	// share a theme (that was the conflation that buried real rules).
	if a, b := themeFromPath("modules/caddyhttp/server.go"), themeFromPath("modules/caddyhttp/metrics.go"); a == b {
		t.Errorf("distinct files in a broad dir must get distinct themes, both got %q", a)
	}
}

// A source file and its test/generated sibling normalize to one concept theme,
// so a fix commit (foo.go) and a review comment on foo_test.go still triangulate.
func TestConceptStem_NormalizesTestAndGeneratedSiblings(t *testing.T) {
	cases := map[string]string{
		"encode":           "encode",
		"encode_test":      "encode",
		"rewrite_test":     "rewrite",
		"client.spec":      "client",
		"zz_version_gen":   "zz_version",
		"schema_generated": "schema",
		"_test":            "_test", // fallback: stripping would empty it
	}
	for in, want := range cases {
		if got := conceptStem(in); got != want {
			t.Errorf("conceptStem(%q) = %q, want %q", in, got, want)
		}
	}

	// End to end: foo.go and foo_test.go land on the same theme.
	if a, b := themeFromPath("modules/logging/filewriter.go"), themeFromPath("modules/logging/filewriter_test.go"); a != b {
		t.Errorf("source and its _test sibling must share a theme, got %q vs %q", a, b)
	}
}

// Triangulation over a broad directory must produce one bundle PER file-concept,
// not one mega-bundle for the whole directory. This is the Caddy-benchmark
// regression guard in synthetic form: modules/caddyhttp had a 47-citation
// mega-bucket before file-concept keying; afterward it splits.
func TestTriangulate_SplitsBroadDirectoryByConcept(t *testing.T) {
	// Two unrelated concepts in the same broad directory, each corroborated by
	// BOTH a revert and a PR review comment (so each is independently eligible).
	comments := []ReviewComment{
		{PRID: "1", CommentID: "11", Path: "modules/caddyhttp/server.go", Line: 10, Body: "this must not block the listener"},
		{PRID: "2", CommentID: "22", Path: "modules/caddyhttp/metrics.go", Line: 20, Body: "do not double-count requests here"},
		// A third concept with ONLY a PR comment (no revert) must stay held back.
		{PRID: "3", CommentID: "33", Path: "modules/caddyhttp/vars.go", Line: 5, Body: "must always clone before mutating"},
	}
	signals := ExtractPRReviews(comments)
	signals = append(signals,
		ColdSignal{SourceType: SourceRevertCommit, ThemeKey: themeFromPath("modules/caddyhttp/server.go"),
			CommitSHA: "s1", MatchedText: "Revert server change"},
		ColdSignal{SourceType: SourceRevertCommit, ThemeKey: themeFromPath("modules/caddyhttp/metrics.go"),
			CommitSHA: "m1", MatchedText: "Revert metrics change"},
	)

	eligible, held := Triangulate(signals)

	gotThemes := make([]string, len(eligible))
	for i, b := range eligible {
		gotThemes[i] = b.ThemeKey
	}
	sort.Strings(gotThemes)
	want := []string{"modules.caddyhttp.metrics", "modules.caddyhttp.server"}
	if len(gotThemes) != 2 || gotThemes[0] != want[0] || gotThemes[1] != want[1] {
		t.Fatalf("broad directory must split into per-concept bundles: got eligible %v, want %v", gotThemes, want)
	}
	// The PR-only concept is single-source → held back, never auto-drafted.
	if len(held) != 1 || held[0].ThemeKey != "modules.caddyhttp.vars" {
		t.Fatalf("PR-only concept must be held back single-source, got %+v", held)
	}
}
