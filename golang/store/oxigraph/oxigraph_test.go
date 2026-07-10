// SPDX-License-Identifier: AGPL-3.0-only

package oxigraph_test

// Oxigraph client tests.
//
// Strategy: use net/http/httptest to stand up minimal SPARQL endpoints
// that the client can hit over real loopback. This proves the HTTP
// path, header negotiation, and status-code handling without requiring
// the actual Oxigraph binary or Docker image. An integration test that
// hits a live Oxigraph endpoint at localhost:7878 lives in
// oxigraph_integration_test.go behind the `integration` build tag.
//
// What these tests cover:
//
//   - New(): rejects malformed URLs and non-http(s) schemes; accepts
//     a well-formed URL.
//   - Health(): returns nil on 2xx, returns a wrapped error on 5xx
//     that includes the status code and body so the operator sees the
//     backend's own message.
//   - Health(): surfaces connection failures (unreachable host) as a
//     wrapped error, not a panic.
//   - Health(): honours context cancellation/timeout — the request
//     stops in-flight rather than blocking past the deadline.
//
// What these tests do NOT cover:
//   - Retry policy (none yet — failure surfaces once and the caller
//     decides what to do).

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/globulario/awareness-graph/golang/store/oxigraph"
)

func TestNew_RejectsBadScheme(t *testing.T) {
	_, err := oxigraph.New("ftp://example/query")
	if err == nil {
		t.Fatal("expected New to reject non-http scheme")
	}
	if !strings.Contains(err.Error(), "scheme") {
		t.Errorf("error should mention scheme; got %v", err)
	}
}

func TestNew_RejectsEmptyHost(t *testing.T) {
	_, err := oxigraph.New("http:///query")
	if err == nil {
		t.Fatal("expected New to reject URL with empty host")
	}
}

func TestNew_AcceptsValidURL(t *testing.T) {
	c, err := oxigraph.New("http://localhost:7878/query")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := c.QueryURL(); got != "http://localhost:7878/query" {
		t.Errorf("QueryURL = %q, want round-trip of input", got)
	}
}

