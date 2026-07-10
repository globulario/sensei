// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"strings"
	"time"

	awarenessclient "github.com/globulario/sensei/golang/client"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func printGraphAuthority(authority *awarenesspb.GraphAuthority) {
	if authority == nil {
		fmt.Println("Authority: unavailable")
		return
	}
	// Verdict + freshness come from the shared interpreter so the CLI agrees
	// with the MCP bridge and editor clients — "authoritative" here requires a
	// current graph, not just the Authoritative bit.
	verdict := awarenessclient.InterpretAuthority(authority)
	freshness := verdict.State
	provenance := strings.ToLower(strings.TrimPrefix(authority.GetBuildProvenanceState().String(), "BUILD_PROVENANCE_STATE_"))
	state := "non-authoritative"
	if verdict.Authoritative {
		state = "authoritative"
	}
	transaction := "uncertified"
	if authority.GetEmbeddedTransactionMatchesSeed() {
		transaction = "certified"
	} else if !authority.GetEmbeddedTransactionStampPresent() {
		transaction = "missing"
	}
	fmt.Printf("Authority: %s (%s, provenance=%s, transaction=%s)\n", state, freshness, provenance, transaction)
	if digest := authority.GetLiveStoreGraphDigestSha256(); digest != "" {
		fmt.Printf("  Live digest:  %s\n", digest)
	}
	if triples := authority.GetLiveStoreGraphTripleCount(); triples > 0 {
		fmt.Printf("  Live triples: %d\n", triples)
	}
	if digest := authority.GetEmbeddedSeedDigestSha256(); digest != "" {
		fmt.Printf("  Seed digest:  %s\n", digest)
	}
	if commit := authority.GetGraphBuildCommit(); commit != "" {
		fmt.Printf("  Build commit: %s\n", commit)
	}
	if ts := authority.GetGraphBuildTimeUnix(); ts != 0 {
		fmt.Printf("  Build time:   %s\n", time.Unix(ts, 0).UTC().Format(time.RFC3339))
	}
	if commit := authority.GetCertifiedAwarenessGraphCommit(); commit != "" {
		fmt.Printf("  Tx awg:       %s\n", commit)
	}
	if commit := authority.GetCertifiedServicesRepoCommit(); commit != "" {
		fmt.Printf("  Tx services:  %s\n", commit)
	}
	if detail := strings.TrimSpace(authority.GetEmbeddedTransactionDetail()); detail != "" {
		fmt.Printf("  Tx detail:    %s\n", detail)
	}
	if detail := strings.TrimSpace(authority.GetGraphFreshnessDetail()); detail != "" {
		fmt.Printf("  Detail:       %s\n", detail)
	}
}
