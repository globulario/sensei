// SPDX-License-Identifier: AGPL-3.0-only

package graphbuild

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/globulario/sensei/internal/repofixture"
)

// This is the mechanism behind the one case issue #176's live matrix still
// broke on: on a fresh store, the first build of a second domain left the
// first UNPROVEN, because subjects were claimed by BOTH domains. Querying
// the store found 9 such subjects, and the SourceFile ones were there
// because every Sensei-onboarded repository has README.md and
// docs/awareness/invariants.yaml, so the unscoped identity collapsed them
// (issue #197).
//
// SliceDigest is not at fault and is not what this tests: it already
// excludes foreign repo tags so a co-owning domain cannot move another
// domain's digest. What moved the digest was the SHARED SUBJECT's other
// properties changing -- and that SHOULD invalidate a proof computed
// against different content. The proof invalidation is downstream of the
// identity collapse, so this asserts the identity, not the proof.
//
// It asserts over every subject each domain's slice contains, not only
// subjects carrying aw:authoredIn -- SourceFile subjects carry no
// authorship, which is exactly why the existing #178 authorship regression
// could pass while this collided.

var anySubjectRE = regexp.MustCompile(`^(<[^>]+>)\s`)

// repoWithSharedPaths builds a checkout whose corpus protects the file
// paths EVERY Sensei-onboarded repository has, so two of them collide by
// default under an unscoped identity.
func repoWithSharedPaths(t *testing.T, name, ownID, domain string) (root, corpus string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), name)
	corpus = filepath.Join(root, "docs", "awareness")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	repofixture.WriteRepositoryIdentity(t, root, domain)

	own := "invariants:\n  - id: " + ownID + "\n    title: \"" + name + " own knowledge\"\n" +
		"    category: authority\n    severity: high\n    status: active\n" +
		"    summary: |\n      Genuinely authored by " + name + ".\n" +
		"    protects:\n      files:\n        - README.md\n        - docs/awareness/invariants.yaml\n"
	if err := os.WriteFile(filepath.Join(corpus, "invariants.yaml"), []byte(own), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, corpus
}

func subjectsOf(nt []byte) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(string(nt), "\n") {
		if m := anySubjectRE.FindStringSubmatch(line); m != nil {
			out[m[1]] = true
		}
	}
	return out
}

