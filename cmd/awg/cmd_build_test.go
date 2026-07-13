// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/seedmeta"
)

func TestFinalizeBuildArtifact_DedupsBeforeMarker(t *testing.T) {
	raw := []byte(
		"<https://example.test/s> <https://example.test/p> \"one\" .\n" +
			"<https://example.test/s> <https://example.test/p> \"one\" .\n" +
			"<https://example.test/s> <https://example.test/q> \"two\" .\n",
	)

	finalNT, marker, uniqueCount, dupCount := finalizeBuildArtifact(raw)
	if uniqueCount != 2 {
		t.Fatalf("uniqueCount=%d, want 2", uniqueCount)
	}
	if dupCount != 1 {
		t.Fatalf("dupCount=%d, want 1", dupCount)
	}
	if marker.TripleCount != 8 {
		t.Fatalf("marker.TripleCount=%d, want 8", marker.TripleCount)
	}
	if got := bytes.Count(finalNT, []byte("\n")); got != 8 {
		t.Fatalf("final triple lines=%d, want 8", got)
	}
}

func TestRunBuild_WritesGraphMarkerFileAfterVerifiedLoad(t *testing.T) {
	repo := t.TempDir()
	awarenessDir := filepath.Join(repo, "docs", "awareness")
	embeddataDir := filepath.Join(repo, "golang", "server", "embeddata")
	if err := os.MkdirAll(awarenessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(embeddataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFixtureYAMLs(filepath.Join("..", "..", "golang", "extractor", "testdata"), awarenessDir); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	markerPath := filepath.Join(repo, ".awg", "graph-authority.json")
	txPath := seedmeta.RuntimeTransactionPath(markerPath)

	var loaded []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/store":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read store body: %v", err)
			}
			loaded = append([]byte(nil), body...)
			w.WriteHeader(http.StatusNoContent)
		case "/query":
			writeVerificationQuery(t, w, loaded, readQueryBody(t, r))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chdir(cwd)
	}()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	if code := runBuild([]string{
		"-input", awarenessDir,
		"-all", // whole-store replace now requires an explicit scope opt-in
		"-store-url", ts.URL + "/store?default",
		"-graph-marker-file", markerPath,
		"-ag-repo", repo,
	}); code != 0 {
		t.Fatalf("runBuild code=%d, want 0", code)
	}
	written, err := seedmeta.ReadMarkerFile(markerPath)
	if err != nil {
		t.Fatalf("read marker file: %v", err)
	}
	loadedMarker, ok := seedmeta.ParseMarker(loaded)
	if !ok {
		t.Fatal("loaded artifact missing marker")
	}
	if written.Digest != loadedMarker.Digest {
		t.Fatalf("marker digest=%s, want %s", written.Digest, loadedMarker.Digest)
	}
	if written.TripleCount != loadedMarker.TripleCount {
		t.Fatalf("marker triple count=%d, want %d", written.TripleCount, loadedMarker.TripleCount)
	}
	txBytes, err := os.ReadFile(txPath)
	if err != nil {
		t.Fatalf("read transaction file: %v", err)
	}
	if !bytes.Contains(txBytes, []byte("seed\tdigest_sha256\t"+loadedMarker.Digest)) {
		t.Fatalf("transaction file missing loaded digest:\n%s", string(txBytes))
	}
}

// TestRunBuild_FailsClosedWithoutScope locks the fail-closed contract: a bare
// `sensei build` (no --repo, no --all, no --output) must refuse to touch the
// store rather than silently replace the whole graph — the exact accidental
// wipe the scope flags prevent.
func TestRunBuild_FailsClosedWithoutScope(t *testing.T) {
	repo := t.TempDir()
	awarenessDir := filepath.Join(repo, "docs", "awareness")
	if err := os.MkdirAll(awarenessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFixtureYAMLs(filepath.Join("..", "..", "golang", "extractor", "testdata"), awarenessDir); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)

	var storeHits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		storeHits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	code := runBuild([]string{
		"-input", awarenessDir,
		"-store-url", ts.URL + "/store?default",
	})
	if code != 2 {
		t.Fatalf("bare build code=%d, want 2 (fail-closed without --repo/--all)", code)
	}
	if storeHits != 0 {
		t.Fatalf("store contacted %d times; fail-closed must not touch the store", storeHits)
	}
}

