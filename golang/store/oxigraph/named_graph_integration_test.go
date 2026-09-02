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
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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
	buf := make([]byte, 8192)
	n, _ := resp.Body.Read(buf)
	return string(buf[:n])
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

// Without the flag, every unqualified query in this codebase returns ZERO rows.
// That is the migration's worst failure mode: rows: [] is an observation, not
// evidence of absence, so a silently empty read surface looks like a clean graph.
func TestIntegration_WithoutUnionDefaultGraphTheReadSurfaceGoesEmpty(t *testing.T) {
	queryURL := startPrivateOxigraph(t, false)
	c, _ := oxigraph.New(queryURL)
	if err := c.LoadGraph(context.Background(), "github.com/globulario/sensei",
		strings.NewReader(triple("https://ex.org/inv.x", "TestX"))); err != nil {
		t.Fatalf("publish: %v", err)
	}
	unqualified := countMatching(t, queryURL, `SELECT (COUNT(*) AS ?n) WHERE { ?s ?p ?o }`)
	if !strings.Contains(unqualified, "0") {
		t.Fatalf("expected an empty unqualified read surface without the flag, got:\n%s", unqualified)
	}
	viaGraph := countMatching(t, queryURL, `SELECT ?g WHERE { GRAPH ?g { ?s ?p ?o } }`)
	if !strings.Contains(viaGraph, "sensei") {
		t.Fatalf("content was not readable via GRAPH ?g either:\n%s", viaGraph)
	}
	_ = url.QueryEscape
}
