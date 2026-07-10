// SPDX-License-Identifier: AGPL-3.0-only

package oxigraph_test

// Reload replace-semantics fixture (Weak Spot 2).
//
// The evidence plane depends on a reload REPLACING the default graph, never
// merging into it. Client.Load uses the SPARQL Graph Store Protocol
// `PUT /store?default` (replace); a regression to POST (merge/append) would make
// reloads accumulate and let removed triples linger — e.g. a candidate promoted
// to a realization would still be queryable from stale triples.
//
// This test needs no live Oxigraph: the httptest handler faithfully models
// Oxigraph's PUT-vs-POST behaviour on /store?default (PUT replaces the default
// graph, POST appends), so switching Load to POST would change the observed
// triple set and fail here. A live-store version lives behind the `integration`
// build tag in oxigraph_reload_integration_test.go.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/globulario/awareness-graph/golang/store/oxigraph"
)

func ntLines(s string) []string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func TestLoad_ReplaceSemantics_Idempotent(t *testing.T) {
	var mu sync.Mutex
	graph := map[string]bool{} // models the store's default graph
	var methods, targets []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		methods = append(methods, r.Method)
		targets = append(targets, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.Method {
		case http.MethodPut: // Graph Store Protocol: REPLACE the default graph
			graph = map[string]bool{}
			for _, ln := range ntLines(string(body)) {
				graph[ln] = true
			}
		case http.MethodPost: // merge/append — the anti-pattern this test guards against
			for _, ln := range ntLines(string(body)) {
				graph[ln] = true
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	c, err := oxigraph.New(ts.URL + "/query")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()

	tA := "<urn:s> <urn:p> <urn:a> ."
	tB := "<urn:s> <urn:p> <urn:b> ."
	tC := "<urn:s> <urn:p> <urn:c> ."
	full := tA + "\n" + tB + "\n" + tC + "\n" // 3 triples
	smaller := tA + "\n" + tB + "\n"          // 2 triples (tC removed)

	mustLoad := func(label, content string) {
		if err := c.Load(ctx, strings.NewReader(content)); err != nil {
			t.Fatalf("Load %s: %v", label, err)
		}
	}
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(graph) }

	// First load establishes the canonical set.
	mustLoad("#1", full)
	if got := count(); got != 3 {
		t.Fatalf("after first load: %d triples, want 3", got)
	}
	// IDEMPOTENCE: reloading identical content must not accumulate.
	mustLoad("#2 (identical)", full)
	if got := count(); got != 3 {
		t.Fatalf("reload of identical content gave %d triples, want 3 — PUT must replace, not accumulate "+
			"(POST/merge would double to 3-via-set then keep growing on real changes)", got)
	}
	// REPLACE: reloading with a triple removed must drop it, not let it linger.
	mustLoad("#3 (tC removed)", smaller)
	if got := count(); got != 2 {
		t.Fatalf("reload with tC removed gave %d triples, want 2 — a lingering triple means merge semantics", got)
	}
	mu.Lock()
	lingered := graph[tC]
	mu.Unlock()
	if lingered {
		t.Fatalf("removed triple %q lingered after reload — reload merged instead of replaced", tC)
	}

	// MECHANISM: every Load is a PUT to the default-graph store endpoint.
	if len(methods) != 3 {
		t.Fatalf("expected 3 Load requests, saw %d", len(methods))
	}
	for i, m := range methods {
		if m != http.MethodPut {
			t.Fatalf("Load request %d used %s, want PUT (replace); POST would merge/append", i, m)
		}
	}
	for i, tg := range targets {
		if !strings.HasPrefix(tg, "/store?") || !strings.Contains(tg, "default") {
			t.Fatalf("Load request %d targeted %q, want /store?default (Graph Store Protocol default graph)", i, tg)
		}
	}
}