// TestRunBuild_ScopedRepoUpdate_NonDestructive exercises the --repo path end to
// end against a stateful fake store: it must issue a scoped SPARQL DELETE (not a
// whole-graph PUT), append the compiled slice and then the recomputed marker,
// and publish a marker file that matches the marker recomputed over the
// post-update store contents.
func TestRunBuild_ScopedRepoUpdate_NonDestructive(t *testing.T) {
	repo := t.TempDir()
	awarenessDir := filepath.Join(repo, "docs", "awareness")
	if err := os.MkdirAll(awarenessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFixtureYAMLs(filepath.Join("..", "..", "golang", "extractor", "testdata"), awarenessDir); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	markerPath := filepath.Join(repo, ".awg", "graph-authority.json")
	txPath := seedmeta.RuntimeTransactionPath(markerPath)

	const domain = "github.com/test/scoped"

	// The bytes GET /store returns — the "post-update" store the scoped build
	// recomputes its whole-graph marker over.
	base := []byte(
		"<https://example.test/a> <https://globular.io/awareness#repo> \"" + domain + "\" .\n" +
			"<https://example.test/a> <https://example.test/p> \"x\" .\n" +
			"<https://example.test/b> <https://example.test/p> \"home\" .\n")
	_, expected := seedmeta.AppendMarker(base)

	var (
		updateBodies []string
		appendBodies []string
		markerLoaded bool
	)
	liveCount := func() int64 {
		var n int64
		for _, ln := range strings.Split(string(base), "\n") {
			if strings.TrimSpace(ln) != "" {
				n++
			}
		}
		if markerLoaded {
			n += 6
		}
		return n
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/update":
			b, _ := io.ReadAll(r.Body)
			updateBodies = append(updateBodies, string(b))
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/store" && r.Method == http.MethodPost:
			b, _ := io.ReadAll(r.Body)
			body := string(b)
			appendBodies = append(appendBodies, body)
			if strings.Contains(body, "SeedBuild") {
				markerLoaded = true
			}
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/store" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/n-triples")
			_, _ = w.Write(base)
		case r.URL.Path == "/query":
			body := readQueryBody(t, r)
			w.Header().Set("Content-Type", "application/sparql-results+json")
			switch {
			case strings.Contains(body, "ASK"):
				_, _ = w.Write([]byte(`{"head":{},"boolean":true}`))
			case strings.Contains(body, "COUNT(*)"):
				_, _ = w.Write([]byte(`{"results":{"bindings":[{"n":{"value":"` + itoa(liveCount()) + `"}}]}}`))
			case strings.Contains(body, "COUNT(DISTINCT ?s)") && strings.Contains(body, "SeedBuild"):
				_, _ = w.Write([]byte(`{"results":{"bindings":[{"n":{"value":"1"}}]}}`))
			default: // Describe(marker)
				_, _ = w.Write([]byte(`{"results":{"bindings":[` +
					`{"p":{"type":"uri","value":"https://globular.io/awareness#seedDigestSha256"},"o":{"type":"literal","value":"` + expected.Digest + `"}},` +
					`{"p":{"type":"uri","value":"https://globular.io/awareness#seedTripleCount"},"o":{"type":"literal","value":"` + itoa(expected.TripleCount) + `"}}` +
					`]}}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	code := runBuild([]string{
		"-input", awarenessDir,
		"-repo", domain,
		"-store-url", ts.URL + "/store?default",
		"-graph-marker-file", markerPath,
		"-graph-transaction-file", txPath,
		"-ag-repo", repo,
	})
	if code != 0 {
		t.Fatalf("scoped runBuild code=%d, want 0", code)
	}

	// A scoped DELETE naming the domain + marker class — never a whole-graph PUT.
	if len(updateBodies) != 1 {
		t.Fatalf("update calls=%d, want 1", len(updateBodies))
	}
	if !strings.Contains(updateBodies[0], domain) || !strings.Contains(updateBodies[0], "SeedBuild") {
		t.Fatalf("scoped delete missing domain/SeedBuild:\n%s", updateBodies[0])
	}
	// Two appends: the domain slice, then the recomputed marker.
	if len(appendBodies) != 2 {
		t.Fatalf("append calls=%d, want 2 (slice, marker)", len(appendBodies))
	}
	if !strings.Contains(appendBodies[0], domain) {
		t.Fatalf("first append (slice) missing domain tag:\n%s", appendBodies[0])
	}
	if !strings.Contains(appendBodies[1], "SeedBuild") {
		t.Fatalf("second append should be the recomputed marker:\n%s", appendBodies[1])
	}
	// Marker file matches the marker recomputed over the post-update store.
	written, err := seedmeta.ReadMarkerFile(markerPath)
	if err != nil {
		t.Fatalf("read marker file: %v", err)
	}
	if written.Digest != expected.Digest || written.TripleCount != expected.TripleCount {
		t.Fatalf("marker file = %s/%d, want %s/%d", written.Digest, written.TripleCount, expected.Digest, expected.TripleCount)
	}
	txBytes, err := os.ReadFile(txPath)
	if err != nil {
		t.Fatalf("read transaction file: %v", err)
	}
	if !bytes.Contains(txBytes, []byte("seed\tdigest_sha256\t"+expected.Digest)) {
		t.Fatalf("transaction file missing recomputed graph digest:\n%s", string(txBytes))
	}
}

func TestScopedDeleteUpdate_TargetsDomainAndMarker(t *testing.T) {
	u := scopedDeleteUpdate("github.com/globulario/services")
	for _, want := range []string{
		`<https://globular.io/awareness#repo> "github.com/globulario/services"`,
		"FILTER NOT EXISTS", // sole-owner guard: never delete a co-owned node
		"https://globular.io/awareness#SeedBuild",
	} {
		if !strings.Contains(u, want) {
			t.Fatalf("scopedDeleteUpdate missing %q:\n%s", want, u)
		}
	}
}

func TestSparqlEscapeLiteral(t *testing.T) {
	if got := sparqlEscapeLiteral(`a"b\c`); got != `a\"b\\c` {
		t.Fatalf("escape=%q, want %q", got, `a\"b\\c`)
	}
}

func TestQueryURLFromStore(t *testing.T) {
	got, err := queryURLFromStore("http://h:7878/store?default")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://h:7878/query" {
		t.Fatalf("queryURLFromStore=%q, want http://h:7878/query", got)
	}
}

func TestCountNTriples(t *testing.T) {
	if n := countNTriples([]byte("a .\n\nb .\n")); n != 2 {
		t.Fatalf("countNTriples=%d, want 2", n)
	}
}
