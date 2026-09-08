// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A certification step that could not run must SAY SO on the build's own
// report.
//
// #347: when no awareness-graph checkout resolved and no --graph-transaction-file
// was given, txPath was never set, so the whole block -- including every branch
// that reports a failure -- was skipped. The build printed `Build complete.`,
// exited 0, wrote no stamp, and said nothing. The operator found out later from
// a different tool, in a message about a missing file.
//
// Certification is optional by design, so this must not fail the build. The
// assertion is only that the outcome is visible.
func TestABuildThatCannotCertifyItsTransactionSaysSo(t *testing.T) {
	// A repository WITHOUT golang/server/embeddata, so the upward auto-detect
	// finds nothing -- which is every governed repo that is not the tool's own.
	repo := t.TempDir()
	awarenessDir := filepath.Join(repo, "docs", "awareness")
	if err := os.MkdirAll(awarenessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFixtureYAMLs(filepath.Join("..", "..", "golang", "extractor", "testdata"), awarenessDir); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	markerPath := filepath.Join(repo, ".sensei", "graph-authority.json")

	var loaded []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/store":
			if r.Method == http.MethodGet {
				w.Header().Set("Content-Type", "application/n-triples")
				_, _ = w.Write(loaded)
				return
			}
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
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	var code int
	out := captureStderr(t, func() {
		code = runBuild([]string{
			"-input", awarenessDir,
			"-all",
			"-store-url", ts.URL + "/store?default",
			"-graph-marker-file", markerPath,
		})
	})

	// Optional by design: an unresolved checkout is not a build failure.
	if code != 0 {
		t.Fatalf("runBuild code=%d, want 0: %s", code, out)
	}
	// No stamp was written, which is the state being reported.
	if _, err := os.Stat(seedmetaTransactionPath(markerPath)); err == nil {
		t.Fatal("a transaction stamp was written without an awareness-graph checkout")
	}
	// And the build said so, naming what it looked for and how to fix it.
	for _, want := range []string{
		"runtime transaction: NOT certified",
		"--ag-repo",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the build did not report %q; a skipped certification was silent:\n%s", want, out)
		}
	}
}

func seedmetaTransactionPath(markerPath string) string {
	return strings.TrimSuffix(markerPath, ".json") + ".transaction.tsv"
}
