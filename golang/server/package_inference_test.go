// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

// pkgStore is a fakeStore that ALSO implements store.PackageAnchorStore, so the
// populated inference path is reachable in tests.
type pkgStore struct {
	fakeStore
	pkgFacts  func(ctx context.Context, prefix string) ([]store.PackageImpactFact, error)
	gotPrefix string
}

func (p *pkgStore) ImpactForPackage(ctx context.Context, prefix string) ([]store.PackageImpactFact, error) {
	p.gotPrefix = prefix
	if p.pkgFacts == nil {
		return nil, nil
	}
	return p.pkgFacts(ctx, prefix)
}

func anchorFact(file, nodeIRI, typeIRI string, extra ...store.ImpactFact) []store.PackageImpactFact {
	out := []store.PackageImpactFact{{
		ImpactFact:    store.ImpactFact{NodeIRI: nodeIRI, TypeIRI: typeIRI},
		SourceFileIRI: mintedIRI(rdf.ClassSourceFile, file),
	}}
	for _, e := range extra {
		e.NodeIRI, e.TypeIRI = nodeIRI, typeIRI
		out = append(out, store.PackageImpactFact{ImpactFact: e, SourceFileIRI: mintedIRI(rdf.ClassSourceFile, file)})
	}
	return out
}

// THE POINT OF THE FEATURE: a file with no direct anchors still learns what
// governs its package. Before this, four governed files in five returned a
// generic component and nothing else, so the guardrail fired and the briefing
// had nothing to say.
func TestPackageInference_SiblingAnchorsAreInferredNotDirect(t *testing.T) {
	invIRI := mintedIRI(rdf.ClassInvariant, "workspace.admission.decision_verification_binding")
	s := newServer(&pkgStore{
		fakeStore: fakeStore{impactForFile: func(context.Context, string) ([]store.ImpactFact, error) {
			return nil, nil // this file has NO direct anchors
		}},
		pkgFacts: func(context.Context, string) ([]store.PackageImpactFact, error) {
			return anchorFact("golang/architecture/workspacecontract/admission.go", invIRI, rdf.ClassInvariant), nil
		},
	})

	resp, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{
		File: "golang/architecture/workspacecontract/identity.go",
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if n := len(resp.GetDirectInvariants()); n != 0 {
		t.Fatalf("direct_invariants=%d, want 0: a sibling's anchor must never become direct", n)
	}
	if n := len(resp.GetInferredInvariants()); n != 1 {
		t.Fatalf("inferred_invariants=%d, want 1", n)
	}
	if got := resp.GetInferredInvariants()[0].GetId(); got != "workspace.admission.decision_verification_binding" {
		t.Fatalf("inferred id = %q", got)
	}
}

// Direct anchors are unchanged, and an anchor that is already direct is not
// repeated as inferred.
func TestPackageInference_DirectAnchorsAreNotDuplicated(t *testing.T) {
	invIRI := mintedIRI(rdf.ClassInvariant, "shared.rule")
	s := newServer(&pkgStore{
		fakeStore: fakeStore{impactForFile: func(context.Context, string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{{NodeIRI: invIRI, TypeIRI: rdf.ClassInvariant}}, nil
		}},
		pkgFacts: func(context.Context, string) ([]store.PackageImpactFact, error) {
			return anchorFact("golang/server/other.go", invIRI, rdf.ClassInvariant), nil
		},
	})

	resp, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{File: "golang/server/impact.go"})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if n := len(resp.GetDirectInvariants()); n != 1 {
		t.Fatalf("direct_invariants=%d, want 1", n)
	}
	if n := len(resp.GetInferredInvariants()); n != 0 {
		t.Fatalf("inferred_invariants=%d, want 0: already stated directly", n)
	}
}

// The prefix filter is coarse because IRI path separators are encoded, so a
// nested package's rules would otherwise climb into this file's context.
func TestPackageInference_NestedDirectoryIsNotThisPackage(t *testing.T) {
	nested := mintedIRI(rdf.ClassInvariant, "nested.rule")
	sibling := mintedIRI(rdf.ClassInvariant, "sibling.rule")
	s := newServer(&pkgStore{
		fakeStore: fakeStore{impactForFile: func(context.Context, string) ([]store.ImpactFact, error) { return nil, nil }},
		pkgFacts: func(context.Context, string) ([]store.PackageImpactFact, error) {
			out := anchorFact("golang/server/sub/deep.go", nested, rdf.ClassInvariant)
			return append(out, anchorFact("golang/server/other.go", sibling, rdf.ClassInvariant)...), nil
		},
	})

	resp, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{File: "golang/server/impact.go"})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if n := len(resp.GetInferredInvariants()); n != 1 {
		t.Fatalf("inferred_invariants=%d, want 1 (only the true sibling)", n)
	}
	if got := resp.GetInferredInvariants()[0].GetId(); got != "sibling.rule" {
		t.Fatalf("inferred id = %q, want sibling.rule — a nested package must not leak up", got)
	}
}

// The file's own anchors do not arrive back as "inferred from a sibling".
func TestPackageInference_ExcludesTheFileItself(t *testing.T) {
	invIRI := mintedIRI(rdf.ClassInvariant, "own.rule")
	s := newServer(&pkgStore{
		fakeStore: fakeStore{impactForFile: func(context.Context, string) ([]store.ImpactFact, error) { return nil, nil }},
		pkgFacts: func(context.Context, string) ([]store.PackageImpactFact, error) {
			return anchorFact("golang/server/impact.go", invIRI, rdf.ClassInvariant), nil
		},
	})
	resp, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{File: "golang/server/impact.go"})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if n := len(resp.GetInferredInvariants()); n != 0 {
		t.Fatalf("inferred_invariants=%d, want 0: the file is not its own sibling", n)
	}
}

