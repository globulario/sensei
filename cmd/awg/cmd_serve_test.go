// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/seedmeta"
)

func TestCheckSPARQLHealth_UsesASKPost(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/sparql-query" {
			t.Fatalf("content-type=%q, want application/sparql-query", got)
		}
		if got := r.Header.Get("Accept"); got != "application/sparql-results+json" {
			t.Fatalf("accept=%q, want application/sparql-results+json", got)
		}
		fmt.Fprint(w, `{"head":{},"boolean":true}`)
	}))
	defer srv.Close()

	if err := checkSPARQLHealth(srv.URL); err != nil {
		t.Fatalf("checkSPARQLHealth: %v", err)
	}
}

func TestWatchBackendHealth_FailsAfterConsecutiveErrors(t *testing.T) {
	t.Parallel()

	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			fmt.Fprint(w, `{"head":{},"boolean":true}`)
			return
		}
		http.Error(w, "backend down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchBackendHealth(ctx, srv.URL, 10*time.Millisecond, 2, errCh)

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("watchBackendHealth returned nil error")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("watchBackendHealth did not report backend failure")
	}
}

// The four cases below were written against selectServeGraphMarkerFile, a helper that
// G4 removed: once resolveServeGraphMarkerFile delegated to the single marker resolver,
// that helper became reachable only from these tests. Four passing tests over
// unreachable code are worse than none, because they report the contract as covered.
//
// They now call the function serve actually runs. Each needs a project root on disk,
// because the contract being tested is precisely "which marker does this resolve to,
// and does it depend on where I stood".

func serveMarkerFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A bare .sensei directory is not a project; the marker resolver requires the
	// same thing resolveProjectRoot searches for.
	if err := os.WriteFile(filepath.Join(root, ".sensei", "config.yaml"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return root
}

func TestServeGraphMarkerFile_IgnoresExistingDefaultInEmbeddedSeedMode(t *testing.T) {
	root := serveMarkerFixture(t)
	if err := os.WriteFile(filepath.Join(root, ".sensei", "graph-authority.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveServeGraphMarkerFile("", false)
	if err != nil {
		t.Fatalf("resolveServeGraphMarkerFile: %v", err)
	}
	if got != "" {
		t.Fatalf("marker=%q, want empty in embedded-seed mode even when a local runtime marker exists", got)
	}
}

func TestServeGraphMarkerFile_SkipsMissingDefaultInSeedMode(t *testing.T) {
	serveMarkerFixture(t)
	got, err := resolveServeGraphMarkerFile("", false)
	if err != nil {
		t.Fatalf("resolveServeGraphMarkerFile: %v", err)
	}
	if got != "" {
		t.Fatalf("marker=%q, want empty in embedded-seed mode", got)
	}
}

func TestServeGraphMarkerFile_UsesDefaultForNoSeed(t *testing.T) {
	root := serveMarkerFixture(t)
	got, err := resolveServeGraphMarkerFile("", true)
	if err != nil {
		t.Fatalf("resolveServeGraphMarkerFile: %v", err)
	}
	want := filepath.Join(root, ".sensei", "graph-authority.json")
	if got != want {
		t.Fatalf("marker=%q, want %q with --no-seed", got, want)
	}
}

func TestServeGraphMarkerFile_ConfiguredPathWins(t *testing.T) {
	serveMarkerFixture(t)
	for _, noSeed := range []bool{true, false} {
		got, err := resolveServeGraphMarkerFile(" /custom/graph-authority.json ", noSeed)
		if err != nil {
			t.Fatalf("noSeed=%v: %v", noSeed, err)
		}
		if got != "/custom/graph-authority.json" {
			t.Fatalf("noSeed=%v: marker=%q, want the configured marker", noSeed, got)
		}
	}
}

func TestSyncDefaultRuntimeMarkerFromLiveStore_RefreshesStaleMarker(t *testing.T) {
	t.Parallel()

	live := seedmeta.Marker{
		IRI:         seedmeta.NamespaceIRI + "seedBuild/sha256-live",
		Digest:      "live",
		TripleCount: 7,
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read query: %v", err)
		}
		q := string(body)
		w.Header().Set("Content-Type", "application/sparql-results+json")
		switch {
		case strings.Contains(q, "COUNT(*)"):
			fmt.Fprint(w, `{"head":{"vars":["n"]},"results":{"bindings":[{"n":{"type":"literal","value":"7"}}]}}`)
		case strings.Contains(q, "SeedBuild"):
			fmt.Fprintf(w, `{"head":{"vars":["m","digest","count"]},"results":{"bindings":[{"m":{"type":"uri","value":%q},"digest":{"type":"literal","value":%q},"count":{"type":"literal","value":"7"}}]}}`, live.IRI, live.Digest)
		default:
			t.Fatalf("unexpected query: %s", q)
		}
	}))
	defer ts.Close()

	markerPath := filepath.Join(t.TempDir(), "graph-authority.json")
	if err := seedmeta.WriteMarkerFile(markerPath, seedmeta.Marker{
		IRI:         seedmeta.NamespaceIRI + "seedBuild/sha256-old",
		Digest:      "old",
		TripleCount: 1,
	}); err != nil {
		t.Fatalf("write stale marker: %v", err)
	}

	var log bytes.Buffer
	if err := syncDefaultRuntimeMarkerFromLiveStore(context.Background(), markerPath, ts.URL, &log); err != nil {
		t.Fatalf("sync marker: %v", err)
	}
	got, err := seedmeta.ReadMarkerFile(markerPath)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if got.Digest != live.Digest || got.IRI != live.IRI || got.TripleCount != live.TripleCount {
		t.Fatalf("marker=%+v, want %+v", got, live)
	}
	if !strings.Contains(log.String(), "refreshed runtime graph marker") {
		t.Fatalf("log=%q, want refresh message", log.String())
	}
}

