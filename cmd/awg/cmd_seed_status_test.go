// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store/oxigraph"
)

func TestSeedStatus_CurrentAcrossGeneratedCommittedAndLive(t *testing.T) {
	agRepo, svcRepo := setupSeedStatusRepos(t)
	if code := runRebuild([]string{"--combined", "--ag-repo", agRepo, "--services-repo", svcRepo, "--no-runtime-reload"}); code != 0 {
		t.Fatalf("runRebuild code=%d, want 0", code)
	}
	seedPath, _ := seedArtifactPaths(true, agRepo)
	seedBytes, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	storeURL := seedStatusStore(t, seedBytes)

	out := captureStdout(t, func() {
		if code := runSeedStatus([]string{
			"--json",
			"--require-current",
			"--seed", seedPath,
			"--ag-repo", agRepo,
			"--services-repo", svcRepo,
			"--oxigraph-url", storeURL,
		}); code != 0 {
			t.Fatalf("runSeedStatus code=%d, want 0", code)
		}
	})
	var got seedStatusResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal json: %v\n%s", err, out)
	}
	if got.OverallState != "current" {
		t.Fatalf("overall_state=%q, want current", got.OverallState)
	}
	if !got.GeneratedVsCommitted.Current || !got.TransactionStamp.Current || !got.LiveStore.Current {
		t.Fatalf("expected all lanes current: %+v", got)
	}
}

func TestSeedStatus_SplitWhenCommittedLiveMatchButGeneratedDrifted(t *testing.T) {
	agRepo, svcRepo := setupSeedStatusRepos(t)
	if code := runRebuild([]string{"--combined", "--ag-repo", agRepo, "--services-repo", svcRepo, "--no-runtime-reload"}); code != 0 {
		t.Fatalf("runRebuild code=%d, want 0", code)
	}
	seedPath, _ := seedArtifactPaths(true, agRepo)
	seedBytes, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	storeURL := seedStatusStore(t, seedBytes)

	writeRepoFile(t, filepath.Join(svcRepo, "docs", "awareness", "required_tests.yaml"), "required_tests:\n  - id: svc.test.one\n    title: Service test changed\n")
	commitAll(t, svcRepo, "drift services awareness")

	out := captureStdout(t, func() {
		if code := runSeedStatus([]string{
			"--json",
			"--require-current",
			"--seed", seedPath,
			"--ag-repo", agRepo,
			"--services-repo", svcRepo,
			"--oxigraph-url", storeURL,
		}); code != 1 {
			t.Fatalf("runSeedStatus code=%d, want 1", code)
		}
	})
	var got seedStatusResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal json: %v\n%s", err, out)
	}
	if got.OverallState != "split" {
		t.Fatalf("overall_state=%q, want split", got.OverallState)
	}
	if got.GeneratedVsCommitted.State != "current" {
		t.Fatalf("generated_vs_committed=%q, want current for external-only drift", got.GeneratedVsCommitted.State)
	}
	if got.TransactionStamp.State != "stale" {
		t.Fatalf("transaction_stamp=%q, want stale", got.TransactionStamp.State)
	}
	if got.LiveStore.State != "current" {
		t.Fatalf("live_store=%q, want current", got.LiveStore.State)
	}
}

