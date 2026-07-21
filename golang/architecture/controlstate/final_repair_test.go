// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

// Central SourceStatus validation: an off-vocabulary impact/availability is rejected everywhere.
func TestFinal_CentralSourceValidation(t *testing.T) {
	if err := validateSourceStatus(SourceStatus{Owner: "o", Schema: "s", Availability: SourceAvailable, Impact: "bogus"}); err == nil {
		t.Fatal("off-vocabulary impact must be rejected")
	}
	if err := validateSourceStatus(SourceStatus{Owner: "o", Schema: "s", Availability: "bogus", Impact: ImpactPrimary}); err == nil {
		t.Fatal("off-vocabulary availability must be rejected")
	}
	if err := validateSourceStatus(SourceStatus{Owner: "", Schema: "s", Availability: SourceAvailable, Impact: ImpactPrimary}); err == nil {
		t.Fatal("missing owner must be rejected")
	}
	// A validated projection with two primaries is rejected (exactly one primary).
	m := ProjectionMeta{SchemaVersion: NavigationDescriptorSchema, ProducerName: ProducerName, ProducerVersion: ProducerVersion, Availability: AvailabilityAvailable, NonAuthoritativeProjection: true,
		Sources: []SourceStatus{{Owner: "a", Schema: "s", Availability: SourceAvailable, Impact: ImpactPrimary}, {Owner: "b", Schema: "s", Availability: SourceAvailable, Impact: ImpactPrimary}}}
	if validateMeta(m, NavigationDescriptorSchema) == nil {
		t.Fatal("two primary sources must be rejected")
	}
}

// Graph-authority identity coherence: the observation's authority identity must equal the
// artifact's graph-authority identity.
func TestFinal_GraphAuthorityIdentityCoherence(t *testing.T) {
	reg := DefaultRegistry()
	id, res := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	b := satisfiedBundle(reg, rdf.ClassContract)
	b.GraphAuthority.Identity = "different-authority"
	if _, err := BuildArtifactState(reg, id, res, b); err == nil {
		t.Fatal("graph-authority identity mismatch must be rejected")
	}
}

