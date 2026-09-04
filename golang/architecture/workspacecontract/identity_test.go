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

// TestComposeIdentity_PartialAlwaysNamesALimitation is the regression test
// for a live finding: sensei synthesis-run's workspace-identity error path
// prints identity.Limitations to explain a non-complete CompositionState,
// but a caller (composeSynthesisRunIdentity) only ever appends a Limitation
// for domain-unbound, revision-resolution, and RPC-connection failures --
// never for a reachable-but-not-authoritative graph or insufficient
// coverage, the other two conditions deriveCompositionState itself checks.
// A run that hit CompositionPartial for exactly one of those two reasons
// printed an empty limitations list: "workspace identity is partial, not
// complete:" with nothing after it. ComposeIdentity must always name the
// dimension(s) it found lacking, regardless of what the caller supplied.
func TestComposeIdentity_PartialAlwaysNamesALimitation(t *testing.T) {
	thinCoverage := baseComposableInputs()
	thinCoverage.CoverageState = "COVERAGE_STATE_THIN"
	id := ComposeIdentity(thinCoverage)
	if len(id.Limitations) == 0 {
		t.Fatal("thin-coverage partial identity carries zero limitations; the reason for partial is unexplained")
	}
	foundCoverage := false
	for _, l := range id.Limitations {
		if l.Scope == "coverage_state" {
			foundCoverage = true
		}
	}
	if !foundCoverage {
		t.Fatalf("expected a coverage_state limitation, got %+v", id.Limitations)
	}

	nonAuthoritative := baseComposableInputs()
	nonAuthoritative.GraphAuthority.Authoritative = false
	id = ComposeIdentity(nonAuthoritative)
	if len(id.Limitations) == 0 {
		t.Fatal("non-authoritative partial identity carries zero limitations; the reason for partial is unexplained")
	}
	// The scope now NAMES which authority proposition failed rather than
	// reporting the family. That is the point of the change this assertion was
	// migrated for: "freshness, seed, or build-provenance is not current" was
	// the same sentence for three conditions with three different repairs. The
	// property this test guards -- a partial identity always says why -- is
	// unchanged and is now more specific.
	foundAuthority := false
	for _, l := range id.Limitations {
		switch l.Scope {
		case "graph_authority", "graph_answer_authority", "workspace_provenance_readiness":
			foundAuthority = true
		}
	}
	if !foundAuthority {
		t.Fatalf("expected an authority limitation, got %+v", id.Limitations)
	}
}

// TestComposeIdentity_CompleteHasNoSyntheticLimitations guards against the
// fix over-firing: a genuinely complete identity must not gain any of the
// new partial-only limitations.
func TestComposeIdentity_CompleteHasNoSyntheticLimitations(t *testing.T) {
	id := ComposeIdentity(baseComposableInputs())
	if len(id.Limitations) != 0 {
		t.Fatalf("expected no limitations on a complete identity, got %+v", id.Limitations)
	}
}