// TestTwoScaffoldedDomainsShareNoRepositoryLocalSubject is the rest of
// #176's fresh-store break: two repositories scaffolded from the real
// `sensei init` corpus must claim no repository-local subject in common.
//
// Every Sensei-onboarded repository scaffolds the same activation rules,
// high-risk-file policy and guardrails and then edits its own, so under an
// unscoped identity two repositories' "activation_rule.auto_briefing" were
// one subject -- the same collapse SourceFile had, in the family #197's
// decision deferred to a bounded audit. The audit classed them
// repository-local (see rdf.Emitter.GuardrailIRI), and this pins the result.
//
// It does NOT require the pack's shared knowledge to be distinct: an
// installed principle pack is portable canonical knowledge with ONE
// identity by design, and custody decides its authorship (#178). Only
// repository-local families are asserted here.
func TestTwoScaffoldedDomainsShareNoRepositoryLocalSubject(t *testing.T) {
	const (
		domainA = "github.com/globulario/sensei"
		domainB = "github.com/globulario/sensei-code"
	)
	templates := filepath.FromSlash("../../../cmd/awg/templates/awareness")
	entries, err := os.ReadDir(templates)
	if err != nil {
		t.Skipf("scaffold templates unavailable: %v", err)
	}
	scaffold := func(name, domain string) (string, string) {
		t.Helper()
		root := filepath.Join(t.TempDir(), name)
		corpus := filepath.Join(root, "docs", "awareness")
		if err := os.MkdirAll(corpus, 0o755); err != nil {
			t.Fatal(err)
		}
		repofixture.WriteRepositoryIdentity(t, root, domain)
		for _, e := range entries {
			body, err := os.ReadFile(filepath.Join(templates, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(corpus, e.Name()), body, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return root, corpus
	}
	compile := func(domain, corpus, root string) Compilation {
		t.Helper()
		comp, err := Compile(context.Background(), CompileRequest{
			Sources: []SourceRoot{{FilesystemPath: corpus, IdentityRoot: root, RepositoryDomain: domain}},
		})
		if err != nil {
			t.Fatalf("compile %s: %v", domain, err)
		}
		return comp
	}

	rootA, corpusA := scaffold("alpha", domainA)
	rootB, corpusB := scaffold("beta", domainB)
	subjectsA := subjectsOf(compile(domainA, corpusA, rootA).CanonicalNTriples)
	subjectsB := subjectsOf(compile(domainB, corpusB, rootB).CanonicalNTriples)

	// The repository-local families. invariant/ is deliberately absent: the
	// installed pack is shared knowledge with one identity by design.
	local := []string{"#sourceFile/", "#guardrail/"}
	var shared []string
	for subject := range subjectsA {
		if !subjectsB[subject] {
			continue
		}
		for _, family := range local {
			if strings.Contains(subject, family) {
				shared = append(shared, subject)
				break
			}
		}
	}
	sort.Strings(shared)
	if len(shared) > 0 {
		t.Fatalf("two scaffolded repositories claim the same repository-local subject(s), so publishing one moves the other's slice:\n  %s",
			strings.Join(shared, "\n  "))
	}

	// Guard against passing for the wrong reason: the scaffold must actually
	// have produced guardrail subjects in both.
	for name, subjects := range map[string]map[string]bool{"alpha": subjectsA, "beta": subjectsB} {
		found := 0
		for subject := range subjects {
			if strings.Contains(subject, "#guardrail/") {
				found++
			}
		}
		if found == 0 {
			t.Errorf("%s produced no guardrail subject; the assertion above proved nothing", name)
		}
	}
}

// TestTwoDomainsShareNoSourceFileSubject is #176's remaining break, at the
// level it actually happens: two registered domains publishing into one
// graph must not claim the same SourceFile subject, so neither one's slice
// can be moved by the other's publication.
func TestTwoDomainsShareNoSourceFileSubject(t *testing.T) {
	const (
		domainA = "github.com/globulario/sensei"
		domainB = "github.com/globulario/sensei-code"
	)
	rootA, corpusA := repoWithSharedPaths(t, "alpha", "alpha.own", domainA)
	rootB, corpusB := repoWithSharedPaths(t, "beta", "beta.own", domainB)

	compile := func(domain, corpus, root string) Compilation {
		t.Helper()
		comp, err := Compile(context.Background(), CompileRequest{
			Sources: []SourceRoot{{
				FilesystemPath:   corpus,
				IdentityRoot:     root,
				RepositoryDomain: domain,
			}},
		})
		if err != nil {
			t.Fatalf("compile %s: %v", domain, err)
		}
		return comp
	}
	compA := compile(domainA, corpusA, rootA)
	compB := compile(domainB, corpusB, rootB)

	subjectsA := subjectsOf(compA.CanonicalNTriples)
	subjectsB := subjectsOf(compB.CanonicalNTriples)

	var shared []string
	for subject := range subjectsA {
		if subjectsB[subject] && strings.Contains(subject, "#sourceFile/") {
			shared = append(shared, subject)
		}
	}
	if len(shared) > 0 {
		t.Fatalf("two domains claim the same SourceFile subject(s), so publishing one moves the other's slice:\n  %s",
			strings.Join(shared, "\n  "))
	}

	// Guard against passing for the wrong reason: both builds must actually
	// have produced SourceFile subjects for the shared paths. A build that
	// emitted none would satisfy the assertion above while proving nothing.
	for name, subjects := range map[string]map[string]bool{"alpha": subjectsA, "beta": subjectsB} {
		found := 0
		for subject := range subjects {
			if strings.Contains(subject, "#sourceFile/") && strings.Contains(subject, "README.md") {
				found++
			}
		}
		if found == 0 {
			t.Errorf("%s produced no SourceFile subject for README.md; the assertion above proved nothing", name)
		}
	}
}