func TestSyncDefaultRuntimeMarkerFromLiveStore_RemovesStaleTransactionStamp(t *testing.T) {
	t.Parallel()

	live := seedmeta.Marker{
		IRI:         seedmeta.NamespaceIRI + "seedBuild/sha256-live",
		Digest:      "live",
		TripleCount: 7,
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read query: %v", err)
		}
		q := string(body)
		w.Header().Set("Content-Type", "application/sparql-results+json")
		switch {
		case strings.Contains(q, "COUNT(*)"):
			fmt.Fprint(w, `{"head":{"vars":["n"]},"results":{"bindings":[{"n":{"type":"literal","value":"7"}}]}}`)
		case strings.Contains(q, "SeedBuild"):
			fmt.Fprintf(w, `{"head":{"vars":["m","digest","count"]},"results":{"bindings":[{"m":{"type":"uri","value":%q},"digest":{"type":"literal","value":%q},"count":{"type":"literal","value":"7"}}]}}`, live.IRI, live.Digest)
		default:
			t.Fatalf("unexpected query: %s", q)
		}
	}))
	defer ts.Close()

	markerPath := filepath.Join(t.TempDir(), "graph-authority.json")
	if err := seedmeta.WriteMarkerFile(markerPath, live); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	txPath := seedmeta.RuntimeTransactionPath(markerPath)
	if err := os.WriteFile(txPath, []byte("format\tv1\nseed\tdigest_sha256\told\nseed\ttriple_count\t1\n"), 0o644); err != nil {
		t.Fatalf("write stale tx: %v", err)
	}

	var log bytes.Buffer
	if err := syncDefaultRuntimeMarkerFromLiveStore(context.Background(), markerPath, ts.URL, &log); err != nil {
		t.Fatalf("sync marker: %v", err)
	}
	if _, err := os.Stat(txPath); !os.IsNotExist(err) {
		t.Fatalf("stale transaction exists err=%v, want removed", err)
	}
	if !strings.Contains(log.String(), "removed stale runtime transaction stamp") {
		t.Fatalf("log=%q, want stale transaction removal message", log.String())
	}
}