// Contradiction owner + availability matrix.
func TestFinal_ContradictionMatrix(t *testing.T) {
	reg := DefaultRegistry()
	id, res := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	cases := []struct {
		name string
		cs   ContradictionSource
		ok   bool
	}{
		{"available empty", ContradictionSource{Owner: "extractor.contradiction", Schema: "c", Identity: "s", Availability: SourceAvailable}, true},
		{"observed no owner", ContradictionSource{Schema: "c", Availability: SourceAvailable}, false},
		{"empty finding id", ContradictionSource{Owner: "o", Schema: "c", Identity: "s", Availability: SourceAvailable, Findings: []ContradictionObservation{{Identity: ""}}}, false},
		{"duplicate finding", ContradictionSource{Owner: "o", Schema: "c", Identity: "s", Availability: SourceAvailable, Findings: []ContradictionObservation{{Identity: "x", Relevant: true}, {Identity: "x"}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := satisfiedBundle(reg, rdf.ClassContract)
			b.Contradiction = tc.cs
			_, err := BuildArtifactState(reg, id, res, b)
			if (err == nil) != tc.ok {
				t.Fatalf("%s: err=%v want ok=%v", tc.name, err, tc.ok)
			}
		})
	}
}

// A degraded source carrying a definitive blocker stays OPEN (never downgraded to degraded).
func TestFinal_DegradedDefinitiveBlockerStaysOpen(t *testing.T) {
	reg := DefaultRegistry()
	b := satisfiedBundle(reg, rdf.ClassContract)
	b.Dimensions["enforcement"] = DimensionObservation{Dimension: "enforcement", SourceOwner: "closure.enforcement", SourceSchema: "d", SourceIdentity: "i", SourceAvailability: SourceDegraded, Outcome: OutcomeDefinitiveBlocker, BlockerIDs: []string{"b1"}}
	id, res := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	st, err := BuildArtifactState(reg, id, res, b)
	if err != nil {
		t.Fatal(err)
	}
	if st.Closure != ClosureOpen {
		t.Fatalf("degraded definitive blocker must remain open, got %q", st.Closure)
	}
}

// Assessment-policy existence + class binding is enforced by the registry validator.
func TestFinal_AssessmentPolicyBinding(t *testing.T) {
	reg := DefaultRegistry()
	for i := range reg.Classes {
		if reg.Classes[i].ClassIRI == rdf.ClassContract {
			reg.Classes[i].AssessmentPolicyID = "does_not_exist"
		}
	}
	if reg.Validate() == nil {
		t.Fatal("assessable class referencing a nonexistent policy must be rejected")
	}
	reg2 := DefaultRegistry()
	for i := range reg2.Classes {
		if reg2.Classes[i].ClassIRI == rdf.ClassContract {
			reg2.Classes[i].AssessmentPolicyID = "boundary.v1" // exists but bound to Boundary
		}
	}
	if reg2.Validate() == nil {
		t.Fatal("assessment policy bound to a different class must be rejected")
	}
}

// The catalog must carry a primary source and the exact registry digest.
func TestFinal_CatalogPrimaryAndDigest(t *testing.T) {
	reg := DefaultRegistry()
	noDigest := catalog(reg, 4)
	noDigest.RegistryDigest = ""
	if ValidateCatalogScope(reg, noDigest) == nil {
		t.Fatal("catalog without a registry digest must be rejected")
	}
	notPrimary := catalog(reg, 4)
	notPrimary.Source.Impact = ImpactRelevant
	if ValidateCatalogScope(reg, notPrimary) == nil {
		t.Fatal("catalog whose source is not primary must be rejected")
	}
}

// Snapshot envelope validation: an observed envelope missing owner/schema, or a negative
// available count, is rejected.
func TestFinal_SnapshotEnvelopeValidation(t *testing.T) {
	reg := DefaultRegistry()
	base := ControlSnapshotInput{RepositoryIdentity: tRepo, Catalog: catalog(reg, 4),
		Authority: GraphAuthoritySummary{Observed: true, Current: true, Integrity: true, Identity: tAuth}}
	// Observed attention envelope with no owner.
	bad := base
	bad.Attention = AttentionObservation{Availability: SourceAvailable}
	if _, err := BuildControlSnapshot(reg, bad); err == nil {
		t.Fatal("observed attention envelope missing owner/schema must be rejected")
	}
	// Negative available count.
	neg := base
	neg.Attention = AttentionObservation{Owner: "controlstate.attention", Schema: "attention", Identity: "attention.collection", Availability: SourceAvailable}
	neg.OpenQuestions = &CountObservation{Owner: "questiondisposition", Schema: "oq", Availability: SourceAvailable, Count: -1}
	if _, err := BuildControlSnapshot(reg, neg); err == nil {
		t.Fatal("negative available count must be rejected")
	}
}

// Attention construction fails closed on a missing field; the seed-verification family exists.
func TestFinal_FailClosedAttentionAndSeed(t *testing.T) {
	if _, err := newAttention("", "s", "id", "", "class", "r", SeverityWarning, "b", nil, false, nil, "a", false); err == nil {
		t.Fatal("attention construction with a missing owner must fail closed")
	}
	if _, err := newAttention("o", "s", "", "", "class", "r", SeverityWarning, "b", nil, false, nil, "a", false); err == nil {
		t.Fatal("attention construction with a missing identity must fail closed")
	}
	seed, err := AttentionForSeedVerification("seedmeta", "seed.marker", "dig", []string{"aw:x"})
	if err != nil || seed.Severity != SeverityCritical || seed.AttentionClass != AttnSeedVerification {
		t.Fatalf("seed verification adapter: sev=%q class=%q err=%v", seed.Severity, seed.AttentionClass, err)
	}
}

// Absolute feedback identities are refused across Windows, UNC, and Unix.
func TestFinal_AbsoluteFeedbackIdentityRefused(t *testing.T) {
	for _, id := range []string{"/etc/passwd", `\\host\share\x`, `C:\Users\x`, `C:/Users/x`, `\abs\path`} {
		if err := validateScopedFeedback(ScopedFeedbackRef{ScopeIdentity: "s", ProjectionDigest: "d", Availability: "feedback_available", VerifiedRecordIDs: []string{id}}); err == nil {
			t.Fatalf("absolute feedback identity %q must be refused", id)
		}
	}
	// A repository-relative identity is accepted.
	if err := validateScopedFeedback(ScopedFeedbackRef{ScopeIdentity: "s", ProjectionDigest: "d", Availability: "feedback_available", VerifiedRecordIDs: []string{"invariant:x"}}); err != nil {
		t.Fatalf("repository-relative identity must be accepted: %v", err)
	}
}
