// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"

	"fmt"
	"github.com/globulario/sensei/golang/reachability"
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
	// REACHABILITY: is the knowledge this graph serves the knowledge that has
	// been admitted?
	//
	// Everything above describes the artifact's agreement WITH ITSELF -- the
	// live store matches its own expected marker, so the block said
	// "authoritative (current)" while the serving graph was eleven days and 159
	// corpus changes behind. An artifact always matches itself. This line is
	// the only one that compares it to the corpus.
	//
	// It is a REPORT and never a rebuild, and a stale or unknown result is
	// reported as unpublished or unestablished knowledge -- never as absence of
	// law.
	if commit := authority.GetGraphBuildCommit(); commit != "" {
		root := reachabilityRepoRoot()
		a := reachability.ResolveFromGit(context.Background(), root, commit)
		fmt.Printf("  %s\n", a.Line())
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

// reachabilityRepoRoot resolves the checkout whose corpus is compared against
// the serving graph. AWG_REPO_ROOT wins when set, because a caller may run the
// CLI from anywhere; otherwise the working directory is used and git decides
// whether it is a repository at all. A wrong or absent root yields UNKNOWN,
// which is a member of the state set rather than a silent pass.
func reachabilityRepoRoot() string {
	if r := strings.TrimSpace(os.Getenv("AWG_REPO_ROOT")); r != "" {
		return r
	}
	return "."
}
