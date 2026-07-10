// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
