// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"
	"testing"
)

// INV-AUTHORITY-SEMANTIC-SCOPE, briefing half: an agent editing the code that
// implements an authority learns which authority it enforces, and that the edit
// grants none of it -- without that code being treated as governed by it.
func TestImplementingCodeIsWarnedButNotGoverned(t *testing.T) {
	domains := []loadedAuthorityDomain{
		{ID: "authority.repo", Label: "Repository mutation", CoversPaths: []string{"golang/architecture/"},
			OwnerService: "service.repo", MustMutateVia: []string{"governed repository edit"}},
		{ID: "authority.completion", Label: "Terminal completion", OwnsState: []string{"task_ledger.event.completed"},
			ImplementedIn: []string{"golang/architecture/completion/"}, OwnerService: "service.completion",
			MustMutateVia: []string{"governed terminal completion"}},
		{ID: "authority.abandonment", Label: "Terminal abandonment", OwnsState: []string{"task_ledger.event.abandoned"},
			ImplementedIn: []string{"golang/architecture/completion/"}, OwnerService: "service.completion",
			MustMutateVia: []string{"governed terminal abandonment"}},
	}
	file := []string{"golang/architecture/completion/abandon.go"}

	governing := matchAuthorityDomains(file, domains)
	if len(governing) != 1 || governing[0].ID != "authority.repo" {
		t.Fatalf("governing authorities = %v, want only the repository authority", ids(governing))
	}

	impl := matchImplementedAuthorities(file, domains)
	if got := ids(impl); strings.Join(got, ",") != "authority.abandonment,authority.completion" {
		t.Fatalf("implemented authorities = %v, want both terminal authorities (one package implements both)", got)
	}
	notices := authorityImplementationNotices(impl)
	if len(notices) != 2 {
		t.Fatalf("notices = %v", notices)
	}
	for _, n := range notices {
		if !strings.Contains(n, "grants no authority") || !strings.Contains(n, "service.completion") {
			t.Fatalf("notice does not separate implementation from authority: %q", n)
		}
	}
	if !strings.Contains(notices[0], "task_ledger.event.abandoned") || !strings.Contains(notices[0], "governed terminal abandonment") {
		t.Fatalf("notice does not name the governed truth and its mutation path: %q", notices[0])
	}

	// A path prefix match is a directory match, not a string prefix.
	if got := matchImplementedAuthorities([]string{"golang/architecture/completionist/x.go"}, domains); len(got) != 0 {
		t.Fatalf("sibling directory matched: %v", ids(got))
	}
	// Implementing code feeds no risk weighting or authority path set.
	for _, p := range authorityCoversPaths(impl) {
		t.Fatalf("implemented_in leaked into covers paths: %q", p)
	}
}

func ids(ds []loadedAuthorityDomain) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.ID)
	}
	return out
}
