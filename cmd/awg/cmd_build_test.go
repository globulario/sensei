// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestFinalizeBuildArtifact_CanonicalizesTripleOrder(t *testing.T) {
	first := []byte(
		"<https://example.test/s2> <https://example.test/p> \"two\" .\n" +
			"<https://example.test/s1> <https://example.test/p> \"one\" .\n")
	second := []byte(
		"<https://example.test/s1> <https://example.test/p> \"one\" .\n" +
			"<https://example.test/s2> <https://example.test/p> \"two\" .\n")

	firstNT, firstMarker, firstUnique, firstDup := finalizeBuildArtifact(first)
	secondNT, secondMarker, secondUnique, secondDup := finalizeBuildArtifact(second)

	if firstUnique != secondUnique || firstDup != secondDup {
		t.Fatalf("dedup counts differ: first=(%d,%d) second=(%d,%d)", firstUnique, firstDup, secondUnique, secondDup)
	}
	if string(firstNT) != string(secondNT) {
		t.Fatalf("final graph bytes differ across input order:\nfirst:\n%s\nsecond:\n%s", firstNT, secondNT)
	}
	if firstMarker != secondMarker {
		t.Fatalf("marker differs across input order: %#v vs %#v", firstMarker, secondMarker)
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
// end against a stateful fake store. RDF is first loaded through Graph Store
// Protocol into a named staging graph. The default graph is then changed by one
// control-only SPARQL transaction, never by embedding raw N-Triples in SPARQL.
func TestRunBuild_ScopedRepoUpdate_NonDestructive(t *testing.T) {
	repo, awarenessDir, markerPath, txPath := scopedBuildFixture(t)
	const domain = "github.com/test/scoped"

	baseWithoutMarker := []byte(
		"<https://example.test/old> <https://globular.io/awareness#repo> \"" + domain + "\" .\n" +
			"<https://example.test/old> <https://example.test/p> \"replace me\" .\n" +
			"<https://example.test/other> <https://globular.io/awareness#repo> \"github.com/test/other\" .\n" +
			"<https://example.test/other> <https://example.test/p> \"preserve me\" .\n")
	currentLive, _ := seedmeta.AppendMarker(baseWithoutMarker)
	var (
		staged           []byte
		updateBodies     []string
		stagingPuts      int
		stagingDeletes   int
		defaultMutations int
	)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/store" && r.Method == http.MethodGet && r.URL.Query().Has("default"):
			w.Header().Set("Content-Type", "application/n-triples")
			_, _ = w.Write(currentLive)
		case r.URL.Path == "/store" && r.Method == http.MethodPut && r.URL.Query().Get("graph") != "":
			stagingPuts++
			staged, _ = io.ReadAll(r.Body)
			if got := r.Header.Get("Content-Type"); got != "application/n-triples" {
				t.Fatalf("staging Content-Type=%q", got)
			}
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/store" && r.Method == http.MethodDelete && r.URL.Query().Get("graph") != "":
			stagingDeletes++
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/store" && (r.Method == http.MethodPost || (r.Method == http.MethodPut && r.URL.Query().Has("default"))):
			defaultMutations++
			w.WriteHeader(http.StatusInternalServerError)
		case r.URL.Path == "/update":
			body, _ := io.ReadAll(r.Body)
			updateBodies = append(updateBodies, string(body))
			if len(staged) == 0 {
				t.Fatal("promotion occurred before candidate graph was staged")
			}
			currentLive = append(retainScopedGraph(currentLive, domain), staged...)
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/query":
			body := readQueryBody(t, r)
			if strings.Contains(body, "ASK") {
				w.Header().Set("Content-Type", "application/sparql-results+json")
				_, _ = w.Write([]byte(`{"head":{},"boolean":true}`))
				return
			}
			writeVerificationQuery(t, w, currentLive, body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	code := runScopedBuildInRepo(t, repo, awarenessDir, markerPath, txPath, domain, ts.URL+"/store?default")
	if code != 0 {
		t.Fatalf("scoped runBuild code=%d, want 0", code)
	}
	if stagingPuts != 1 {
		t.Fatalf("staging PUTs=%d, want 1", stagingPuts)
	}
	if stagingDeletes == 0 {
		t.Fatal("staging graph was not cleaned up")
	}
	if defaultMutations != 0 {
		t.Fatalf("default graph direct mutations=%d, want 0", defaultMutations)
	}
	if len(updateBodies) != 1 {
		t.Fatalf("update calls=%d, want 1", len(updateBodies))
	}
	update := updateBodies[0]
	for _, want := range []string{domain, "SeedBuild", "ADD GRAPH <urn:sensei:graph-staging:sha256:", "TO DEFAULT", "DROP GRAPH"} {
		if !strings.Contains(update, want) {
			t.Fatalf("promotion update missing %q:\n%s", want, update)
		}
	}
	if strings.Contains(update, "INSERT DATA") || strings.Contains(update, string(staged)) {
		t.Fatalf("promotion update embeds RDF payload instead of control operations:\n%s", update)
	}
	if !bytes.Contains(staged, []byte(domain)) || !bytes.Contains(staged, []byte("SeedBuild")) {
		t.Fatalf("staging payload lacks replacement domain or marker:\n%s", staged)
	}
	if bytes.Contains(currentLive, []byte("replace me")) || !bytes.Contains(currentLive, []byte("preserve me")) {
		t.Fatalf("scoped publication did not replace only the target domain:\n%s", currentLive)
	}

	expected, ok := seedmeta.ParseMarker(currentLive)
	if !ok {
		t.Fatal("published graph missing marker")
	}
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
		t.Fatalf("transaction file missing published graph digest:\n%s", txBytes)
	}
}

func TestRunBuild_ScopedRepoUpdate_StageFailurePreservesLiveGraph(t *testing.T) {
	repo, awarenessDir, markerPath, txPath := scopedBuildFixture(t)
	const domain = "github.com/test/scoped"
	currentLive, oldMarker := seedmeta.AppendMarker([]byte(
		"<https://example.test/old> <https://globular.io/awareness#repo> \"" + domain + "\" .\n" +
			"<https://example.test/old> <https://example.test/p> \"still live\" .\n"))
	var updateCalls int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/store" && r.Method == http.MethodGet:
			_, _ = w.Write(currentLive)
		case r.URL.Path == "/store" && r.Method == http.MethodPut && r.URL.Query().Get("graph") != "":
			http.Error(w, "injected staging failure", http.StatusBadRequest)
		case r.URL.Path == "/update":
			updateCalls++
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/query":
			body := readQueryBody(t, r)
			if strings.Contains(body, "ASK") {
				w.Header().Set("Content-Type", "application/sparql-results+json")
				_, _ = w.Write([]byte(`{"head":{},"boolean":true}`))
				return
			}
			writeVerificationQuery(t, w, currentLive, body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	if code := runScopedBuildInRepo(t, repo, awarenessDir, markerPath, txPath, domain, ts.URL+"/store?default"); code == 0 {
		t.Fatal("stage failure unexpectedly succeeded")
	}
	if updateCalls != 0 {
		t.Fatalf("promotion calls=%d, want 0 after staging failure", updateCalls)
	}
	got, ok := seedmeta.ParseMarker(currentLive)
	if !ok || got != oldMarker || !bytes.Contains(currentLive, []byte("still live")) {
		t.Fatal("staging failure changed the live generation")
	}
}

func TestRunBuild_ScopedRepoUpdate_PromotionFailurePreservesLiveGraphAndCleansStage(t *testing.T) {
	repo, awarenessDir, markerPath, txPath := scopedBuildFixture(t)
	const domain = "github.com/test/scoped"
	currentLive, oldMarker := seedmeta.AppendMarker([]byte(
		"<https://example.test/old> <https://globular.io/awareness#repo> \"" + domain + "\" .\n" +
			"<https://example.test/old> <https://example.test/p> \"still live\" .\n"))
	var cleanupCalls int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/store" && r.Method == http.MethodGet:
			_, _ = w.Write(currentLive)
		case r.URL.Path == "/store" && r.Method == http.MethodPut && r.URL.Query().Get("graph") != "":
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/store" && r.Method == http.MethodDelete && r.URL.Query().Get("graph") != "":
			cleanupCalls++
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/update":
			http.Error(w, "injected promotion failure", http.StatusBadRequest)
		case r.URL.Path == "/query":
			body := readQueryBody(t, r)
			if strings.Contains(body, "ASK") {
				w.Header().Set("Content-Type", "application/sparql-results+json")
				_, _ = w.Write([]byte(`{"head":{},"boolean":true}`))
				return
			}
			writeVerificationQuery(t, w, currentLive, body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	if code := runScopedBuildInRepo(t, repo, awarenessDir, markerPath, txPath, domain, ts.URL+"/store?default"); code == 0 {
		t.Fatal("promotion failure unexpectedly succeeded")
	}
	if cleanupCalls == 0 {
		t.Fatal("failed promotion did not clean the staging graph")
	}
	got, ok := seedmeta.ParseMarker(currentLive)
	if !ok || got != oldMarker || !bytes.Contains(currentLive, []byte("still live")) {
		t.Fatal("failed promotion changed the live generation")
	}
}

func TestRunBuild_ScopedRepoUpdate_AmbiguousResponseResolvedByLiveMarker(t *testing.T) {
	repo, awarenessDir, markerPath, txPath := scopedBuildFixture(t)
	const domain = "github.com/test/scoped"
	base, _ := seedmeta.AppendMarker([]byte(
		"<https://example.test/old> <https://globular.io/awareness#repo> \"" + domain + "\" .\n" +
			"<https://example.test/old> <https://example.test/p> \"old\" .\n"))
	currentLive := base
	var staged []byte

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/store" && r.Method == http.MethodGet:
			_, _ = w.Write(currentLive)
		case r.URL.Path == "/store" && r.Method == http.MethodPut && r.URL.Query().Get("graph") != "":
			staged, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/store" && r.Method == http.MethodDelete && r.URL.Query().Get("graph") != "":
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/update":
			currentLive = append(retainScopedGraph(currentLive, domain), staged...)
			http.Error(w, "response lost after commit", http.StatusInternalServerError)
		case r.URL.Path == "/query":
			body := readQueryBody(t, r)
			if strings.Contains(body, "ASK") {
				w.Header().Set("Content-Type", "application/sparql-results+json")
				_, _ = w.Write([]byte(`{"head":{},"boolean":true}`))
				return
			}
			writeVerificationQuery(t, w, currentLive, body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	if code := runScopedBuildInRepo(t, repo, awarenessDir, markerPath, txPath, domain, ts.URL+"/store?default"); code != 0 {
		t.Fatalf("ambiguous committed response code=%d, want success after marker verification", code)
	}
	if bytes.Contains(currentLive, []byte("\"old\"")) {
		t.Fatal("ambiguous committed response did not expose the replacement generation")
	}
}

func scopedBuildFixture(t *testing.T) (repo, awarenessDir, markerPath, txPath string) {
	t.Helper()
	repo = t.TempDir()
	awarenessDir = filepath.Join(repo, "docs", "awareness")
	if err := os.MkdirAll(awarenessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFixtureYAMLs(filepath.Join("..", "..", "golang", "extractor", "testdata"), awarenessDir); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	markerPath = filepath.Join(repo, ".awg", "graph-authority.json")
	txPath = seedmeta.RuntimeTransactionPath(markerPath)
	return repo, awarenessDir, markerPath, txPath
}

func runScopedBuildInRepo(t *testing.T, repo, awarenessDir, markerPath, txPath, domain, storeURL string) int {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	return runBuild([]string{
		"-input", awarenessDir,
		"-repo", domain,
		"-store-url", storeURL,
		"-graph-marker-file", markerPath,
		"-graph-transaction-file", txPath,
		"-ag-repo", repo,
	})
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

func TestRetainScopedGraph_RemovesOnlySoleOwnedTargetAndMarker(t *testing.T) {
	const target = "github.com/test/target"
	nt := []byte(
		"<https://example.test/target> <https://globular.io/awareness#repo> \"github.com/test/target\" .\n" +
			"<https://example.test/target> <https://example.test/p> \"remove\" .\n" +
			"<https://example.test/shared> <https://globular.io/awareness#repo> \"github.com/test/target\" .\n" +
			"<https://example.test/shared> <https://globular.io/awareness#repo> \"github.com/test/other\" .\n" +
			"<https://example.test/shared> <https://example.test/p> \"keep\" .\n" +
			"<https://globular.io/awareness#seedBuild/sha256-old> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#SeedBuild> .\n" +
			"<https://globular.io/awareness#seedBuild/sha256-old> <https://example.test/p> \"remove marker\" .\n" +
			"<https://example.test/home> <https://example.test/p> \"keep\" .\n")

	got := string(retainScopedGraph(nt, target))
	for _, want := range []string{"https://example.test/shared", "https://example.test/home"} {
		if !strings.Contains(got, want) {
			t.Fatalf("retained graph missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"https://example.test/target", "seedBuild/sha256-old"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("retained graph contains removed subject %q:\n%s", unwanted, got)
		}
	}
}

func TestScopedPromoteStagingUpdate_ContainsOnlyControlOperations(t *testing.T) {
	u := scopedPromoteStagingUpdate("github.com/test/repo", "urn:sensei:graph-staging:sha256:abc")
	for _, want := range []string{"DELETE", "ADD GRAPH <urn:sensei:graph-staging:sha256:abc> TO DEFAULT", "DROP GRAPH <urn:sensei:graph-staging:sha256:abc>"} {
		if !strings.Contains(u, want) {
			t.Fatalf("promotion update missing %q:\n%s", want, u)
		}
	}
	if strings.Contains(u, "INSERT DATA") {
		t.Fatalf("promotion update must not embed RDF payload:\n%s", u)
	}
}

func TestNamedGraphStoreURL_ReplacesDefaultSelector(t *testing.T) {
	got, err := namedGraphStoreURL("http://h:7878/store?default", "urn:sensei:stage:abc")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/store" || u.Query().Get("graph") != "urn:sensei:stage:abc" || u.Query().Has("default") {
		t.Fatalf("named graph URL=%q", got)
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
