// SPDX-License-Identifier: AGPL-3.0-only

package graphgeneration

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/seedmeta"
)

// A WHOLE-STORE REPLACE MUST REPLACE THE IDENTITY THAT DESCRIBES THE STORE.
//
// `sensei build --all` rewrote every triple and left the store-scoped
// generation naming the PREVIOUS publication, because only --repo published a
// proof set. expectedGraphMarker prefers that record over the marker file, so
// every freshness-gated surface compared the live store against a generation it
// was not built to and refused:
//
//	graph freshness stale for briefing: live store missing expected graph
//	marker f87c6912...
//
// Measured 2026-09-02 on a brand-new store whose port had last been used three
// days earlier: it inherited that port's generation record, and a build that
// agreed with the store exactly could not serve a briefing.
func TestAColdStartGenerationReplacesThePreviousOne(t *testing.T) {
	prev := &Set{
		Generation: Generation{MarkerDigest: "old", TripleCount: 10},
		Marker:     seedmeta.Marker{Digest: "old", IRI: "iri/old", TripleCount: 10},
		Domains:    map[string]DomainProof{},
	}
	fresh := seedmeta.Marker{Digest: "new", IRI: "iri/new", TripleCount: 20}

	next := Compose(prev,
		Generation{MarkerDigest: fresh.Digest, MarkerIRI: fresh.IRI, TripleCount: fresh.TripleCount},
		fresh, nil,
		"", // a cold-start build names no domain
		nil, nil, nil)

	if next.Marker.Digest != "new" {
		t.Fatalf("the published generation still names %q: a store whose content was "+
			"replaced kept the identity of what it replaced", next.Marker.Digest)
	}
	if next.Generation.MarkerDigest != "new" {
		t.Fatalf("generation marker digest = %q, want the freshly built one", next.Generation.MarkerDigest)
	}
}

// AN UNNAMED DOMAIN IS NOT A DOMAIN.
//
// builtDomain was added to the wanted set unconditionally, so a cold-start
// build would create an entry keyed by the empty string with a slice digest
// computed over nothing -- a proof-shaped record about no subject.
func TestAnUnnamedBuiltDomainIsNotRecordedAsADomain(t *testing.T) {
	next := Compose(nil,
		Generation{MarkerDigest: "d"},
		seedmeta.Marker{Digest: "d", IRI: "iri/d", TripleCount: 1},
		nil, "   ", nil, nil, nil)

	for domain := range next.Domains {
		if strings.TrimSpace(domain) == "" {
			t.Fatalf("an entry was recorded for the empty domain: %+v", next.Domains[domain])
		}
	}
}

// A STORE MUST NOT KEEP CLAIMING A GENERATION IT NO LONGER HOLDS.
//
// When a cold-start build cannot publish a replacement set -- Write refuses one
// with no domain proofs, because publishing it would drop every proof while
// claiming to be a publication -- the previous pointer must still go. The
// content it described has already been replaced by the time that refusal is
// reached, and expectedGraphMarker prefers the store-scoped record over the
// marker file, so leaving it makes a fresh store inherit a stale identity.
func TestInvalidateDropsAPointerThatDescribesReplacedContent(t *testing.T) {
	dir := t.TempDir()
	prev := &Set{
		Generation: Generation{MarkerDigest: "old", TripleCount: 10},
		Marker:     seedmeta.Marker{Digest: "old", IRI: "iri/old", TripleCount: 10},
		Domains:    map[string]DomainProof{"d": {SliceDigest: "x", SliceTripleCount: 10}},
	}
	if err := Write(dir, "http://127.0.0.1:9999/store?default", prev); err != nil {
		t.Fatalf("seed the previous generation: %v", err)
	}
	if _, err := Load(dir); err != nil {
		t.Fatalf("the fixture never established a current pointer, so the case "+
			"under test cannot be reached: %v", err)
	}

	if err := Invalidate(dir); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("the store still names a generation after the content it described " +
			"was replaced: a fresh build inherits a stale identity")
	}
}

// Invalidating a store that never published is not an error: "there is nothing
// to drop" and "the drop failed" are different, and only one is a problem.
func TestInvalidateIsQuietWhenNothingWasPublished(t *testing.T) {
	if err := Invalidate(t.TempDir()); err != nil {
		t.Fatalf("Invalidate on a store with no pointer: %v", err)
	}
}
