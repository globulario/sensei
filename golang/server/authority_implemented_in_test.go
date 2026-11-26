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

// #390 review: information must not displace obligation. With the bounded list
// already full of mandatory actions, no notice enters; with room, notices go
// after every obligation, never before.
func TestImplementationNoticesNeverDisplaceObligations(t *testing.T) {
	notices := []string{"Implements authority [A]: ...", "Implements authority [B]: ..."}
	full := []string{"o1", "o2", "o3", "o4", "o5", "o6", "o7", "o8"}
	if got := appendInformational(append([]string(nil), full...), notices, 8); strings.Join(got, "|") != strings.Join(full, "|") {
		t.Fatalf("a full obligation list changed: %v", got)
	}
	got := appendInformational([]string{"o1", "o2", "o3", "o4", "o5", "o6", "o7"}, notices, 8)
	if len(got) != 8 || got[6] != "o7" || got[7] != notices[0] {
		t.Fatalf("notices must only fill spare capacity after the obligations: %v", got)
	}
	if got := appendInformational([]string{"o1"}, notices, 8); len(got) != 3 || got[0] != "o1" {
		t.Fatalf("obligation not first: %v", got)
	}
}

// Editing the promotion implementation warns about the promotion authority
// without making that code governed by it.
func TestEditingPromotionImplementationWarnsWithoutGoverning(t *testing.T) {
	domains := []loadedAuthorityDomain{
		{ID: "authority.repo", CoversPaths: []string{"golang/architecture/", "docs/awareness/"}, OwnerService: "service.repo"},
		{ID: "authority.promotion", Label: "Governed promotion", OwnsState: []string{"governed_knowledge.promoted_record"},
			ImplementedIn: []string{"golang/architecture/questionpromotion/"}, OwnerService: "service.promotion",
			MustMutateVia: []string{"governed promotion"}},
	}
	file := []string{"golang/architecture/questionpromotion/promote.go"}
	if g := matchAuthorityDomains(file, domains); len(g) != 1 || g[0].ID != "authority.repo" {
		t.Fatalf("governing = %v", ids(g))
	}
	impl := matchImplementedAuthorities(file, domains)
	if len(impl) != 1 || !strings.Contains(authorityImplementationNotices(impl)[0], "governed_knowledge.promoted_record") {
		t.Fatalf("no promotion notice: %v", ids(impl))
	}
	if g := matchImplementedAuthorities([]string{"docs/awareness/invariants.yaml"}, domains); len(g) != 0 {
		t.Fatalf("a corpus edit was reported as promotion implementation: %v", ids(g))
	}
}
