// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// METADATA SCOPES ITS COUNTS TO THE REQUESTED DOMAIN AND ITS AUTHORITY TO THE
// HOME DOMAIN.
//
// TestAllDomainScopedSurfacesProjectTheSameAuthority says in its own comment
// that query, briefing, impact and resolve stamped the home domain's authority
// onto a domain-scoped answer "while metadata read the requested domain
// correctly". Metadata is the surface that did NOT get fixed, and the comment
// records it as the one that was already right.
//
// It is not. Metadata builds its authority from graphAuthorityFromSnapshot,
// which is graphAuthorityFromSnapshotFor with closureDomain "" -- and it does so
// BEFORE it reads req.GetDomain() at all. So the closure proof behind the
// verdict is evaluated for the home domain, then attached to counts that were
// scoped to the caller's domain.
//
// The composite is the dangerous part, not either half: an agent asking
// awareness_metadata(domain=X) is handed X's node counts beside an
// AUTHORITATIVE verdict that is about a different repository's publication.
// This is the primary graph-health tool -- the one an agent reaches for first
// -- so the answer it gives is the one everything downstream trusts.
func TestMetadataAuthorityAnswersForTheDomainItWasAsked(t *testing.T) {
	s, asked := authorityReferentServer(t)
	ctx := context.Background()

	resp, err := s.Metadata(ctx, &awarenesspb.MetadataRequest{Domain: pubTestDomain})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}

	// 1. It must have ASKED about the domain it was given. Producing an
	//    agreeable verdict without evaluating the requested domain is the
	//    failure even when the verdict happens to be right.
	sawRequested := false
	for _, d := range *asked {
		if d == pubTestDomain {
			sawRequested = true
		}
	}
	if !sawRequested {
		t.Errorf("closure was evaluated for %v, never for the requested domain %q: "+
			"the verdict answers a question the caller did not ask", *asked, pubTestDomain)
	}

	// 2. And the verdict must reflect that domain. pubTestDomain is UNPROVEN in
	//    this fixture while the home domain is proven, so an AUTHORITATIVE
	//    answer here is the home domain's authority wearing the caller's scope.
	if resp.GetAuthority().GetVerdict() == awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE {
		t.Errorf("metadata reported AUTHORITATIVE for %q, which has no closure proof; "+
			"the home domain's authority was stamped onto a domain-scoped answer", pubTestDomain)
	}
	if resp.GetAuthority().GetAuthoritative() {
		t.Errorf("metadata reported authoritative=true for unproven domain %q", pubTestDomain)
	}
}

// POSITIVE CONTROL. The assertion above must be reachable: a proven domain has
// to be able to come back AUTHORITATIVE, or the negative case proves nothing
// and would keep passing if Metadata started refusing everything.
func TestMetadataStillReportsAuthoritativeForAProvenDomain(t *testing.T) {
	s, _ := authorityReferentServer(t)
	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{Domain: s.homeDomain})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if resp.GetAuthority().GetVerdict() != awarenesspb.AuthorityVerdict_AUTHORITY_VERDICT_AUTHORITATIVE {
		t.Fatalf("a proven, fresh domain was not reported AUTHORITATIVE (%v): "+
			"the negative case above cannot distinguish a scoped verdict from a blanket refusal",
			resp.GetAuthority().GetVerdict())
	}
}

// authorityCoveredFiles maps each file in this package that projects a
// GraphAuthority to the surface in TestAllDomainScopedSurfacesProjectTheSameAuthority
// that drives it. Adding a projection site without adding a surface case fails
// TestEveryAuthorityProjectingSurfaceIsCovered below.
var authorityCoveredFiles = map[string]string{
	"query.go":           "query",
	"resolve.go":         "resolve",
	"briefing.go":        "briefing",
	"impact.go":          "impact",
	"metadata.go":        "metadata",
	"graph_authority.go": "graphAuthorityFor",
	// preflight.go projects authority from PublicationDomain, a DIFFERENT
	// question with its own contract ("when empty NO publication is resolved"),
	// so it is deliberately not one of the domain-scoped surfaces. Named here
	// so the omission is a recorded decision rather than a gap.
	"preflight.go": "",
}

// THE DRIFT DETECTOR MUST KNOW WHAT IT IS SUPPOSED TO COVER.
//
// TestAllDomainScopedSurfacesProjectTheSameAuthority promises that "a surface
// added later that forgets to scope its attestation fails here without anyone
// remembering to write a new test for it". Its surface list was hardcoded, so
// that promise was false: three of the six projecting files were absent, and
// metadata -- absent, and assumed correct in the comment -- was the one still
// carrying the defect.
//
// A name is not a scope. This test is what makes the name true.
func TestEveryAuthorityProjectingSurfaceIsCovered(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	// Any call that builds a GraphAuthority for a caller-supplied scope.
	projects := regexp.MustCompile(`graphAuthorityFor\(|graphAuthorityFromSnapshotFor?\(`)

	found := map[string]bool{}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatalf("read %s: %v", n, err)
		}
		if projects.Match(b) {
			found[n] = true
		}
	}

	// REFUSE TO PASS VACUOUSLY. A scan that matched nothing -- a renamed helper,
	// a broken pattern -- would report success exactly like full coverage. That
	// is the non-execution false green this repository keeps recording, and it
	// is the specific way a coverage checker dies quietly.
	if len(found) == 0 {
		t.Fatal("no authority-projecting file found: the scan matched nothing, " +
			"which is a broken check and not a covered package")
	}

	for f := range found {
		if _, declared := authorityCoveredFiles[f]; !declared {
			t.Errorf("%s projects a GraphAuthority for a requested scope but is not "+
				"covered by TestAllDomainScopedSurfacesProjectTheSameAuthority. Add a "+
				"surface case there and an entry to authorityCoveredFiles, or state "+
				"why it is not domain-scoped.", f)
		}
	}
	for f := range authorityCoveredFiles {
		if !found[f] {
			t.Errorf("authorityCoveredFiles names %s, which no longer projects authority: "+
				"the covered set is describing a surface that is gone", f)
		}
	}
}
