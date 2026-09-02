// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

// The isolation property httptest cannot prove: that publishing ONE domain
// leaves another domain's assertions about the SAME subject untouched.
//
// This file starts its OWN Oxigraph on a free port. It never uses
// AWARENESS_OXIGRAPH_URL or localhost:7878: these are WRITE tests, and a
// shared endpoint may hold real or quarantined evidence that a PUT would
// destroy.

package oxigraph_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/store/oxigraph"
)

func startPrivateOxigraph(t *testing.T, unionDefaultGraph bool) string {
	t.Helper()
	bin := ""
	for _, cand := range []string{"bin/oxigraph", "../../../bin/oxigraph"} {
		if abs, err := filepath.Abs(cand); err == nil {
			if st, err := os.Stat(abs); err == nil && !st.IsDir() {
				bin = abs
				break
			}
		}
	}
	if bin == "" {
		if p, err := exec.LookPath("oxigraph"); err == nil {
			bin = p
		}
	}
	if bin == "" {
		t.Skip("oxigraph binary unavailable: run scripts/fetch-oxigraph.sh")
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	args := []string{"serve", "--location", t.TempDir(), "--bind", addr}
	if unionDefaultGraph {
		args = append(args, "--union-default-graph")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, args...)
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start oxigraph: %v", err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })

	queryURL := "http://" + addr + "/query"
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Post(queryURL, "application/sparql-query", strings.NewReader("ASK{}"))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return queryURL
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("oxigraph did not become healthy at %s", queryURL)
	return ""
}

func countMatching(t *testing.T, queryURL, sparql string) string {
	t.Helper()
	resp, err := http.Post(queryURL, "application/sparql-query", strings.NewReader(sparql))
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	return string(body)
}

// countValue decodes the ?n binding of a SPARQL COUNT.
//
// Substring-matching the raw result is FALSE-GREEN: Oxigraph reports an integer
// COUNT with datatype http://www.w3.org/2001/XMLSchema#integer, and that URI
// contains "0", so strings.Contains(result, "0") succeeds for ANY count. A test
// written that way passes whether or not the property holds.
func countValue(t *testing.T, queryURL, sparql string) int {
	t.Helper()
	var out struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	raw := countMatching(t, queryURL, sparql)
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode count result: %v\nraw: %s", err, raw)
	}
	if len(out.Results.Bindings) != 1 {
		t.Fatalf("expected exactly one binding, got %d\nraw: %s", len(out.Results.Bindings), raw)
	}
	b, ok := out.Results.Bindings[0]["n"]
	if !ok {
		t.Fatalf("no ?n binding\nraw: %s", raw)
	}
	n, err := strconv.Atoi(b.Value)
	if err != nil {
		t.Fatalf("count %q is not an integer: %v", b.Value, err)
	}
	return n
}

func triple(subject, object string) string {
	return fmt.Sprintf("<%s> <https://globular.io/awareness#requiresTest> %q .\n", subject, object)
}

// The defect this replaces: publishing sensei moved the services slice from
// 059f47624207 to 5d0eb2f4eb85 and cost services its closure proof, because
// slice identity was computed over tagged SUBJECTS and 173 identifiers are
// legitimately co-authored. Scoped to a graph, the PUT cannot reach a neighbour.
func TestIntegration_PublishingOneDomainLeavesAnotherUntouched(t *testing.T) {
	queryURL := startPrivateOxigraph(t, true)
	c, err := oxigraph.New(queryURL)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx := context.Background()

	const (
		sensei   = "github.com/globulario/sensei"
		services = "github.com/globulario/services"
		shared   = "https://ex.org/inv.shared"
	)

	if err := c.LoadGraph(ctx, sensei, strings.NewReader(triple(shared, "TestFromSensei"))); err != nil {
		t.Fatalf("publish sensei: %v", err)
	}
	if err := c.LoadGraph(ctx, services, strings.NewReader(triple(shared, "TestFromServices"))); err != nil {
		t.Fatalf("publish services: %v", err)
	}

	both := countMatching(t, queryURL, `SELECT ?g ?o WHERE { GRAPH ?g { <`+shared+`> ?p ?o } } ORDER BY ?g`)
	if !strings.Contains(both, "TestFromSensei") || !strings.Contains(both, "TestFromServices") {
		t.Fatalf("two domains could not both speak about one subject:\n%s", both)
	}

	// Republish ONE domain. Under subject ownership this is what cost the
	// neighbour its proof.
	if err := c.LoadGraph(ctx, sensei, strings.NewReader(triple(shared, "TestFromSenseiV2"))); err != nil {
		t.Fatalf("republish sensei: %v", err)
	}

	after := countMatching(t, queryURL, `SELECT ?g ?o WHERE { GRAPH ?g { <`+shared+`> ?p ?o } } ORDER BY ?g`)
	if !strings.Contains(after, "TestFromSenseiV2") {
		t.Fatalf("republish did not take effect:\n%s", after)
	}
	if !strings.Contains(after, "TestFromServices") {
		t.Fatalf("publishing sensei destroyed the services assertion — the original defect:\n%s", after)
	}
	if strings.Contains(after, "TestFromSensei\"") {
		t.Fatalf("republish did not REPLACE the domain's own graph:\n%s", after)
	}
}