func setupSeedStatusRepos(t *testing.T) (string, string) {
	t.Helper()
	agRepo := t.TempDir()
	svcRepo := t.TempDir()

	if err := os.MkdirAll(filepath.Join(agRepo, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(agRepo, "golang", "server", "embeddata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFixtureYAMLs(filepath.Join("..", "..", "golang", "extractor", "testdata"), filepath.Join(agRepo, "docs", "awareness")); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(svcRepo, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(svcRepo, "docs", "intent"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, filepath.Join(svcRepo, "docs", "awareness", "namespaces.yaml"), "namespaces:\n  - id: globular.services\n    path: docs/awareness\n")
	writeRepoFile(t, filepath.Join(svcRepo, "docs", "awareness", "required_tests.yaml"), "required_tests:\n  - id: svc.test.one\n    title: Service test\n")

	initGitRepo(t, agRepo)
	initGitRepo(t, svcRepo)
	return agRepo, svcRepo
}

func seedStatusStore(t *testing.T, loaded []byte) string {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/query":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read query body: %v", err)
			}
			writeVerificationQuery(t, w, loaded, string(body))
		case "/store":
			// The Graph Store Protocol dump the content check reads. A store
			// that cannot serialize itself cannot prove its integrity, so the
			// fixture serves the same bytes it answers queries from.
			w.Header().Set("Content-Type", "application/n-triples")
			_, _ = w.Write(loaded)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts.URL + "/store?default"
}

func TestSeedStatus_LiveStoreUnknownWhenQueryFails(t *testing.T) {
	agRepo, svcRepo := setupSeedStatusRepos(t)
	if code := runRebuild([]string{"--combined", "--ag-repo", agRepo, "--services-repo", svcRepo, "--no-runtime-reload"}); code != 0 {
		t.Fatalf("runRebuild code=%d, want 0", code)
	}
	seedPath, _ := seedArtifactPaths(true, agRepo)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "backend unavailable", http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	out := captureStdout(t, func() {
		if code := runSeedStatus([]string{
			"--json",
			"--seed", seedPath,
			"--ag-repo", agRepo,
			"--services-repo", svcRepo,
			"--oxigraph-url", ts.URL + "/store?default",
		}); code != 0 {
			t.Fatalf("runSeedStatus code=%d, want 0 without require-current", code)
		}
	})
	var got seedStatusResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal json: %v\n%s", err, out)
	}
	if got.LiveStore.State != "degraded" {
		t.Fatalf("live_store=%q, want degraded", got.LiveStore.State)
	}
	if got.OverallState != "degraded" && got.OverallState != "unknown" {
		t.Fatalf("overall_state=%q, want degraded/unknown", got.OverallState)
	}
}

func TestSeedStatus_LiveStoreDownWhenQueryEndpointIsUnreachable(t *testing.T) {
	agRepo, svcRepo := setupSeedStatusRepos(t)
	if code := runRebuild([]string{"--combined", "--ag-repo", agRepo, "--services-repo", svcRepo, "--no-runtime-reload"}); code != 0 {
		t.Fatalf("runRebuild code=%d, want 0", code)
	}
	seedPath, _ := seedArtifactPaths(true, agRepo)

	out := captureStdout(t, func() {
		if code := runSeedStatus([]string{
			"--json",
			"--seed", seedPath,
			"--ag-repo", agRepo,
			"--services-repo", svcRepo,
			"--oxigraph-url", "http://127.0.0.1:1/store?default",
		}); code != 0 {
			t.Fatalf("runSeedStatus code=%d, want 0 without require-current", code)
		}
	})
	var got seedStatusResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal json: %v\n%s", err, out)
	}
	if got.LiveStore.State != "down" {
		t.Fatalf("live_store=%q, want down", got.LiveStore.State)
	}
	if got.OverallState != "down" {
		t.Fatalf("overall_state=%q, want down", got.OverallState)
	}
}

func TestSeedStatus_BlockedWhenCombinedSeedLosesServicesRepo(t *testing.T) {
	agRepo, svcRepo := setupSeedStatusRepos(t)
	if code := runRebuild([]string{"--combined", "--ag-repo", agRepo, "--services-repo", svcRepo, "--no-runtime-reload"}); code != 0 {
		t.Fatalf("runRebuild code=%d, want 0", code)
	}
	seedPath, _ := seedArtifactPaths(true, agRepo)
	seedBytes, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	storeURL := seedStatusStore(t, seedBytes)

	out := captureStdout(t, func() {
		if code := runSeedStatus([]string{
			"--json",
			"--seed", seedPath,
			"--ag-repo", agRepo,
			"--services-repo", filepath.Join(agRepo, "not-services"),
			"--oxigraph-url", storeURL,
		}); code != 0 {
			t.Fatalf("runSeedStatus code=%d, want 0 without require-current", code)
		}
	})
	var got seedStatusResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal json: %v\n%s", err, out)
	}
	if got.GeneratedVsCommitted.State != "blocked" {
		t.Fatalf("generated_vs_committed=%q, want blocked", got.GeneratedVsCommitted.State)
	}
	if got.TransactionStamp.State != "blocked" {
		t.Fatalf("transaction_stamp=%q, want blocked", got.TransactionStamp.State)
	}
	if got.OverallState != "blocked" {
		t.Fatalf("overall_state=%q, want blocked", got.OverallState)
	}
	if !strings.Contains(got.OverallDetail, "paired services repo") {
		t.Fatalf("overall_detail=%q, want paired services repo explanation", got.OverallDetail)
	}
}

