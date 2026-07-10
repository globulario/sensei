// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
