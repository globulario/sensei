// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

// §12.1-.4 — the source-bound dimension contract rejects ownerless/mismatched/contradictory
// observations, and a degraded/unavailable source can never close.
func TestRepair_DimensionSourceContract(t *testing.T) {
	reg := DefaultRegistry()
	id, res := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	base := satisfiedBundle(reg, rdf.ClassContract)

	cases := []struct {
		name string
		obs  DimensionObservation
	}{
		{"ownerless", DimensionObservation{Dimension: "scope", SourceOwner: "", SourceAvailability: SourceAvailable, Outcome: OutcomeSatisfied}},
		{"owner mismatch", DimensionObservation{Dimension: "scope", SourceOwner: "wrong.owner", SourceSchema: "d", SourceIdentity: "i", SourceAvailability: SourceAvailable, Outcome: OutcomeSatisfied}},
		{"degraded satisfied", DimensionObservation{Dimension: "scope", SourceOwner: "closure.scope", SourceSchema: "d", SourceIdentity: "i", SourceAvailability: SourceDegraded, Outcome: OutcomeSatisfied}},
		{"unavailable open", DimensionObservation{Dimension: "scope", SourceOwner: "closure.scope", SourceAvailability: SourceUnavailable, Outcome: OutcomeDefinitiveBlocker, BlockerIDs: []string{"b"}}},
		{"off-vocab availability", DimensionObservation{Dimension: "scope", SourceOwner: "closure.scope", SourceSchema: "d", SourceIdentity: "i", SourceAvailability: "bogus", Outcome: OutcomeSatisfied}},
		{"blocker without id", DimensionObservation{Dimension: "scope", SourceOwner: "closure.scope", SourceSchema: "d", SourceIdentity: "i", SourceAvailability: SourceAvailable, Outcome: OutcomeDefinitiveBlocker}},
		{"satisfied with blocker", DimensionObservation{Dimension: "scope", SourceOwner: "closure.scope", SourceSchema: "d", SourceIdentity: "i", SourceAvailability: SourceAvailable, Outcome: OutcomeSatisfied, BlockerIDs: []string{"b"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := base
			b.Dimensions = map[string]DimensionObservation{}
			for k, v := range base.Dimensions {
				b.Dimensions[k] = v
			}
			b.Dimensions["scope"] = tc.obs
			if _, err := BuildArtifactState(reg, id, res, b); err == nil {
				t.Fatalf("%s must be rejected", tc.name)
			}
		})
	}
}

// §12.6-.8 — contradiction absence is honest.
func TestRepair_ContradictionSourceHonesty(t *testing.T) {
	reg := DefaultRegistry()
	id, res := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})

	// Available source with zero relevant findings → contradiction dimension satisfied → closes.
	ok := satisfiedBundle(reg, rdf.ClassContract)
	ok.Contradiction = ContradictionSource{Owner: "extractor.contradiction", Schema: "c", Identity: "src", Availability: SourceAvailable}
	st, _ := BuildArtifactState(reg, id, res, ok)
	if st.Closure != ClosureClosed {
		t.Fatalf("available contradiction source with no findings must allow close, got %q", st.Closure)
	}

	// Absent contradiction source (unavailable) → unknown + partial. Graph authority alone never
	// proves absence.
	absent := satisfiedBundle(reg, rdf.ClassContract)
	absent.Contradiction = ContradictionSource{Owner: "extractor.contradiction", Schema: "c", Availability: SourceUnavailable}
	st2, _ := BuildArtifactState(reg, id, res, absent)
	if st2.Closure == ClosureClosed {
		t.Fatal("absent contradiction source must not close (authority alone never proves absence)")
	}
	if st2.Availability != AvailabilityPartial {
		t.Fatalf("absent contradiction source → partial, got %q", st2.Availability)
	}
}

// §12.9-.10 — every dimension/lifecycle/feedback source appears in the ledger.
func TestRepair_SourceLedgerCompleteness(t *testing.T) {
	reg := DefaultRegistry()
	id, res := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	b := satisfiedBundle(reg, rdf.ClassContract)
	b.Feedback = &ScopedFeedbackRef{ScopeIdentity: "scope", ProjectionDigest: "dig", Availability: "feedback_available"}
	st, err := BuildArtifactState(reg, id, res, b)
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]bool{}
	for _, s := range st.Sources {
		owners[s.Owner] = true
	}
	for _, want := range []string{"graph_authority", "closure.scope", "closure.enforcement", "extractor.contradiction", "briefingfeedback"} {
		if !owners[want] {
			t.Errorf("source ledger missing %q", want)
		}
	}
	// Lifecycle source present for Contract (governed_status policy).
	if !owners["governed"] {
		t.Errorf("source ledger missing lifecycle owner")
	}
}

