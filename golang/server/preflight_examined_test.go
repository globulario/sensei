// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

// Three evidence states, and the middle one is the whole of #220:
//
//	Absent       — no evidence about this file at all
//	EmptyProven  — the graph holds this file, and nothing governs it
//	Governed     — the graph holds this file, and something governs it
//
// Examination used to be DEFINED as having an anchor, so EmptyProven could not
// be expressed: a file the graph holds and governs by nothing read exactly like
// a file nobody had ever analysed. Note that case 2 is built with no anchor at
// all — an invented low-severity invariant would produce "governed by something
// minor", which is a different state and would let this pass while the missing
// one stayed missing.
func TestPreflightCoverageDistinguishesExaminedFromUnknown(t *testing.T) {
	const file = "internal/x/x.go"

	newServer := func(iris []string, facts []store.ImpactFact) *server {
		invalidateImplementationPatternCacheForTest()
		return newTestServer(fakeStore{
			impactForFile: func(_ context.Context, path string) ([]store.ImpactFact, error) {
				if path == file {
					return facts, nil
				}
				return nil, nil
			},
			sourceFileIRIs: func(_ context.Context, path string) ([]string, error) {
				if path == file {
					return iris, nil
				}
				return nil, nil
			},
		})
	}
	preflight := func(t *testing.T, s *server) *awarenesspb.CoverageSummary {
		t.Helper()
		resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
			Task:  "adjust the helper",
			Files: []string{file},
		})
		if err != nil {
			t.Fatalf("Preflight: %v", err)
		}
		return resp.GetCoverage()
	}

	t.Run("absent — never examined", func(t *testing.T) {
		got := preflight(t, newServer(nil, nil))
		if got.GetIndexedFileCount() != 0 || got.GetDirectAnchorCount() != 0 {
			t.Fatalf("indexed=%d anchors=%d, want 0/0", got.GetIndexedFileCount(), got.GetDirectAnchorCount())
		}
		if got.GetSufficient() {
			t.Fatalf("a file the graph has never seen must not report coverage: note=%q", got.GetNote())
		}
	})

	t.Run("empty proven — examined, nothing governs it", func(t *testing.T) {
		got := preflight(t, newServer([]string{fileIRI(file)}, nil))
		if got.GetDirectAnchorCount() != 0 {
			t.Fatalf("anchors=%d, want 0 — this state must be constructible with no anchor",
				got.GetDirectAnchorCount())
		}
		if got.GetIndexedFileCount() != 1 {
			t.Fatalf("indexed=%d, want 1 — the graph holds this file", got.GetIndexedFileCount())
		}
		if !got.GetSufficient() {
			t.Fatalf("examined-and-ungoverned must be sufficient coverage: note=%q", got.GetNote())
		}
		if !strings.Contains(got.GetNote(), "examined") {
			t.Fatalf("note must say the file was examined; got %q", got.GetNote())
		}
	})

	t.Run("governed — examined, something governs it", func(t *testing.T) {
		got := preflight(t, newServer([]string{fileIRI(file)},
			invariantFacts("example.governs_x", "governs x", "high")))
		if got.GetDirectAnchorCount() == 0 {
			t.Fatal("anchors=0, want the governing invariant")
		}
		if got.GetIndexedFileCount() != 1 || !got.GetSufficient() {
			t.Fatalf("indexed=%d sufficient=%v, want 1/true", got.GetIndexedFileCount(), got.GetSufficient())
		}
	})
}

// Anchors still prove examination on their own. The lookup adds a second,
// independent route to the same fact; it does not replace the first.
func TestPreflightAnchorsAloneStillCountAsExamined(t *testing.T) {
	const file = "internal/x/x.go"
	invalidateImplementationPatternCacheForTest()
	s := newTestServer(fakeStore{
		impactForFile: func(_ context.Context, path string) ([]store.ImpactFact, error) {
			if path == file {
				return invariantFacts("example.governs_x", "governs x", "high"), nil
			}
			return nil, nil
		},
		// No source-file index answer at all, as a store that cannot serve the
		// lookup would behave.
	})
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "adjust the helper",
		Files: []string{file},
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if resp.GetCoverage().GetIndexedFileCount() != 1 {
		t.Fatalf("indexed=%d, want 1 — an anchor is itself evidence the file was examined",
			resp.GetCoverage().GetIndexedFileCount())
	}
}