// Unattributed content must be claimable by NO domain. A partially migrated
// corpus therefore reports affected domains UNPROVEN by construction rather
// than falling back to subject tags.
func TestIntegration_DefaultGraphContentIsAttributableToNoDomain(t *testing.T) {
	queryURL := startPrivateOxigraph(t, true)
	c, _ := oxigraph.New(queryURL)
	ctx := context.Background()

	legacy := "<https://ex.org/legacy> <https://globular.io/awareness#label> \"unmigrated\" .\n"
	if err := c.Load(ctx, strings.NewReader(legacy)); err != nil {
		t.Fatalf("load default graph: %v", err)
	}
	if err := c.LoadGraph(ctx, "github.com/globulario/sensei",
		strings.NewReader(triple("https://ex.org/inv.x", "TestX"))); err != nil {
		t.Fatalf("publish sensei: %v", err)
	}

	// Visible to the existing unqualified read surface...
	all := countMatching(t, queryURL, `SELECT ?s WHERE { ?s ?p ?o } ORDER BY ?s`)
	if !strings.Contains(all, "legacy") || !strings.Contains(all, "inv.x") {
		t.Fatalf("--union-default-graph did not expose both legacy and named content:\n%s", all)
	}

	// ...but claimable by no domain.
	attributed := countMatching(t, queryURL, `SELECT ?g ?s WHERE { GRAPH ?g { ?s ?p ?o } }`)
	if strings.Contains(attributed, "legacy") {
		t.Fatalf("unattributed content was claimed by a domain:\n%s", attributed)
	}
}

// Named graphs are ALREADY in use, and they are not domains.
//
// `sensei build` PUTs a candidate slice and a SECOND seed marker into
// urn:sensei:graph-staging:<marker>, then promotes it in one transaction. Two
// properties matter, and they pull in opposite directions:
//
//	default-only reads  atomic publication holds; a named-graph domain is INVISIBLE
//	union reads         a domain becomes visible; so does every in-flight candidate
//
// This test pins BOTH, because they are why the carrier lands while the read
// surface stays default-only. The earlier version of this test asserted only
// the second half and did so with a substring check that passed for any count.
func TestIntegration_UnionReadsWouldExposeInFlightStagingGraphs(t *testing.T) {
	const stagingIRI = "urn:sensei:graph-staging:testmarker"

	for _, tc := range []struct {
		name              string
		union             bool
		wantUnqualified   int
		wantStagingHidden bool
	}{
		{"default-only reads: staging is invisible, atomicity holds", false, 0, true},
		{"union reads: staging leaks into every unqualified query", true, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queryURL := startPrivateOxigraph(t, tc.union)
			c, err := oxigraph.New(queryURL)
			if err != nil {
				t.Fatalf("new: %v", err)
			}
			ctx := context.Background()

			// A published domain...
			if err := c.LoadGraph(ctx, "github.com/globulario/sensei",
				strings.NewReader(triple("https://ex.org/inv.x", "TestX"))); err != nil {
				t.Fatalf("publish domain: %v", err)
			}
			// ...and an UNPUBLISHED candidate staged by sensei build.
			staged := triple("https://ex.org/inv.candidate", "TestCandidate")
			req, err := http.NewRequest(http.MethodPut,
				strings.TrimSuffix(queryURL, "/query")+"/store?graph="+url.QueryEscape(stagingIRI),
				strings.NewReader(staged))
			if err != nil {
				t.Fatalf("stage request: %v", err)
			}
			req.Header.Set("Content-Type", "application/n-triples")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("stage candidate: %v", err)
			}
			_ = resp.Body.Close()

			got := countValue(t, queryURL, `SELECT (COUNT(*) AS ?n) WHERE { ?s ?p ?o }`)
			if got != tc.wantUnqualified {
				t.Fatalf("unqualified read surface = %d triples, want %d", got, tc.wantUnqualified)
			}

			leaked := countValue(t, queryURL,
				`SELECT (COUNT(*) AS ?n) WHERE { ?s ?p ?o . FILTER(CONTAINS(STR(?s), "inv.candidate")) }`)
			if tc.wantStagingHidden && leaked != 0 {
				t.Fatalf("an unpublished candidate was visible to an unqualified read (%d triples)", leaked)
			}
			if !tc.wantStagingHidden && leaked == 0 {
				t.Fatal("expected union reads to expose the staged candidate; they did not, " +
					"so this test no longer demonstrates the hazard")
			}
		})
	}
}

// A relative graph name is NOT rejected by Oxigraph -- it is resolved against
// the server's base URL, so "github.com/globulario/sensei" published at
// 127.0.0.1:7988 becomes http://127.0.0.1:7988/github.com/globulario/sensei.
// The endpoint would become part of the domain's identity and the same
// repository would be a different graph at a different address. This pins the
// mapping against a real store, on two different ports.
func TestIntegration_GraphIRIIsIndependentOfTheEndpoint(t *testing.T) {
	const domain = "github.com/globulario/sensei"
	seen := map[string]bool{}

	for _, run := range []string{"first endpoint", "second endpoint"} {
		queryURL := startPrivateOxigraph(t, false)
		c, err := oxigraph.New(queryURL)
		if err != nil {
			t.Fatalf("%s: new: %v", run, err)
		}
		if err := c.LoadGraph(context.Background(), domain,
			strings.NewReader(triple("https://ex.org/inv.x", "TestX"))); err != nil {
			t.Fatalf("%s: publish: %v", run, err)
		}
		raw := countMatching(t, queryURL, `SELECT ?g WHERE { GRAPH ?g { ?s ?p ?o } }`)
		if !strings.Contains(raw, oxigraph.GraphIRI(domain)) {
			t.Fatalf("%s: graph IRI is not the stable mapping %q:\n%s", run, oxigraph.GraphIRI(domain), raw)
		}
		if strings.Contains(raw, "127.0.0.1") {
			t.Fatalf("%s: the endpoint leaked into the graph IRI:\n%s", run, raw)
		}
		seen[oxigraph.GraphIRI(domain)] = true
	}
	if len(seen) != 1 {
		t.Fatalf("one domain produced %d distinct graph IRIs", len(seen))
	}
}
