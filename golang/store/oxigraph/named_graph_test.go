// SPDX-License-Identifier: AGPL-3.0-only

package oxigraph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The graph slot is the provenance slot: N-Triples has three components and an
// assertion has four. These proofs cover the addressing and the refusal; the
// isolation property itself needs a real store and lives in the integration
// lane.

func TestGraphURLEscapesTheDomain(t *testing.T) {
	c, err := New("http://127.0.0.1:7878/query")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	got := c.graphURL("github.com/globulario/sensei")

	// A domain is a repository URL. Unescaped, its ":" and "/" would terminate
	// or re-key the query parameter and the PUT would land somewhere else --
	// possibly on another domain's graph.
	if strings.Contains(got, "graph=https://") {
		t.Fatalf("domain was not escaped, so the PUT target is ambiguous: %s", got)
	}
	want := "http://127.0.0.1:7878/store?graph=https%3A%2F%2Fgithub.com%2Fglobulario%2Fsensei"
	if got != want {
		t.Fatalf("graph URL\n  got  %s\n  want %s", got, want)
	}
}

func TestGraphURLIsDistinctPerDomain(t *testing.T) {
	c, _ := New("http://127.0.0.1:7878/query")

	// A relative graph name is resolved against the SERVER's base URL, so the
	// endpoint would leak into the domain's identity. The graph IRI must be the
	// same no matter which store is being written to.
	other, _ := New("http://10.1.2.3:9999/query")
	if x, y := GraphIRI("github.com/globulario/sensei"), GraphIRI("github.com/globulario/sensei"); x != y {
		t.Fatalf("graph IRI is unstable: %s vs %s", x, y)
	}
	if !strings.HasPrefix(GraphIRI("github.com/globulario/sensei"), "https://") {
		t.Fatalf("graph IRI is not absolute: %s", GraphIRI("github.com/globulario/sensei"))
	}
	if a, b := c.graphURL("github.com/globulario/sensei"), other.graphURL("github.com/globulario/sensei"); !strings.HasSuffix(a, strings.SplitN(b, "/store?", 2)[1]) {
		t.Fatalf("the same domain addressed a different graph on a different endpoint:\n  %s\n  %s", a, b)
	}

	if a, b := c.graphURL("github.com/globulario/sensei"), c.graphURL("github.com/globulario/services"); a == b {
		t.Fatalf("two domains addressed the same graph: %s", a)
	}
	if g, d := c.graphURL("github.com/globulario/sensei"), c.storeURL(); g == d {
		t.Fatal("a named graph resolved to the default-graph endpoint")
	}
}

// An unnamed domain must be REFUSED, never redirected to the default graph:
// a caller that meant to publish one domain and instead replaced the whole
// store would destroy every other domain's assertions.
func TestLoadGraphRefusesAnUnnamedDomain(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RequestURI()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c, err := New(srv.URL + "/query")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	for _, domain := range []string{"", "   ", "\t"} {
		err := c.LoadGraph(context.Background(), domain, strings.NewReader(""))
		if err == nil {
			t.Fatalf("domain %q: accepted, and would have replaced the default graph", domain)
		}
		if got != "" {
			t.Fatalf("domain %q: refused but still issued a request to %s", domain, got)
		}
	}
}

// An ALIAS is a second graph for one logical domain. Each of these addresses a
// different named graph than the canonical spelling, so a later publication
// under the canonical form replaces only one of them and stale assertions
// survive beside current ones -- exactly the per-domain replacement this
// primitive promises.
func TestLoadGraphRefusesNoncanonicalDomainAliases(t *testing.T) {
	var reached string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = r.URL.RequestURI()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c, err := New(srv.URL + "/query")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	for _, alias := range []string{
		"GitHub.com/globulario/sensei",         // case
		"https://github.com/globulario/sensei", // scheme
		"github.com",                           // host with no path
		"github.com/globulario/sensei ",        // trailing whitespace
	} {
		reached = ""
		if err := c.LoadGraph(context.Background(), alias, strings.NewReader("")); err == nil {
			t.Fatalf("alias %q accepted: it addresses a second graph for one logical domain", alias)
		}
		if reached != "" {
			t.Fatalf("alias %q refused but still issued a request to %s", alias, reached)
		}
	}

	// The canonical spelling must still work, or the check is just a wall.
	if err := c.LoadGraph(context.Background(), "github.com/globulario/sensei", strings.NewReader("")); err != nil {
		t.Fatalf("canonical domain rejected: %v", err)
	}
}

func TestLoadGraphPutsToTheDomainsGraph(t *testing.T) {
	var method, uri, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, uri = r.Method, r.URL.RequestURI()
		b := make([]byte, 512)
		n, _ := r.Body.Read(b)
		body = string(b[:n])
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c, _ := New(srv.URL + "/query")
	const nt = `<https://ex.org/s> <https://ex.org/p> "o" .` + "\n"
	if err := c.LoadGraph(context.Background(), "github.com/globulario/sensei", strings.NewReader(nt)); err != nil {
		t.Fatalf("load graph: %v", err)
	}
	if method != http.MethodPut {
		t.Fatalf("method = %s, want PUT (Graph Store Protocol replace)", method)
	}
	if !strings.Contains(uri, "graph=") || strings.Contains(uri, "default") {
		t.Fatalf("PUT went to %s, not a named graph", uri)
	}
	if body != nt {
		t.Fatalf("body altered in transit:\n  got  %q\n  want %q", body, nt)
	}
}