// The lookup fails closed in both directions it cannot answer, because picking
// one repository's file for a path that names several is the identity collapse
// SourceFile scoping removed (#197).
func TestSourceFileExaminedFailsClosedOnAmbiguityAndErrors(t *testing.T) {
	const file = "README.md"
	otherRepoIRI := strings.Trim(rdf.MintSourceFileIRI("github.com/other/repo", file), "<>")

	t.Run("same path in two repositories", func(t *testing.T) {
		s := newTestServer(fakeStore{
			sourceFileIRIs: func(_ context.Context, _ string) ([]string, error) {
				return []string{fileIRI(file), otherRepoIRI}, nil
			},
		})
		examined, blindSpot := s.sourceFileExamined(context.Background(), file, "")
		if examined {
			t.Fatal("an ambiguous path must not report the file as examined")
		}
		if !strings.Contains(blindSpot, "index_ambiguous_for_") {
			t.Fatalf("the ambiguity must be reported, got %q", blindSpot)
		}
	})

	t.Run("a named domain selects its own repository's file", func(t *testing.T) {
		s := newTestServer(fakeStore{
			sourceFileIRIs: func(_ context.Context, _ string) ([]string, error) {
				return []string{fileIRI(file), otherRepoIRI}, nil
			},
		})
		examined, blindSpot := s.sourceFileExamined(context.Background(), file, testRepositoryIdentity)
		if !examined {
			t.Fatalf("the requested repository's file must resolve, got blind spot %q", blindSpot)
		}
		if blindSpot != "" {
			t.Fatalf("no ambiguity remains once the domain selects one repository: %q", blindSpot)
		}
	})

	t.Run("another repository's file is not this domain's evidence", func(t *testing.T) {
		s := newTestServer(fakeStore{
			sourceFileIRIs: func(_ context.Context, _ string) ([]string, error) {
				return []string{otherRepoIRI}, nil
			},
		})
		examined, _ := s.sourceFileExamined(context.Background(), file, testRepositoryIdentity)
		if examined {
			t.Fatal("a file belonging to another repository must not count as examined here")
		}
	})

	t.Run("lookup failure is a blind spot, never a clean answer", func(t *testing.T) {
		s := newTestServer(fakeStore{
			sourceFileIRIs: func(_ context.Context, _ string) ([]string, error) {
				return nil, context.DeadlineExceeded
			},
		})
		examined, blindSpot := s.sourceFileExamined(context.Background(), file, "")
		if examined {
			t.Fatal("a failed lookup must never read as examined")
		}
		if !strings.Contains(blindSpot, "index_lookup_failed_for_") {
			t.Fatalf("the failure must be reported, got %q", blindSpot)
		}
	})
}

// Coverage speaks for the whole request. One examined file does not cover the
// file beside it that the graph has never seen — its silence is about itself.
func TestPreflightPartialExaminationIsNotCoverage(t *testing.T) {
	const examined = "internal/x/x.go"
	const unknown = "internal/y/y.go"
	invalidateImplementationPatternCacheForTest()
	s := newTestServer(fakeStore{
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) { return nil, nil },
		sourceFileIRIs: func(_ context.Context, path string) ([]string, error) {
			if path == examined {
				return []string{fileIRI(examined)}, nil
			}
			return nil, nil
		},
	})
	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "adjust the helpers",
		Files: []string{examined, unknown},
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	cov := resp.GetCoverage()
	if cov.GetIndexedFileCount() != 1 || cov.GetFileCount() != 2 {
		t.Fatalf("indexed=%d of %d, want 1 of 2", cov.GetIndexedFileCount(), cov.GetFileCount())
	}
	if cov.GetSufficient() {
		t.Fatalf("a partially examined request must not report coverage: note=%q", cov.GetNote())
	}
	if !strings.Contains(cov.GetNote(), "only 1 of 2") {
		t.Fatalf("note must say how much of the request is examined; got %q", cov.GetNote())
	}
}

// A source-file identity that predates repository scoping carries no
// repository. It is evidence that SOME repository holds this path, and a caller
// that named a domain asked about one — so it is not evidence for that domain,
// and accepting it would invent the attribution ParseSourceFileIRI refuses to
// invent (#197).
func TestUnscopedSourceFileIdentityIsNotDomainEvidence(t *testing.T) {
	const file = "README.md"
	legacyIRI := "https://globular.io/awareness#sourceFile/" + rdf.EncodeIRIPath(file)

	t.Run("with a named domain it is not evidence", func(t *testing.T) {
		s := newTestServer(fakeStore{
			sourceFileIRIs: func(_ context.Context, _ string) ([]string, error) {
				return []string{legacyIRI}, nil
			},
		})
		examined, blindSpot := s.sourceFileExamined(context.Background(), file, testRepositoryIdentity)
		if examined {
			t.Fatal("an unscoped identity must not count as the requested repository's file")
		}
		if !strings.Contains(blindSpot, "index_unscoped_for_") {
			t.Fatalf("the reason must be reported, got %q", blindSpot)
		}
	})

	t.Run("the requested repository's own file still resolves beside it", func(t *testing.T) {
		s := newTestServer(fakeStore{
			sourceFileIRIs: func(_ context.Context, _ string) ([]string, error) {
				return []string{legacyIRI, fileIRI(file)}, nil
			},
		})
		examined, blindSpot := s.sourceFileExamined(context.Background(), file, testRepositoryIdentity)
		if !examined || blindSpot != "" {
			t.Fatalf("examined=%v blindSpot=%q, want true and no blind spot", examined, blindSpot)
		}
	})

	t.Run("with no domain named there is nothing to attribute it to", func(t *testing.T) {
		s := newTestServer(fakeStore{
			sourceFileIRIs: func(_ context.Context, _ string) ([]string, error) {
				return []string{legacyIRI}, nil
			},
		})
		examined, blindSpot := s.sourceFileExamined(context.Background(), file, "")
		if !examined || blindSpot != "" {
			t.Fatalf("examined=%v blindSpot=%q, want true and no blind spot", examined, blindSpot)
		}
	})
}
