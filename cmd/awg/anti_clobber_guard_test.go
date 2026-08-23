// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// countOnlyServer simulates a live store that already holds initialCount
// triples before anything is PUT to it in this test. Before the first PUT,
// every /query answers a plain COUNT(*) with initialCount — the pre-existing
// live state guardAgainstLiveShrink is meant to see. After a PUT lands, it
// switches to the shared writeVerificationQuery fixture (marker-aware) so
// runRebuild's own post-reload verifyLoadedGraph call also succeeds against
// what was actually just loaded.
func countOnlyServer(t *testing.T, initialCount int64) *httptest.Server {
	t.Helper()
	var loaded []byte
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/store" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/n-triples")
			_, _ = w.Write(loaded)
		case r.URL.Path == "/store" && r.Method == http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read load body: %v", err)
			}
			loaded = append([]byte(nil), body...)
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/query":
			if loaded == nil {
				w.Header().Set("Content-Type", "application/sparql-results+json")
				_, _ = w.Write([]byte(`{"results":{"bindings":[{"n":{"value":"` + itoa(initialCount) + `"}}]}}`))
				return
			}
			writeVerificationQuery(t, w, loaded, readQueryBody(t, r))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestGuardAgainstLiveShrink_RefusesDramaticShrink(t *testing.T) {
	ts := countOnlyServer(t, 186825)
	defer ts.Close()

	err := guardAgainstLiveShrink(ts.URL+"/store?default", 7887)
	if err == nil {
		t.Fatal("expected the guard to refuse a >50% shrink")
	}
	if !strings.Contains(err.Error(), "186825") || !strings.Contains(err.Error(), "7887") {
		t.Fatalf("error should name both counts, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Fatalf("error should mention the escape hatch, got: %v", err)
	}
}

func TestGuardAgainstLiveShrink_AllowsWhenLiveStoreEmpty(t *testing.T) {
	ts := countOnlyServer(t, 0)
	defer ts.Close()

	if err := guardAgainstLiveShrink(ts.URL+"/store?default", 42); err != nil {
		t.Fatalf("cold-start load into an empty store should never trip the guard: %v", err)
	}
}

func TestGuardAgainstLiveShrink_AllowsGrowthAndSmallChange(t *testing.T) {
	ts := countOnlyServer(t, 1000)
	defer ts.Close()

	if err := guardAgainstLiveShrink(ts.URL+"/store?default", 1200); err != nil {
		t.Fatalf("growth should never trip the guard: %v", err)
	}
	if err := guardAgainstLiveShrink(ts.URL+"/store?default", 600); err != nil {
		t.Fatalf("a <=50%% shrink should not trip the guard: %v", err)
	}
}

func TestGuardAgainstLiveShrink_AllowsWhenLiveStoreUnreachable(t *testing.T) {
	if err := guardAgainstLiveShrink("http://127.0.0.1:1/store?default", 5); err != nil {
		t.Fatalf("an unreachable store has nothing to protect: %v", err)
	}
}

// TestRunRebuild_RefusesLiveReloadThatWouldShrinkStore is the end-to-end
// proof: a real runRebuild invocation (not --no-runtime-reload) against a
// live store that currently holds a much larger graph must refuse, and
// --force must be required to override it. This is the exact incident this
// guard exists to prevent: a routine self-only rebuild/propose call silently
// replacing a live combined deployment.
func TestRunRebuild_RefusesLiveReloadThatWouldShrinkStore(t *testing.T) {
	agRepo, svcRepo := setupSeedStatusRepos(t)
	ts := countOnlyServer(t, 999999)
	defer ts.Close()

	code := runRebuild([]string{
		"--ag-repo", agRepo,
		"--services-repo", svcRepo,
		"--oxigraph-url", ts.URL + "/store?default",
	})
	if code == 0 {
		t.Fatal("expected runRebuild to refuse the reload without --force")
	}

	seedPath, _ := seedArtifactPaths(false, agRepo)
	if _, err := os.Stat(seedPath); err == nil {
		t.Fatal("refused reload must not promote local embeddata artifacts either")
	}
}

func TestRunRebuild_ForceFlagBypassesShrinkGuard(t *testing.T) {
	agRepo, svcRepo := setupSeedStatusRepos(t)
	ts := countOnlyServer(t, 999999)
	defer ts.Close()

	code := runRebuild([]string{
		"--ag-repo", agRepo,
		"--services-repo", svcRepo,
		"--oxigraph-url", ts.URL + "/store?default",
		"--force",
	})
	if code != 0 {
		t.Fatalf("runRebuild --force code=%d, want 0", code)
	}
}
