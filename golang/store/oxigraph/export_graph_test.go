// SPDX-License-Identifier: AGPL-3.0-only

package oxigraph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// twoDomainStore serves two publication domains, each in its own named graph,
// and records every request it was asked for.
//
// It answers the Graph Store Protocol the way a real store does: a GET carrying
// ?graph=<iri> returns THAT graph, and a request for the union (no graph
// parameter) returns everything. A reader that assembled one domain's graph
// from the union would pass a single-domain fixture and leak on a real store,
// which is the whole reason the fixture holds two.
type twoDomainStore struct {
	graphs   map[string]string
	requests []string
	methods  []string
}

func newTwoDomainStore(t *testing.T) (*twoDomainStore, *Client) {
	t.Helper()
	s := &twoDomainStore{graphs: map[string]string{
		"https://github.com/globulario/sensei-code": "<https://example.org/a> <https://example.org/p> \"A\" .\n",
		"https://github.com/globulario/services":    "<https://example.org/b> <https://example.org/p> \"B\" .\n",
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests = append(s.requests, r.URL.RequestURI())
		s.methods = append(s.methods, r.Method)
		graph := r.URL.Query().Get("graph")
		if graph == "" {
			// The union. Serving it here is what lets the leak test fail loudly
			// rather than silently returning one domain.
			var all strings.Builder
			for _, v := range s.graphs {
				all.WriteString(v)
			}
			w.Header().Set("Content-Type", "application/n-triples")
			_, _ = w.Write([]byte(all.String()))
			return
		}
		body, ok := s.graphs[graph]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/n-triples")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL + "/query")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return s, c
}

// The defect this surface exists to make impossible: one domain's export must
// never carry another's assertions. A CONSTRUCT over the default graph reads the
// store's union, and on a single-domain store the difference is invisible.
func TestExportDomainGraphReturnsOnlyThatDomainsGraph(t *testing.T) {
	store, c := newTwoDomainStore(t)

	nt, err := c.ExportDomainGraph(context.Background(), "github.com/globulario/sensei-code")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(string(nt), `"A"`) {
		t.Fatalf("the exported graph does not carry its own domain's triples:\n%s", nt)
	}
	if strings.Contains(string(nt), `"B"`) {
		t.Fatalf("domain A's export leaked domain B's assertions:\n%s", nt)
	}
	// Addressed BY the graph slot, not filtered after the fact.
	if len(store.requests) != 1 {
		t.Fatalf("requests = %v, want exactly one", store.requests)
	}
	wantGraph := url.QueryEscape(GraphIRI("github.com/globulario/sensei-code"))
	if !strings.Contains(store.requests[0], "graph="+wantGraph) {
		t.Fatalf("request %q does not address the domain's named graph", store.requests[0])
	}
	// Read-only: the Graph Store Protocol's read verb, never a write verb.
	if store.methods[0] != http.MethodGet {
		t.Fatalf("export used %s; an export must not write", store.methods[0])
	}
}

// A non-canonical spelling addresses a DIFFERENT graph URL, so it would export
// an empty graph nobody publishes to and be indistinguishable from a domain that
// exists and is empty. It is refused before any request is issued -- the same
// contract LoadGraph applies to writes.
func TestExportDomainGraphRefusesNoncanonicalAndUnnamedDomains(t *testing.T) {
	store, c := newTwoDomainStore(t)

	for _, domain := range []string{
		"",
		"   ",
		"GitHub.com/globulario/sensei-code", // case
		"https://github.com/globulario/sensei-code", // scheme
		"github.com",                            // host with no path
		"github.com/globulario/sensei-code?x=1", // query
	} {
		if _, err := c.ExportDomainGraph(context.Background(), domain); err == nil {
			t.Fatalf("domain %q was accepted", domain)
		}
	}
	if len(store.requests) != 0 {
		t.Fatalf("a refused export still reached the store: %v", store.requests)
	}
}

// A domain this store publishes nothing for is NOT an empty graph. Returning
// empty bytes would let a caller snapshot nothing and bind work to it.
func TestExportDomainGraphReportsAnUnpublishedDomainRatherThanEmptyBytes(t *testing.T) {
	_, c := newTwoDomainStore(t)

	nt, err := c.ExportDomainGraph(context.Background(), "github.com/globulario/not-published")
	if err == nil {
		t.Fatalf("an unpublished domain exported %d bytes", len(nt))
	}
	if !strings.Contains(err.Error(), "serves no graph") {
		t.Fatalf("error does not say the store serves no graph for it: %v", err)
	}
}

// Two exports of one unchanged graph are the same bytes, so anything deriving
// identity from them derives the same identity.
func TestExportDomainGraphIsDeterministicForAnUnchangedGraph(t *testing.T) {
	_, c := newTwoDomainStore(t)

	first, err := c.ExportDomainGraph(context.Background(), "github.com/globulario/services")
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.ExportDomainGraph(context.Background(), "github.com/globulario/services")
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("two exports of one unchanged graph differ:\n%s\n---\n%s", first, second)
	}
}