// Domain scope applies to inferred anchors too. A neighbour's rule from another
// repo is exactly the leak the scoping invariant forbids, and arriving by
// inference does not make it admissible.
func TestPackageInference_ForeignDomainIsScopedOut(t *testing.T) {
	homeIRI := mintedIRI(rdf.ClassInvariant, "home.rule")
	foreignIRI := mintedIRI(rdf.ClassInvariant, "foreign.rule")
	s := newServer(&pkgStore{
		fakeStore: fakeStore{impactForFile: func(context.Context, string) ([]store.ImpactFact, error) { return nil, nil }},
		pkgFacts: func(context.Context, string) ([]store.PackageImpactFact, error) {
			out := anchorFact("golang/server/a.go", homeIRI, rdf.ClassInvariant,
				store.ImpactFact{Predicate: rdf.PropRepo, Object: "globular"})
			return append(out, anchorFact("golang/server/b.go", foreignIRI, rdf.ClassInvariant,
				store.ImpactFact{Predicate: rdf.PropRepo, Object: "github.com/other/repo"})...), nil
		},
	})

	resp, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{
		File: "golang/server/impact.go", Domain: "globular",
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	for _, n := range resp.GetInferredInvariants() {
		if n.GetId() == "foreign.rule" {
			t.Fatal("a foreign-domain anchor leaked in through package inference")
		}
	}
	if len(resp.GetInferredInvariants()) != 1 {
		t.Fatalf("inferred_invariants=%d, want 1 (home only)", len(resp.GetInferredInvariants()))
	}
}

// A failed walk must not sink the direct answer, and must not look like "this
// package is ungoverned" either.
func TestPackageInference_QueryFailureIsTypedNotSilent(t *testing.T) {
	invIRI := mintedIRI(rdf.ClassInvariant, "direct.rule")
	s := newServer(&pkgStore{
		fakeStore: fakeStore{impactForFile: func(context.Context, string) ([]store.ImpactFact, error) {
			return []store.ImpactFact{{NodeIRI: invIRI, TypeIRI: rdf.ClassInvariant}}, nil
		}},
		pkgFacts: func(context.Context, string) ([]store.PackageImpactFact, error) {
			return nil, errors.New("backend exploded")
		},
	})

	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "golang/server/impact.go"})
	if err != nil {
		t.Fatalf("Briefing must survive a failed package walk: %v", err)
	}
	if !strings.Contains(resp.GetProse(), "Package-level inference unavailable") {
		t.Fatalf("failed walk must be stated, got:\n%s", resp.GetProse())
	}
	if !strings.Contains(resp.GetProse(), "NOT evidence the package is ungoverned") {
		t.Fatal("the unavailable note must warn against reading absence as safety")
	}
	// The direct answer is untouched.
	if !strings.Contains(resp.GetProse(), "direct.rule") {
		t.Fatal("a failed inference must not sink the direct architectural answer")
	}
}

// The rendered section must mark inferred anchors as the package's, not the
// file's, and attribute the sibling they came from.
func TestPackageInference_ProseSeparatesAndAttributes(t *testing.T) {
	invIRI := mintedIRI(rdf.ClassInvariant, "pkg.rule")
	s := newServer(&pkgStore{
		fakeStore: fakeStore{impactForFile: func(context.Context, string) ([]store.ImpactFact, error) { return nil, nil }},
		pkgFacts: func(context.Context, string) ([]store.PackageImpactFact, error) {
			return anchorFact("golang/server/admission.go", invIRI, rdf.ClassInvariant), nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "golang/server/identity.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	prose := resp.GetProse()
	for _, want := range []string{
		"Inferred from this package",
		"NOT to this file",
		"via golang/server/admission.go",
		"govern the package, not necessarily this file",
	} {
		if !strings.Contains(prose, want) {
			t.Fatalf("prose missing %q:\n%s", want, prose)
		}
	}
}

// A repository-root file has no package to inherit from. That is a real answer,
// not a degraded one.
func TestPackageInference_RootFileIsNotUnavailable(t *testing.T) {
	s := newServer(&pkgStore{
		fakeStore: fakeStore{impactForFile: func(context.Context, string) ([]store.ImpactFact, error) { return nil, nil }},
		pkgFacts: func(context.Context, string) ([]store.PackageImpactFact, error) {
			t.Fatal("must not query the backend for a root-level file")
			return nil, nil
		},
	})
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{File: "main.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if strings.Contains(resp.GetProse(), "Package-level inference unavailable") {
		t.Fatal("a root-level file has no package; that is not an outage")
	}
}

// The prefix handed to the backend is the package directory, so the query
// cannot accidentally scan the whole graph.
func TestPackageInference_QueriesTheDirectoryPrefix(t *testing.T) {
	ps := &pkgStore{
		fakeStore: fakeStore{impactForFile: func(context.Context, string) ([]store.ImpactFact, error) { return nil, nil }},
	}
	s := newServer(ps)
	if _, err := s.Impact(context.Background(), &awarenesspb.ImpactRequest{File: "golang/server/impact.go"}); err != nil {
		t.Fatalf("Impact: %v", err)
	}
	// The package prefix is a repo-relative PATH prefix now, not an IRI
	// prefix: SourceFile subjects are repository-scoped (issue #197), so a
	// string prefix over the subject would also have to match the
	// repository segment.
	want := "golang/server/"
	if ps.gotPrefix != want {
		t.Fatalf("prefix = %q, want %q", ps.gotPrefix, want)
	}
}
