// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
)

func TestRequireAtomicCrossRepoGraphState_Current(t *testing.T) {
	agRepo, svcRepo := setupSeedStatusRepos(t)
	if code := runRebuild([]string{"--ag-repo", agRepo, "--services-repo", svcRepo, "--no-runtime-reload"}); code != 0 {
		t.Fatalf("runRebuild code=%d, want 0", code)
	}
	if err := requireAtomicCrossRepoGraphState(agRepo, svcRepo); err != nil {
		t.Fatalf("requireAtomicCrossRepoGraphState: %v", err)
	}
}

func TestRequireAtomicCrossRepoGraphState_FailsWhenTransactionDrifts(t *testing.T) {
	agRepo, svcRepo := setupSeedStatusRepos(t)
	if code := runRebuild([]string{"--ag-repo", agRepo, "--services-repo", svcRepo, "--no-runtime-reload"}); code != 0 {
		t.Fatalf("runRebuild code=%d, want 0", code)
	}
	writeRepoFile(t, filepath.Join(svcRepo, "docs", "awareness", "required_tests.yaml"), "required_tests:\n  - id: svc.test.one\n    title: Service test changed\n")
	commitAll(t, svcRepo, "drift services awareness")

	err := requireAtomicCrossRepoGraphState(agRepo, svcRepo)
	if err == nil {
		t.Fatal("expected atomicity check to fail on transaction drift")
	}
	if !strings.Contains(err.Error(), "transaction stamp is stale") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRequireBenchmarkAuthority_UsesLiveCurrentCertifiedServer(t *testing.T) {
	prevMeta := metadataRPC
	prevAtomic := benchmarkAtomicGuard
	metadataRPC = func(context.Context, string) (*awarenesspb.MetadataResponse, error) {
		return &awarenesspb.MetadataResponse{
			GraphFreshnessState:            awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
			GraphFreshnessDetail:           "live store matches expected validated graph artifact",
			EmbeddedTransactionMatchesSeed: true,
			EmbeddedTransactionDetail:      "embedded transaction certifies embedded seed",
			CertifiedAwarenessGraphCommit:  "abc123",
			CertifiedServicesRepoCommit:    "def456",
		}, nil
	}
	benchmarkAtomicGuard = func(string, string) error {
		t.Fatal("benchmarkAtomicGuard should not run when live server is current and certified")
		return nil
	}
	defer func() {
		metadataRPC = prevMeta
		benchmarkAtomicGuard = prevAtomic
	}()

	if err := requireBenchmarkAuthority(context.Background(), "localhost:10120", "", ""); err != nil {
		t.Fatalf("requireBenchmarkAuthority: %v", err)
	}
}

func TestRequireBenchmarkAuthority_FailsClosedWhenLiveServerStale(t *testing.T) {
	prevMeta := metadataRPC
	prevAtomic := benchmarkAtomicGuard
	metadataRPC = func(context.Context, string) (*awarenesspb.MetadataResponse, error) {
		return &awarenesspb.MetadataResponse{
			GraphFreshnessState:  awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_STALE,
			GraphFreshnessDetail: "live store missing expected graph marker demo",
		}, nil
	}
	benchmarkAtomicGuard = func(string, string) error {
		t.Fatal("benchmarkAtomicGuard should not override a reachable stale live server")
		return nil
	}
	defer func() {
		metadataRPC = prevMeta
		benchmarkAtomicGuard = prevAtomic
	}()

	err := requireBenchmarkAuthority(context.Background(), "localhost:10120", "", "")
	if err == nil {
		t.Fatal("expected stale live server to fail closed")
	}
	if !strings.Contains(err.Error(), "live AWG server is not authoritative") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRequireBenchmarkAuthority_FallsBackToLocalAtomicityWhenMetadataUnavailable(t *testing.T) {
	prevMeta := metadataRPC
	prevAtomic := benchmarkAtomicGuard
	metadataRPC = func(context.Context, string) (*awarenesspb.MetadataResponse, error) {
		return nil, context.DeadlineExceeded
	}
	called := false
	benchmarkAtomicGuard = func(string, string) error {
		called = true
		return nil
	}
	defer func() {
		metadataRPC = prevMeta
		benchmarkAtomicGuard = prevAtomic
	}()

	if err := requireBenchmarkAuthority(context.Background(), "localhost:10120", "/ag", "/svc"); err != nil {
		t.Fatalf("requireBenchmarkAuthority: %v", err)
	}
	if !called {
		t.Fatal("expected local atomicity fallback when metadata is unavailable")
	}
}
