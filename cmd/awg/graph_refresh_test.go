// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/globulario/awareness-graph/golang/seedmeta"
)

func TestReloadOxigraphStore_ReplacesGraphAndRemovesStaleTriples(t *testing.T) {
	var loaded []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/store":
			if r.Method != http.MethodPut {
				t.Fatalf("method=%s, want PUT", r.Method)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read load body: %v", err)
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

	artifactA, markerA := seedmeta.AppendMarker([]byte(strings.Join([]string{
		"<https://example.test/s> <https://example.test/p> <https://example.test/x> .",
		"<https://example.test/s> <https://example.test/p> <https://example.test/y> .",
	}, "\n") + "\n"))
	artifactB, markerB := seedmeta.AppendMarker([]byte("<https://example.test/s> <https://example.test/p> <https://example.test/x> .\n"))

	if err := reloadOxigraphStore(artifactA, ts.URL+"/store?default"); err != nil {
		t.Fatalf("load A: %v", err)
	}
	if err := verifyLoadedGraph(ts.URL+"/store?default", artifactA); err != nil {
		t.Fatalf("verify A: %v", err)
	}
	if !strings.Contains(string(loaded), "<https://example.test/y>") {
		t.Fatal("artifact A should contain stale candidate triple Y before replacement")
	}

	if err := reloadOxigraphStore(artifactB, ts.URL+"/store?default"); err != nil {
		t.Fatalf("load B: %v", err)
	}
	if err := verifyLoadedGraph(ts.URL+"/store?default", artifactB); err != nil {
		t.Fatalf("verify B: %v", err)
	}
	if strings.Contains(string(loaded), "<https://example.test/y>") {
		t.Fatal("triple Y survived replacement load")
	}
	got, ok := seedmeta.ParseMarker(loaded)
	if !ok {
		t.Fatal("reloaded graph missing marker")
	}
	if got.Digest != markerB.Digest {
		t.Fatalf("live digest=%s, want %s", got.Digest, markerB.Digest)
	}
	if got.TripleCount != markerB.TripleCount {
		t.Fatalf("live triple count=%d, want %d", got.TripleCount, markerB.TripleCount)
	}
	if got.Digest == markerA.Digest {
		t.Fatal("replacement load left old digest in place")
	}
}

func TestVerifyLoadedGraph_FailsOnDigestMismatch(t *testing.T) {
	artifact, _ := seedmeta.AppendMarker([]byte("<https://example.test/s> <https://example.test/p> <https://example.test/x> .\n"))
	other, _ := seedmeta.AppendMarker([]byte("<https://example.test/s> <https://example.test/p> <https://example.test/y> .\n"))
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/query":
			writeVerificationQuery(t, w, other, readQueryBody(t, r))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer ts.Close()

	if err := verifyLoadedGraph(ts.URL+"/store?default", artifact); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("verifyLoadedGraph error=%v, want digest mismatch", err)
	}
}

func TestRunRebuild_FailsWhenLiveVerificationFails(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFixtureYAMLs(filepath.Join("..", "..", "golang", "extractor", "testdata"), filepath.Join(repo, "docs", "awareness")); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)

	other, _ := seedmeta.AppendMarker([]byte("<https://example.test/s> <https://example.test/p> <https://example.test/y> .\n"))
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/store":
			w.WriteHeader(http.StatusNoContent)
		case "/query":
			writeVerificationQuery(t, w, other, readQueryBody(t, r))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	if code := runRebuild([]string{"--ag-repo", repo, "--oxigraph-url", ts.URL + "/store?default"}); code == 0 {
		t.Fatal("runRebuild should fail non-zero when live-store verification fails")
	}
	if _, err := os.Stat(filepath.Join(repo, "golang", "server", "embeddata", "awareness.nt")); !os.IsNotExist(err) {
		t.Fatalf("awareness.nt should remain unpromoted on live verification failure, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "golang", "server", "embeddata", "awareness.transaction.tsv")); !os.IsNotExist(err) {
		t.Fatalf("awareness.transaction.tsv should remain unpromoted on live verification failure, stat err=%v", err)
	}
}

func TestRunRebuild_PromotesSeedAndTransactionAfterLiveVerification(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFixtureYAMLs(filepath.Join("..", "..", "golang", "extractor", "testdata"), filepath.Join(repo, "docs", "awareness")); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)

	var loaded []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/store":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read load body: %v", err)
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

	if code := runRebuild([]string{"--ag-repo", repo, "--oxigraph-url", ts.URL + "/store?default"}); code != 0 {
		t.Fatalf("runRebuild code=%d, want 0", code)
	}
	seedPath := filepath.Join(repo, "golang", "server", "embeddata", "awareness.nt")
	txPath := filepath.Join(repo, "golang", "server", "embeddata", "awareness.transaction.tsv")
	seedBytes, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("read promoted seed: %v", err)
	}
	if !bytes.Equal(seedBytes, loaded) {
		t.Fatal("promoted seed should match the live verified artifact")
	}
	txBytes, err := os.ReadFile(txPath)
	if err != nil {
		t.Fatalf("read promoted transaction stamp: %v", err)
	}
	if !strings.Contains(string(txBytes), "seed\tdigest_sha256\t") {
		t.Fatalf("transaction stamp missing seed digest:\n%s", string(txBytes))
	}
}

func copyFixtureYAMLs(srcDir, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(srcDir, entry.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dstDir, entry.Name()), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func readQueryBody(t *testing.T, r *http.Request) string {
	t.Helper()
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read query body: %v", err)
	}
	return string(b)
}

func writeVerificationQuery(t *testing.T, w http.ResponseWriter, loaded []byte, body string) {
	t.Helper()
	marker, ok := seedmeta.ParseMarker(loaded)
	if !ok {
		t.Fatalf("verification fixture missing marker")
	}
	w.Header().Set("Content-Type", "application/sparql-results+json")
	switch {
	case strings.Contains(body, "COUNT(*)"):
		_, _ = w.Write([]byte(`{"results":{"bindings":[{"n":{"value":"` + itoa(marker.TripleCount) + `"}}]}}`))
	case strings.Contains(body, "COUNT(DISTINCT ?s)") && strings.Contains(body, "SeedBuild"):
		_, _ = w.Write([]byte(`{"results":{"bindings":[{"n":{"value":"1"}}]}}`))
	default:
		_, _ = w.Write([]byte(`{"results":{"bindings":[` +
			`{"p":{"type":"uri","value":"https://globular.io/awareness#seedDigestSha256"},"o":{"type":"literal","value":"` + marker.Digest + `"}},` +
			`{"p":{"type":"uri","value":"https://globular.io/awareness#seedTripleCount"},"o":{"type":"literal","value":"` + itoa(marker.TripleCount) + `"}}` +
			`]}}`))
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
