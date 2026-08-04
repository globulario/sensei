// SPDX-License-Identifier: AGPL-3.0-only

package workspacecontract

import "testing"

func baseComposableInputs() IdentityInputs {
	return IdentityInputs{
		RepositoryDomainSource: RepositoryDomainConfigured,
		RepositoryDomain:       "github.com/globulario/sensei",
		Revision:               "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RevisionStatus:         RevisionResolved,
		GraphDigestStatus:      GraphDigestNotRequested,
		GraphAuthority: &GraphAuthority{
			Authoritative:        true,
			GraphFreshnessState:  "current",
			SeedState:            "SEED_STATE_CURRENT",
			BuildProvenanceState: "BUILD_PROVENANCE_STATE_STAMPED",
		},
		CoverageState: coverageStateSufficient,
	}
}

// TestComposeIdentity_SufficientCoverageIsComplete is the positive control:
// every existing condition true, plus sufficient coverage, is still
// complete.
func TestComposeIdentity_SufficientCoverageIsComplete(t *testing.T) {
	id := ComposeIdentity(baseComposableInputs())
	if id.CompositionState != CompositionComplete {
		t.Fatalf("CompositionState = %q, want complete", id.CompositionState)
	}
}

// TestComposeIdentity_AuthoritativeButThinCoverageIsPartial is the real
// regression test for a live review finding: a graph can be authoritative
// (fresh, stamped, seed matches) while still being COVERAGE_STATE_THIN --
// composing on a technically-genuine but functionally-uninformed graph must
// not be reported as complete. No existing fixture exercised this
// combination: docs/fixtures/workspace/v1/identity/partial.json is already
// partial for an unrelated reason (authoritative=false), so it passed both
// before and after this fix without ever exercising the new condition.
func TestComposeIdentity_AuthoritativeButThinCoverageIsPartial(t *testing.T) {
	in := baseComposableInputs()
	in.CoverageState = "COVERAGE_STATE_THIN"
	id := ComposeIdentity(in)
	if id.CompositionState != CompositionPartial {
		t.Fatalf("CompositionState = %q, want partial (authoritative but thin coverage must never be complete)", id.CompositionState)
	}
}

// TestComposeIdentity_AuthoritativeButEmptyCoverageIsPartial covers the
// other non-sufficient CoverageState value below the sufficient threshold.
func TestComposeIdentity_AuthoritativeButEmptyCoverageIsPartial(t *testing.T) {
	in := baseComposableInputs()
	in.CoverageState = "COVERAGE_STATE_EMPTY"
	id := ComposeIdentity(in)
	if id.CompositionState != CompositionPartial {
		t.Fatalf("CompositionState = %q, want partial", id.CompositionState)
	}
}

// TestComposeIdentity_NonAuthoritativeIsStillPartialRegardlessOfCoverage
// guards that GraphAuthority.Authoritative's own meaning is untouched by
// this fix: a non-authoritative graph is partial even with sufficient
// coverage, exactly as before.
func TestComposeIdentity_NonAuthoritativeIsStillPartialRegardlessOfCoverage(t *testing.T) {
	in := baseComposableInputs()
	in.GraphAuthority.Authoritative = false
	id := ComposeIdentity(in)
	if id.CompositionState != CompositionPartial {
		t.Fatalf("CompositionState = %q, want partial", id.CompositionState)
	}
}
