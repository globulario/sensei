// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

var benchmarkAtomicGuard = requireAtomicCrossRepoGraphState
var benchmarkAuthorityGuard = requireBenchmarkAuthority

func requireBenchmarkAuthority(ctx context.Context, addr, agRepo, svcRepo string) error {
	addr = strings.TrimSpace(addr)
	if addr != "" {
		metaCtx := ctx
		if metaCtx == nil {
			var cancel context.CancelFunc
			metaCtx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
		}
		resp, err := metadataRPC(metaCtx, addr, "")
		if err == nil {
			return validateLiveBenchmarkAuthority(resp)
		}
	}
	return benchmarkAtomicGuard(agRepo, svcRepo)
}

func validateLiveBenchmarkAuthority(resp *awarenesspb.MetadataResponse) error {
	if resp == nil {
		return fmt.Errorf("live AWG metadata missing")
	}
	if resp.GetGraphFreshnessState() != awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT {
		return fmt.Errorf("live AWG server is not authoritative: freshness=%s (%s)",
			strings.ToLower(strings.TrimPrefix(resp.GetGraphFreshnessState().String(), "GRAPH_FRESHNESS_STATE_")),
			strings.TrimSpace(resp.GetGraphFreshnessDetail()))
	}
	if !resp.GetEmbeddedTransactionMatchesSeed() {
		return fmt.Errorf("live AWG server is not transaction-certified: %s", strings.TrimSpace(resp.GetEmbeddedTransactionDetail()))
	}
	if strings.TrimSpace(resp.GetCertifiedAwarenessGraphCommit()) == "" || strings.TrimSpace(resp.GetCertifiedServicesRepoCommit()) == "" {
		return fmt.Errorf("live AWG server is missing certified cross-repo commits")
	}
	return nil
}

func requireAtomicCrossRepoGraphState(agRepo, svcRepo string) error {
	if agRepo == "" {
		return fmt.Errorf("cannot prove cross-repo atomicity: awareness-graph repo not found")
	}
	if svcRepo == "" {
		return fmt.Errorf("cannot prove cross-repo atomicity: services repo not found")
	}
	inputDirs, intentDir, err := collectInputDirs(svcRepo, agRepo)
	if err != nil {
		return fmt.Errorf("cannot prove cross-repo atomicity: %w", err)
	}
	if len(inputDirs) == 0 {
		return fmt.Errorf("cannot prove cross-repo atomicity: no awareness input directories found")
	}
	seedPath := resolveAtomicSeedPath(agRepo)
	committedSeed, err := os.ReadFile(seedPath)
	if err != nil {
		return fmt.Errorf("cannot prove cross-repo atomicity: read committed seed: %w", err)
	}
	generated, _, _, err := generateNT(inputDirs, intentDir, svcRepo, agRepo, false)
	if err != nil {
		return fmt.Errorf("cannot prove cross-repo atomicity: generate awareness graph: %w", err)
	}
	agOnly := generateAgOnlyNT(agRepo)
	seedFreshness := evaluateSeedFreshness(committedSeed, generated, agOnly)
	if seedFreshness.level != auditPASS {
		return fmt.Errorf("cross-repo atomicity failed: committed seed is stale (%s)", seedFreshness.summary)
	}
	txPath := defaultTransactionPath(agRepo)
	committedTx, err := os.ReadFile(txPath)
	if err != nil {
		return fmt.Errorf("cannot prove cross-repo atomicity: read committed transaction stamp: %w", err)
	}
	currentTx, err := buildTransactionTSV(agRepo, svcRepo, generated)
	if err != nil {
		return fmt.Errorf("cannot prove cross-repo atomicity: build transaction stamp: %w", err)
	}
	txFreshness := evaluateBuildTransactionFreshness(committedTx, currentTx)
	if txFreshness.level != auditPASS {
		return fmt.Errorf("cross-repo atomicity failed: committed transaction stamp is stale (%s)", txFreshness.summary)
	}
	return nil
}

func resolveAtomicSeedPath(agRepo string) string {
	return filepath.Join(agRepo, "golang", "server", "embeddata", "awareness.nt")
}