// §12.13 — an arbitrary secondary failure cannot manufacture unavailable (only a bad primary can).
func TestRepair_SecondaryFailureCannotManufactureUnavailable(t *testing.T) {
	reg := DefaultRegistry()
	id, res := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	b := satisfiedBundle(reg, rdf.ClassContract)
	b.Lifecycle = LifecycleSource{Observed: true, Availability: SourceUnavailable} // relevant source down
	st, _ := BuildArtifactState(reg, id, res, b)
	if st.Availability == AvailabilityUnavailable {
		t.Fatal("a secondary (relevant) failure must not manufacture unavailable")
	}
	if st.Availability != AvailabilityPartial {
		t.Fatalf("relevant failure → partial, got %q", st.Availability)
	}
	// Primary (graph authority) down → unavailable.
	b2 := satisfiedBundle(reg, rdf.ClassContract)
	b2.GraphAuthority = GraphAuthorityObservation{Observed: false, Identity: tAuth}
	st2, _ := BuildArtifactState(reg, id, res, b2)
	if st2.Availability != AvailabilityUnavailable {
		t.Fatalf("primary failure → unavailable, got %q", st2.Availability)
	}
}

// §12.14-.15 — identity/class-resolution mismatch + padded identity are rejected.
func TestRepair_IdentityCoherence(t *testing.T) {
	reg := DefaultRegistry()
	// A fabricated canonical class that disagrees with the observed classes.
	bad := ArtifactIdentity{NodeIRI: "aw:x", CanonicalClass: rdf.ClassContract, ObservedClasses: []string{rdf.ClassComponent}, RepositoryIdentity: tRepo, GraphAuthorityIdentity: tAuth}
	res := ClassResolution{CanonicalClass: rdf.ClassContract}
	if _, err := BuildArtifactState(reg, bad, res, satisfiedBundle(reg, rdf.ClassContract)); err == nil {
		t.Fatal("fabricated canonical class must be rejected")
	}
	// Padded identity.
	padded := ArtifactIdentity{NodeIRI: " aw:x", CanonicalClass: rdf.ClassContract, ObservedClasses: []string{rdf.ClassContract}, RepositoryIdentity: tRepo, GraphAuthorityIdentity: tAuth}
	if err := ValidateArtifactIdentity(reg, padded, reg.ResolveCanonicalClass([]string{rdf.ClassContract})); err == nil {
		t.Fatal("padded node IRI must be rejected")
	}
}

// §12.16-.19 — catalog scope isolation.
func TestRepair_CatalogScopeIsolation(t *testing.T) {
	reg := DefaultRegistry()
	base := catalog(reg, 4)
	mutate := []struct {
		name string
		fn   func(*CatalogSnapshot)
	}{
		{"foreign domain", func(c *CatalogSnapshot) { c.Artifacts[0].Identity.DomainIdentity = "other" }},
		{"authority mismatch", func(c *CatalogSnapshot) { c.Artifacts[0].Identity.GraphAuthorityIdentity = "other" }},
		{"class/family mismatch", func(c *CatalogSnapshot) { c.Artifacts[0].Family = "patterns" }},
		{"class!=canonical", func(c *CatalogSnapshot) { c.Artifacts[0].Class = rdf.ClassBoundary }},
	}
	for _, m := range mutate {
		t.Run(m.name, func(t *testing.T) {
			c := base
			c.Artifacts = append([]ArtifactSummary(nil), base.Artifacts...)
			m.fn(&c)
			if err := ValidateCatalogScope(reg, c); err == nil {
				t.Fatalf("%s must be rejected", m.name)
			}
		})
	}
}

// §12.31-.32 — no raw error or answer text, and no absolute repository root, is serialized.
func TestRepair_NoLeaks(t *testing.T) {
	reg := DefaultRegistry()
	id, res := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	b := satisfiedBundle(reg, rdf.ClassContract)
	b.Feedback = &ScopedFeedbackRef{ScopeIdentity: "scope", ProjectionDigest: "dig", Availability: "feedback_available", VerifiedRecordIDs: []string{"invariant:x"}}
	st, _ := BuildArtifactState(reg, id, res, b)
	blob, _ := json.Marshal(st)
	if strings.Contains(string(blob), "/tmp/") || strings.Contains(string(blob), "/home/") {
		t.Fatalf("absolute path leaked: %s", blob)
	}
	// An absolute-path feedback identity is rejected up front.
	if err := validateScopedFeedback(ScopedFeedbackRef{ScopeIdentity: "s", ProjectionDigest: "d", Availability: "feedback_available", VerifiedRecordIDs: []string{"/abs/path"}}); err == nil {
		t.Fatal("absolute-path feedback identity must be rejected")
	}
	// A non-Phase-9.6 availability is rejected (never reinterpreted).
	if err := validateScopedFeedback(ScopedFeedbackRef{ScopeIdentity: "s", ProjectionDigest: "d", Availability: "made_up"}); err == nil {
		t.Fatal("non-Phase-9.6 feedback availability must be rejected")
	}
}
