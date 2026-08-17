// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/closure"
	"github.com/globulario/sensei/golang/graphgeneration"
	"github.com/globulario/sensei/golang/seedmeta"
)

const (
	proofSetStoreURL = "http://127.0.0.1:7878/query"
	liveDigest       = "1111111111111111111111111111111111111111111111111111111111111111"
	otherDigest      = "2222222222222222222222222222222222222222222222222222222222222222"
)

func snapshotAt(digest string) graphFreshnessSnapshot {
	return graphFreshnessSnapshot{verification: seedmeta.Verification{
		State: seedmeta.FreshnessCurrent,
		Live:  seedmeta.Marker{Digest: digest, IRI: "urn:sensei:seed:" + digest, TripleCount: 4},
	}}
}

func writeProofSet(t *testing.T, digest string, domains map[string]graphgeneration.DomainProof) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir, err := graphgeneration.Dir(proofSetStoreURL)
	if err != nil {
		t.Fatalf("resolve proof set dir: %v", err)
	}
	set := &graphgeneration.Set{
		Generation: graphgeneration.Generation{MarkerDigest: digest, MarkerIRI: "urn:sensei:seed:" + digest, TripleCount: 4},
		Marker:     seedmeta.Marker{Digest: digest, IRI: "urn:sensei:seed:" + digest, TripleCount: 4},
		Domains:    domains,
	}
	if err := graphgeneration.Write(dir, proofSetStoreURL, set); err != nil {
		t.Fatalf("write proof set: %v", err)
	}
}

func proven(domain, digest string) graphgeneration.DomainProof {
	return graphgeneration.DomainProof{
		Report: &closure.Report{
			Domain: domain, MarkerDigest: digest,
			ExpectedToProject: 2, Projected: 2, ClosureProven: true,
		},
		SliceDigest: "slice-" + domain,
	}
}

// This is issue #176 at the surface that reports authority.
//
// The publication was produced by building services. sensei-code was not built,
// and under the old per-repository layout its own copy of the proof would now
// describe a publication that no longer exists. It must nonetheless be
// authoritative, because its slice did not change.
func TestDomainStaysAuthoritativeWhenAnotherDomainWasBuilt(t *testing.T) {
	writeProofSet(t, liveDigest, map[string]graphgeneration.DomainProof{
		"github.com/globulario/services":    proven("github.com/globulario/services", liveDigest),
		"github.com/globulario/sensei-code": proven("github.com/globulario/sensei-code", liveDigest),
	})

	for _, domain := range []string{"github.com/globulario/services", "github.com/globulario/sensei-code"} {
		s := newServer(nil)
		s.oxigraphQueryURL = proofSetStoreURL
		s.homeDomain = domain
		// Deliberately absent: this server has no local marker file. Authority
		// must come from the store's proof set, not from a per-repository copy.
		s.graphMarkerFile = ""

		state, detail := graphClosureState(s, snapshotAt(liveDigest))
		if state != closure.SemanticClosureProven {
			t.Fatalf("%s is not authoritative after another domain was published: %s — %s", domain, state, detail)
		}
	}
}

// A proof set that does not describe the live publication must not be used.
// The server falls back to the legacy path, which is fail-closed when there is
// no marker file either.
func TestStaleProofSetIsNotUsed(t *testing.T) {
	writeProofSet(t, otherDigest, map[string]graphgeneration.DomainProof{
		"github.com/globulario/services": proven("github.com/globulario/services", otherDigest),
	})

	s := newServer(nil)
	s.oxigraphQueryURL = proofSetStoreURL
	s.homeDomain = "github.com/globulario/services"
	s.graphMarkerFile = ""

	state, _ := graphClosureState(s, snapshotAt(liveDigest))
	if state == closure.SemanticClosureProven {
		t.Fatal("a proof set describing a different publication was accepted for this one")
	}
}

// A set that covers the live publication but carries no proof for this domain
// is a real negative. It must say which domain and why, not fall through to a
// more agreeable file elsewhere.
func TestDomainMissingFromLiveProofSetIsUnproven(t *testing.T) {
	writeProofSet(t, liveDigest, map[string]graphgeneration.DomainProof{
		"github.com/globulario/services": proven("github.com/globulario/services", liveDigest),
	})

	s := newServer(nil)
	s.oxigraphQueryURL = proofSetStoreURL
	s.homeDomain = "github.com/globulario/sensei-code"
	s.graphMarkerFile = ""

	state, detail := graphClosureState(s, snapshotAt(liveDigest))
	if state != closure.SemanticClosureUnproven {
		t.Fatalf("an uncovered domain was not reported as unproven: %s", state)
	}
	if !strings.Contains(detail, "sensei-code") {
		t.Fatalf("the detail does not name the domain that lacks a proof: %q", detail)
	}
}

// A recorded carry-forward refusal must reach the operator verbatim. This is
// the difference between "we could not check" and silence.
func TestRecordedRefusalIsReportedVerbatim(t *testing.T) {
	writeProofSet(t, liveDigest, map[string]graphgeneration.DomainProof{
		"github.com/globulario/sensei-code": {
			SliceDigest:         "slice-changed",
			CarryForwardRefusal: "the slice for \"github.com/globulario/sensei-code\" changed during a publication of \"github.com/globulario/services\"",
		},
	})

	s := newServer(nil)
	s.oxigraphQueryURL = proofSetStoreURL
	s.homeDomain = "github.com/globulario/sensei-code"
	s.graphMarkerFile = ""

	state, detail := graphClosureState(s, snapshotAt(liveDigest))
	if state != closure.SemanticClosureUnproven {
		t.Fatalf("a refused carry-forward was not reported as unproven: %s", state)
	}
	if !strings.Contains(detail, "changed during a publication") {
		t.Fatalf("the recorded reason was lost: %q", detail)
	}
}
