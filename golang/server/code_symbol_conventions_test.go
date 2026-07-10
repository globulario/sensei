// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"
	"testing"
)

// Briefing prose must surface the SCIP reference layer as "shared call
// conventions": targets referenced by >=2 sibling symbols in the file, so a
// change to one site prompts checking the others (the completeness signal).
func TestAppendCodeContext_SharedConventions(t *testing.T) {
	syms := []codeSymbol{
		{id: "command/issue.go:issueClose", label: "issueClose",
			references: []string{"external:Fprintf", "utils/color.go:Red"}},
		{id: "command/issue.go:issueReopen", label: "issueReopen",
			references: []string{"external:Fprintf", "utils/color.go:Green"}},
		{id: "command/issue.go:issueList", label: "issueList",
			references: []string{"external:Fprintf"}},
		{id: "command/issue.go:displayURL", label: "displayURL",
			references: []string{"utils/color.go:Red"}}, // Red now shared by 2
	}
	var b strings.Builder
	appendCodeContextSection(&b, syms, 25)
	out := b.String()

	if !strings.Contains(out, "Shared call conventions") {
		t.Fatalf("missing conventions section:\n%s", out)
	}
	// Fprintf is referenced by 3 siblings — must be surfaced with the count + names.
	if !strings.Contains(out, "3 symbols reference Fprintf:") {
		t.Errorf("want '3 symbols reference Fprintf'; got:\n%s", out)
	}
	if !strings.Contains(out, "issueClose") || !strings.Contains(out, "issueReopen") || !strings.Contains(out, "issueList") {
		t.Errorf("Fprintf convention should list all three siblings; got:\n%s", out)
	}
	// Red is shared by exactly 2 → still a convention.
	if !strings.Contains(out, "2 symbols reference Red:") {
		t.Errorf("want '2 symbols reference Red'; got:\n%s", out)
	}
	// Green is referenced by only 1 symbol → NOT a convention.
	if strings.Contains(out, "reference Green:") {
		t.Errorf("Green (1 referrer) must not appear as a convention; got:\n%s", out)
	}
}

func TestRefDisplay(t *testing.T) {
	cases := map[string]string{
		"external:Fprintf":            "Fprintf",
		"command/issue.go:issueClose": "issueClose",
		"utils/color.go:Red":          "Red",
		"bareName":                    "bareName",
	}
	for in, want := range cases {
		if got := refDisplay(in); got != want {
			t.Errorf("refDisplay(%q) = %q, want %q", in, got, want)
		}
	}
}
