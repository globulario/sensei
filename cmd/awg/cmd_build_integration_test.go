// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/seedmeta"
)

// TestIntegration_ScopedRepoPublication_RealOxigraph proves the publication
// contract against the real parser and transaction engine. The exact authored
// Sensei corpus is staged as N-Triples, promoted through control-only SPARQL,
// and must preserve a foreign domain already present in the default graph.
func TestIntegration_ScopedRepoPublication_RealOxigraph(t *testing.T) {
	oxi, err := findOxigraphBinary()
	if err != nil {
		t.Skipf("Oxigraph binary unavailable: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, oxi, "serve", "--location", t.TempDir(), "--bind", addr)
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start Oxigraph: %v", err)
	}
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()

	queryURL := "http://" + addr + "/query"
	if !waitForSPARQLHealthy(queryURL, 10*time.Second) {
		t.Fatalf("Oxigraph did not become healthy at %s:\n%s", queryURL, logs.String())
	}
	storeURL := "http://" + addr + "/store?default"

	baseline, _ := seedmeta.AppendMarker([]byte(
		"<https://example.test/foreign> <https://globular.io/awareness#repo> \"github.com/test/foreign\" .\n" +
			"<https://example.test/foreign> <https://example.test/p> \"must survive\" .\n"))
	if err := uploadNTriples(http.DefaultClient, storeURL, baseline); err != nil {
		t.Fatalf("load baseline: %v", err)
	}

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(t.TempDir(), "graph-authority.json")
	txPath := seedmeta.RuntimeTransactionPath(markerPath)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatal(err)
	}

	const domain = "github.com/globulario/sensei"
	if code := runBuild([]string{
		"-input", filepath.Join(repoRoot, "docs", "awareness"),
		"-repo", domain,
		"-store-url", storeURL,
		"-graph-marker-file", markerPath,
		"-graph-transaction-file", txPath,
		"-ag-repo", repoRoot,
	}); code != 0 {
		t.Fatalf("scoped real-Oxigraph build code=%d:\n%s", code, logs.String())
	}

	resp, err := http.Get(storeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	live, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(live, []byte("github.com/test/foreign")) || !bytes.Contains(live, []byte("must survive")) {
		t.Fatalf("foreign domain was lost:\n%s", live)
	}
	if !bytes.Contains(live, []byte(domain)) {
		t.Fatalf("Sensei domain was not published:\n%s", live)
	}
	marker, ok := seedmeta.ParseMarker(live)
	if !ok {
		t.Fatal("live graph has no whole-generation marker")
	}
	written, err := seedmeta.ReadMarkerFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	if marker != written {
		t.Fatalf("live marker %#v does not match receipt %#v", marker, written)
	}
}