func TestReconcileRuntimeTransactionStamp_KeepsMatchingStamp(t *testing.T) {
	t.Parallel()

	markerPath := filepath.Join(t.TempDir(), "graph-authority.json")
	marker := seedmeta.Marker{
		IRI:         seedmeta.NamespaceIRI + "seedBuild/sha256-live",
		Digest:      "live",
		TripleCount: 7,
	}
	txPath := seedmeta.RuntimeTransactionPath(markerPath)
	if err := os.MkdirAll(filepath.Dir(txPath), 0o755); err != nil {
		t.Fatalf("mkdir tx dir: %v", err)
	}
	if err := os.WriteFile(txPath, []byte("format\tv1\nseed\tdigest_sha256\tlive\nseed\ttriple_count\t7\n"), 0o644); err != nil {
		t.Fatalf("write tx: %v", err)
	}

	if err := reconcileRuntimeTransactionStamp(markerPath, marker, io.Discard); err != nil {
		t.Fatalf("reconcile tx: %v", err)
	}
	if _, err := os.Stat(txPath); err != nil {
		t.Fatalf("matching transaction removed: %v", err)
	}
}

func TestResolveServeRepoContext(t *testing.T) {
	dir := t.TempDir()

	if r, d, err := resolveServeRepoContext("", ""); err != nil || r != "" || d != "" {
		t.Fatalf("neither set must disable feedback, got %q %q %v", r, d, err)
	}
	if _, _, err := resolveServeRepoContext(dir, ""); err == nil {
		t.Fatal("root without domain must fail")
	}
	if _, _, err := resolveServeRepoContext("", "d"); err == nil {
		t.Fatal("domain without root must fail")
	}
	if _, _, err := resolveServeRepoContext(" "+dir, "d"); err == nil {
		t.Fatal("padded root must fail")
	}
	if _, _, err := resolveServeRepoContext(dir, "a b"); err == nil {
		t.Fatal("whitespace domain must fail")
	}
	// The wrapper MAY resolve a relative root (the caller selected it explicitly).
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	os.Chdir(dir)
	sub := "subdir"
	if err := os.Mkdir(filepath.Join(dir, sub), 0o755); err != nil {
		t.Fatal(err)
	}
	r, d, err := resolveServeRepoContext(sub, "github.com/x/y")
	if err != nil {
		t.Fatalf("relative root should resolve in the wrapper: %v", err)
	}
	if !filepath.IsAbs(r) || d != "github.com/x/y" {
		t.Fatalf("wrapper must forward an absolute root + domain, got %q %q", r, d)
	}
	// A missing root fails.
	if _, _, err := resolveServeRepoContext(filepath.Join(dir, "missing"), "d"); err == nil {
		t.Fatal("missing root must fail")
	}
}

// Serve outside a project, both modes. These are the same two answers the resolver
// gives, reached through serve's own entry point, and they are what makes the removal
// of serve's dead "unresolvable root" branch safe: embedded mode never needed a
// project, and --no-seed should not be allowed to invent one.
func TestServeGraphMarkerFile_OutsideAProject(t *testing.T) {
	t.Chdir(t.TempDir())

	got, err := resolveServeGraphMarkerFile("", false)
	if err != nil {
		t.Errorf("embedded-seed mode outside a project: %v (it needs no marker file)", err)
	}
	if got != "" {
		t.Errorf("embedded-seed mode resolved a marker file outside a project: %q", got)
	}

	// --no-seed makes the marker file the authority, so a marker under a directory
	// that is not the project would certify a different graph from each directory.
	if _, err := resolveServeGraphMarkerFile("", true); err == nil {
		t.Error("--no-seed outside a project resolved a marker file instead of refusing")
	}
}
