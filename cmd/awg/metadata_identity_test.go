// SPDX-License-Identifier: AGPL-3.0-only

package main

// FINDING 2, raised by blind review of head 59372102 at cmd_gate.go:77.
//
// MetadataResponse is the ONLY response type in this proto that states the served generation
// twice: live_store_graph_digest_sha256 = 46 at the top level, and the same value at
// GraphAuthority field 10. Every other response carries it only inside GraphAuthority.
//
// The review's stated mechanism is wrong in two ways, measured and recorded so the correction
// is not lost: graphAuthorityFromSnapshotFor discards a *DomainPublication and not an error, and
// it has exactly one return, which is never nil -- so from THIS server the two representations
// are computed by the same call on the same snapshot and cannot disagree or be absent.
//
// The conclusion holds for a case the review did not name: an OLDER SERVER sends field 46 and no
// field 67, so the auxiliary structure is nil while the canonical identity is stated. Refusing
// that manufactures a refusal out of a missing structure while valid evidence sits beside it.
//
// The five cases below are the five the repair must distinguish, each driven independently.

import (
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func servedReader(t *testing.T) graphReader {
	t.Helper()
	servedWorld(t, declaredGen, "")
	r := productionReaderFor(emptyFlags(), servedWitnessDomain, "")
	if !r.declaresGeneration() {
		t.Fatal("the fixture reader declares no generation, so none of these cases can be decided")
	}
	return r
}

// F2-CASE-1. Canonical top-level digest correct, Authority absent. THE HOLDING CASE: an older
// server states which generation answered and carries no authority structure. It must verify.
func TestACorrectTopLevelDigestIsSufficientWithNoAuthorityStructure(t *testing.T) {
	r := servedReader(t)
	if err := r.verifyServedMetadata(&awarenesspb.MetadataResponse{
		LiveStoreGraphDigestSha256: declaredGen,
	}); err != nil {
		t.Fatalf("a response stating the declared ACTIVE generation in the canonical field was "+
			"refused because the auxiliary authority structure was nil: %v", err)
	}
}

// F2-CASE-2. Canonical top-level digest present and WRONG, Authority absent. Must refuse -- the
// nil authority must not become a reason to skip the comparison either.
func TestAWrongTopLevelDigestRefusesEvenWithNoAuthorityStructure(t *testing.T) {
	r := servedReader(t)
	err := r.verifyServedMetadata(&awarenesspb.MetadataResponse{
		LiveStoreGraphDigestSha256: foreignGen,
	})
	if err == nil {
		t.Fatalf("a response served by generation %s was accepted while %s declares %s ACTIVE",
			foreignGen, servedWitnessDomain, declaredGen)
	}
	if !strings.Contains(err.Error(), foreignGen) || !strings.Contains(err.Error(), declaredGen) {
		t.Errorf("the refusal does not name both generations: %v", err)
	}
}

// F2-CASE-3. Authority present and correct, top level empty. The server proved identity through
// the composed verdict and not through the marker field; still sufficient.
func TestACorrectAuthorityDigestIsSufficientWithAnEmptyTopLevelField(t *testing.T) {
	r := servedReader(t)
	if err := r.verifyServedMetadata(&awarenesspb.MetadataResponse{
		Authority: &awarenesspb.GraphAuthority{LiveStoreGraphDigestSha256: declaredGen},
	}); err != nil {
		t.Fatalf("a correct authority-borne served generation was refused: %v", err)
	}
}

// F2-CASE-4. BOTH present and INCONSISTENT. Neither is preferred: a response describing two
// generations attests to neither, so it is refused in both orderings -- a preference rule would
// pass one of them and the choice of which would be arbitrary.
func TestTwoDisagreeingIdentityRepresentationsAreRefusedInBothOrderings(t *testing.T) {
	r := servedReader(t)
	for _, c := range []struct {
		name       string
		top, inner string
	}{
		{"top matches the declaration, authority does not", declaredGen, foreignGen},
		{"authority matches the declaration, top does not", foreignGen, declaredGen},
	} {
		err := r.verifyServedMetadata(&awarenesspb.MetadataResponse{
			LiveStoreGraphDigestSha256: c.top,
			Authority:                  &awarenesspb.GraphAuthority{LiveStoreGraphDigestSha256: c.inner},
		})
		if err == nil {
			t.Errorf("%s: a response stating two different served generations was accepted", c.name)
			continue
		}
		if !strings.Contains(err.Error(), "two served generations") {
			t.Errorf("%s: the refusal does not name the disagreement, so it may have been refused "+
				"for one representation being wrong rather than for the two disagreeing: %v", c.name, err)
		}
	}
	// And agreement is still accepted, or the disagreement rule would be a blanket refusal.
	if err := r.verifyServedMetadata(&awarenesspb.MetadataResponse{
		LiveStoreGraphDigestSha256: declaredGen,
		Authority:                  &awarenesspb.GraphAuthority{LiveStoreGraphDigestSha256: declaredGen},
	}); err != nil {
		t.Errorf("two agreeing representations of the declared generation were refused: %v", err)
	}
}

// F2-CASE-5. NEITHER representation establishes identity. Refuse: unestablished must never read
// as established, which is the family's own absence rule.
func TestAResponseEstablishingNoServedIdentityIsRefused(t *testing.T) {
	r := servedReader(t)
	for name, resp := range map[string]*awarenesspb.MetadataResponse{
		"no fields at all": {},
		"an authority stating no generation": {Authority: &awarenesspb.GraphAuthority{
			Authoritative:       true,
			GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
		}},
	} {
		if err := r.verifyServedMetadata(resp); err == nil {
			t.Errorf("%s: a response that established no served generation was accepted", name)
		}
	}
}

// F2-INERT. The inert direction still holds for this seam too: with nothing declared ACTIVE,
// every shape above proceeds. Without this, the repair could be a blanket refusal and cases 2, 4
// and 5 would all still pass.
func TestTheMetadataSeamIsInertWhenNothingIsDeclaredActive(t *testing.T) {
	servedWorld(t, "", "")
	r := productionReaderFor(emptyFlags(), servedWitnessDomain, "")
	if r.declaresGeneration() {
		t.Fatal("the fixture declares a generation; it cannot witness the inert case")
	}
	for name, resp := range map[string]*awarenesspb.MetadataResponse{
		"nothing stated":      {},
		"a foreign top level": {LiveStoreGraphDigestSha256: foreignGen},
		"disagreeing halves": {LiveStoreGraphDigestSha256: foreignGen,
			Authority: &awarenesspb.GraphAuthority{LiveStoreGraphDigestSha256: declaredGen}},
	} {
		if err := r.verifyServedMetadata(resp); err != nil {
			t.Errorf("%s: refused while nothing is declared ACTIVE: %v", name, err)
		}
	}
}

// F2-DRIVEN. The older-server case at a real command, because the repair must be visible where
// the refusal was: `domains` consumes a MetadataResponse and nothing else, so a server stating
// only the canonical field is exactly the input that head 59372102 refused.
func TestDomainsAcceptsAnOlderServerThatStatesOnlyTheCanonicalDigest(t *testing.T) {
	addr := startServedAdversary(t, &servedAdversary{
		servedGeneration: declaredGen,
		// No authority structure at all, as a server built before field 67 existed.
		omitAuthority: true,
		// ... but the canonical top-level served identity IS stated.
		statesTopLevelDigest: true,
	})
	servedWorld(t, declaredGen, addr)

	var code int
	out := captureStdout(t, func() { code = runDomains(nil) })
	if code != 0 {
		t.Fatalf("domains refused a server that stated the declared ACTIVE generation in the "+
			"canonical field, because the auxiliary authority structure was absent (exit=%d):\n%s",
			code, out)
	}
	if !strings.Contains(out, servedWitnessDomain) {
		t.Errorf("no domain list was produced:\n%s", out)
	}
}

// F2-DRIVEN-opposite. The same older server serving the WRONG generation must still be refused:
// accepting the canonical field must not become accepting whatever it says.
func TestDomainsStillRefusesAnOlderServerServingAnUndeclaredGeneration(t *testing.T) {
	addr := startServedAdversary(t, &servedAdversary{
		servedGeneration:     foreignGen,
		omitAuthority:        true,
		statesTopLevelDigest: true,
	})
	servedWorld(t, declaredGen, addr)

	var code int
	out := captureStdout(t, func() { code = runDomains(nil) })
	if code == 0 {
		t.Fatalf("domains accepted generation %s from a server with no authority structure while "+
			"%s declares %s ACTIVE:\n%s", foreignGen, servedWitnessDomain, declaredGen, out)
	}
	if strings.Contains(out, servedWitnessDomain) {
		t.Errorf("the domain list still reached stdout:\n%s", out)
	}
}
