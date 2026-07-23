package briefing

import (
	"strings"
	"testing"

	githubapi "github.com/globulario/sensei-github-app/internal/github"
)

func TestBuildIsDeterministic(t *testing.T) {
	input := Input{
		Repository: "globulario/example",
		PRNumber:   42,
		BaseSHA:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		HeadSHA:    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Files: []githubapi.PullRequestFile{
			{Filename: "internal/auth/token.go", Status: "modified", Additions: 12, Deletions: 3},
			{Filename: "internal/auth/token_test.go", Status: "modified", Additions: 20, Deletions: 1},
			{Filename: ".github/workflows/ci.yml", Status: "modified", Additions: 2, Deletions: 2},
		},
	}

	first := Build(input)
	second := Build(input)
	if first != second {
		t.Fatalf("Build() is not deterministic\nfirst: %#v\nsecond: %#v", first, second)
	}
	for _, expected := range []string{
		CommentMarker,
		"`globulario/example`",
		"`#42`",
		"Changed files: **3**",
		"Changed test files: **1**",
		"`authorization surface`",
		"`CI workflow`",
	} {
		if !strings.Contains(first.CommentBody, expected) {
			t.Errorf("briefing missing %q", expected)
		}
	}
}
