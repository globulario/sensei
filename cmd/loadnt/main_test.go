// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/seedmeta"
)

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "in.nt")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	return p
}

func TestRun_MissingInput(t *testing.T) {
	code := run([]string{}, os.Stdout, os.Stderr)
	if code != loadExitUserError {
		t.Fatalf("exit=%d, want %d", code, loadExitUserError)
	}
}

func TestRun_InvalidNTriples(t *testing.T) {
	p := writeTempFile(t, "<broken> <triple> .\n")
	code := run([]string{"-input", p}, os.Stdout, os.Stderr)
	if code != loadExitRuntime {
		t.Fatalf("exit=%d, want %d", code, loadExitRuntime)
	}
}

func TestRun_ValidTriples_SendsExpectedRequest(t *testing.T) {
	var method, ctype, query string
	stamped, marker := seedmeta.AppendMarker([]byte("<https://a> <https://b> <https://c> .\n"))
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/store":
			method = r.Method
			ctype = r.Header.Get("Content-Type")
			query = r.URL.RawQuery
			w.WriteHeader(http.StatusNoContent)
		case "/query":
			switch {
			case r.Header.Get("Content-Type") == "application/sparql-query" && strings.Contains(readBody(t, r), "COUNT(*)"):
				_, _ = w.Write([]byte(`{"results":{"bindings":[{"n":{"value":"7"}}]}}`))
			default:
				_, _ = w.Write([]byte(`{"results":{"bindings":[
{"p":{"type":"uri","value":"https://globular.io/awareness#seedDigestSha256"},"o":{"type":"literal","value":"` + marker.Digest + `"}},
{"p":{"type":"uri","value":"https://globular.io/awareness#seedTripleCount"},"o":{"type":"literal","value":"7"}}
]}}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	p := writeTempFile(t, string(stamped))
	code := run([]string{"-input", p, "-oxigraph-url", ts.URL + "/store?default"}, os.Stdout, os.Stderr)
	if code != loadExitOK {
		t.Fatalf("exit=%d, want %d", code, loadExitOK)
	}
	if method != http.MethodPut {
		t.Fatalf("method=%s, want PUT", method)
	}
	if ctype != "application/n-triples" {
		t.Fatalf("content-type=%q, want application/n-triples", ctype)
	}
	if query != "default" {
		t.Fatalf("query=%q, want default", query)
	}
}

func readBody(t *testing.T, r *http.Request) string {
	t.Helper()
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func TestRun_Non2xx_ReturnsError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad load", http.StatusBadRequest)
	}))
	defer ts.Close()

	p := writeTempFile(t, "<https://a> <https://b> <https://c> .\n")
	code := run([]string{"-input", p, "-oxigraph-url", ts.URL + "/store?default"}, os.Stdout, os.Stderr)
	if code != loadExitRuntime {
		t.Fatalf("exit=%d, want %d", code, loadExitRuntime)
	}
}

func TestNormalizeStoreURL_QueryEndpointGetsStorePath(t *testing.T) {
	got, err := normalizeStoreURL("http://localhost:7878/query")
	if err != nil {
		t.Fatalf("normalizeStoreURL: %v", err)
	}
	if !strings.Contains(got, "/store") {
		t.Fatalf("normalized=%q, want /store", got)
	}
	if !strings.Contains(got, "default") {
		t.Fatalf("normalized=%q, want default query target", got)
	}
}
