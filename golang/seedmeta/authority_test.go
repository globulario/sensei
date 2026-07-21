// SPDX-License-Identifier: AGPL-3.0-only

package seedmeta

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/globulario/sensei/golang/store"
)

// discovererFake is a minimal real-verifier discoverer: it returns pre-built SeedBuild ClassFacts
// (independent, whole-store discovery — NOT a lookup of the expected IRI) plus a live triple
// count. AdmitLiveMarker runs for real over it.
type discovererFake struct {
	facts     []store.ImpactFact
	liveCount int64
	countErr  error
	factsErr  error
}

func (f discovererFake) CountTriples(_ context.Context) (int64, error) {
	if f.countErr != nil {
		return 0, f.countErr
	}
	return f.liveCount, nil
}

func (f discovererFake) ClassFacts(_ context.Context, _ string, _ int) ([]store.ImpactFact, error) {
	if f.factsErr != nil {
		return nil, f.factsErr
	}
	return f.facts, nil
}

func derivedIRI(digest string) string { return NamespaceIRI + "seedBuild/sha256-" + digest }

// markerFacts builds the ClassFacts rows a live SeedBuild marker at subject `iri` would yield: the
// rdf:type row, the digest literal (when non-empty), and the triple-count literal (when > 0). The
// subject IRI is the caller's choice so tests can build genuine (digest-derived) and adversarial
// (mismatched-subject) markers.
func markerFacts(iri, digest string, count int64) []store.ImpactFact {
	facts := []store.ImpactFact{
		{NodeIRI: iri, TypeIRI: markerClassIRI, Predicate: "http://www.w3.org/1999/02/22-rdf-syntax-ns#type", Object: markerClassIRI, ObjectIsIRI: true},
	}
	if digest != "" {
		facts = append(facts, store.ImpactFact{NodeIRI: iri, Predicate: markerDigestIRI, Object: digest})
	}
	if count > 0 {
		facts = append(facts, store.ImpactFact{NodeIRI: iri, Predicate: markerTripleCountIRI, Object: strconv.FormatInt(count, 10)})
	}
	return facts
}