func TestNormalizeOxigraphQueryURL_UsesQueryEndpoint(t *testing.T) {
	got, err := normalizeOxigraphQueryURL("http://example.test:7878/store?default")
	if err != nil {
		t.Fatalf("normalizeOxigraphQueryURL: %v", err)
	}
	if !strings.HasSuffix(got, "/query") {
		t.Fatalf("query url=%q, want /query suffix", got)
	}
}

func TestSeedStatusStoreFixtureCarriesCurrentMarker(t *testing.T) {
	artifact, marker := seedmeta.AppendMarker([]byte("<https://example.test/s> <https://example.test/p> <https://example.test/x> .\n"))
	storeURL := seedStatusStore(t, artifact)
	queryURL, err := normalizeOxigraphQueryURL(storeURL)
	if err != nil {
		t.Fatalf("normalizeOxigraphQueryURL: %v", err)
	}
	client, err := oxigraph.New(queryURL)
	if err != nil {
		t.Fatalf("oxigraph.New: %v", err)
	}
	defer client.Close()
	verification := seedmeta.VerifyLiveStore(context.Background(), client, marker)
	if verification.State != seedmeta.FreshnessCurrent {
		t.Fatalf("verification state=%s, want CURRENT", verification.State)
	}
}

// TestSeedStatus_ContentDriftWithPreservedTripleCount is #282 end to end: a
// live store that lost one real triple and gained one unrelated triple keeps
// its marker and its count, so every identity lane still lines up. Only a
// recomputed content digest can see it.
func TestSeedStatus_ContentDriftWithPreservedTripleCount(t *testing.T) {
	agRepo, svcRepo := setupSeedStatusRepos(t)
	if code := runRebuild([]string{"--combined", "--ag-repo", agRepo, "--services-repo", svcRepo, "--no-runtime-reload"}); code != 0 {
		t.Fatalf("runRebuild code=%d, want 0", code)
	}
	seedPath, _ := seedArtifactPaths(true, agRepo)
	seedBytes, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	marker, ok := seedmeta.ParseMarker(seedBytes)
	if !ok {
		t.Fatal("seed carries no marker")
	}
	mutated := swapOneNonMarkerTriple(t, seedBytes, marker)
	storeURL := seedStatusStore(t, mutated)

	out := captureStdout(t, func() {
		if code := runSeedStatus([]string{
			"--json",
			"--require-current",
			"--seed", seedPath,
			"--ag-repo", agRepo,
			"--services-repo", svcRepo,
			"--oxigraph-url", storeURL,
		}); code != 1 {
			t.Fatalf("runSeedStatus code=%d, want 1 for a drifted store", code)
		}
	})
	var got seedStatusResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal json: %v\n%s", err, out)
	}
	if got.LiveTripleCount != marker.TripleCount {
		t.Fatalf("live_triple_count=%d, want %d — the drift must be count-preserving", got.LiveTripleCount, marker.TripleCount)
	}
	if got.LiveDigestSHA256 != marker.Digest {
		t.Fatalf("live marker digest=%q, want the expected marker %q to still be present", got.LiveDigestSHA256, marker.Digest)
	}
	if got.LiveContent.State != "drifted" {
		t.Fatalf("live_content=%q, want drifted", got.LiveContent.State)
	}
	if got.LiveStore.Current {
		t.Fatalf("live_store reported current for a drifted store: %+v", got.LiveStore)
	}
	if got.OverallState == "current" {
		t.Fatalf("overall_state=current for a drifted store: %+v", got)
	}
}

// swapOneNonMarkerTriple deletes one real triple and inserts one unrelated
// triple, leaving the marker triples and the total count untouched.
func swapOneNonMarkerTriple(t *testing.T, nt []byte, marker seedmeta.Marker) []byte {
	t.Helper()
	needle := "<" + marker.IRI + "> "
	lines := strings.Split(string(nt), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, needle) {
			continue
		}
		lines[i] = "<https://example.test/swapped> <https://example.test/p> <https://example.test/o> ."
		return []byte(strings.Join(lines, "\n"))
	}
	t.Fatal("no non-marker triple to swap")
	return nil
}
