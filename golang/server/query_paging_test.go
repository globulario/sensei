// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/store"
)

// pagingStore is a fakeStore that can page and count a class, as the oxigraph
// client does.
type pagingStore struct {
	fakeStore
	population int
	countErr   error
}

func (p pagingStore) page(classIRI string, limit, offset int) []store.ImpactFact {
	var out []store.ImpactFact
	for i := offset; i < p.population && len(out) < limit; i++ {
		out = append(out, store.ImpactFact{
			NodeIRI: fmt.Sprintf("https://globular.io/awareness#invariant/inv%03d", i),
			TypeIRI: classIRI,
		})
	}
	return out
}

func (p pagingStore) ClassFactsPage(_ context.Context, classIRI string, limit, offset int) ([]store.ImpactFact, error) {
	return p.page(classIRI, limit, offset), nil
}

func (p pagingStore) ClassFactsScopedPage(_ context.Context, classIRI, _, _ string, limit, offset int) ([]store.ImpactFact, error) {
	return p.page(classIRI, limit, offset), nil
}

func (p pagingStore) ClassCount(_ context.Context, _ string) (int, error) {
	if p.countErr != nil {
		return 0, p.countErr
	}
	return p.population, nil
}

func (p pagingStore) ClassCountScoped(_ context.Context, _, _, _ string) (int, error) {
	return p.ClassCount(context.Background(), "")
}

func byClass(offset int) *awarenesspb.QueryRequest {
	return &awarenesspb.QueryRequest{
		Mode:   awarenesspb.QueryMode_QUERY_MODE_BY_CLASS,
		Class:  awarenesspb.QueryClass_QUERY_CLASS_INVARIANT,
		Limit:  100,
		Offset: int32(offset),
	}
}

// A class larger than one page can be walked to completion. Before the offset
// existed the rows past the cap were unreachable by any request, so a caller
// could not enumerate a class at all.
func TestQuery_ByClass_PagesToCompletion(t *testing.T) {
	s := newTestServer(pagingStore{population: 250})
	seen := map[string]bool{}
	offset, pages := 0, 0
	for {
		resp, err := s.Query(context.Background(), byClass(offset))
		if err != nil {
			t.Fatalf("page at offset %d: %v", offset, err)
		}
		if !resp.GetTotalKnown() || resp.GetTotal() != 250 {
			t.Fatalf("page at offset %d does not report the population: total=%d known=%v",
				offset, resp.GetTotal(), resp.GetTotalKnown())
		}
		for _, r := range resp.GetRows() {
			if seen[r.GetId()] {
				t.Fatalf("row %s was served twice, so paging overlaps", r.GetId())
			}
			seen[r.GetId()] = true
		}
		offset += len(resp.GetRows())
		pages++
		if !resp.GetTruncated() {
			break
		}
		if pages > 10 {
			t.Fatal("paging did not terminate")
		}
	}
	if len(seen) != 250 {
		t.Fatalf("walked %d of 250 rows; enumeration is still incomplete", len(seen))
	}
	if pages != 3 {
		t.Fatalf("expected 3 pages of at most 100, got %d", pages)
	}
}

// The last page says so. A caller that cannot tell a complete answer from a
// capped one reports the cap as the population, which is the whole defect.
func TestQuery_ByClass_TruncatedMarksOnlyIncompletePages(t *testing.T) {
	s := newTestServer(pagingStore{population: 150})
	first, err := s.Query(context.Background(), byClass(0))
	if err != nil {
		t.Fatal(err)
	}
	if !first.GetTruncated() {
		t.Fatal("a full page with rows remaining was not marked truncated")
	}
	last, err := s.Query(context.Background(), byClass(100))
	if err != nil {
		t.Fatal(err)
	}
	if last.GetTruncated() {
		t.Fatal("the final page was marked truncated, so a caller would page forever")
	}
	if len(last.GetRows()) != 50 {
		t.Fatalf("final page has %d rows, want 50", len(last.GetRows()))
	}
}

// An unknown total must not be reported as a total of zero. Conflating the two
// is the same class of bug as silent truncation: it reads as a definite fact
// about the graph.
func TestQuery_ByClass_UnknownTotalIsNotZero(t *testing.T) {
	s := newTestServer(pagingStore{population: 250, countErr: fmt.Errorf("count unavailable")})
	resp, err := s.Query(context.Background(), byClass(0))
	if err != nil {
		t.Fatalf("a failed count must not fail the query: %v", err)
	}
	if resp.GetTotalKnown() {
		t.Fatal("total_known is set although the count failed")
	}
	if !resp.GetTruncated() {
		t.Fatal("a full page with an unknown total must be treated as possibly truncated")
	}
}

// A store that cannot page refuses an offset instead of serving page 0 again.
// Silently returning rows the caller has already seen, as though they were
// new, is worse than refusing.
func TestQuery_ByClass_OffsetRefusedWhenStoreCannotPage(t *testing.T) {
	s := newTestServer(fakeStore{
		classFacts: func(_ context.Context, classIRI string, _ int) ([]store.ImpactFact, error) {
			return []store.ImpactFact{{NodeIRI: "https://globular.io/awareness#invariant/a", TypeIRI: classIRI}}, nil
		},
	})
	if _, err := s.Query(context.Background(), byClass(100)); status.Code(err) != codes.Unimplemented {
		t.Fatalf("offset on a non-paging store returned %v, want Unimplemented", err)
	}
	// Offset zero is still served: it asks for the first page, which every
	// store can produce.
	if _, err := s.Query(context.Background(), byClass(0)); err != nil {
		t.Fatalf("page 0 was refused on a non-paging store: %v", err)
	}
}

// The per-page cap is unchanged; paging is how a caller gets past it.
func TestQuery_ByClass_PerPageCapStillApplies(t *testing.T) {
	s := newTestServer(pagingStore{population: 500})
	resp, err := s.Query(context.Background(), &awarenesspb.QueryRequest{
		Mode:  awarenesspb.QueryMode_QUERY_MODE_BY_CLASS,
		Class: awarenesspb.QueryClass_QUERY_CLASS_INVARIANT,
		Limit: 9999,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetRows()) != maxQueryLimit {
		t.Fatalf("one page returned %d rows, want the %d cap", len(resp.GetRows()), maxQueryLimit)
	}
	if !resp.GetTruncated() || resp.GetTotal() != 500 {
		t.Fatalf("the capped page does not say what it withheld: truncated=%v total=%d", resp.GetTruncated(), resp.GetTotal())
	}
}