// The required REAL-VERIFIER matrix: AdmitLiveMarker runs for real over an independent discovery.
// Core law: AuthorityStaleAdmissible / AuthorityCurrent come ONLY from a genuinely discovered,
// self-consistent marker at its own digest-derived IRI; a differing digest literal attached to a
// non-derived subject (e.g. the expected IRI) is a marker identity contradiction → integrity.
func TestAdmitLiveMarker_Matrix(t *testing.T) {
	expected := Marker{IRI: derivedIRI("expected-d"), Digest: "expected-d", TripleCount: 100}
	cases := []struct {
		name       string
		expected   Marker
		fake       discovererFake
		wantState  AuthorityObservationState
		wantReason string
		wantLiveID string
	}{
		{
			"current: independently discovered marker matches expected",
			expected,
			discovererFake{facts: markerFacts(derivedIRI("expected-d"), "expected-d", 100), liveCount: 100},
			AuthorityCurrent, "", "expected-d",
		},
		{
			"stale-admissible: genuine older marker at its OWN digest-derived IRI",
			expected,
			discovererFake{facts: markerFacts(derivedIRI("older-d"), "older-d", 90), liveCount: 90},
			AuthorityStaleAdmissible, AuthorityReasonLiveMarkerDigestMismatch, "older-d",
		},
		{
			"integrity: a DIFFERING digest literal under the expected IRI is a contradiction",
			expected,
			discovererFake{facts: markerFacts(expected.IRI, "different-d", 100), liveCount: 100},
			AuthorityIntegrityFailed, AuthorityReasonLiveMarkerIRIDigestMismatch, "different-d",
		},
		{
			"integrity: marker subject IRI disagrees with its digest",
			expected,
			discovererFake{facts: markerFacts("aw:seed/handcrafted", "older-d", 90), liveCount: 90},
			AuthorityIntegrityFailed, AuthorityReasonLiveMarkerIRIDigestMismatch, "older-d",
		},
		{
			"integrity: stale marker with a MISSING marker count",
			expected,
			discovererFake{facts: markerFacts(derivedIRI("older-d"), "older-d", 0), liveCount: 90},
			AuthorityIntegrityFailed, AuthorityReasonLiveMarkerCountMissing, "older-d",
		},
		{
			"integrity: marker count differs from the ACTUAL live graph count",
			expected,
			discovererFake{facts: markerFacts(derivedIRI("older-d"), "older-d", 90), liveCount: 42},
			AuthorityIntegrityFailed, AuthorityReasonLiveGraphCountMismatch, "older-d",
		},
		{
			"integrity: MULTIPLE SeedBuild markers → ambiguous authority",
			expected,
			discovererFake{
				facts:     append(markerFacts(derivedIRI("a-d"), "a-d", 100), markerFacts(derivedIRI("b-d"), "b-d", 100)...),
				liveCount: 100,
			},
			AuthorityIntegrityFailed, AuthorityReasonMultipleLiveMarkers, "",
		},
		{
			"unobserved: no live marker discovered",
			expected,
			discovererFake{facts: nil, liveCount: 100},
			AuthorityUnobserved, AuthorityReasonLiveMarkerAbsent, "",
		},
		{
			"unobserved: marker present but digest literal missing",
			expected,
			discovererFake{facts: markerFacts(derivedIRI("older-d"), "", 90), liveCount: 90},
			AuthorityUnobserved, AuthorityReasonLiveMarkerDigestMissing, "",
		},
		{
			"unobserved: empty live graph",
			expected,
			discovererFake{facts: nil, liveCount: 0},
			AuthorityUnobserved, AuthorityReasonLiveStoreEmpty, "",
		},
		{
			"unobserved: discovery query error",
			expected,
			discovererFake{countErr: fmt.Errorf("boom")},
			AuthorityUnobserved, AuthorityReasonVerificationQueryFailed, "",
		},
		{
			"unobserved: expected marker absent (comparison metadata missing)",
			Marker{},
			discovererFake{facts: markerFacts(derivedIRI("expected-d"), "expected-d", 100), liveCount: 100},
			AuthorityUnobserved, AuthorityReasonExpectedMarkerAbsent, "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obs := AdmitLiveMarker(context.Background(), tc.fake, tc.expected)
			if obs.State != tc.wantState {
				t.Fatalf("state = %v, want %v (obs: %+v)", obs.State, tc.wantState, obs)
			}
			if obs.Reason != tc.wantReason {
				t.Fatalf("reason = %q, want %q", obs.Reason, tc.wantReason)
			}
			if obs.LiveIdentity != tc.wantLiveID {
				t.Fatalf("live identity = %q, want %q", obs.LiveIdentity, tc.wantLiveID)
			}
			// Global admissibility laws.
			if (obs.State == AuthorityCurrent || obs.State == AuthorityStaleAdmissible) && obs.LiveIdentity == "" {
				t.Fatal("current/stale-admissible require an independently discovered live identity")
			}
			// The expected digest is NEVER a fallback live identity: any reported LiveIdentity
			// must be a digest genuinely present in the discovered facts.
			if obs.LiveIdentity != "" && !digestInFacts(tc.fake.facts, obs.LiveIdentity) {
				t.Fatalf("live identity %q was not independently discovered (expected digest must never be a fallback)", obs.LiveIdentity)
			}
		})
	}
}

func digestInFacts(facts []store.ImpactFact, digest string) bool {
	for _, f := range facts {
		if f.Predicate == markerDigestIRI && f.Object == digest {
			return true
		}
	}
	return false
}

// Legitimate equality: when the independent discovery proves the expected digest, the identity IS
// the (equal) live digest — that is discovery, not substitution.
func TestAdmitLiveMarker_LegitimateEquality(t *testing.T) {
	expected := Marker{IRI: derivedIRI("same-d"), Digest: "same-d", TripleCount: 10}
	obs := AdmitLiveMarker(context.Background(), discovererFake{facts: markerFacts(derivedIRI("same-d"), "same-d", 10), liveCount: 10}, expected)
	if obs.State != AuthorityCurrent || obs.LiveIdentity != "same-d" {
		t.Fatalf("independently proven equality must be current with the live identity: %+v", obs)
	}
}

// The expected digest is never substituted as an observed identity: with the SAME expected marker,
// an absent live marker is unobserved with an EMPTY identity — the expected digest does not leak in.
func TestAdmitLiveMarker_ExpectedNeverFallbackIdentity(t *testing.T) {
	expected := Marker{IRI: derivedIRI("expected-d"), Digest: "expected-d", TripleCount: 100}
	obs := AdmitLiveMarker(context.Background(), discovererFake{facts: nil, liveCount: 100}, expected)
	if obs.State != AuthorityUnobserved || obs.LiveIdentity != "" {
		t.Fatalf("absent live marker must be unobserved with no identity, got %+v", obs)
	}
	if obs.LiveIdentity == expected.Digest {
		t.Fatal("the expected digest must never surface as the observed live identity")
	}
}
