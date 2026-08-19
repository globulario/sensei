// SPDX-License-Identifier: AGPL-3.0-only

package graphbuild

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
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