// TestHealth_OK pins the happy path. Verifies (1) the client posts to
// the SPARQL endpoint with content-type sparql-query, (2) it accepts a
// 2xx as healthy. The server side asserts on the request shape so a
// silent client-side regression (e.g. wrong content-type) fails the
// test.
func TestHealth_OK(t *testing.T) {
	var sawContentType string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/sparql-results+json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"head":{},"boolean":true}`))
	}))
	defer ts.Close()

	c, err := oxigraph.New(ts.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if sawContentType != "application/sparql-query" {
		t.Errorf("server saw Content-Type = %q, want application/sparql-query", sawContentType)
	}
}

// TestHealth_500_IncludesStatusAndBody pins that the operator sees the
// backend's own error text. Just "got non-200" without the body is
// hostile when debugging mid-incident.
func TestHealth_500_IncludesStatusAndBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("backend storage full"))
	}))
	defer ts.Close()

	c, _ := oxigraph.New(ts.URL)
	err := c.Health(context.Background())
	if err == nil {
		t.Fatal("expected Health to return error on 500")
	}
	msg := err.Error()
	if !strings.Contains(msg, "500") {
		t.Errorf("error should include HTTP status; got %v", err)
	}
	if !strings.Contains(msg, "backend storage full") {
		t.Errorf("error should include response body; got %v", err)
	}
}

// TestHealth_Unreachable asserts that a connect failure surfaces as a
// regular wrapped error — NOT a panic, NOT a hang. This is the
// production-critical case: -require-store=false relies on the error
// being recoverable to log a warning and continue.
func TestHealth_Unreachable(t *testing.T) {
	// Port 1 is reserved for tcpmux per IANA and very unlikely to have
	// anything listening. The connection refusal is the test signal.
	c, _ := oxigraph.New("http://127.0.0.1:1/query")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := c.Health(ctx)
	if err == nil {
		t.Fatal("expected Health to error against unreachable endpoint")
	}
	if !strings.Contains(err.Error(), "oxigraph health") {
		t.Errorf("error should be wrapped with oxigraph context; got %v", err)
	}
}

// TestHealth_RespectsContextCancellation drives a server that hangs
// forever and asserts Health returns when ctx is cancelled. Without
// this, a slow/wedged backend could block the server's startup health
// check past any reasonable timeout.
//
// Defer ordering matters here. httptest.Server.Close() blocks until
// every active handler returns; we need to unblock the handler BEFORE
// Close fires, or the test deadlocks itself in cleanup. The two
// defers below run LIFO — `close(hang)` first, then `ts.Close()` —
// so the handler is always free to exit before Close starts waiting.
func TestHealth_RespectsContextCancellation(t *testing.T) {
	hang := make(chan struct{})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Exit on either: the test's hang-channel close (cleanup path)
		// or the request context being cancelled (the happy-path
		// signal that the client honored its own ctx).
		select {
		case <-hang:
		case <-r.Context().Done():
		}
	}))
	// LIFO: close(hang) runs first, ts.Close() second. If the client
	// cancelled cleanly the handler is already gone; otherwise close
	// (hang) frees it before Close waits on it.
	defer ts.Close()
	defer close(hang)

	c, _ := oxigraph.New(ts.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := c.Health(ctx)
	dur := time.Since(start)

	if err == nil {
		t.Fatal("expected Health to error on ctx cancel")
	}
	// Generous bound — CI runners are noisy — but tight enough to
	// catch "no cancellation at all" (which would block until the test
	// times out).
	if dur > 2*time.Second {
		t.Errorf("Health took %v to honor 100ms ctx cancel; want < 2s", dur)
	}
}

func TestDescribe_OK(t *testing.T) {
	var sawBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sawBody = string(body)
		w.Header().Set("Content-Type", "application/sparql-results+json")
		_, _ = w.Write([]byte(`{
  "results": {
    "bindings": [
      {"p":{"type":"uri","value":"http://www.w3.org/2000/01/rdf-schema#label"},
       "o":{"type":"literal","value":"Label"}},
      {"p":{"type":"uri","value":"https://globular.io/awareness#affects"},
       "o":{"type":"uri","value":"https://globular.io/awareness#failureMode/test.example.failure"}}
    ]
  }
}`))
	}))
	defer ts.Close()

	c, _ := oxigraph.New(ts.URL)
	got, err := c.Describe(context.Background(), "https://globular.io/awareness#invariant/test.example.invariant")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if !strings.Contains(sawBody, "<https://globular.io/awareness#invariant/test.example.invariant> ?p ?o") {
		t.Fatalf("Describe SPARQL body unexpected: %q", sawBody)
	}
	if len(got) != 2 {
		t.Fatalf("len(triples)=%d, want 2", len(got))
	}
	if got[0].ObjectIsIRI {
		t.Fatalf("expected first triple literal, got IRI")
	}
	if !got[1].ObjectIsIRI {
		t.Fatalf("expected second triple IRI, got literal")
	}
}

func TestDescribe_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream unavailable"))
	}))
	defer ts.Close()
	c, _ := oxigraph.New(ts.URL)
	_, err := c.Describe(context.Background(), "https://globular.io/awareness#invariant/test.example.invariant")
	if err == nil {
		t.Fatal("Describe expected error on 502")
	}
	if !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "upstream unavailable") {
		t.Fatalf("Describe error missing status/body: %v", err)
	}
}

func TestImpactForFile_OK(t *testing.T) {
	var sawBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sawBody = string(body)
		w.Header().Set("Content-Type", "application/sparql-results+json")
		_, _ = w.Write([]byte(`{
  "results": {
    "bindings": [
      {
        "node":{"type":"uri","value":"https://globular.io/awareness#invariant/test.example.invariant"},
        "type":{"type":"uri","value":"https://globular.io/awareness#Invariant"},
        "p":{"type":"uri","value":"http://www.w3.org/2000/01/rdf-schema#label"},
        "o":{"type":"literal","value":"Invariant Label"}
      }
    ]
  }
}`))
	}))
	defer ts.Close()
	c, _ := oxigraph.New(ts.URL)
	got, err := c.ImpactForFile(context.Background(), "https://globular.io/awareness#sourceFile/test%2Fexample.go")
	if err != nil {
		t.Fatalf("ImpactForFile: %v", err)
	}
	fileIRI := "<https://globular.io/awareness#sourceFile/test%2Fexample.go>"
	if !strings.Contains(sawBody, fileIRI+" <https://globular.io/awareness#implements> ?node") {
		t.Fatalf("direct implements arm missing from query: %q", sawBody)
	}
	// The 2-hop arm (file → implements → invariant ← affects ← failure_mode)
	// must also be in the query so failure modes surface without explicit protects.files.
	if !strings.Contains(sawBody, fileIRI+" <https://globular.io/awareness#implements> ?inv") {
		t.Fatalf("2-hop via-invariant arm missing from query: %q", sawBody)
	}
	if !strings.Contains(sawBody, "<https://globular.io/awareness#affects> ?inv") {
		t.Fatalf("aw:affects link missing from query: %q", sawBody)
	}
	if len(got) != 1 || got[0].NodeIRI == "" || got[0].TypeIRI == "" {
		t.Fatalf("unexpected facts: %#v", got)
	}
}

// TestImpactForFile_FailureModeViaInvariantChain verifies the 2-hop path:
// file → aw:implements → invariant ← aw:affects ← failure_mode.
// Without this path, failure modes that use related_invariants instead of
// explicit protects.files never surface in briefings.
func TestImpactForFile_FailureModeViaInvariantChain(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/sparql-results+json")
		// Simulate Oxigraph returning a failure mode reached via the 2-hop arm.
		_, _ = w.Write([]byte(`{
  "results": {
    "bindings": [
      {
        "node":{"type":"uri","value":"https://globular.io/awareness#failureMode/node_agent.buildid_skip_ignores_checksum"},
        "type":{"type":"uri","value":"https://globular.io/awareness#FailureMode"},
        "p":{"type":"uri","value":"http://www.w3.org/2000/01/rdf-schema#label"},
        "o":{"type":"literal","value":"ApplyPackageRelease build_id skip ignores checksum"}
      }
    ]
  }
}`))
	}))
	defer ts.Close()
	c, _ := oxigraph.New(ts.URL)
	got, err := c.ImpactForFile(context.Background(), "https://globular.io/awareness#sourceFile/golang%2Fnode_agent%2Fnode_agent_server%2Fapply_package_release.go")
	if err != nil {
		t.Fatalf("ImpactForFile: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 fact (failure mode via invariant chain), got %d: %#v", len(got), got)
	}
	if got[0].TypeIRI != "https://globular.io/awareness#FailureMode" {
		t.Errorf("TypeIRI = %q, want FailureMode", got[0].TypeIRI)
	}
}

func TestClassFacts_OK(t *testing.T) {
	var sawBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sawBody = string(body)
		w.Header().Set("Content-Type", "application/sparql-results+json")
		_, _ = w.Write([]byte(`{
  "results": {
    "bindings": [
      {
        "node":{"type":"uri","value":"https://globular.io/awareness#invariant/test.example.invariant"},
        "p":{"type":"uri","value":"http://www.w3.org/2000/01/rdf-schema#label"},
        "o":{"type":"literal","value":"Invariant Label"}
      }
    ]
  }
}`))
	}))
	defer ts.Close()
	c, _ := oxigraph.New(ts.URL)
	got, err := c.ClassFacts(context.Background(), "https://globular.io/awareness#Invariant", 10)
	if err != nil {
		t.Fatalf("ClassFacts: %v", err)
	}
	if !strings.Contains(sawBody, "?node <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#Invariant>") {
		t.Fatalf("unexpected class query body: %q", sawBody)
	}
	if len(got) != 1 || got[0].TypeIRI != "https://globular.io/awareness#Invariant" {
		t.Fatalf("unexpected class facts: %#v", got)
	}
}
